package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fasthttp/websocket"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// recordingSessionWS 模擬 /sessions/:id/ws，把收到的 client 訊息 type 依序送進 frames。
// input "boom" 會推一個 error 事件再回 IDLE（模擬額度用盡的失敗 run）。
func recordingSessionWS(t *testing.T) (*httptest.Server, <-chan string) {
	t.Helper()
	frames := make(chan string, 16)
	up := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		for {
			var in map[string]string
			if err := c.ReadJSON(&in); err != nil {
				return
			}
			frames <- in["type"]
			if in["type"] == "input" && in["data"] == "boom" {
				_ = c.WriteJSON(map[string]any{"type": "status", "value": "THINKING"})
				_ = c.WriteJSON(map[string]any{"type": "error", "content": "usage limit reached"})
				_ = c.WriteJSON(map[string]any{"type": "status", "value": "IDLE"})
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv, frames
}

func expectFrame(t *testing.T, frames <-chan string, want string) {
	t.Helper()
	select {
	case got := <-frames:
		if got != want {
			t.Fatalf("收到 %q，預期 %q", got, want)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("逾時未收到 %q", want)
	}
}

// 回歸：ResumeQueue 不能走 begin()，否則會清掉快取的錯誤並把狀態誤設為 running（佇列為空時不會有 run 結束它）。
func TestRegistryResumeQueue_DoesNotAlterTurnState(t *testing.T) {
	srv, frames := recordingSessionWS(t)
	reg := NewRegistry(func(string) string { return "ws" + strings.TrimPrefix(srv.URL, "http") }, http.Header{})

	res, err := reg.Ask(context.Background(), "s1", "boom", 3*time.Second)
	if err != nil || res.TimedOut || res.Error != "usage limit reached" {
		t.Fatalf("前置：失敗 run 應帶出錯誤: %+v err=%v", res, err)
	}
	expectFrame(t, frames, "input")

	if err := reg.ResumeQueue("s1"); err != nil {
		t.Fatal(err)
	}
	expectFrame(t, frames, "queue_resume")

	state, _, _, _, lastErr, connected, _ := reg.Status("s1")
	if state != StateIdle {
		t.Fatalf("resume 後狀態應仍為 idle，實際 %q", state)
	}
	if lastErr != "usage limit reached" {
		t.Fatalf("resume 不應清掉快取的錯誤，實際 %q", lastErr)
	}
	if !connected {
		t.Fatal("應已連線")
	}
}

func TestResumeQueueTool(t *testing.T) {
	database := setupTestDB(t)
	s, err := database.CreateSession("target", "", t.TempDir(), "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	srv, frames := recordingSessionWS(t)
	reg := NewRegistry(func(string) string { return "ws" + strings.TrimPrefix(srv.URL, "http") }, http.Header{})

	mcpSrv := httptest.NewServer(NewHTTPHandler(database, nil, reg, 0))
	defer mcpSrv.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	client := gomcp.NewClient(&gomcp.Implementation{Name: "resume-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &gomcp.StreamableClientTransport{Endpoint: mcpSrv.URL, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	call := func(id string) *gomcp.CallToolResult {
		t.Helper()
		res, err := session.CallTool(ctx, &gomcp.CallToolParams{Name: "resume_queue", Arguments: map[string]any{"session_id": id}})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}

	if res := call(s.ID); res.IsError {
		t.Fatalf("存在的 session 應成功: %v", res.Content)
	}
	expectFrame(t, frames, "queue_resume")

	for _, bad := range []string{"", "   ", "no-such-session"} {
		if res := call(bad); !res.IsError {
			t.Fatalf("session_id=%q 應回錯誤", bad)
		}
	}
	select {
	case got := <-frames:
		t.Fatalf("無效 session 不應送出任何 WS 訊息，收到 %q", got)
	case <-time.After(200 * time.Millisecond):
	}
}
