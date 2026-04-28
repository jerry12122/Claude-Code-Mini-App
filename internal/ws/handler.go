package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	fiberws "github.com/gofiber/contrib/websocket"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/claude"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/cursor"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/gemini"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/shell"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/tg"
)

func clearPendingDenials(database *db.DB, sessionID string) {
	if err := database.UpdatePendingDenials(sessionID, ""); err != nil {
		log.Printf("[ws] clear pending_denials: %v", err)
	}
}

func clearShellPending(database *db.DB, sessionID string) {
	if err := database.UpdateShellPending(sessionID, ""); err != nil {
		log.Printf("[ws] 清除 shell_pending 失敗: %v", err)
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
	Type  string   `json:"type"`
	Data  string   `json:"data,omitempty"`
	Tools []string `json:"tools,omitempty"`
	Mode  string   `json:"mode,omitempty"`
}

type serverMsg struct {
	Type         string            `json:"type"`
	Value        string            `json:"value,omitempty"`
	Content      string            `json:"content,omitempty"`
	ID           int64             `json:"id,omitempty"`
	Tools        interface{}       `json:"tools,omitempty"`
	Messages     json.RawMessage   `json:"messages,omitempty"`
	InputMode    string            `json:"input_mode,omitempty"`
	ShellType    string            `json:"shell_type,omitempty"`
	WorkDir      string            `json:"work_dir,omitempty"`
	Command      string            `json:"command,omitempty"`
	Line         string            `json:"line,omitempty"`
	WorkDirKey   string            `json:"work_dir_key,omitempty"`
	Stream       string            `json:"stream,omitempty"`
	ExitCode     int               `json:"exit_code,omitempty"`
	ShellPending *shellPendingInfo `json:"shell_pending,omitempty"`
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

func NewHandler(database *db.DB, botToken string, shellCfg ShellOpts) func(*fiberws.Conn) {
	return func(c *fiberws.Conn) {
		sessionID := c.Params("id")
		tgUserID, _ := c.Locals("tg_id").(int64)

		sess, err := database.GetSession(sessionID)
		if err != nil {
			log.Printf("[ws] session %s missing: %v", sessionID, err)
			c.Close()
			return
		}

		log.Printf("[ws] session %s connected (agent=%s agentSessionID=%q mode=%s)", sessionID, sess.AgentType, sess.AgentSessionID, sess.PermissionMode)
		defer log.Printf("[ws] session %s disconnected", sessionID)

		var mu sync.Mutex
		agentType := sess.AgentType
		if agentType == "" {
			agentType = agent.TypeClaude
		}
		agentSessionID := sess.AgentSessionID

		isClaude := agentType == agent.TypeClaude

		send := func(msg serverMsg) bool {
			b, _ := json.Marshal(msg)
			return c.WriteMessage(1, b) == nil
		}

		unsub := hub.Subscribe(sessionID, send)
		defer unsub()

		broadcast := func(msg serverMsg) {
			hub.Broadcast(sessionID, msg)
		}

		syncData, err := buildSyncPayload(database, sessionID)
		if err != nil {
			log.Printf("[ws] buildSyncPayload: %v", err)
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
		send(syncMsg)

		shellTimeoutSec := 60
		if ts := strings.TrimSpace(shellCfg.Timeout); ts != "" {
			if d, err := time.ParseDuration(ts); err == nil {
				if sec := int(d.Seconds()); sec > 0 {
					shellTimeoutSec = sec
				}
			}
		}

		if isClaude && sess.PendingDenials != "" {
			send(serverMsg{Type: "permission_request", Tools: json.RawMessage(sess.PendingDenials)})
			log.Printf("[ws] restored pending_denials session=%s", sessionID)
		}

		if strings.TrimSpace(sess.ShellPending) != "" {
			var p shellPendingPayload
			if err := json.Unmarshal([]byte(sess.ShellPending), &p); err == nil {
				if absDir, err := resolveShellWorkDir(sess); err == nil {
					send(serverMsg{Type: "shell_command_request", Command: p.Cmd, Line: p.Line, WorkDirKey: absDir})
				}
			}
			log.Printf("[ws] 還原 shell_pending for session %s", sessionID)
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

			if err := database.AddMessage(sessionID, "user", command); err != nil {
				log.Printf("[ws] shell AddMessage: %v", err)
			}
			broadcast(serverMsg{Type: "user_message", Content: command})

			msgID, err := database.CreatePendingMessageWithRole(sessionID, db.RoleShell)
			if err != nil {
				broadcast(serverMsg{Type: "error", Content: err.Error()})
				return
			}

			ctx, cancel := context.WithCancel(context.Background())
			shellTaskStart(sessionID, cancel, msgID)

			if err := database.UpdateSessionStatus(sessionID, db.SessionStatusRunning); err != nil {
				log.Printf("[ws] shell running status: %v", err)
			}
			broadcast(serverMsg{Type: "status", Value: StateShellRunning})

			go func(command string, msgID int64, wdir string) {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("[ws] shell goroutine panic: %v", r)
					}
					shellTaskEnd(sessionID, msgID)
				}()
				var cbMu sync.Mutex
				var done atomic.Bool
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
							log.Printf("[ws] shell append: %v", err)
						}
						broadcast(serverMsg{Type: "shell_delta", Stream: "stdout", Content: e.Text})
					case shell.EventDeltaStderr:
						chunk := appendShellDBChunk("stderr", e.Text)
						if err := database.AppendMessageContent(msgID, chunk); err != nil {
							log.Printf("[ws] shell append: %v", err)
						}
						broadcast(serverMsg{Type: "shell_delta", Stream: "stderr", Content: e.Text})
					case shell.EventError:
						if e.Text != "" {
							_ = database.AppendMessageContent(msgID, "\n[error] "+e.Text+"\n")
						}
						broadcast(serverMsg{Type: "shell_error", Content: e.Text})
					case shell.EventDone:
						done.Store(true)
						finalizeShellMessage(database, msgID, e.ExitCode)
						broadcast(serverMsg{Type: "shell_done", ExitCode: e.ExitCode})
					}
				})
				if !done.Load() {
					appendText := "\n[interrupted]\n"
					if err != nil {
						appendText = "\n[error] " + err.Error() + "\n"
					}
					_ = database.AppendMessageContent(msgID, appendText)
					finalizeShellMessage(database, msgID, -1)
					broadcast(serverMsg{Type: "shell_done", ExitCode: -1})
				}
				if err := database.UpdateSessionStatus(sessionID, db.SessionStatusIdle); err != nil {
					log.Printf("[ws] shell idle status: %v", err)
				}
				broadcast(serverMsg{Type: "status", Value: idleUIStatus(database, sessionID)})
			}(command, msgID, workDir)
		}

		// runAgent：與 WS 解耦，任務在背景執行。
		// allowedOnce：僅「允許此操作」該次 retry 帶入 --allowedTools，不可寫入 DB（否則變成永久 allowlist）。
		runAgent := func(prompt string, allowedOnce []string) {
			shellTaskCancel(sessionID)
			clearShellPending(database, sessionID)
			clearInMemoryShellApproval(sessionID)
			taskCancel(sessionID)
			if err := database.FinalizePendingMessagesForSession(sessionID); err != nil {
				log.Printf("[ws] FinalizePendingMessagesForSession: %v", err)
			}

			s, err := database.GetSession(sessionID)
			if err != nil {
				log.Printf("[ws] GetSession: %v", err)
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
				log.Printf("[ws] CreatePendingMessage: %v", err)
				broadcast(serverMsg{Type: "error", Content: err.Error()})
				return
			}

			ctx, cancel := context.WithCancel(context.Background())
			taskStart(sessionID, cancel, msgID)

			if err := database.UpdateSessionStatus(sessionID, db.SessionStatusRunning); err != nil {
				log.Printf("[ws] UpdateSessionStatus running: %v", err)
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

			opts := agent.RunOptions{
				Prompt:       prompt,
				SessionID:    agSid,
				WorkDir:      wdir,
				ExtraArgs:    extra,
				CliExtraArgs: cliExtra,
			}

			runner, err := agent.NewRunner(agentType)
			if err != nil {
				log.Printf("[ws] NewRunner %s: %v", agentType, err)
				_ = database.FinalizeMessage(msgID)
				_ = database.UpdateSessionStatus(sessionID, db.SessionStatusIdle)
				taskEnd(sessionID)
				broadcast(serverMsg{Type: "error", Content: err.Error()})
				broadcast(serverMsg{Type: "status", Value: idleUIStatus(database, sessionID)})
				return
			}

			log.Printf("[ws] start %s.Run agentSessionID=%q mode=%s msgID=%d", runner.Name(), opts.SessionID, pm, msgID)

			go func(opts agent.RunOptions, msgID int64) {
				defer taskEnd(sessionID)

				permDenied := false

				err := runner.Run(ctx, opts, func(e agent.Event) {
					if ctx.Err() != nil {
						return
					}
					switch e.Type {
					case agent.EventStreamStart:
						broadcast(serverMsg{Type: "status", Value: StateStreaming})

					case agent.EventThinking:
						// 思考鏈：不寫 DB、不改狀態，直接廣播給前端覆寫顯示。
						if e.Text != "" {
							broadcast(serverMsg{Type: "thinking", Content: e.Text})
						}

					case agent.EventDelta:
						if e.Text != "" {
							if err := database.AppendMessageContent(msgID, e.Text); err != nil {
								log.Printf("[ws] AppendMessageContent: %v", err)
							}
							broadcast(serverMsg{Type: "delta", Content: e.Text})
						}

					case agent.EventSessionInit:
						if e.SessionID == "" {
							return
						}
						mu.Lock()
						if e.SessionID != agentSessionID {
							agentSessionID = e.SessionID
							if err := database.UpdateAgentSessionID(sessionID, agentSessionID); err != nil {
								log.Printf("[ws] UpdateAgentSessionID: %v", err)
							}
						}
						mu.Unlock()

					case agent.EventPermDenied:
						if !isClaude {
							return
						}
						permDenied = true
						if raw, err := json.Marshal(e.Denials); err == nil {
							if err := database.UpdatePendingDenials(sessionID, string(raw)); err != nil {
								log.Printf("[ws] UpdatePendingDenials: %v", err)
							}
						}
						if err := database.UpdateSessionStatus(sessionID, db.SessionStatusAwaitingConfirm); err != nil {
							log.Printf("[ws] UpdateSessionStatus awaiting_confirm: %v", err)
						}
						broadcast(serverMsg{Type: "status", Value: StateAwaitingConfirm})
						broadcast(serverMsg{Type: "permission_request", Tools: e.Denials})
						go tg.Notify(botToken, tgUserID, fmt.Sprintf("⚠️ *%s* 需要授權確認，請開啟 App", sess.Name))

					case agent.EventDone:
						mu.Lock()
						if e.SessionID != "" && e.SessionID != agentSessionID {
							agentSessionID = e.SessionID
							if err := database.UpdateAgentSessionID(sessionID, agentSessionID); err != nil {
								log.Printf("[ws] UpdateAgentSessionID: %v", err)
							}
						}
						mu.Unlock()

						if !permDenied {
							rt := strings.TrimSpace(e.ResultText)
							if rt != "" {
								if err := database.UpdateMessageResultText(msgID, rt); err != nil {
									log.Printf("[ws] UpdateMessageResultText: %v", err)
								}
								broadcast(serverMsg{Type: "message_result_text", ID: msgID, Content: rt})
							}
							if err := database.FinalizeMessage(msgID); err != nil {
								log.Printf("[ws] FinalizeMessage: %v", err)
							}
							clearPendingDenials(database, sessionID)
							if err := database.UpdateSessionStatus(sessionID, db.SessionStatusIdle); err != nil {
								log.Printf("[ws] UpdateSessionStatus idle: %v", err)
							}
							broadcast(serverMsg{Type: "status", Value: idleUIStatus(database, sessionID)})
							go tg.Notify(botToken, tgUserID, fmt.Sprintf("✅ *%s* 任務完成", sess.Name))
						}

					case agent.EventError:
						if e.Err != nil {
							broadcast(serverMsg{Type: "error", Content: e.Err.Error()})
						}
					}
				})

				if err != nil {
					if ctx.Err() != nil {
						log.Printf("[ws] %s.Run cancelled", agentType)
					} else {
						log.Printf("[ws] %s.Run error: %v", agentType, err)
						broadcast(serverMsg{Type: "error", Content: err.Error()})
					}
					if err := database.FinalizeMessage(msgID); err != nil {
						log.Printf("[ws] FinalizeMessage (err path): %v", err)
					}
					if !permDenied {
						if err := database.UpdateSessionStatus(sessionID, db.SessionStatusIdle); err != nil {
							log.Printf("[ws] UpdateSessionStatus idle: %v", err)
						}
						broadcast(serverMsg{Type: "status", Value: idleUIStatus(database, sessionID)})
					}
				} else {
					log.Printf("[ws] %s.Run finished OK", agentType)
				}
			}(opts, msgID)
		}

		startShellGoroutine := func(line string, absDir string) {
			msgID, err := database.CreatePendingMessageWithRole(sessionID, db.RoleShell)
			if err != nil {
				log.Printf("[ws] CreatePendingMessageWithRole shell: %v", err)
				broadcast(serverMsg{Type: "error", Content: err.Error()})
				return
			}

			ctx, cancel := context.WithCancel(context.Background())
			shellTaskStart(sessionID, cancel, msgID)

			if err := database.UpdateSessionStatus(sessionID, db.SessionStatusRunning); err != nil {
				log.Printf("[ws] UpdateSessionStatus running (shell): %v", err)
			}
			broadcast(serverMsg{Type: "status", Value: StateShellExec})

			go func(line string, msgID int64, workDir string) {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("[ws] shell_exec goroutine panic: %v", r)
					}
					shellTaskEnd(sessionID, msgID)
				}()
				var cbMu sync.Mutex
				var done atomic.Bool
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
							log.Printf("[ws] shell append: %v", err)
						}
						broadcast(serverMsg{Type: "shell_delta", Stream: "stdout", Content: e.Text})
					case shell.EventDeltaStderr:
						chunk := appendShellDBChunk("stderr", e.Text)
						if err := database.AppendMessageContent(msgID, chunk); err != nil {
							log.Printf("[ws] shell append: %v", err)
						}
						broadcast(serverMsg{Type: "shell_delta", Stream: "stderr", Content: e.Text})
					case shell.EventError:
						if e.Text != "" {
							_ = database.AppendMessageContent(msgID, "\n[error] "+e.Text+"\n")
						}
						broadcast(serverMsg{Type: "shell_error", Content: e.Text})
					case shell.EventDone:
						done.Store(true)
						finalizeShellMessage(database, msgID, e.ExitCode)
						broadcast(serverMsg{Type: "shell_done", ExitCode: e.ExitCode, ID: msgID})
					}
				})
				if !done.Load() {
					appendText := "\n[interrupted]\n"
					if err != nil {
						appendText = "\n[error] " + err.Error() + "\n"
					}
					_ = database.AppendMessageContent(msgID, appendText)
					finalizeShellMessage(database, msgID, -1)
					broadcast(serverMsg{Type: "shell_done", ExitCode: -1, ID: msgID})
				}
				if err := database.UpdateSessionStatus(sessionID, db.SessionStatusIdle); err != nil {
					log.Printf("[ws] UpdateSessionStatus idle (shell): %v", err)
				}
				broadcast(serverMsg{Type: "status", Value: idleUIStatus(database, sessionID)})
			}(line, msgID, absDir)
		}

		runShell := func(line string) {
			taskCancel(sessionID)
			if err := database.FinalizePendingMessagesForSession(sessionID); err != nil {
				log.Printf("[ws] FinalizePendingMessagesForSession (shell): %v", err)
			}

			s, err := database.GetSession(sessionID)
			if err != nil {
				log.Printf("[ws] GetSession (shell): %v", err)
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
				log.Printf("[ws] ListShellCommandsForWorkDirKey: %v", err)
				broadcast(serverMsg{Type: "error", Content: err.Error()})
				return
			}
			effective := shell.EffectiveAllowlist(shellCfg.AllowedCommands, dbCmds)

			runDirect, needConfirm, err := shell.ClassifyShellLine(line, effective)
			if err != nil {
				log.Printf("[ws] shell 分類拒絕: %v", err)
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
				if err := database.AddMessage(sessionID, "user", line); err != nil {
					log.Printf("[ws] 儲存 user 訊息 (shell confirm) 失敗: %v", err)
				}
				broadcast(serverMsg{Type: "user_message", Content: line})
				if err := database.UpdateShellPending(sessionID, string(b)); err != nil {
					log.Printf("[ws] UpdateShellPending: %v", err)
					broadcast(serverMsg{Type: "error", Content: err.Error()})
					return
				}
				if err := database.UpdateSessionStatus(sessionID, db.SessionStatusAwaitingShellConfirm); err != nil {
					log.Printf("[ws] UpdateSessionStatus awaiting_shell_confirm: %v", err)
				}
				broadcast(serverMsg{Type: "shell_command_request", Command: payload.Cmd, Line: payload.Line, WorkDirKey: absDir})
				broadcast(serverMsg{Type: "status", Value: StateAwaitingShellConfirm})
				return
			}
			if !runDirect {
				return
			}

			if err := database.AddMessage(sessionID, "user", line); err != nil {
				log.Printf("[ws] 儲存 user 訊息 (shell) 失敗: %v", err)
			}
			broadcast(serverMsg{Type: "user_message", Content: line})

			startShellGoroutine(line, absDir)
		}

		handleShellAllowExecute := func(remember bool) {
			taskCancel(sessionID)
			if err := database.FinalizePendingMessagesForSession(sessionID); err != nil {
				log.Printf("[ws] FinalizePendingMessagesForSession (shell allow): %v", err)
			}

			sess, err := database.GetSession(sessionID)
			if err != nil {
				log.Printf("[ws] GetSession (shell allow): %v", err)
				broadcast(serverMsg{Type: "error", Content: err.Error()})
				return
			}
			if strings.TrimSpace(sess.ShellPending) == "" {
				return
			}
			var p shellPendingPayload
			if err := json.Unmarshal([]byte(sess.ShellPending), &p); err != nil {
				log.Printf("[ws] shell_pending JSON: %v", err)
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
					log.Printf("[ws] AddShellWorkdirCommand: %v", err)
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

			switch msg.Type {
			case "input":
				log.Printf("[ws] input len=%d", len(msg.Data))
				sIn, err := database.GetSession(sessionID)
				if err != nil {
					log.Printf("[ws] GetSession (input): %v", err)
					continue
				}
				if strings.TrimSpace(sIn.ShellPending) != "" {
					broadcast(serverMsg{Type: "error", Content: "請先處理待確認的 Shell 指令"})
					continue
				}
				if err := database.AddMessage(sessionID, "user", msg.Data); err != nil {
					log.Printf("[ws] save user message: %v", err)
				}
				broadcast(serverMsg{Type: "user_message", Content: msg.Data})
				runAgent(msg.Data, nil)

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
					log.Printf("[ws] UpdateSessionInputMode: %v", err)
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
				if !isClaude {
					log.Printf("[ws] agent=%s: allow_once ignored", agentType)
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
				clearPendingDenials(database, sessionID)
				if err := database.UpdateAllowedTools(sessionID, nil); err != nil {
					log.Printf("[ws] UpdateAllowedTools: %v", err)
				}
				once := make([]string, 0, len(msg.Tools))
				for _, t := range msg.Tools {
					t = strings.TrimSpace(t)
					if t != "" {
						once = append(once, t)
					}
				}
				if len(once) == 0 {
					log.Printf("[ws] allow_once: empty tools")
					continue
				}
				runAgent("please retry the previous operation", once)

			case "deny_once":
				if !isClaude {
					log.Printf("[ws] agent=%s: deny_once ignored", agentType)
					continue
				}
				clearPendingDenials(database, sessionID)
				runAgent("[Permission denied by user. Please acknowledge that you cannot perform the requested operation and stop.]", nil)

			case "set_mode":
				if agentType != agent.TypeClaude && agentType != agent.TypeCursor && agentType != agent.TypeGemini {
					log.Printf("[ws] agent=%s: set_mode ignored", agentType)
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
					log.Printf("[ws] UpdatePermissionMode: %v", err)
				}
				broadcast(serverMsg{Type: "status", Value: idleUIStatus(database, sessionID)})
				log.Println("[ws] permission mode:", msg.Mode)

			case "reset_context":
				taskCancel(sessionID)
				shellTaskCancel(sessionID)
				clearInMemoryShellApproval(sessionID)
				_ = database.FinalizePendingMessagesForSession(sessionID)
				mu.Lock()
				agentSessionID = ""
				mu.Unlock()
				if err := database.UpdateAgentSessionID(sessionID, ""); err != nil {
					log.Printf("[ws] clear agent_session_id: %v", err)
				}
				if err := database.ClearMessages(sessionID); err != nil {
					log.Printf("[ws] ClearMessages: %v", err)
				}
				clearPendingDenials(database, sessionID)
				clearShellPending(database, sessionID)
				if err := database.UpdateSessionStatus(sessionID, db.SessionStatusIdle); err != nil {
					log.Printf("[ws] UpdateSessionStatus idle: %v", err)
				}
				broadcast(serverMsg{Type: "reset"})
				broadcast(serverMsg{Type: "status", Value: idleUIStatus(database, sessionID)})
				log.Printf("[ws] session %s reset", sessionID)

			case "interrupt":
				if shellTaskActive(sessionID) {
					// 只 cancel；DB 寫入與 finalize 由 goroutine 的 !done 分支負責
					shellTaskCancel(sessionID)
					continue
				}
				taskCancel(sessionID)
				_ = database.FinalizePendingMessagesForSession(sessionID)
				clearShellPending(database, sessionID)
				clearInMemoryShellApproval(sessionID)
				if err := database.UpdateSessionStatus(sessionID, db.SessionStatusIdle); err != nil {
					log.Printf("[ws] UpdateSessionStatus idle: %v", err)
				}
				broadcast(serverMsg{Type: "status", Value: idleUIStatus(database, sessionID)})

			case "shell_allow_once":
				handleShellAllowExecute(false)

			case "shell_allow_remember_workdir":
				handleShellAllowExecute(true)

			case "shell_deny":
				clearShellPending(database, sessionID)
				clearInMemoryShellApproval(sessionID)
				if err := database.UpdateSessionStatus(sessionID, db.SessionStatusIdle); err != nil {
					log.Printf("[ws] UpdateSessionStatus idle (shell_deny): %v", err)
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
				log.Printf("[ws] 收到 shell_exec，長度=%d", len(line))
				runShell(line)
			}
		}
	}
}
