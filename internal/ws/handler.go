package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	fiberws "github.com/gofiber/contrib/websocket"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/antigravity"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/claude"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/codex"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/cursor"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/kiro"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/kiroacp"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/model"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/quota"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/shell"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/tg"
	"log/slog"
)

func clearPendingDenials(database *db.DB, sessionID string) {
	if err := database.UpdatePendingDenials(sessionID, ""); err != nil {
		slog.Info(fmt.Sprintf("[ws] clear pending_denials: %v", err))
	}
}

// clearStaleAwaitingConfirm：沒有任務在跑、也沒有 Claude 的 pending_denials，卻停在 awaiting_confirm
// （例如 kiroacp 等授權時 server 重啟）就改回 idle。回傳是否有改。
func clearStaleAwaitingConfirm(database *db.DB, sessionID string) bool {
	if taskIsActive(sessionID) {
		return false
	}
	s, err := database.GetSession(sessionID)
	if err != nil || s.Status != db.SessionStatusAwaitingConfirm || strings.TrimSpace(s.PendingDenials) != "" {
		return false
	}
	if err := database.UpdateSessionStatus(sessionID, db.SessionStatusIdle); err != nil {
		slog.Info(fmt.Sprintf("[ws] clear stale awaiting_confirm: %v", err))
		return false
	}
	return true
}

func clearShellPending(database *db.DB, sessionID string) {
	if err := database.UpdateShellPending(sessionID, ""); err != nil {
		slog.Info(fmt.Sprintf("[ws] 清除 shell_pending 失敗: %v", err))
	}
}

func resolveShellWorkDir(session *db.Session) (absDir string, err error) {
	wdir := strings.TrimSpace(session.WorkDir)
	if wdir == "" {
		return "", fmt.Errorf("工作目錄未設定")
	}
	absDir, err = filepath.Abs(filepath.Clean(wdir))
	if err != nil {
		return "", fmt.Errorf("工作目錄無效: %w", err)
	}
	st, err := os.Stat(absDir)
	if err != nil || !st.IsDir() {
		return "", fmt.Errorf("工作目錄不存在或不是資料夾")
	}
	return absDir, nil
}

const (
	StateIdle                 = "IDLE"
	StateThinking             = "THINKING"
	StateStreaming            = "STREAMING"
	StateAwaitingConfirm      = "AWAITING_CONFIRM"
	StateAwaitingShellConfirm = "AWAITING_SHELL_CONFIRM"
	StateShellExec            = "SHELL_EXEC"
)

type clientMsg struct {
	RequestID     string   `json:"request_id,omitempty"`
	AttachmentIDs []string `json:"attachment_ids,omitempty"`
	Type          string   `json:"type"`
	Data          string   `json:"data,omitempty"`
	Tools         []string `json:"tools,omitempty"`
	Mode          string   `json:"mode,omitempty"`
	Model         string   `json:"model,omitempty"`
	Effort        string   `json:"effort,omitempty"`
	ID            int64    `json:"id,omitempty"` // queue_remove 的目標
}

type serverMsg struct {
	RequestID    string             `json:"request_id,omitempty"`
	CreatedAt    string             `json:"created_at,omitempty"`
	Attachments  []db.Attachment    `json:"attachments,omitempty"`
	Type         string             `json:"type"`
	Value        string             `json:"value,omitempty"`
	Content      string             `json:"content,omitempty"`
	ID           int64              `json:"id,omitempty"`
	Tools        interface{}        `json:"tools,omitempty"`
	RetryTools   bool               `json:"retry_tools,omitempty"` // 舊 Claude 重跑：本輪放行整個工具，而非單次操作
	Messages     json.RawMessage    `json:"messages,omitempty"`
	InputMode    string             `json:"input_mode,omitempty"`
	ShellType    string             `json:"shell_type,omitempty"`
	WorkDir      string             `json:"work_dir,omitempty"`
	Command      string             `json:"command,omitempty"`
	Line         string             `json:"line,omitempty"`
	WorkDirKey   string             `json:"work_dir_key,omitempty"`
	Stream       string             `json:"stream,omitempty"`
	ExitCode     int                `json:"exit_code,omitempty"`
	ShellPending *shellPendingInfo  `json:"shell_pending,omitempty"`
	Quota        *quota.Payload     `json:"quota,omitempty"`
	Model        *model.Payload     `json:"model,omitempty"`
	Queue        []db.QueuedMessage `json:"queue,omitempty"`
	QueuePaused  bool               `json:"queue_paused,omitempty"`
	Author       string             `json:"author,omitempty"` // 說話者暱稱；空字串＝擁有者。presence 事件中為事件主角
	Online       []string           `json:"online,omitempty"` // presence：目前在線名單
}

type shellPendingPayload struct {
	Line        string `json:"line"`
	Cmd         string `json:"cmd"`
	RequestedAt string `json:"requested_at"`
}

// ShellOpts 直連 shell 設定（來自 config）。
type ShellOpts struct {
	Enabled         bool
	Timeout         string // 例如 "60s"
	MaxOutputBytes  int
	AllowedCommands []string // 非空時啟用指令白名單（見 internal/shell/allowlist.go）
}

