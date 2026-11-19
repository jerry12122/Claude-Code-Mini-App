// wsConn：單一 WebSocket 連線的狀態與基本收發。

package ws

import (
	"sync"

	fiberws "github.com/gofiber/contrib/websocket"

	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/antigravity"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/claude"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/codex"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/cursor"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/kiro"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/kiroacp"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/quota"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/tg"
)

// wsConn 是單一 WebSocket 連線的狀態。原本是 NewHandler 裡的區域變數與閉包，
// 提出來後每個行為成為方法，依職責放在 conn_*.go。
type wsConn struct {
	database        *db.DB
	shellCfg        ShellOpts
	quotaSvc        *quota.Service
	notifyCfg       tg.NotifyConfig
	ho              handlerOptions
	c               *fiberws.Conn
	sessionID       string
	tgUserID        int64
	roomWake        bool
	mcpAgent        bool
	shareID         int64
	isGuest         bool
	guestNick       string
	author          string
	readOnly        bool
	sess            *db.Session
	mu              sync.Mutex
	agentType       string
	botToken        string
	agentSessionID  string
	isClaude        bool
	writer          *socketWriter
	shellTimeoutSec int
	drainIfIdle     func()
	resumeLaunched  bool
	dispatcher      *sessionDispatcher
	resumeQueue     func()
}

func (h *wsConn) send(msg serverMsg) bool {
	if h.isGuest && msg.Handoff != nil {
		handoff := *msg.Handoff
		handoff.SystemInstruction = ""
		msg.Handoff = &handoff
	}
	return h.writer.Send(msg)
}

func (h *wsConn) broadcast(msg serverMsg) {
	if msg.Type == "status" {
		msg = msg.withTaskTiming(h.sessionID)
	}
	hub.Broadcast(h.sessionID, msg)
}
