package codex

import (
	"strings"
	"testing"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
)

const sampleJSONL = `{"type":"thread.started","thread_id":"019f35d2-322c-7342-a6e0-1b26ce4904ed"}
{"type":"turn.started"}
{"type":"item.started","item":{"id":"item_0","type":"command_execution","status":"in_progress"}}
{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"POC_OK"}}
{"type":"turn.completed","usage":{"input_tokens":8270,"cached_input_tokens":0,"output_tokens":2}}`

func TestParseEventThreadStarted(t *testing.T) {
	ev, err := ParseEvent([]byte(`{"type":"thread.started","thread_id":"abc-123"}`))
	if err != nil {
		t.Fatal(err)
	}
	if ev.ThreadID != "abc-123" {
		t.Fatalf("thread_id=%q", ev.ThreadID)
	}
}

func TestDispatchEvents(t *testing.T) {
	var events []agent.Event
	cb := func(e agent.Event) { events = append(events, e) }

	r := &Runner{}
	st := &dispatchState{}
	for _, line := range strings.Split(strings.TrimSpace(sampleJSONL), "\n") {
		ev, err := ParseEvent([]byte(line))
		if err != nil {
			t.Fatal(err)
		}
		r.dispatch(ev, cb, st)
	}

	if len(events) < 3 {
		t.Fatalf("expected at least 3 events, got %d", len(events))
	}
	if events[0].Type != agent.EventSessionInit || events[0].SessionID == "" {
		t.Fatalf("first event: %+v", events[0])
	}
	foundActivity := false
	foundDelta := false
	for _, e := range events {
		if e.Type == agent.EventActivity && e.Text == "執行指令中…" {
			foundActivity = true
		}
		if e.Type == agent.EventDelta && e.Text == "POC_OK" {
			foundDelta = true
		}
	}
	if !foundActivity {
		t.Fatal("missing EventActivity")
	}
	if !foundDelta {
		t.Fatal("missing EventDelta")
	}
	if !st.sawTurnCompleted {
		t.Fatal("expected sawTurnCompleted")
	}
}

func TestBuildArgsFirstTurn(t *testing.T) {
	args := buildArgs(agent.RunOptions{
		Prompt:  "hello",
		WorkDir: "/tmp/wd",
	})
	joined := strings.Join(args, " ")
	for _, want := range []string{"exec", "--json", "--skip-git-repo-check", "-C", "/tmp/wd", "-s", "workspace-write", "hello"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %q", want, joined)
		}
	}
}

func TestBuildArgsResume(t *testing.T) {
	args := buildArgs(agent.RunOptions{
		Prompt:    "continue",
		SessionID: "thread-uuid",
		WorkDir:   "/tmp/wd",
	})
	if args[0] != "exec" || args[1] != "resume" || args[2] != "thread-uuid" {
		t.Fatalf("resume args: %v", args)
	}
	if strings.Contains(strings.Join(args, " "), "-C") {
		t.Fatal("resume should not include -C")
	}
}

func TestActivityLabel(t *testing.T) {
	if ActivityLabel("command_execution") != "執行指令中…" {
		t.Fatal("command_execution label")
	}
	if ActivityLabel("reasoning") != "" {
		t.Fatal("reasoning should be ignored")
	}
}
