package mcp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	fiberws "github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/tg"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/ws"
)

// e2eGate：prompt 含 "block" 時 runner 卡住直到收到訊號。
var e2eGate = make(chan struct{})

type e2eRunner struct{}

func (e2eRunner) Name() string { return "fakemcp" }

func (e2eRunner) Run(ctx context.Context, opts agent.RunOptions, cb agent.EventCallback) error {
	if strings.Contains(opts.Prompt, "block") {
		select {
		case <-e2eGate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if strings.Contains(opts.Prompt, "Q:quota") {
		return errors.New("You've hit your usage limit, try again at 4:42 PM")
	}
	cb(agent.Event{Type: agent.EventDelta, Text: "reply-to:" + question(opts.Prompt)})
	cb(agent.Event{Type: agent.EventDone})
	return nil
}

// question 從（可能包了 envelope 的）prompt 中取出 "Q:..." 那段。
func question(s string) string {
	if i := strings.Index(s, "Q:"); i >= 0 {
		return strings.Fields(s[i:])[0]
	}
	return s
}

func init() { agent.Register("fakemcp", func() agent.Runner { return e2eRunner{} }) }

// 走真正的 ws.NewHandler：驗證 ask_session 拿到全文、忙碌時拒絕、send_message 對忙碌目標排隊而非打斷。
func TestAskSessionEndToEnd(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/e2e.db")
	if err != nil {
		t.Fatal(err)
	}
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Get("/sessions/:id/ws", fiberws.New(ws.NewHandler(database, "", ws.ShellOpts{}, nil, tg.NotifyConfig{})))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = app.Listener(ln) }()
	t.Cleanup(func() { _ = app.Shutdown(); database.Close() })
	port := ln.Addr().(*net.TCPAddr).Port

	d := &deps{db: database, maxHops: DefaultMaxHops}
	d.reg = NewRegistry(func(id string) string { return fmt.Sprintf("ws://127.0.0.1:%d/sessions/%s/ws", port, id) }, http.Header{})

	asker, _ := database.CreateSession("asker", "", t.TempDir(), "default", "fakemcp", nil, "agent")
	target, _ := database.CreateSession("target", "", t.TempDir(), "default", "fakemcp", nil, "agent")
	ctx := context.Background()

	_, out, err := d.askSession(ctx, nil, askSessionIn{SessionID: target.ID, FromSessionID: asker.ID, Text: "Q:1+1?", TimeoutSec: 5})
	if err != nil {
		t.Fatal(err)
	}
	if out.TimedOut || out.State != StateIdle || out.Text != "reply-to:Q:1+1?" {
		t.Fatalf("ask 應拿到全文: %+v", out)
	}

	// 目標執行中：ask 拒絕；send_message 排隊、不打斷。
	if _, _, err := d.sendMessage(ctx, nil, sendMessageIn{SessionID: target.ID, Text: "block Q:long"}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "target running", func() bool {
		s, _ := database.GetSession(target.ID)
		return s.Status == db.SessionStatusRunning
	})
	if _, _, err := d.askSession(ctx, nil, askSessionIn{SessionID: target.ID, Text: "Q:x", TimeoutSec: 1}); err == nil {
		t.Fatal("目標忙碌時 ask 應拒絕")
	}
	if _, _, err := d.sendMessage(ctx, nil, sendMessageIn{SessionID: target.ID, Text: "Q:queued"}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "queued", func() bool { q, _ := database.ListQueuedMessages(target.ID); return len(q) == 1 })
	e2eGate <- struct{}{}
	waitUntil(t, "long finished then queued ran", func() bool {
		msgs, _ := database.ListMessages(target.ID)
		var replies []string
		for _, m := range msgs {
			if m.Role == "claude" && m.Status == db.MessageStatusDone {
				replies = append(replies, m.Content)
			}
		}
		return strings.Join(replies, "|") == "reply-to:Q:1+1?|reply-to:Q:long|reply-to:Q:queued"
	})
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", what)
}
