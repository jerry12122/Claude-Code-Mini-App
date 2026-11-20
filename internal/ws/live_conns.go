package ws

import "sync"

// liveConns 記住同一 session 上還開著的連線，讓重置能清掉每一條的續接記憶。
var liveConns = struct {
	mu sync.Mutex
	m  map[string]map[*wsConn]struct{}
}{m: map[string]map[*wsConn]struct{}{}}

func trackLive(h *wsConn) func() {
	liveConns.mu.Lock()
	if liveConns.m[h.sessionID] == nil {
		liveConns.m[h.sessionID] = map[*wsConn]struct{}{}
	}
	liveConns.m[h.sessionID][h] = struct{}{}
	liveConns.mu.Unlock()
	return func() {
		liveConns.mu.Lock()
		delete(liveConns.m[h.sessionID], h)
		if len(liveConns.m[h.sessionID]) == 0 {
			delete(liveConns.m, h.sessionID)
		}
		liveConns.mu.Unlock()
	}
}

func clearLiveAgentSession(sessionID string) {
	liveConns.mu.Lock()
	conns := make([]*wsConn, 0, len(liveConns.m[sessionID]))
	for conn := range liveConns.m[sessionID] {
		conns = append(conns, conn)
	}
	liveConns.mu.Unlock()
	for _, conn := range conns {
		conn.mu.Lock()
		conn.agentSessionID = ""
		conn.mu.Unlock()
	}
}
