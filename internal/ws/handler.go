package ws

import (
	"encoding/json"
	"fmt"

	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	fiberws "github.com/gofiber/contrib/websocket"

	"log/slog"

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
	"github.com/jerry12122/Claude-Code-Mini-App/internal/tg"
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
	StateAwaitingInput        = "AWAITING_INPUT"
	StateAwaitingShellConfirm = "AWAITING_SHELL_CONFIRM"
	StateShellExec            = "SHELL_EXEC"
)

// lingerNoticeDelay：result 後程序超過這段時間仍未退出，才通知前端「等待喚醒中」。
var lingerNoticeDelay = 3 * time.Second // var：測試縮短用

type clientMsg struct {
	BatchID         string              `json:"batch_id,omitempty"`
	BatchItems      []db.QueueBatchItem `json:"items,omitempty"`
	interrupted     bool                // 僅 RunNow 內部設定；未匯出，不接受 JSON 輸入。
	invalidUTF8     bool                // 僅 readLoop 設定：queue_edit 的原始 frame 含無效 UTF-8（JSON 解碼會悄悄換成 U+FFFD）。
	Answers         []questionAnswer    `json:"answers,omitempty"`
	RequestID       string              `json:"request_id,omitempty"`
	AttachmentIDs   []string            `json:"attachment_ids,omitempty"`
	Type            string              `json:"type"`
	Data            string              `json:"data,omitempty"`
	Tools           []string            `json:"tools,omitempty"`
	Mode            string              `json:"mode,omitempty"`
	Model           string              `json:"model,omitempty"`
	Effort          string              `json:"effort,omitempty"`
	ID              int64               `json:"id,omitempty"`         // queue_remove 的目標
	Generation      int64               `json:"generation,omitempty"` // 僅內部 quota_resume 攜帶，用來對上這一次的世代
	SourceSessionID string              `json:"source_session_id,omitempty"`
	// RequireIdle、RoomID、PMSessionID 只給工作室喚醒連線上的工具重置。
	// 一般客戶端送出的 reset_context 仍是明確取消，這些欄位不改變那個語意。
	RequireIdle bool   `json:"require_idle,omitempty"`
	RoomID      string `json:"room_id,omitempty"`
	PMSessionID string `json:"pm_session_id,omitempty"`
	// Previous、EditContent 只給 queue_edit。用 RawMessage 保留「缺欄／null／型別錯」的差異，不影響其他訊息的解析。
	Previous    json.RawMessage `json:"previous,omitempty"`
	EditContent json.RawMessage `json:"content,omitempty"`
}

