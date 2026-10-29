package mcp

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

func TestQueueTools(t *testing.T) {
	database := setupTestDB(t)
	create := func(name string) string {
		t.Helper()
		s, err := database.CreateSession(name, "", t.TempDir(), "default", "claude", nil, "agent")
		if err != nil {
			t.Fatal(err)
		}
		return s.ID
	}
	sid, other := create("target"), create("other")
	if err := database.AddAttachment(&db.Attachment{ID: "file", SessionID: sid, Name: "note.txt", StorageName: "private.txt", MimeType: "text/plain", Size: 1}); err != nil {
		t.Fatal(err)
	}
	enqueue := func(id, text string, files ...string) *db.QueuedMessage {
		t.Helper()
		q, err := database.EnqueueMessage(id, text, "Alice", files...)
		if err != nil {
			t.Fatal(err)
		}
		return q
	}
	first, second, foreign := enqueue(sid, "first", "file"), enqueue(sid, "second"), enqueue(other, "foreign")
	if err := database.SetQueuePaused(sid, true); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(NewHTTPHandler(database, nil, nil, 0))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	client := gomcp.NewClient(&gomcp.Implementation{Name: "queue-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &gomcp.StreamableClientTransport{Endpoint: srv.URL, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, tool := range listed.Tools {
		found[tool.Name] = true
	}
	if !found["get_queue"] || !found["delete_queue_message"] {
		t.Fatal("queue tools must be discoverable")
	}
	call := func(name string, args map[string]any, out any, wantError bool) {
		t.Helper()
		res, err := session.CallTool(ctx, &gomcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		if res.IsError != wantError {
			t.Fatalf("%s(%v): IsError=%v, content=%v", name, args, res.IsError, res.Content)
		}
		if out != nil {
			if err := json.Unmarshal([]byte(res.Content[0].(*gomcp.TextContent).Text), out); err != nil {
				t.Fatal(err)
			}
		}
	}
	var out getQueueOut
	call("get_queue", map[string]any{"session_id": " " + sid + " "}, &out, false)
	if !out.QueuePaused || len(out.Queue) != 2 || out.Queue[0].ID != first.ID || out.Queue[1].ID != second.ID {
		t.Fatalf("queue order or pause state lost: %+v", out)
	}
	if out.Queue[0].Content != "first" || out.Queue[0].Author != "Alice" || out.Queue[0].CreatedAt == "" || len(out.Queue[0].Attachments) != 1 || out.Queue[0].Attachments[0].Name != "note.txt" {
		t.Fatalf("queue metadata lost: %+v", out.Queue[0])
	}
	var deleted okOut
	for _, id := range []int64{foreign.ID, first.ID, first.ID, second.ID} {
		call("delete_queue_message", map[string]any{"session_id": sid, "message_id": id}, &deleted, false)
		if !deleted.OK {
			t.Fatal("delete must acknowledge success")
		}
	}
	call("get_queue", map[string]any{"session_id": sid}, &out, false)
	if out.Queue == nil || len(out.Queue) != 0 || !out.QueuePaused {
		t.Fatalf("empty queue must be [] and remain paused: %+v", out)
	}
	call("get_queue", map[string]any{"session_id": other}, &out, false)
	if len(out.Queue) != 1 || out.Queue[0].ID != foreign.ID || out.QueuePaused {
		t.Fatalf("other session was affected: %+v", out)
	}
	for _, id := range []string{"", " ", "missing"} {
		call("get_queue", map[string]any{"session_id": id}, nil, true)
		call("delete_queue_message", map[string]any{"session_id": id, "message_id": foreign.ID}, nil, true)
	}
	for _, id := range []int64{0, -1} {
		call("delete_queue_message", map[string]any{"session_id": other, "message_id": id}, nil, true)
	}
	call("delete_queue_message", map[string]any{"session_id": other}, nil, true)
}
