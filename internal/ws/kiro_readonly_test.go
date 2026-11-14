package ws

import (
	"strings"
	"testing"

	"github.com/fasthttp/websocket"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
)

func TestLegacyKiroReadOnly(t *testing.T) {
	database, wsURL := startTestServer(t)
	s, err := database.CreateSession("legacy", "", t.TempDir(), "default", agent.TypeKiro, nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AddMessage(s.ID, "user", "preserved history", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := database.EnqueueMessage(s.ID, "preserved queue", ""); err != nil {
		t.Fatal(err)
	}
	if err := database.SetQueuePaused(s.ID, true); err != nil {
		t.Fatal(err)
	}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(s.ID), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if sync := readQuestionEvent(t, conn, "sync"); !strings.Contains(string(sync.Messages), "preserved history") || len(sync.Queue) != 1 {
		t.Fatalf("history/queue missing: %+v", sync)
	}
	for _, kind := range []string{"input", "queue_resume", "queue_remove", "set_input_mode", "shell_run", "shell_exec", "shell_approve", "shell_allow_once", "shell_allow_remember_workdir", "shell_deny", "shell_cancel", "question_answer", "allow_once", "deny_once", "set_mode", "set_model", "set_effort", "reset_context", "interrupt", "refresh_quota"} {
		if err := conn.WriteJSON(clientMsg{Type: kind, Data: "blocked", Mode: "shell", Model: "changed", Effort: "high", ID: 1, RequestID: "rejected-input"}); err != nil {
			t.Fatal(err)
		}
		want := "error"
		if kind == "input" {
			want = "input_rejected"
		}
		event := readQuestionEvent(t, conn, want)
		if event.Code != "agent_disabled" || event.Content != agent.DisabledReason(agent.TypeKiro) || event.RequestID != "rejected-input" {
			t.Fatalf("%s: %+v", kind, event)
		}
	}
	if err := conn.WriteJSON(clientMsg{Type: "ping"}); err != nil {
		t.Fatal(err)
	}
	readQuestionEvent(t, conn, "pong")
	msgs, err := database.ListMessages(s.ID)
	if err != nil || len(msgs) != 1 || msgs[0].Content != "preserved history" {
		t.Fatalf("history changed: %+v, %v", msgs, err)
	}
	paused, err := database.QueuePaused(s.ID)
	if err != nil || !paused || queueLen(database, s.ID) != 1 {
		t.Fatalf("queue changed: paused=%v err=%v", paused, err)
	}
	current, err := database.GetSession(s.ID)
	if err != nil || current.InputMode != "agent" || current.AgentSessionID != "" || current.Model != "" || current.Effort != "" || current.PermissionMode != "default" {
		t.Fatalf("session changed: %+v, %v", current, err)
	}
}
