package ws

import (
	"encoding/json"
	"strings"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

// SyncPayload is sent to the client on WebSocket connect (type "sync").
type SyncPayload struct {
	UIState         string
	Messages        json.RawMessage
	InputMode       string
	ShellType       string
	ShellPendingCmd *shellPendingInfo
}

func computeWSState(sess *db.Session, msgs []*db.Message) string {
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
	msgs, err := database.ListMessages(sessionID)
	if err != nil {
		return SyncPayload{}, err
	}
	uiState := computeWSState(sess, msgs)
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
