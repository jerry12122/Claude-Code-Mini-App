package ws

import (
	"encoding/json"
	"sync"
	"time"

	fiberws "github.com/gofiber/contrib/websocket"
)

// Each connection has one bounded writer. A slow reader reconnects and syncs
// from the database instead of blocking the agent or another subscriber.
type socketWriter struct {
	conn    *fiberws.Conn
	mu      sync.Mutex
	stopped bool
	queue   chan []byte
	done    chan struct{}
	exited  chan struct{}
}

func newSocketWriter(c *fiberws.Conn) *socketWriter {
	w := &socketWriter{conn: c, queue: make(chan []byte, 128), done: make(chan struct{}), exited: make(chan struct{})}
	go func() {
		defer close(w.exited)
		defer w.stop()
		for {
			select {
			case <-w.done:
				return
			case b := <-w.queue:
				select {
				case <-w.done:
					return
				default:
				}
				if err := c.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
					return
				}
				if err := c.WriteMessage(fiberws.TextMessage, b); err != nil {
					return
				}
			}
		}
	}()
	return w
}

func (w *socketWriter) stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.stopped {
		w.stopped = true
		close(w.done)
		_ = w.conn.Close()
	}
}

func (w *socketWriter) Send(msg serverMsg) bool {
	b, err := json.Marshal(msg)
	if err != nil {
		return false
	}
	w.mu.Lock()
	if w.stopped {
		w.mu.Unlock()
		return false
	}
	select {
	case w.queue <- b:
		w.mu.Unlock()
		return true
	default:
		w.mu.Unlock()
		w.stop()
		return false
	}
}

func (w *socketWriter) Close() {
	w.stop()
	// Fiber recycles the connection after the handler returns. No writer may
	// retain it beyond that point, including a broadcast copied before unsubscribe.
	<-w.exited
}
