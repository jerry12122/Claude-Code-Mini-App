package ws

import (
	"encoding/json"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

// computeWSState 依 DB 狀態與訊息列推算前端狀態字串
func computeWSState(sess *db.Session, msgs []*db.Message) string {
	if sess.PendingDenials != "" {
		return StateAwaitingConfirm
	}
	if sess.Status == db.SessionStatusAwaitingConfirm {
		return StateAwaitingConfirm
	}
	if sess.Status == db.SessionStatusRunning {
		for i := len(msgs) - 1; i >= 0; i-- {
			m := msgs[i]
			if m.Role == "claude" && m.Status == db.MessageStatusPending {
				if m.Content == "" {
					return StateThinking
				}
				return StateStreaming
			}
		}
		return StateThinking
	}
	return StateIdle
}

// buildSyncPayload 產生 sync 用的 UI 狀態與 messages JSON
func buildSyncPayload(database *db.DB, sessionID string) (uiState string, messagesJSON json.RawMessage, err error) {
	sess, err := database.GetSession(sessionID)
	if err != nil {
		return "", nil, err
	}
	msgs, err := database.ListMessages(sessionID)
	if err != nil {
		return "", nil, err
	}
	uiState = computeWSState(sess, msgs)
	type row struct {
		ID      int64  `json:"id"`
		Role    string `json:"role"`
		Content string `json:"content"`
		Status  string `json:"status"`
	}
	out := make([]row, 0, len(msgs))
	for _, m := range msgs {
		st := m.Status
		if st == "" {
			st = db.MessageStatusDone
		}
		out = append(out, row{ID: m.ID, Role: m.Role, Content: m.Content, Status: st})
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return "", nil, err
	}
	return uiState, raw, nil
}
