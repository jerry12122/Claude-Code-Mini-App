package ws

import (
	"sync"
	"testing"
	"time"

	fiberws "github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

// ponytail: 這些帶全域runner/gate的WS fixtures循序執行；平行fixture需先把gate改為DB範圍。
type wsTestLifecycle struct {
	database *db.DB
	mu       sync.Mutex
	closing  bool
	conns    map[*fiberws.Conn]struct{}
	handlers sync.WaitGroup
}

func newWSTestLifecycle(database *db.DB) *wsTestLifecycle {
	return &wsTestLifecycle{database: database, conns: map[*fiberws.Conn]struct{}{}}
}

func (l *wsTestLifecycle) wrap(handler func(*fiberws.Conn)) func(*fiberws.Conn) {
	return func(c *fiberws.Conn) {
		l.mu.Lock()
		if l.closing {
			l.mu.Unlock()
			_ = c.Close()
			return
		}
		l.conns[c] = struct{}{}
		l.handlers.Add(1)
		l.mu.Unlock()
		defer func() { l.mu.Lock(); delete(l.conns, c); l.mu.Unlock(); l.handlers.Done() }()
		handler(c)
	}
}

func (l *wsTestLifecycle) close(t *testing.T, app *fiber.App, served <-chan struct{}) {
	t.Helper()
	l.mu.Lock()
	l.closing = true
	// 保持wrapper的defer不能返回，避免Fiber回收後才碰舊Conn指標。
	for c := range l.conns {
		_ = c.Close()
	}
	l.mu.Unlock()
	if err := app.Shutdown(); err != nil {
		t.Errorf("WS fixture shutdown: %v", err)
	}
	handlersDone := make(chan struct{})
	go func() { l.handlers.Wait(); close(handlersDone) }()
	select {
	case <-handlersDone:
	case <-time.After(5 * time.Second):
		t.Fatal("WS fixture handlers未返回")
	}
	select {
	case <-served:
	case <-time.After(5 * time.Second):
		t.Fatal("WS fixture listener未返回")
	}
	sessions, err := l.database.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sessions {
		// 只在所有斷言之後收尾：不允許成功回合再drain出下一輪。
		if err := l.database.SetQueuePaused(s.ID, true); err != nil {
			t.Fatal(err)
		}
		taskCancel(s.ID)
		shellTaskCancel(s.ID)
	}
	waitFor(t, "WS fixture完整工作defer", func() bool { gate.mu.Lock(); defer gate.mu.Unlock(); return gate.inflight == 0 })
	if stats := l.database.Stats(); stats.InUse != 0 {
		t.Fatalf("DB.Close前仍有借出連線: %+v", stats)
	}
	if err := l.database.Close(); err != nil {
		t.Errorf("WS fixture DB.Close: %v", err)
	}
	if stats := l.database.Stats(); stats.OpenConnections != 0 {
		t.Errorf("DB.Close後仍有開啟連線: %+v", stats)
	}
}
