package ws

import "sync"

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
