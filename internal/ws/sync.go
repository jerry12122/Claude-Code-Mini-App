package ws

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

// SyncPayload is sent to the client on WebSocket connect (type "sync").
type SyncPayload struct {
	UIState         string
	Messages        json.RawMessage
	InputMode       string
	ShellType       string
	ShellPendingCmd *shellPendingInfo
	// Telegram：sync 不帶歷史；TelegramScope 為目前已驗證的 account/peer（未驗證時為 nil）。
	TelegramScope *tgScope
}

func computeWSState(sess *db.Session, msgs []*db.Message) string {
	if sess.Status == db.SessionStatusAwaitingInput {
		return StateAwaitingInput
	}
	if strings.TrimSpace(sess.ShellPending) != "" {
		return StateAwaitingShellConfirm
	}
	if peekShellPending(sess.ID) != nil {
		return StateShellAwaitingApproval
	}
	if sess.PendingDenials != "" {
		return StateAwaitingConfirm
	}
	if sess.Status == db.SessionStatusAwaitingConfirm {
		return StateAwaitingConfirm
	}
	if sess.Status == db.SessionStatusRunning {
		for i := len(msgs) - 1; i >= 0; i-- {
			m := msgs[i]
			if m.Role == db.RoleShell && m.Status == db.MessageStatusPending {
				if m.Content == "" {
					return StateShellExec
				}
				return StateShellRunning
			}
			if m.Role == "claude" && m.Status == db.MessageStatusPending {
				if m.Content == "" {
					return StateThinking
				}
				return StateStreaming
			}
		}
		return StateThinking
	}
	if sess.InputMode == "shell" {
		return StateShellIdle
	}
	return StateIdle
}

func buildSyncPayload(database *db.DB, sessionID string) (SyncPayload, error) {
	sess, err := database.GetSession(sessionID)
	if err != nil {
		return SyncPayload{}, err
	}
	// Telegram：歷史由 HTTP 直通 getHistory，sync 只帶 scope；其餘 agent 維持完整列表。
	var msgs []*db.Message
	var scope *tgScope
	if sess.AgentType == agent.TypeTelegram {
		// 歷史改由 HTTP 直通 getHistory。sync 只帶目前 scope，不從資料庫帶訊息。
		if telegramScopeFn != nil {
			if acct, peer, ok := telegramScopeFn(sessionID); ok {
				scope = &tgScope{AccountID: acct, PeerID: peer}
			}
		}
	} else {
		var err error
		if msgs, err = database.ListMessages(sessionID); err != nil {
			return SyncPayload{}, err
		}
	}
	uiState := computeWSState(sess, msgs)
	raw, err := marshalSyncRows(msgs)
	if err != nil {
		return SyncPayload{}, err
	}
	im := sess.InputMode
	if im == "" {
		im = "agent"
	}
	return SyncPayload{
		UIState:         uiState,
		Messages:        raw,
		InputMode:       im,
		ShellType:       shellTypeString(),
		ShellPendingCmd: peekShellPending(sess.ID),
		TelegramScope:   scope,
	}, nil
}

// idleUIStatus is the resting UI state when no task is running (respects input_mode).
func idleUIStatus(database *db.DB, sessionID string) string {
	s, err := database.GetSession(sessionID)
	if err != nil {
		return StateIdle
	}
	if s.InputMode == "shell" {
		return StateShellIdle
	}
	return StateIdle
}

// loadQueueState 讀出佇列與暫停旗標；讀取失敗只記 log，回空佇列讓 UI 照常運作。
func loadQueueState(database *db.DB, sessionID string) ([]db.QueuedMessage, bool) {
	q, err := database.ListQueuedMessages(sessionID)
	if err != nil {
		slog.Info(fmt.Sprintf("[ws] ListQueuedMessages: %v", err))
		return nil, false
	}
	paused, _ := database.QueuePaused(sessionID)
	return q, paused
}

// RemoveQueuedMessage 與佇列啟動共用鎖，刪除後推播最新佇列給所有訂閱者。
// 刪除是冪等操作：訊息不在指定 session 的佇列時仍成功。
func RemoveQueuedMessage(database *db.DB, sessionID string, id int64) error {
	dl := dispatchLock(sessionID)
	dl.Lock()
	defer dl.Unlock()
	if err := database.DeleteQueuedMessage(sessionID, id); err != nil {
		return err
	}
	q, err := database.ListQueuedMessages(sessionID)
	if err != nil {
		return err
	}
	paused, err := database.QueuePaused(sessionID)
	if err != nil {
		return err
	}
	hub.Broadcast(sessionID, serverMsg{Type: "queue_update", Queue: q, QueuePaused: paused})
	return nil
}

// syncMsgRow 是 sync／telegram_scope 事件中的一則訊息（Telegram 欄位與 HTTP 分頁同一份 DTO）。
type syncMsgRow struct {
	ID          int64           `json:"id"`
	Role        string          `json:"role"`
	Content     string          `json:"content"`
	ResultText  string          `json:"result_text,omitempty"`
	Status      string          `json:"status"`
	CreatedAt   string          `json:"created_at,omitempty"`
	Author      string          `json:"author,omitempty"`
	Attachments []db.Attachment `json:"attachments,omitempty"`
	// Telegram 時間線欄位。
	Telegram        *db.TelegramRef `json:"telegram,omitempty"`
	Delivery        string          `json:"delivery,omitempty"`
	ClientMessageID string          `json:"client_message_id,omitempty"`
}

func marshalSyncRows(msgs []*db.Message) (json.RawMessage, error) {
	out := make([]syncMsgRow, 0, len(msgs))
	for _, m := range msgs {
		st := m.Status
		if st == "" {
			st = db.MessageStatusDone
		}
		out = append(out, syncMsgRow{
			ID: m.ID, Role: m.Role, Content: m.Content, ResultText: m.ResultText,
			Status: st, CreatedAt: m.CreatedAt, Author: m.Author, Attachments: m.Attachments,
			Telegram: m.Telegram, Delivery: m.Delivery, ClientMessageID: m.ClientMessageID,
		})
	}
	return json.Marshal(out)
}