type serverMsg struct {
	BatchID      string             `json:"batch_id,omitempty"`
	Question     *pendingQuestion   `json:"question,omitempty"`
	RequestID    string             `json:"request_id,omitempty"`
	Code         string             `json:"code,omitempty"` // 機器可讀的拒絕原因，例如 update_in_progress
	CreatedAt    string             `json:"created_at,omitempty"`
	RunStartedAt int64              `json:"run_started_at,omitempty"` // status/sync：任務開始時間，Unix 毫秒。
	ServerTime   int64              `json:"server_time,omitempty"`    // 同份快照的伺服器時間，供前端校正時鐘差。
	Attachments  []db.Attachment    `json:"attachments,omitempty"`
	Type         string             `json:"type"`
	Role         string             `json:"role,omitempty"`
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
	// sync 與 queue_update 才帶這兩欄。指標讓 none 也能送出空的時間，清掉畫面舊狀態。
	QuotaResumeState *string            `json:"quota_resume_state,omitempty"`
	QuotaResumeAt    *string            `json:"quota_resume_at,omitempty"`
	Started          *bool              `json:"started,omitempty"`   // type=quota_resumed：內部喚醒後是否已有任務在跑
	Lingering        bool               `json:"lingering,omitempty"` // type=sync：status 已 IDLE 但任務程序仍活著（等喚醒中）
	Author           string             `json:"author,omitempty"`    // 說話者暱稱；空字串＝擁有者。presence 事件中為事件主角
	Online           []string           `json:"online,omitempty"`    // presence：目前在線名單
	Handoff          *manualHandoffInfo `json:"handoff,omitempty"`   // type=handoff_available：自動交接未成功時的手動接手資訊

	HistoryHasMore  bool `json:"history_has_more,omitempty"`
	MaxMessageBytes int  `json:"max_message_bytes,omitempty"`

	// Telegram 時間線（與 HTTP 分頁同一份 DTO；其他 agent 不使用）。
	Telegram                *db.TelegramRef `json:"telegram,omitempty"`
	Delivery                string          `json:"delivery,omitempty"`
	ClientMessageID         string          `json:"client_message_id,omitempty"`
	ReplacesClientMessageID string          `json:"replaces_client_message_id,omitempty"`
	Error                   string          `json:"error,omitempty"`      // type=delivery 的送達錯誤
	SyncState               string          `json:"sync_state,omitempty"` // type=sync（Telegram）：ready／offline
	TelegramScope           *tgScope        `json:"telegram_scope,omitempty"`
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

func NewHandler(database *db.DB, botToken string, shellCfg ShellOpts, quotaSvc *quota.Service, notifyCfg tg.NotifyConfig, opts ...HandlerOption) func(*fiberws.Conn) {
	var ho handlerOptions
	for _, opt := range opts {
		opt(&ho)
	}
	notifyCfg = notifyConfigFrom(notifyCfg)
	setGateDB(database)
	return func(c *fiberws.Conn) {
		h := &wsConn{
			database:  database,
			shellCfg:  shellCfg,
			quotaSvc:  quotaSvc,
			notifyCfg: notifyCfg,
			ho:        ho,
			c:         c,
		}
		h.sessionID = h.c.Params("id")
		h.tgUserID, _ = h.c.Locals("tg_id").(int64)
		h.roomWake, _ = h.c.Locals("room_wake").(bool)
		h.mcpAgent, _ = h.c.Locals("mcp_agent").(bool)

		// 訪客身分由 authMiddleware 寫入 Locals；擁有者沒有這些值。訪客一律沒有 tg_id，不觸發 Telegram 通知。
		h.shareID, _ = h.c.Locals("share_id").(int64)
		h.isGuest = h.shareID != 0
		h.guestNick, _ = h.c.Locals("share_nickname").(string)
		guestRole, _ := h.c.Locals("share_role").(string)
		guestMode, _ := h.c.Locals("share_mode").(string)
		guestExpires, _ := h.c.Locals("share_expires_at").(time.Time)
		h.author = "" // 本連線送出的訊息署名；空字串＝擁有者
		// 角色在後端強制：訪客除 editor 外一律唯讀（未知角色也視為唯讀）。
		h.readOnly = h.isGuest && guestRole != db.ShareRoleEditor
		if h.isGuest {
			h.tgUserID = 0
			h.author = h.guestNick
		}

		var err error
		h.sess, err = h.database.GetSession(h.sessionID)
		if err != nil {
			slog.Info(fmt.Sprintf("[ws] session %s missing: %v", h.sessionID, err))
			h.c.Close()
			return
		}
		if h.isGuest {
			// 縱深防禦：snapshot 不該走到這裡；連線瞬間分享可能剛被結束或到期，再確認一次。
			sh, err := h.database.GetShare(h.shareID)
			if guestMode == db.ShareModeSnapshot || err != nil || !sh.Active(time.Now()) || sh.SessionID != h.sessionID || h.guestNick == "" {
				slog.Info(fmt.Sprintf("[ws] 拒絕訪客連線 share=%d session=%s", h.shareID, h.sessionID))
				h.c.Close()
				return
			}
		}
		slog.Info(fmt.Sprintf("[ws] session %s connected (agent=%s agentSessionID=%q mode=%s)", h.sessionID, h.sess.AgentType, h.sess.AgentSessionID, h.sess.PermissionMode))
		defer slog.Info(fmt.Sprintf("[ws] session %s disconnected", h.sessionID))

		h.agentType = h.sess.AgentType
		if h.agentType == "" {
			h.agentType = agent.TypeClaude
		}
		// Telegram 會話的「執行」只是把訊息轉出去，每次送出都推「任務完成」是干擾；空 token 讓 notifyTaskAsync 直接略過。
		// 必須是本連線的區域變數，不能改到外層共用的 botToken。
		h.botToken = botToken
		if h.agentType == agent.TypeTelegram {
			h.botToken = ""
		}
		h.agentSessionID = h.sess.AgentSessionID

		h.isClaude = h.agentType == agent.TypeClaude

		h.writer = newSocketWriter(h.c)
		defer h.writer.Close()

		unsub := hub.Subscribe(h.sessionID, h.send)
		defer unsub()

		// 在線名單登記；訪客另有 kick（結束分享）與到期 timer。
		entry := &connEntry{sessionID: h.sessionID, shareID: h.shareID, nickname: h.guestNick}
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
			h.send(serverMsg{Type: "share_ended", Content: reason})
		}
		defer func() {
			kickMu.Lock()
			finished = true
			kickMu.Unlock()
		}()
		removePresence := presenceAdd(entry)
		if h.isGuest {
			slog.Info(fmt.Sprintf("[ws] 訪客 %q 連線 share=%d role=%s", h.guestNick, h.shareID, guestRole))
			expiry := time.AfterFunc(time.Until(guestExpires), func() { entry.kick(shareEndExpired) })
			defer expiry.Stop()
		}
		defer func() {
			removePresence()
			announcePresence(h.sessionID, "leave", entry.displayName(), h.isGuest)
		}()

		if !h.sendInitialSync(entry) {
			return
		}
		h.restoreState()
		h.initDispatcher()
		untrackLive := trackLive(h)
		defer untrackLive()
		h.readLoop()
	}
}
