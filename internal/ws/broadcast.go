package ws

import (
	"sync"

	fiberws "github.com/gofiber/contrib/websocket"
)

// broadcaster：同一 session 可有多條 WS 同時訂閱，任務事件廣播給全部訂閱者
type broadcaster struct {
	mu     sync.Mutex
	nextID uint64
	subs   map[string]map[uint64]func(serverMsg) bool
}

var hub = &broadcaster{subs: make(map[string]map[uint64]func(serverMsg) bool)}

// Subscribe 註冊廣播；回傳的 unsub 必須在連線關閉時呼叫
func (b *broadcaster) Subscribe(sessionID string, send func(serverMsg) bool) (unsub func()) {
	b.mu.Lock()
	b.nextID++
	id := b.nextID
	if b.subs[sessionID] == nil {
		b.subs[sessionID] = make(map[uint64]func(serverMsg) bool)
	}
	b.subs[sessionID][id] = send
	b.mu.Unlock()
	return func() {
		b.mu.Lock()
		if m, ok := b.subs[sessionID]; ok {
			delete(m, id)
			if len(m) == 0 {
				delete(b.subs, sessionID)
			}
		}
		b.mu.Unlock()
	}
}

// has 回報該 session 目前是否有任何訂閱連線（沒有人在看就不必組裝事件）。
func (b *broadcaster) has(sessionID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs[sessionID]) > 0
}

// Broadcast 將訊息送給該 session 所有訂閱連線
func (b *broadcaster) Broadcast(sessionID string, msg serverMsg) {
	b.mu.Lock()
	m := b.subs[sessionID]
	sends := make([]func(serverMsg) bool, 0, len(m))
	for _, send := range m {
		sends = append(sends, send)
	}
	b.mu.Unlock()
	for _, send := range sends {
		send(msg)
	}
}

// eventsKey：全域事件頻道（非特定 session），供側欄即時更新列表。
const eventsKey = "*"

// NotifySessionsChanged 通知所有 /events 訂閱者重新抓 session 列表。
func NotifySessionsChanged() {
	hub.Broadcast(eventsKey, serverMsg{Type: "sessions_changed"})
}

// BroadcastSessionMessage 向指定 session 的所有 WS 訂閱者推播一則新訊息。
func BroadcastSessionMessage(sessionID, msgType, content, role, author string, msgID int64, createdAt string) {
	hub.Broadcast(sessionID, serverMsg{
		Type:      msgType,
		Role:      role,
		Content:   content,
		ID:        msgID,
		Author:    author,
		CreatedAt: createdAt,
	})
}

// NewEventsHandler：只推不收的全域事件 WS；讀迴圈僅用來偵測斷線。
func NewEventsHandler() func(*fiberws.Conn) {
	return func(c *fiberws.Conn) {
		writer := newSocketWriter(c)
		defer writer.Close()
		unsub := hub.Subscribe(eventsKey, writer.Send)
		defer unsub()
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}
}
