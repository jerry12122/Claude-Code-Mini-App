package ws

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/fasthttp/websocket"
	fiberws "github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

func TestDispatcherRetryAcrossConnectionsRunsOnce(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/dispatch.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	s, err := database.CreateSession("retry", "", "", "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	starts, accepts, rejects := 0, 0, 0
	d := sessionDispatcher{
		database: database, sessionID: s.ID, actor: "owner",
		start: func(_ clientMsg, q *db.QueuedMessage) error {
			if _, err := database.PromoteQueuedMessage(s.ID, *q); err != nil {
				return err
			}
			starts++
			return nil
		},
		reply: func(m serverMsg) bool {
			if m.Type == "input_accepted" {
				accepts++
			}
			if m.Type == "input_rejected" {
				rejects++
			}
			return true
		},
		broadcastQueue: func() {}, broadcast: func(serverMsg) {},
	}
	input := clientMsg{Type: "input", RequestID: "same", Data: "once"}
	d.Dispatch(input)
	reconnected := d
	reconnected.Dispatch(input)
	input.Data = "different"
	reconnected.Dispatch(input)
	if starts != 1 || accepts != 2 || rejects != 1 {
		t.Fatalf("starts=%d accepts=%d rejects=%d", starts, accepts, rejects)
	}
}

func TestFullSocketQueueDoesNotBlockOtherSubscribers(t *testing.T) {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	results := make(chan error, 1)
	app.Get("/ws", fiberws.New(func(c *fiberws.Conn) {
		// A full queue models a writer stalled in a network write, without a
		// timing-dependent large-payload flood or a real five-second timeout.
		w := &socketWriter{conn: c, queue: make(chan socketPacket, 1), done: make(chan struct{}), exited: make(chan struct{})}
		close(w.exited)
		defer w.Close()
		if !w.Send(serverMsg{Type: "first"}) {
			results <- fmt.Errorf("initial send failed")
			return
		}
		b := &broadcaster{subs: make(map[string]map[uint64]func(serverMsg) bool)}
		b.Subscribe("s", w.Send)
		received := false
		b.Subscribe("s", func(serverMsg) bool { received = true; return true })
		b.Broadcast("s", serverMsg{Type: "next"})
		if !received || w.Send(serverMsg{Type: "after-close"}) {
			results <- fmt.Errorf("full/stopped writer blocked or accepted another message")
			return
		}
		results <- nil
	}))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = app.Listener(ln) }()
	defer app.Shutdown()
	conn, _, err := websocket.DefaultDialer.Dial("ws://"+ln.Addr().String()+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	select {
	case err := <-results:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("broadcast blocked behind a full socket queue")
	}
}
