// wsConn 的佇列廣播與暫停。

package ws

import (
	"fmt"

	"log/slog"

	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/antigravity"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/claude"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/codex"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/cursor"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/kiro"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/kiroacp"
)

func (h *wsConn) broadcastQueue() {
	msg, err := queueUpdate(h.database, h.sessionID)
	if err != nil {
		slog.Info(fmt.Sprintf("[ws] queue update: %v", err))
		return
	}
	h.broadcast(msg)
}

func (h *wsConn) pauseQueue() {
	if err := h.database.PauseQueueIfPending(h.sessionID); err != nil {
		slog.Info(fmt.Sprintf("[ws] PauseQueueIfPending: %v", err))
	}
	h.broadcastQueue()
}