func NewHandler(database *db.DB, botToken string, shellCfg ShellOpts, quotaSvc *quota.Service, notifyCfg tg.NotifyConfig) func(*fiberws.Conn) {
	notifyCfg = notifyConfigFrom(notifyCfg)
	return func(c *fiberws.Conn) {
		sessionID := c.Params("id")
		tgUserID, _ := c.Locals("tg_id").(int64)

		// 訪客身分由 authMiddleware 寫入 Locals；擁有者沒有這些值。訪客一律沒有 tg_id，不觸發 Telegram 通知。
		shareID, _ := c.Locals("share_id").(int64)
		isGuest := shareID != 0
		guestNick, _ := c.Locals("share_nickname").(string)
		guestRole, _ := c.Locals("share_role").(string)
		guestMode, _ := c.Locals("share_mode").(string)
		guestExpires, _ := c.Locals("share_expires_at").(time.Time)
		author := "" // 本連線送出的訊息署名；空字串＝擁有者
		// 角色在後端強制：訪客除 editor 外一律唯讀（未知角色也視為唯讀）。
		readOnly := isGuest && guestRole != db.ShareRoleEditor
		if isGuest {
			tgUserID = 0
			author = guestNick
		}

		sess, err := database.GetSession(sessionID)
		if err != nil {
			slog.Info(fmt.Sprintf("[ws] session %s missing: %v", sessionID, err))
			c.Close()
			return
		}
		if isGuest {
			// 縱深防禦：snapshot 不該走到這裡；連線瞬間分享可能剛被結束或到期，再確認一次。
			sh, err := database.GetShare(shareID)
			if guestMode == db.ShareModeSnapshot || err != nil || !sh.Active(time.Now()) || sh.SessionID != sessionID || guestNick == "" {
				slog.Info(fmt.Sprintf("[ws] 拒絕訪客連線 share=%d session=%s", shareID, sessionID))
				c.Close()
				return
			}
		}
		slog.Info(fmt.Sprintf("[ws] session %s connected (agent=%s agentSessionID=%q mode=%s)", sessionID, sess.AgentType, sess.AgentSessionID, sess.PermissionMode))
		defer slog.Info(fmt.Sprintf("[ws] session %s disconnected", sessionID))

		var mu sync.Mutex      // 保護 agentSessionID 等連線狀態
		var writeMu sync.Mutex // 序列化 WebSocket 寫入（goroutine 不可並發 WriteMessage）
		agentType := sess.AgentType
		if agentType == "" {
			agentType = agent.TypeClaude
		}
		agentSessionID := sess.AgentSessionID

		isClaude := agentType == agent.TypeClaude

		// connDone：handler 返回後 fiber 會回收 conn。hub.Broadcast 先複製訂閱者清單再呼叫 send，
		// 其他連線的廣播（例如 presence leave）可能在本連線結束後才送到，所以 send 必須在回收後變成 no-op。
		connDone := false
		send := func(msg serverMsg) bool {
			b, _ := json.Marshal(msg)
			writeMu.Lock()
			defer writeMu.Unlock()
			if connDone {
				return false
			}
			return c.WriteMessage(1, b) == nil
		}
		defer func() {
			writeMu.Lock()
			connDone = true
			writeMu.Unlock()
		}()

		unsub := hub.Subscribe(sessionID, send)
		defer unsub()

		// 在線名單登記；訪客另有 kick（結束分享）與到期 timer。
		entry := &connEntry{sessionID: sessionID, shareID: shareID, nickname: guestNick}
		// kick 可能由到期 timer 或 KickShare 從別的 goroutine 呼叫；handler 返回後 fiber 會回收 conn，
		// 所以以 kickMu + finished 保證 kick 不會在 handler 結束後碰到已回收的連線。
		var kickMu sync.Mutex
		finished := false
		entry.kick = func(reason string) {
			kickMu.Lock()
			defer kickMu.Unlock()
			if finished {
				return
			}
			send(serverMsg{Type: "share_ended", Content: reason})
			_ = c.WriteControl(fiberws.CloseMessage, fiberws.FormatCloseMessage(4001, "share ended"), time.Now().Add(time.Second))
			c.Close()
		}
		defer func() {
			kickMu.Lock()
			finished = true
			kickMu.Unlock()
		}()
		removePresence := presenceAdd(entry)
		if isGuest {
			slog.Info(fmt.Sprintf("[ws] 訪客 %q 連線 share=%d role=%s", guestNick, shareID, guestRole))
			expiry := time.AfterFunc(time.Until(guestExpires), func() { entry.kick(shareEndExpired) })
			defer expiry.Stop()
		}
		defer func() {
			removePresence()
			announcePresence(sessionID, "leave", entry.displayName(), isGuest)
		}()

		broadcast := func(msg serverMsg) {
			hub.Broadcast(sessionID, msg)
		}

		syncData, err := buildSyncPayload(database, sessionID)
		if err != nil {
			slog.Info(fmt.Sprintf("[ws] buildSyncPayload: %v", err))
			syncData = SyncPayload{UIState: StateIdle, InputMode: "agent", ShellType: shellTypeString()}
		}
		syncMsg := serverMsg{
			Type:         "sync",
			Value:        syncData.UIState,
			Messages:     syncData.Messages,
			InputMode:    syncData.InputMode,
			ShellType:    syncData.ShellType,
			ShellPending: syncData.ShellPendingCmd,
		}
		if quotaSvc != nil {
			q := quotaSvc.Get(agentType)
			p := q.ToPayload()
			syncMsg.Quota = &p
		}
		syncMsg.Model = sessionModelPayload(sess)
		syncMsg.Queue, syncMsg.QueuePaused = loadQueueState(database, sessionID)
		send(syncMsg)
		announcePresence(sessionID, "join", entry.displayName(), isGuest)

		// 進入會話時若 quota cache 已過期，背景補打一次並推播更新（cache 未過期則 RefreshAfterRun 內部直接跳過）。
		if quotaSvc != nil {
			go func(at string) {
				snap, err := quotaSvc.RefreshAfterRun(context.Background(), at)
				if err != nil {
					slog.Info(fmt.Sprintf("[quota] refresh on connect %s: %v", at, err))
				}
				p := snap.ToPayload()
				broadcast(serverMsg{Type: "quota_update", Quota: &p})
			}(agentType)
		}

		shellTimeoutSec := 60
		if ts := strings.TrimSpace(shellCfg.Timeout); ts != "" {
			if d, err := time.ParseDuration(ts); err == nil {
				if sec := int(d.Seconds()); sec > 0 {
					shellTimeoutSec = sec
				}
			}
		}

		if isClaude && sess.PendingDenials != "" {
			send(serverMsg{Type: "permission_request", Tools: json.RawMessage(sess.PendingDenials), RetryTools: true})
			slog.Info(fmt.Sprintf("[ws] restored pending_denials session=%s", sessionID))
		}
		if tools, ok := permPending(sessionID); ok {
			send(serverMsg{Type: "permission_request", Tools: tools})
		}

		if strings.TrimSpace(sess.ShellPending) != "" {
			var p shellPendingPayload
			if err := json.Unmarshal([]byte(sess.ShellPending), &p); err == nil {
				if absDir, err := resolveShellWorkDir(sess); err == nil {
					send(serverMsg{Type: "shell_command_request", Command: p.Cmd, Line: p.Line, WorkDirKey: absDir})
				}
			}
			slog.Info(fmt.Sprintf("[ws] 還原 shell_pending for session %s", sessionID))
		}

		beginShellRun := func(command string, workDir string) {
			if taskIsActive(sessionID) {
				broadcast(serverMsg{Type: "error", Content: "AI is running; cannot run shell"})
				return
			}
			if shellTaskActive(sessionID) {
				broadcast(serverMsg{Type: "error", Content: "shell already running"})
				return
			}
			clearInMemoryShellApproval(sessionID)

			if err := database.AddMessage(sessionID, "user", command, author); err != nil {
				slog.Info(fmt.Sprintf("[ws] shell AddMessage: %v", err))
			}
			broadcast(serverMsg{Type: "user_message", Content: command, Author: author})

			msgID, err := database.CreatePendingMessageWithRole(sessionID, db.RoleShell)
			if err != nil {
				broadcast(serverMsg{Type: "error", Content: err.Error()})
				return
			}

			ctx, cancel := context.WithCancel(context.Background())
			shellTaskStart(sessionID, cancel, msgID)

			if err := database.UpdateSessionStatus(sessionID, db.SessionStatusRunning); err != nil {
				slog.Info(fmt.Sprintf("[ws] shell running status: %v", err))
			}
			broadcast(serverMsg{Type: "status", Value: StateShellRunning})

			go func(command string, msgID int64, wdir string) {
				defer func() {
					if r := recover(); r != nil {
						slog.Info(fmt.Sprintf("[ws] shell goroutine panic: %v", r))
					}
					shellTaskEnd(sessionID, msgID)
				}()
				var cbMu sync.Mutex
				var done atomic.Bool
				var shellErrText string
				var exitCode int
				err := shell.Run(ctx, shell.RunOptions{Command: command, WorkDir: wdir, Timeout: shellTimeoutSec}, func(e shell.Event) {
					cbMu.Lock()
					defer cbMu.Unlock()
					if ctx.Err() != nil {
						return
					}
					switch e.Type {
					case shell.EventDeltaStdout:
						chunk := appendShellDBChunk("stdout", e.Text)
						if err := database.AppendMessageContent(msgID, chunk); err != nil {
							slog.Info(fmt.Sprintf("[ws] shell append: %v", err))
						}
						broadcast(serverMsg{Type: "shell_delta", Stream: "stdout", Content: e.Text})
					case shell.EventDeltaStderr:
						chunk := appendShellDBChunk("stderr", e.Text)
						if err := database.AppendMessageContent(msgID, chunk); err != nil {
							slog.Info(fmt.Sprintf("[ws] shell append: %v", err))
						}
						broadcast(serverMsg{Type: "shell_delta", Stream: "stderr", Content: e.Text})
					case shell.EventError:
						shellErrText = e.Text
						if e.Text != "" {
							_ = database.AppendMessageContent(msgID, "\n[error] "+e.Text+"\n")
						}
						broadcast(serverMsg{Type: "shell_error", Content: e.Text})
					case shell.EventDone:
						done.Store(true)
						exitCode = e.ExitCode
						finalizeShellMessage(database, msgID, e.ExitCode)
						broadcast(serverMsg{Type: "shell_done", ExitCode: e.ExitCode})
					}
				})
				interrupted := !done.Load()
				if interrupted {
					appendText := "\n[interrupted]\n"
					if err != nil {
						appendText = "\n[error] " + err.Error() + "\n"
					}
					_ = database.AppendMessageContent(msgID, appendText)
					finalizeShellMessage(database, msgID, -1)
					exitCode = -1
					broadcast(serverMsg{Type: "shell_done", ExitCode: -1})
				}
				if err := database.UpdateSessionStatus(sessionID, db.SessionStatusIdle); err != nil {
					slog.Info(fmt.Sprintf("[ws] shell idle status: %v", err))
				}
				broadcast(serverMsg{Type: "status", Value: idleUIStatus(database, sessionID)})
				finishShellNotify(botToken, tgUserID, notifyCfg, sess.Name, command, shellErrText, exitCode, interrupted && err == nil, err)
			}(command, msgID, workDir)
		}

		// drainIfIdle 於 runAgent 之後賦值；先宣告讓任務 goroutine 收尾時可呼叫。
		var drainIfIdle func()
		broadcastQueue := func() {
			q, paused := loadQueueState(database, sessionID)
			broadcast(serverMsg{Type: "queue_update", Queue: q, QueuePaused: paused})
		}
		pauseQueue := func() {
			if err := database.PauseQueueIfPending(sessionID); err != nil {
				slog.Info(fmt.Sprintf("[ws] PauseQueueIfPending: %v", err))
			}
			broadcastQueue()
		}

		// runAgent：與 WS 解耦，任務在背景執行。
		// allowedOnce：僅「允許此操作」該次 retry 帶入 --allowedTools，不可寫入 DB（否則變成永久 allowlist）。
		runAgent := func(prompt string, allowedOnce []string) {
			shellTaskCancel(sessionID)
			clearShellPending(database, sessionID)
			clearInMemoryShellApproval(sessionID)
			taskCancel(sessionID)
			if err := database.FinalizePendingMessagesForSession(sessionID); err != nil {
				slog.Info(fmt.Sprintf("[ws] FinalizePendingMessagesForSession: %v", err))
			}

			s, err := database.GetSession(sessionID)
			if err != nil {
				slog.Info(fmt.Sprintf("[ws] GetSession: %v", err))
				broadcast(serverMsg{Type: "error", Content: err.Error()})
				return
			}
			mu.Lock()
			pm := s.PermissionMode
			agSid := s.AgentSessionID
			wdir := s.WorkDir
			var cliExtra []string
			if len(s.CliExtraArgs) > 0 {
				cliExtra = append([]string(nil), s.CliExtraArgs...)
			}
			mu.Unlock()

			msgID, err := database.CreatePendingMessage(sessionID)
			if err != nil {
				slog.Info(fmt.Sprintf("[ws] CreatePendingMessage: %v", err))
				broadcast(serverMsg{Type: "error", Content: err.Error()})
				return
			}

			ctx, cancel := context.WithCancel(context.Background())
			taskStart(sessionID, cancel, msgID)

			if err := database.UpdateSessionStatus(sessionID, db.SessionStatusRunning); err != nil {
				slog.Info(fmt.Sprintf("[ws] UpdateSessionStatus running: %v", err))
			}
			broadcast(serverMsg{Type: "status", Value: StateThinking})

			extra := map[string]string{}
			if pm != "" {
				extra[agent.ArgPermissionMode] = pm
			}
			if isClaude && len(allowedOnce) > 0 {
				extra[agent.ArgAllowedTools] = strings.Join(allowedOnce, ",")
			}
			if agentType == agent.TypeCursor && pm == "bypassPermissions" {
				extra[agent.ArgForce] = "true"
			}
			if m := model.ParseModelFromCliArgs(cliExtra); m != "" {
				extra[agent.ArgModel] = m
			}
			if s.Model != "" {
				extra[agent.ArgModel] = s.Model
			}
			if s.Effort != "" && agentType != agent.TypeCursor {
				extra[agent.ArgEffort] = s.Effort
			}

			opts := agent.RunOptions{
				Prompt:       prompt,
				SessionID:    agSid,
				WorkDir:      wdir,
				ExtraArgs:    extra,
				CliExtraArgs: cliExtra,
				OnStart:      func(pid int) { taskSetPid(sessionID, msgID, pid) },
			}

			// 等授權一定要有人處理：由 MCP（mcp_token）發起的 run 沒有 tg_id，退回白名單唯一使用者；訪客不退回。
			confirmNotifyID := func() int64 {
				if tgUserID == 0 && !isGuest {
					id, _ := database.DefaultNotifyTgIDIfSingle()
					return id
				}
				return tgUserID
			}

			// askUser：中途授權共用流程（kiroacp 的 RequestPermission 與 Claude 的 --permission-prompt-tool 都走這裡）。
			// 廣播 permission_request 給前端並阻塞，直到使用者 allow_once/deny_once 或 rctx 取消。
			askUser := func(rctx context.Context, tools any) bool {
				pe := &pendingPermEntry{ch: make(chan bool, 1), tools: tools}
				permSet(sessionID, pe)

				_ = database.UpdateSessionStatus(sessionID, db.SessionStatusAwaitingConfirm)
				broadcast(serverMsg{Type: "status", Value: StateAwaitingConfirm})
				broadcast(serverMsg{Type: "permission_request", Tools: tools})
				notifyTaskAsync(botToken, confirmNotifyID(), notifyCfg, tg.TaskAlert{
					SessionName: sess.Name,
					Outcome:     tg.OutcomeConfirm,
				})

				var allow bool
				select {
				case allow = <-pe.ch:
				case <-rctx.Done():
					allow = false
				}

				permClear(sessionID, pe)
				if rctx.Err() == nil {
					_ = database.UpdateSessionStatus(sessionID, db.SessionStatusRunning)
					broadcast(serverMsg{Type: "status", Value: StateStreaming})
				}
				return allow
			}

			// kiroacp 互動式授權：runner 中途收到工具授權請求時同步呼叫。
			if agentType == agent.TypeKiroACP {
				opts.RequestPermission = func(rctx context.Context, req agent.PermissionRequest) string {
					title := strings.TrimSpace(req.Title)
					if title == "" {
						title = "工具授權請求"
					}
					allow := askUser(rctx, []map[string]string{{"tool_name": title}})

					// 依 kind 把粗粒度 allow/deny 對應成該請求的 optionId（robust，不寫死 id）。
					wantKinds := []string{"reject_once", "reject_always"}
					if allow {
						wantKinds = []string{"allow_once", "allow_always"}
					}
					for _, k := range wantKinds {
						for _, o := range req.Options {
							if o.Kind == k {
								return o.OptionID
							}
						}
					}
					if allow {
						for _, o := range req.Options {
							if !strings.HasPrefix(o.Kind, "reject") {
								return o.OptionID
							}
						}
					}
					return "" // deny / cancel
				}
			}

			runner, err := agent.NewRunner(agentType)
			if err != nil {
				slog.Info(fmt.Sprintf("[ws] NewRunner %s: %v", agentType, err))
				_ = database.FinalizeMessage(msgID)
				_ = database.UpdateSessionStatus(sessionID, db.SessionStatusIdle)
				taskEnd(sessionID, msgID)
				broadcast(serverMsg{Type: "error", Content: err.Error()})
				broadcast(serverMsg{Type: "status", Value: idleUIStatus(database, sessionID)})
				pauseQueue()
				notifyTaskAsync(botToken, tgUserID, notifyCfg, tg.TaskAlert{
					SessionName: sess.Name,
					AgentType:   agentType,
					Outcome:     tg.OutcomeError,
					Prompt:      prompt,
					Error:       err.Error(),
				})
				return
			}
			slog.Info(fmt.Sprintf("[ws] start %s.Run agentSessionID=%q mode=%s msgID=%d", runner.Name(), opts.SessionID, pm, msgID))

			// Claude + bypassPermissions：命中 settings 的 permissions.ask 時，claude 經 --permission-prompt-tool
			// 問 miniapp（/mcp/perm），由 askUser 走既有授權面板。mcp_token 未設定時不啟用，行為與過去相同。
			var permAsker *claudeAsker
			permCleanup := func() {}
			if isClaude && pm == "bypassPermissions" {
				if _, _, ok := claudePermMCP(); ok {
					if cfgPath, rmCfg, err := writePermMCPConfig(sessionID); err != nil {
						slog.Info(fmt.Sprintf("[ws] perm mcp 設定檔建立失敗，略過權限詢問: %v", err))
					} else {
						a, unreg := registerClaudeAsker(sessionID, ctx, askUser)
						permAsker = a
						extra[agent.ArgPermPromptConfig] = cfgPath
						permCleanup = func() { unreg(); rmCfg() }
					}
				}
			}

			go func(opts agent.RunOptions, msgID int64, userPrompt string) {
				defer permCleanup()
				defer taskEnd(sessionID, msgID)

				permDenied := false
				runFailed := false
				succeeded := false
				errText := ""
				var cancelled bool

				// appendNotice：把說明寫進本則回覆（落 DB＋即時廣播）。尚未開始串流時先補 streaming 狀態，
				// 前端的 delta 只會接在最後一則訊息上。
				streamed := false
				appendNotice := func(text string) {
					if text == "" {
						return
					}
					if !streamed {
						streamed = true
						broadcast(serverMsg{Type: "status", Value: StateStreaming})
					}
					if err := database.AppendMessageContent(msgID, text); err != nil {
						slog.Info(fmt.Sprintf("[ws] AppendMessageContent (notice): %v", err))
					}
					broadcast(serverMsg{Type: "delta", Content: text})
				}

				// 收尾決定佇列走向。等授權時不動佇列；成功才續跑；失敗／中斷暫停，避免錯誤連鎖。
				// 成功時先 taskEnd 解除登記，drainIfIdle 才會看到閒置（外層 defer taskEnd 之後自然 no-op）。
				defer func() {
					switch {
					case permDenied:
					case succeeded && !runFailed && !cancelled:
						taskEnd(sessionID, msgID)
						drainIfIdle()
					default:
						pauseQueue()
					}
				}()

				// Kiro / Codex stream 不含 model，run 前解析並推送。
				if agentType == agent.TypeKiro || agentType == agent.TypeKiroACP || agentType == agent.TypeCodex {
					info := resolveStreamless(s, agentType)
					if p := persistInfoUpdate(database, sessionID, info); p != nil {
						broadcast(serverMsg{Type: "model_update", Model: p})
					}
				}

				err := runner.Run(ctx, opts, func(e agent.Event) {
					// 中止後仍處理 delta/done/error，避免進程收尾時文字被丟棄成空白訊息。
					if ctx.Err() != nil {
						switch e.Type {
						case agent.EventDelta, agent.EventDone, agent.EventError, agent.EventPermDenied, agent.EventSessionInit:
						default:
							return
						}
					}
					switch e.Type {
					case agent.EventStreamStart:
						streamed = true
						broadcast(serverMsg{Type: "status", Value: StateStreaming})

					case agent.EventThinking:
						// 思考鏈：不寫 DB、不改狀態，直接廣播給前端覆寫顯示。
						if e.Text != "" {
							broadcast(serverMsg{Type: "thinking", Content: e.Text})
						}

					case agent.EventActivity:
						if e.Text != "" {
							broadcast(serverMsg{Type: "activity", Content: e.Text})
						}

					case agent.EventDelta:
						if e.Text != "" {
							if err := database.AppendMessageContent(msgID, e.Text); err != nil {
								slog.Info(fmt.Sprintf("[ws] AppendMessageContent: %v", err))
							}
							broadcast(serverMsg{Type: "delta", Content: e.Text})
						}

					case agent.EventSessionInit:
						if e.SessionID == "" && e.Model == nil {
							return
						}
						mu.Lock()
						if e.SessionID != "" && e.SessionID != agentSessionID {
							agentSessionID = e.SessionID
							if err := database.UpdateAgentSessionID(sessionID, agentSessionID); err != nil {
								slog.Info(fmt.Sprintf("[ws] UpdateAgentSessionID: %v", err))
							}
						}
						mu.Unlock()
						if e.Model != nil {
							p := persistModelUpdate(database, sessionID, e.Model)
							if p.DisplayText != "" && p.DisplayText != "—" {
								broadcast(serverMsg{Type: "model_update", Model: &p})
							}
						}

					case agent.EventPermDenied:
						if !isClaude {
							return
						}
						// 已由使用者在授權面板回答過的（prompt-tool 回 deny 後 result 仍會列出），不再重複詢問。
						denials := permAsker.dropAnswered(e.Denials)
						// 被 deny 規則擋下、或「允許」後重跑仍被拒的，按允許也不會過：改成說明原因，不進等授權。
						pending, blocked := splitDenials(denials)
						pending, still := splitStillDenied(allowedOnce, pending)
						appendNotice(blockedNotice(blocked))
						appendNotice(stillDeniedNotice(still))
						if len(pending) == 0 {
							return
						}
						permDenied = true
						if raw, err := json.Marshal(pending); err == nil {
							if err := database.UpdatePendingDenials(sessionID, string(raw)); err != nil {
								slog.Info(fmt.Sprintf("[ws] UpdatePendingDenials: %v", err))
							}
						}
						if err := database.UpdateSessionStatus(sessionID, db.SessionStatusAwaitingConfirm); err != nil {
							slog.Info(fmt.Sprintf("[ws] UpdateSessionStatus awaiting_confirm: %v", err))
						}
						broadcast(serverMsg{Type: "status", Value: StateAwaitingConfirm})
						broadcast(serverMsg{Type: "permission_request", Tools: pending, RetryTools: true})
						notifyTaskAsync(botToken, confirmNotifyID(), notifyCfg, tg.TaskAlert{
							SessionName: sess.Name,
							Outcome:     tg.OutcomeConfirm,
						})

					case agent.EventDone:
						mu.Lock()
						if e.SessionID != "" && e.SessionID != agentSessionID {
							agentSessionID = e.SessionID
							if err := database.UpdateAgentSessionID(sessionID, agentSessionID); err != nil {
								slog.Info(fmt.Sprintf("[ws] UpdateAgentSessionID: %v", err))
							}
						}
						mu.Unlock()

						rt := strings.TrimSpace(e.ResultText)
						if rt != "" {
							if err := database.UpdateMessageResultText(msgID, rt); err != nil {
								slog.Info(fmt.Sprintf("[ws] UpdateMessageResultText: %v", err))
							}
							if err := database.FillMessageContentIfEmpty(msgID, rt); err != nil {
								slog.Info(fmt.Sprintf("[ws] FillMessageContentIfEmpty: %v", err))
							}
							broadcast(serverMsg{Type: "message_result_text", ID: msgID, Content: rt})
						}

						// 中止中收到 result：只救內容，不當成功完成（避免提早「任務完成」）。
						if ctx.Err() != nil {
							return
						}

						if !permDenied && !runFailed {
							succeeded = true
							if err := database.FinalizeMessage(msgID); err != nil {
								slog.Info(fmt.Sprintf("[ws] FinalizeMessage: %v", err))
							}
							clearPendingDenials(database, sessionID)
							if err := database.UpdateSessionStatus(sessionID, db.SessionStatusIdle); err != nil {
								slog.Info(fmt.Sprintf("[ws] UpdateSessionStatus idle: %v", err))
							}
							// 這個 WS 連線正在跑此 session（使用者正在看），完成時順手標已讀，
							// 避免 last_active 更新後被自己的任務完成誤判成未讀。
							if err := database.MarkSessionRead(sessionID); err != nil {
								slog.Info(fmt.Sprintf("[ws] MarkSessionRead: %v", err))
							}
							broadcast(serverMsg{Type: "status", Value: idleUIStatus(database, sessionID)})
							notifyTaskAsync(botToken, tgUserID, notifyCfg, tg.TaskAlert{
								SessionName: sess.Name,
								Outcome:     tg.OutcomeSuccess,
							})
							if quotaSvc != nil {
								go func(at string) {
									snap, err := quotaSvc.RefreshAfterRun(context.Background(), at)
									if err != nil {
										slog.Info(fmt.Sprintf("[quota] refresh after run %s: %v", at, err))
									}
									p := snap.ToPayload()
									broadcast(serverMsg{Type: "quota_update", Quota: &p})
								}(agentType)
							}
						}

					case agent.EventError:
						if e.Err != nil {
							runFailed = true
							errText = agent.PreferErrorText(errText, e.Err.Error())
							broadcast(serverMsg{Type: "error", Content: errText})
						}
					}
				})

				if errors.Is(ctx.Err(), context.Canceled) {
					cancelled = true
				}

				if err != nil {
					if cancelled {
						slog.Info(fmt.Sprintf("[ws] %s.Run cancelled", agentType))
					} else {
						slog.Info(fmt.Sprintf("[ws] %s.Run error: %v", agentType, err))
						if !runFailed {
							runFailed = true
							errText = err.Error()
						} else {
							errText = agent.PreferErrorText(errText, err.Error())
						}
						broadcast(serverMsg{Type: "error", Content: errText})
					}
				} else if cancelled {
					slog.Info(fmt.Sprintf("[ws] %s.Run cancelled (exit 0)", agentType))
				} else {
					slog.Info(fmt.Sprintf("[ws] %s.Run finished OK", agentType))
				}

				if (err != nil || cancelled || (runFailed && err == nil)) && !permDenied {
					if finErr := database.FinalizeMessage(msgID); finErr != nil {
						slog.Info(fmt.Sprintf("[ws] FinalizeMessage (err/cancel path): %v", finErr))
					}
					if stErr := database.UpdateSessionStatus(sessionID, db.SessionStatusIdle); stErr != nil {
						slog.Info(fmt.Sprintf("[ws] UpdateSessionStatus idle (err/cancel path): %v", stErr))
					}
					broadcast(serverMsg{Type: "status", Value: idleUIStatus(database, sessionID)})
				}

				if permDenied {
					return
				}
				if runFailed && !cancelled {
					notifyTaskAsync(botToken, tgUserID, notifyCfg, tg.TaskAlert{
						SessionName: sess.Name,
						AgentType:   agentType,
						Outcome:     tg.OutcomeError,
						Prompt:      userPrompt,
						Error:       errText,
					})
				} else if cancelled {
					notifyTaskAsync(botToken, tgUserID, notifyCfg, tg.TaskAlert{
						SessionName: sess.Name,
						AgentType:   agentType,
						Outcome:     tg.OutcomeCancelled,
						Prompt:      userPrompt,
					})
				}
			}(opts, msgID, prompt)
		}

		startInput := func(input clientMsg, queued *db.QueuedMessage) error {
			prompt, err := attachmentPrompt(database, sessionID, input.Data, input.AttachmentIDs)
			if err != nil {
				return err
			}
			// 排隊訊息保留原送出者（可能是別條連線、別人排的），其餘用本連線身分。
			who := author
			if queued != nil {
				who = queued.Author
			}
			// 訪客訊息送給 agent 前加 [暱稱] 前綴，agent 才分得出是誰在說話。
			if who != "" {
				prompt = "[" + who + "] " + prompt
			}
			var m *db.Message
			if queued != nil {
				m, err = database.PromoteQueuedMessage(sessionID, *queued)
			} else {
				m, err = database.AddUserMessageWithAttachments(sessionID, input.Data, who, input.AttachmentIDs)
			}
			if err != nil {
				return err
			}
			broadcast(serverMsg{Type: "user_message", ID: m.ID, Content: m.Content, CreatedAt: m.CreatedAt, Attachments: m.Attachments, Author: m.Author})
			if input.RequestID != "" {
				send(serverMsg{Type: "input_accepted", RequestID: input.RequestID})
			}
			runAgent(prompt, nil)
			return nil
		}

		// agentBusy：此時送 input 會打斷進行中的工作（runAgent 開頭會 taskCancel），因此改排隊。
		// 不看 DB status：awaiting_confirm 在 set_mode 等路徑不一定會被清掉，會讓閒置 session 永遠排隊。
		// kiroacp 等授權時任務仍在跑（taskIsActive），Claude 等授權以 pending_denials 為準。
		agentBusy := func(s *db.Session) bool {
			return taskIsActive(sessionID) || shellTaskActive(sessionID) ||
				strings.TrimSpace(s.PendingDenials) != ""
		}

		// drainQueueLocked 呼叫前須持有 dispatchLock。
		drainQueueLocked := func() {
			if paused, err := database.QueuePaused(sessionID); err != nil || paused {
				return
			}
			items, err := database.ListQueuedMessages(sessionID)
			if err != nil {
				slog.Info(fmt.Sprintf("[ws] ListQueuedMessages: %v", err))
				return
			}
			if len(items) == 0 {
				return
			}
			q := items[0]
			// Keep the queued item when an attachment is missing or persistence fails.
			if err := startInput(clientMsg{Data: q.Content, AttachmentIDs: q.AttachmentIDs}, &q); err != nil {
				_ = database.SetQueuePaused(sessionID, true)
				broadcast(serverMsg{Type: "error", Content: err.Error()})
				broadcastQueue()
				return
			}
			broadcastQueue()
		}

		// dispatchInput：閒置就執行；忙碌就排隊（不打斷進行中的任務）。
		queueHasItems := func() bool {
			q, err := database.ListQueuedMessages(sessionID)
			return err == nil && len(q) > 0
		}
		dispatchInput := func(input clientMsg) {
			reject := func(err error) {
				if input.RequestID != "" {
					send(serverMsg{Type: "input_rejected", RequestID: input.RequestID, Content: err.Error()})
				} else {
					send(serverMsg{Type: "error", Content: err.Error()})
				}
			}
			if strings.TrimSpace(input.Data) == "" && len(input.AttachmentIDs) == 0 {
				reject(fmt.Errorf("請輸入訊息或加入附件"))
				return
			}
			if _, err := attachmentPrompt(database, sessionID, input.Data, input.AttachmentIDs); err != nil {
				reject(err)
				return
			}
			dl := dispatchLock(sessionID)
			dl.Lock()
			defer dl.Unlock()
			s, err := database.GetSession(sessionID)
			if err != nil {
				slog.Info(fmt.Sprintf("[ws] GetSession (input): %v", err))
				reject(err)
				return
			}
			if strings.TrimSpace(s.ShellPending) != "" {
				reject(fmt.Errorf("請先處理待確認的 Shell 指令"))
				return
			}
			if agentBusy(s) || queueHasItems() {
				// 佇列還有東西（例如失敗後暫停）時也要排到尾端，否則閒置時的新 input 會插隊到舊項目前面。
				if _, err := database.EnqueueMessage(sessionID, input.Data, author, input.AttachmentIDs...); err != nil {
					slog.Info(fmt.Sprintf("[ws] EnqueueMessage: %v", err))
					reject(err)
					return
				}
				broadcastQueue()
				if input.RequestID != "" {
					send(serverMsg{Type: "input_accepted", RequestID: input.RequestID})
				}
				// 閒置且未暫停（例如重啟前留下的佇列）：沒有任務收尾會觸發續跑，這裡直接取出最早的一則。
				if !agentBusy(s) {
					drainQueueLocked()
				}
				return
			}
			if err := startInput(input, nil); err != nil {
				reject(err)
			}
		}

		// drainIfIdle：session 真的閒置時才取出下一則（任務成功收尾、授權解除、手動繼續共用）。
		drainIfIdle = func() {
			dl := dispatchLock(sessionID)
			dl.Lock()
			defer dl.Unlock()
			if s, err := database.GetSession(sessionID); err == nil && !agentBusy(s) && strings.TrimSpace(s.ShellPending) == "" {
				drainQueueLocked()
			}
			broadcastQueue()
		}

		resumeQueue := func() {
			if err := database.SetQueuePaused(sessionID, false); err != nil {
				slog.Info(fmt.Sprintf("[ws] SetQueuePaused: %v", err))
				return
			}
			drainIfIdle()
		}

		startShellGoroutine := func(line string, absDir string) {
			msgID, err := database.CreatePendingMessageWithRole(sessionID, db.RoleShell)
			if err != nil {
				slog.Info(fmt.Sprintf("[ws] CreatePendingMessageWithRole shell: %v", err))
				broadcast(serverMsg{Type: "error", Content: err.Error()})
				return
			}

			ctx, cancel := context.WithCancel(context.Background())
			shellTaskStart(sessionID, cancel, msgID)

			if err := database.UpdateSessionStatus(sessionID, db.SessionStatusRunning); err != nil {
				slog.Info(fmt.Sprintf("[ws] UpdateSessionStatus running (shell): %v", err))
			}
			broadcast(serverMsg{Type: "status", Value: StateShellExec})

			go func(line string, msgID int64, workDir string) {
				defer func() {
					if r := recover(); r != nil {
						slog.Info(fmt.Sprintf("[ws] shell_exec goroutine panic: %v", r))
					}
					shellTaskEnd(sessionID, msgID)
				}()
				var cbMu sync.Mutex
				var done atomic.Bool
				var shellErrText string
				var exitCode int
				err := shell.Run(ctx, shell.RunOptions{Command: line, WorkDir: workDir, Timeout: shellTimeoutSec}, func(e shell.Event) {
					cbMu.Lock()
					defer cbMu.Unlock()
					if ctx.Err() != nil {
						return
					}
					switch e.Type {
					case shell.EventDeltaStdout:
						chunk := appendShellDBChunk("stdout", e.Text)
						if err := database.AppendMessageContent(msgID, chunk); err != nil {
							slog.Info(fmt.Sprintf("[ws] shell append: %v", err))
						}
						broadcast(serverMsg{Type: "shell_delta", Stream: "stdout", Content: e.Text})
					case shell.EventDeltaStderr:
						chunk := appendShellDBChunk("stderr", e.Text)
						if err := database.AppendMessageContent(msgID, chunk); err != nil {
							slog.Info(fmt.Sprintf("[ws] shell append: %v", err))
						}
						broadcast(serverMsg{Type: "shell_delta", Stream: "stderr", Content: e.Text})
					case shell.EventError:
						shellErrText = e.Text
						if e.Text != "" {
							_ = database.AppendMessageContent(msgID, "\n[error] "+e.Text+"\n")
						}
						broadcast(serverMsg{Type: "shell_error", Content: e.Text})
					case shell.EventDone:
						done.Store(true)
						exitCode = e.ExitCode
						finalizeShellMessage(database, msgID, e.ExitCode)
						broadcast(serverMsg{Type: "shell_done", ExitCode: e.ExitCode, ID: msgID})
					}
				})
				interrupted := !done.Load()
				if interrupted {
					appendText := "\n[interrupted]\n"
					if err != nil {
						appendText = "\n[error] " + err.Error() + "\n"
					}
					_ = database.AppendMessageContent(msgID, appendText)
					finalizeShellMessage(database, msgID, -1)
					exitCode = -1
					broadcast(serverMsg{Type: "shell_done", ExitCode: -1, ID: msgID})
				}
				if err := database.UpdateSessionStatus(sessionID, db.SessionStatusIdle); err != nil {
					slog.Info(fmt.Sprintf("[ws] UpdateSessionStatus idle (shell): %v", err))
				}
				broadcast(serverMsg{Type: "status", Value: idleUIStatus(database, sessionID)})
				finishShellNotify(botToken, tgUserID, notifyCfg, sess.Name, line, shellErrText, exitCode, interrupted && err == nil, err)
			}(line, msgID, absDir)
		}

		runShell := func(line string) {
			taskCancel(sessionID)
			if err := database.FinalizePendingMessagesForSession(sessionID); err != nil {
				slog.Info(fmt.Sprintf("[ws] FinalizePendingMessagesForSession (shell): %v", err))
			}

			s, err := database.GetSession(sessionID)
			if err != nil {
				slog.Info(fmt.Sprintf("[ws] GetSession (shell): %v", err))
				broadcast(serverMsg{Type: "error", Content: err.Error()})
				return
			}
			if strings.TrimSpace(s.ShellPending) != "" {
				broadcast(serverMsg{Type: "error", Content: "請先處理待確認的 Shell 指令"})
				return
			}
			if strings.TrimSpace(s.PendingDenials) != "" {
				broadcast(serverMsg{Type: "error", Content: "請先處理工具授權請求"})
				return
			}

			absDir, err := resolveShellWorkDir(s)
			if err != nil {
				broadcast(serverMsg{Type: "error", Content: err.Error()})
				return
			}

			dbCmds, err := database.ListShellCommandsForWorkDirKey(absDir)
			if err != nil {
				slog.Info(fmt.Sprintf("[ws] ListShellCommandsForWorkDirKey: %v", err))
				broadcast(serverMsg{Type: "error", Content: err.Error()})
				return
			}
			effective := shell.EffectiveAllowlist(shellCfg.AllowedCommands, dbCmds)

			runDirect, needConfirm, err := shell.ClassifyShellLine(line, effective)
			if err != nil {
				slog.Info(fmt.Sprintf("[ws] shell 分類拒絕: %v", err))
				broadcast(serverMsg{Type: "error", Content: err.Error()})
				return
			}

			line = strings.TrimSpace(line)

			if needConfirm {
				cmd := shell.FirstCommandName(line)
				payload := shellPendingPayload{
					Line:        line,
					Cmd:         cmd,
					RequestedAt: time.Now().UTC().Format(time.RFC3339),
				}
				b, err := json.Marshal(payload)
				if err != nil {
					broadcast(serverMsg{Type: "error", Content: err.Error()})
					return
				}
				if err := database.AddMessage(sessionID, "user", line, author); err != nil {
					slog.Info(fmt.Sprintf("[ws] 儲存 user 訊息 (shell confirm) 失敗: %v", err))
				}
				broadcast(serverMsg{Type: "user_message", Content: line, Author: author})
				if err := database.UpdateShellPending(sessionID, string(b)); err != nil {
					slog.Info(fmt.Sprintf("[ws] UpdateShellPending: %v", err))
					broadcast(serverMsg{Type: "error", Content: err.Error()})
					return
				}
				if err := database.UpdateSessionStatus(sessionID, db.SessionStatusAwaitingShellConfirm); err != nil {
					slog.Info(fmt.Sprintf("[ws] UpdateSessionStatus awaiting_shell_confirm: %v", err))
				}
				broadcast(serverMsg{Type: "shell_command_request", Command: payload.Cmd, Line: payload.Line, WorkDirKey: absDir})
				broadcast(serverMsg{Type: "status", Value: StateAwaitingShellConfirm})
				return
			}
			if !runDirect {
				return
			}

			if err := database.AddMessage(sessionID, "user", line, author); err != nil {
				slog.Info(fmt.Sprintf("[ws] 儲存 user 訊息 (shell) 失敗: %v", err))
			}
			broadcast(serverMsg{Type: "user_message", Content: line, Author: author})

			startShellGoroutine(line, absDir)
		}

		handleShellAllowExecute := func(remember bool) {
			taskCancel(sessionID)
			if err := database.FinalizePendingMessagesForSession(sessionID); err != nil {
				slog.Info(fmt.Sprintf("[ws] FinalizePendingMessagesForSession (shell allow): %v", err))
			}

			sess, err := database.GetSession(sessionID)
			if err != nil {
				slog.Info(fmt.Sprintf("[ws] GetSession (shell allow): %v", err))
				broadcast(serverMsg{Type: "error", Content: err.Error()})
				return
			}
			if strings.TrimSpace(sess.ShellPending) == "" {
				return
			}
			var p shellPendingPayload
			if err := json.Unmarshal([]byte(sess.ShellPending), &p); err != nil {
				slog.Info(fmt.Sprintf("[ws] shell_pending JSON: %v", err))
				clearShellPending(database, sessionID)
				_ = database.UpdateSessionStatus(sessionID, db.SessionStatusIdle)
				broadcast(serverMsg{Type: "error", Content: "Shell 待確認資料損壞"})
				broadcast(serverMsg{Type: "status", Value: idleUIStatus(database, sessionID)})
				return
			}
			line := strings.TrimSpace(p.Line)
			if line == "" {
				clearShellPending(database, sessionID)
				_ = database.UpdateSessionStatus(sessionID, db.SessionStatusIdle)
				broadcast(serverMsg{Type: "status", Value: idleUIStatus(database, sessionID)})
				return
			}
			if shell.LineContainsShellChaining(line) {
				broadcast(serverMsg{Type: "error", Content: "不允許指令串接、管線或換行（&&、||、|、; 等）"})
				clearShellPending(database, sessionID)
				_ = database.UpdateSessionStatus(sessionID, db.SessionStatusIdle)
				broadcast(serverMsg{Type: "status", Value: idleUIStatus(database, sessionID)})
				return
			}
			absDir, err := resolveShellWorkDir(sess)
			if err != nil {
				broadcast(serverMsg{Type: "error", Content: err.Error()})
				return
			}
			if remember {
				if err := database.AddShellWorkdirCommand(absDir, p.Cmd); err != nil {
					slog.Info(fmt.Sprintf("[ws] AddShellWorkdirCommand: %v", err))
				}
			}
			clearShellPending(database, sessionID)
			startShellGoroutine(line, absDir)
		}

		for {
			_, raw, err := c.ReadMessage()
			if err != nil {
				break
			}

			var msg clientMsg
			if err := json.Unmarshal(raw, &msg); err != nil {
				continue
			}

			if msg.Type == "ping" {
				send(serverMsg{Type: "pong"})
				continue
			}
			// 唯讀訪客（viewer）：除 ping 外一律丟棄並回錯誤事件，不進入任何處理。角色由後端強制。
			if readOnly {
				slog.Info(fmt.Sprintf("[ws] 丟棄唯讀訪客 %q 的 %q 指令 (share=%d)", guestNick, msg.Type, shareID))
				send(serverMsg{Type: "error", Content: "唯讀分享，無法執行此操作"})
				continue
			}

			switch msg.Type {
			case "input":
				slog.Info(fmt.Sprintf("[ws] input len=%d", len(msg.Data)))
				dispatchInput(msg)

			case "queue_remove":
				if err := database.DeleteQueuedMessage(sessionID, msg.ID); err != nil {
					slog.Info(fmt.Sprintf("[ws] DeleteQueuedMessage: %v", err))
				}
				broadcastQueue()

			case "queue_resume":
				resumeQueue()

			case "set_input_mode":
				sx, err := database.GetSession(sessionID)
				if err == nil && sx.Status == db.SessionStatusAwaitingConfirm {
					send(serverMsg{Type: "error", Content: "awaiting confirmation; cannot switch input mode"})
					continue
				}
				if err == nil && strings.TrimSpace(sx.ShellPending) != "" {
					send(serverMsg{Type: "error", Content: "awaiting shell confirmation; cannot switch input mode"})
					continue
				}
				if taskIsActive(sessionID) || shellTaskActive(sessionID) || peekShellPending(sessionID) != nil {
					send(serverMsg{Type: "error", Content: "busy; cannot switch input mode"})
					continue
				}
				mode := strings.TrimSpace(msg.Mode)
				if mode != "agent" && mode != "shell" {
					continue
				}
				if err := database.UpdateSessionInputMode(sessionID, mode); err != nil {
					slog.Info(fmt.Sprintf("[ws] UpdateSessionInputMode: %v", err))
					continue
				}
				broadcast(serverMsg{Type: "input_mode_changed", Value: mode, ShellType: shellTypeString()})

			case "shell_run":
				cmd := strings.TrimSpace(msg.Data)
				if cmd == "" {
					continue
				}
				if taskIsActive(sessionID) {
					broadcast(serverMsg{Type: "error", Content: "AI is running"})
					continue
				}
				if shellTaskActive(sessionID) || peekShellPending(sessionID) != nil {
					broadcast(serverMsg{Type: "error", Content: "shell busy"})
					continue
				}
				sx, err := database.GetSession(sessionID)
				if err != nil {
					broadcast(serverMsg{Type: "error", Content: err.Error()})
					continue
				}
				if sx.PermissionMode != "bypassPermissions" {
					setShellPending(sessionID, &shellPendingInfo{
						Command:   cmd,
						WorkDir:   sx.WorkDir,
						ShellType: shellTypeString(),
					})
					broadcast(serverMsg{
						Type:      "shell_approval_request",
						Content:   cmd,
						WorkDir:   sx.WorkDir,
						ShellType: shellTypeString(),
					})
					broadcast(serverMsg{Type: "status", Value: StateShellAwaitingApproval})
					continue
				}
				beginShellRun(cmd, sx.WorkDir)

			case "shell_approve":
				if taskIsActive(sessionID) || shellTaskActive(sessionID) {
					broadcast(serverMsg{Type: "error", Content: "another task is running"})
					continue
				}
				p := takeShellPending(sessionID)
				if p == nil {
					continue
				}
				beginShellRun(p.Command, p.WorkDir)

			case "shell_cancel":
				if takeShellPending(sessionID) != nil {
					broadcast(serverMsg{Type: "shell_approval_cancelled"})
					broadcast(serverMsg{Type: "status", Value: idleUIStatus(database, sessionID)})
				}

			case "allow_once":
				// kiroacp 互動式授權：解析 pending 而非重跑。
				if permResolve(sessionID, true) {
					continue
				}
				if !isClaude {
					slog.Info(fmt.Sprintf("[ws] agent=%s: allow_once ignored (no pending request)", agentType))
					if clearStaleAwaitingConfirm(database, sessionID) {
						broadcast(serverMsg{Type: "status", Value: idleUIStatus(database, sessionID)})
					}
					continue
				}
				sAllow, err := database.GetSession(sessionID)
				if err != nil {
					continue
				}
				if strings.TrimSpace(sAllow.ShellPending) != "" {
					broadcast(serverMsg{Type: "error", Content: "請先處理待確認的 Shell 指令"})
					continue
				}
				once := make([]string, 0, len(msg.Tools))
				for _, t := range msg.Tools {
					t = strings.TrimSpace(t)
					if t != "" {
						once = append(once, t)
					}
				}
				if len(once) == 0 {
					slog.Info(fmt.Sprintf("[ws] allow_once: empty tools"))
					continue
				}
				// 多人同時回應同一個授權時先到先贏：認領 pending_denials 成功者才繼續，其餘視為已處理。
				if claimed, err := database.ClaimPendingDenials(sessionID); err != nil || !claimed {
					slog.Info(fmt.Sprintf("[ws] allow_once 略過：授權已被處理或不存在 (err=%v)", err))
					continue
				}
				if err := database.UpdateAllowedTools(sessionID, nil); err != nil {
					slog.Info(fmt.Sprintf("[ws] UpdateAllowedTools: %v", err))
				}
				runAgent("please retry the previous operation", once)

			case "deny_once":
				// kiroacp 互動式授權：解析 pending 而非重跑。
				if permResolve(sessionID, false) {
					continue
				}
				if !isClaude {
					slog.Info(fmt.Sprintf("[ws] agent=%s: deny_once ignored (no pending request)", agentType))
					if clearStaleAwaitingConfirm(database, sessionID) {
						broadcast(serverMsg{Type: "status", Value: idleUIStatus(database, sessionID)})
					}
					continue
				}
				// 先到先贏：同 allow_once，已被另一端處理就不再重跑。
				if claimed, err := database.ClaimPendingDenials(sessionID); err != nil || !claimed {
					slog.Info(fmt.Sprintf("[ws] deny_once 略過：授權已被處理或不存在 (err=%v)", err))
					continue
				}
				// 使用者拒絕代表計畫被打斷，後面排隊的不自動接著跑。
				pauseQueue()
				runAgent("[Permission denied by user. Please acknowledge that you cannot perform the requested operation and stop.]", nil)

			case "set_mode":
				if agentType != agent.TypeClaude && agentType != agent.TypeCursor && agentType != agent.TypeAntigravity && agentType != agent.TypeGemini && agentType != agent.TypeKiro && agentType != agent.TypeKiroACP {
					slog.Info(fmt.Sprintf("[ws] agent=%s: set_mode ignored", agentType))
					continue
				}
				sMode, err := database.GetSession(sessionID)
				if err != nil {
					continue
				}
				if strings.TrimSpace(sMode.ShellPending) != "" {
					broadcast(serverMsg{Type: "error", Content: "請先處理待確認的 Shell 指令"})
					continue
				}
				clearPendingDenials(database, sessionID)
				if err := database.UpdatePermissionMode(sessionID, msg.Mode); err != nil {
					slog.Info(fmt.Sprintf("[ws] UpdatePermissionMode: %v", err))
				}
				// 「允許並記住」會走到這裡：授權已解除但 status 原本不會被改回，重連時 sync 仍顯示等授權。
				if sMode.Status == db.SessionStatusAwaitingConfirm && !taskIsActive(sessionID) {
					if err := database.UpdateSessionStatus(sessionID, db.SessionStatusIdle); err != nil {
						slog.Info(fmt.Sprintf("[ws] UpdateSessionStatus idle (set_mode): %v", err))
					}
				}
				broadcast(serverMsg{Type: "status", Value: idleUIStatus(database, sessionID)})
				slog.Info(fmt.Sprintf("[ws] permission mode: %v", msg.Mode))
				// 等授權期間排隊的訊息：授權解除後接著跑（非失敗，不暫停）。
				drainIfIdle()

			case "set_model":
				if err := database.UpdateModel(sessionID, strings.TrimSpace(msg.Model)); err != nil {
					slog.Info(fmt.Sprintf("[ws] UpdateModel: %v", err))
				} else if isStreamless(agentType) {
					// stream 不含 model，不會有人回填 badge；切換當下就要更新。
					if cur, gerr := database.GetSession(sessionID); gerr == nil {
						if p := persistInfoUpdate(database, sessionID, resolveStreamless(cur, agentType)); p != nil {
							broadcast(serverMsg{Type: "model_update", Model: p})
						}
					}
				}
				broadcast(serverMsg{Type: "status", Value: idleUIStatus(database, sessionID)})
				slog.Info(fmt.Sprintf("[ws] model: %v", msg.Model))

			case "set_effort":
				if agentType == agent.TypeCursor {
					slog.Info(fmt.Sprintf("[ws] agent=%s: set_effort ignored", agentType))
					continue
				}
				if err := database.UpdateEffort(sessionID, strings.TrimSpace(msg.Effort)); err != nil {
					slog.Info(fmt.Sprintf("[ws] UpdateEffort: %v", err))
				}
				broadcast(serverMsg{Type: "status", Value: idleUIStatus(database, sessionID)})
				slog.Info(fmt.Sprintf("[ws] effort: %v", msg.Effort))

			case "reset_context":
				taskCancel(sessionID)
				shellTaskCancel(sessionID)
				clearInMemoryShellApproval(sessionID)
				_ = database.FinalizePendingMessagesForSession(sessionID)
				mu.Lock()
				agentSessionID = ""
				mu.Unlock()
				if err := database.UpdateAgentSessionID(sessionID, ""); err != nil {
					slog.Info(fmt.Sprintf("[ws] clear agent_session_id: %v", err))
				}
				if err := database.ClearMessages(sessionID); err != nil {
					slog.Info(fmt.Sprintf("[ws] ClearMessages: %v", err))
				}
				clearPendingDenials(database, sessionID)
				clearShellPending(database, sessionID)
				if err := database.ClearQueuedMessages(sessionID); err != nil {
					slog.Info(fmt.Sprintf("[ws] ClearQueuedMessages: %v", err))
				}
				broadcastQueue()
				if err := database.UpdateSessionStatus(sessionID, db.SessionStatusIdle); err != nil {
					slog.Info(fmt.Sprintf("[ws] UpdateSessionStatus idle: %v", err))
				}
				broadcast(serverMsg{Type: "reset"})
				broadcast(serverMsg{Type: "status", Value: idleUIStatus(database, sessionID)})
				slog.Info(fmt.Sprintf("[ws] session %s reset", sessionID))

			case "interrupt":
				if shellTaskActive(sessionID) {
					// 只 cancel；DB 寫入與 finalize 由 goroutine 的 !done 分支負責
					shellTaskCancel(sessionID)
					continue
				}
				// 兩段式停止：第一次按只送優雅信號（可能被子進程忽略，狀態/DB 不動，任務繼續跑）；
				// 第二次按（或 PID 還沒拿到）才 cancel context 強制 KillTree，
				// 收尾一律由 Run goroutine 的既有邏輯處理，避免前端提早變 IDLE 但進程還在跑。
				// 使用者按停止＝不要自動接著跑：軟中斷時子進程可能仍正常結束（exit 0），不暫停會續跑佇列。
				// kiroacp 等授權時子進程在等我們回覆，軟信號不會有反應，直接強制取消。
				if _, waiting := permPending(sessionID); waiting {
					taskCancel(sessionID)
					pauseQueue()
					continue
				}
				if taskInterrupt(sessionID) {
					pauseQueue()
				} else if clearStaleAwaitingConfirm(database, sessionID) {
					broadcast(serverMsg{Type: "status", Value: idleUIStatus(database, sessionID)})
				}

			case "shell_allow_once":
				handleShellAllowExecute(false)

			case "shell_allow_remember_workdir":
				handleShellAllowExecute(true)

			case "shell_deny":
				clearShellPending(database, sessionID)
				clearInMemoryShellApproval(sessionID)
				if err := database.UpdateSessionStatus(sessionID, db.SessionStatusIdle); err != nil {
					slog.Info(fmt.Sprintf("[ws] UpdateSessionStatus idle (shell_deny): %v", err))
				}
				broadcast(serverMsg{Type: "status", Value: idleUIStatus(database, sessionID)})

			case "shell_exec":
				if !shellCfg.Enabled {
					broadcast(serverMsg{Type: "error", Content: "直連 Shell 未啟用（請在 config.yaml 設定 shell.enabled: true）"})
					continue
				}
				sSh, err := database.GetSession(sessionID)
				if err != nil {
					continue
				}
				if strings.TrimSpace(sSh.ShellPending) != "" {
					broadcast(serverMsg{Type: "error", Content: "請先處理待確認的 Shell 指令"})
					continue
				}
				if strings.TrimSpace(sSh.PendingDenials) != "" {
					broadcast(serverMsg{Type: "error", Content: "請先處理工具授權請求"})
					continue
				}
				line := strings.TrimSpace(msg.Data)
				if line == "" {
					continue
				}
				slog.Info(fmt.Sprintf("[ws] 收到 shell_exec，長度=%d", len(line)))
				runShell(line)

			case "refresh_quota":
				if quotaSvc == nil {
					continue
				}
				go func(at string) {
					snap, _ := quotaSvc.RefreshManual(context.Background(), at)
					p := snap.ToPayload()
					broadcast(serverMsg{Type: "quota_update", Quota: &p})
				}(agentType)
			}
		}
	}
}
