package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	fiberws "github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/tg"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/ws"
)

// 使用真實 MCP／WS／SQLite，只以既有 runner 替身模擬額度失敗。
func TestQueueResumeEndToEndAfterRestart(t *testing.T) {
	path := t.TempDir() + "/resume.db"
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	start := func() (*db.DB, func(string, map[string]any) *gomcp.CallToolResult, func()) {
		t.Helper()
		database, err := db.Open(path)
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
		reg := NewRegistry(func(id string) string {
			return fmt.Sprintf("ws://%s/sessions/%s/ws", ln.Addr(), id)
		}, nil)
		srv := httptest.NewServer(NewHTTPHandler(database, nil, reg, 0))
		client := gomcp.NewClient(&gomcp.Implementation{Name: "queue-e2e", Version: "1"}, nil)
		session, err := client.Connect(ctx, &gomcp.StreamableClientTransport{Endpoint: srv.URL, DisableStandaloneSSE: true}, nil)
		if err != nil {
			t.Fatal(err)
		}
		call := func(name string, args map[string]any) *gomcp.CallToolResult {
			t.Helper()
			res, err := session.CallTool(ctx, &gomcp.CallToolParams{Name: name, Arguments: args})
			if err != nil {
				t.Fatal(err)
			}
			if res.IsError {
				t.Fatalf("%s: %v", name, res.Content)
			}
			return res
		}
		stop := func() {
			_ = session.Close()
			srv.Close()
			reg.mu.Lock()
			for _, st := range reg.sessions {
				_ = st.conn.Close()
			}
			reg.mu.Unlock()
			_ = app.Shutdown()
			_ = database.Close()
		}
		return database, call, stop
	}
	database, call, stop := start()
	defer func() { stop() }()
	s, err := database.CreateSession("cloudserver", "", t.TempDir(), "default", "fakemcp", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	call("send_message", map[string]any{"session_id": s.ID, "text": "block Q:quota"})
	waitUntil(t, "quota run started", func() bool {
		got, _ := database.GetSession(s.ID)
		return got != nil && got.Status == db.SessionStatusRunning
	})
	for _, text := range []string{"Q:244", "Q:248"} {
		call("send_message", map[string]any{"session_id": s.ID, "text": text})
	}
	waitUntil(t, "two queued messages", func() bool { n, _ := database.CountQueuedMessages(s.ID); return n == 2 })
	e2eGate <- struct{}{}
	waitUntil(t, "failure persisted and queue paused", func() bool {
		got, _ := database.GetSession(s.ID)
		paused, _ := database.QueuePaused(s.ID)
		return got != nil && got.Status == db.SessionStatusIdle && got.LastErrorText != "" && paused
	})
	assertFailure := func() string {
		t.Helper()
		res := call("list_sessions", nil)
		raw, _ := json.Marshal(res.StructuredContent)
		var out listSessionsOut
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		if len(out.Sessions) != 1 {
			t.Fatalf("sessions=%s", raw)
		}
		got := out.Sessions[0]
		if got.Name != "cloudserver" || got.LastErrorText != "You've hit your usage limit, try again at 4:42 PM" || got.LastErrorAt == "" || !got.QueuePaused || got.QueueLength != 2 {
			t.Fatalf("失敗與佇列狀態不符: %s", raw)
		}
		return got.LastErrorAt
	}
	before := assertFailure()
	stop()
	stop = func() {}
	database, call, stop = start()
	if after := assertFailure(); after != before {
		t.Fatalf("重啟改變失敗時間: %s -> %s", before, after)
	}
	call("resume_queue", map[string]any{"session_id": s.ID})
	waitUntil(t, "queue completed in order and failure cleared", func() bool {
		got, _ := database.GetSession(s.ID)
		n, _ := database.CountQueuedMessages(s.ID)
		msgs, _ := database.ListMessages(s.ID)
		var replies []string
		for _, m := range msgs {
			if m.Role == "claude" && m.Status == db.MessageStatusDone && m.Content != "" {
				replies = append(replies, m.Content)
			}
		}
		return got != nil && got.LastErrorText == "" && got.LastErrorAt == "" && n == 0 && strings.Join(replies, "|") == "reply-to:Q:244|reply-to:Q:248"
	})
}
