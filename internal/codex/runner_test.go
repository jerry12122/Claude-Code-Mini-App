package codex

import (
	"errors"
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
		if e.Type == agent.EventActivity && e.Text == "執行指令" {
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
	for _, want := range []string{"exec", "--json", "--skip-git-repo-check", "--dangerously-bypass-approvals-and-sandbox", "-C", "/tmp/wd", "hello"} {
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
	if !strings.Contains(strings.Join(args, " "), "--dangerously-bypass-approvals-and-sandbox") {
		t.Fatalf("resume missing bypass flag: %v", args)
	}
}

// runLines 模擬 Run：逐行 dispatch 後呼叫 finish。
func runLines(t *testing.T, jsonl string, waitErr error, stderr string) (*dispatchState, []agent.Event) {
	t.Helper()
	var events []agent.Event
	cb := func(e agent.Event) { events = append(events, e) }
	r := &Runner{}
	st := &dispatchState{}
	for _, line := range strings.Split(strings.TrimSpace(jsonl), "\n") {
		ev, err := ParseEvent([]byte(line))
		if err != nil {
			t.Fatal(err)
		}
		r.dispatch(ev, cb, st)
	}
	st.finish(cb, waitErr, stderr)
	return st, events
}

// codex-cli 0.159.2 實測：無效 model 時的事件序列（exit 1）。
const invalidModelJSONL = `{"type":"thread.started","thread_id":"t1"}
{"type":"item.completed","item":{"id":"item_0","type":"error","message":"Model metadata for ` + "`no-such-model-xyz`" + ` not found."}}
{"type":"turn.started"}
{"type":"error","message":"{\"type\":\"error\",\"status\":400,\"error\":{\"type\":\"invalid_request_error\",\"message\":\"The 'no-such-model-xyz' model is not supported when using Codex with a ChatGPT account.\"}}"}
{"type":"turn.failed","error":{"message":"{\"type\":\"error\",\"status\":400,\"error\":{\"type\":\"invalid_request_error\",\"message\":\"The 'no-such-model-xyz' model is not supported when using Codex with a ChatGPT account.\"}}"}}`

func TestInvalidModelSingleReadableError(t *testing.T) {
	_, events := runLines(t, invalidModelJSONL, errors.New("exit status 1"), stdinNotice+"\n")
	var errs []string
	for _, e := range events {
		if e.Type == agent.EventError {
			errs = append(errs, e.Err.Error())
		}
		if e.Type == agent.EventDelta {
			t.Fatalf("warning item must not become delta: %q", e.Text)
		}
	}
	want := "The 'no-such-model-xyz' model is not supported when using Codex with a ChatGPT account."
	if len(errs) != 1 || errs[0] != want {
		t.Fatalf("errors=%q", errs)
	}
}

func TestTransientErrorThenCompletedIsNotFailure(t *testing.T) {
	jsonl := `{"type":"thread.started","thread_id":"t1"}
{"type":"error","message":"Reconnecting... 1/5"}
{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"OK"}}
{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}`
	_, events := runLines(t, jsonl, nil, "")
	for _, e := range events {
		if e.Type == agent.EventError {
			t.Fatalf("unexpected error: %v", e.Err)
		}
	}
}

func TestStderrFallbackStripsStdinNotice(t *testing.T) {
	_, events := runLines(t, `{"type":"thread.started","thread_id":"t1"}`, errors.New("exit status 2"), stdinNotice+"\nboom\n")
	if len(events) != 2 || events[1].Type != agent.EventError || events[1].Err.Error() != "codex failed: boom" {
		t.Fatalf("events=%+v", events)
	}
}

// codex-cli 0.159.2 實測：工具呼叫前後各一段 agent_message。
func TestMultipleAgentMessagesSeparated(t *testing.T) {
	jsonl := `{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"I’ll run the command."}}
{"type":"item.started","item":{"id":"item_1","type":"command_execution","status":"in_progress"}}
{"type":"item.completed","item":{"id":"item_1","type":"command_execution","status":"completed"}}
{"type":"item.completed","item":{"id":"item_2","type":"agent_message","text":"probe123"}}
{"type":"turn.completed"}`
	_, events := runLines(t, jsonl, nil, "")
	var text strings.Builder
	for _, e := range events {
		if e.Type == agent.EventDelta {
			text.WriteString(e.Text)
		}
	}
	if text.String() != "I’ll run the command.\n\nprobe123" {
		t.Fatalf("text=%q", text.String())
	}
}

func TestBuildArgsModelAndEffort(t *testing.T) {
	args := buildArgs(agent.RunOptions{
		Prompt:  "hello",
		WorkDir: "/tmp/wd",
		ExtraArgs: map[string]string{
			agent.ArgModel:  "gpt-5.5",
			agent.ArgEffort: "high",
		},
	})
	joined := strings.Join(args, " ")
	for _, want := range []string{"-m gpt-5.5", "-c model_reasoning_effort=high"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %q", want, joined)
		}
	}
}

func TestBuildArgsResumeModelAndEffort(t *testing.T) {
	args := buildArgs(agent.RunOptions{
		Prompt:    "continue",
		SessionID: "thread-uuid",
		ExtraArgs: map[string]string{
			agent.ArgModel:  "gpt-5.5",
			agent.ArgEffort: "high",
		},
	})
	joined := strings.Join(args, " ")
	for _, want := range []string{"-m gpt-5.5", "-c model_reasoning_effort=high"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %q", want, joined)
		}
	}
}

func TestActivityLabel(t *testing.T) {
	if ActivityLabel("command_execution") != "執行指令" {
		t.Fatal("command_execution label")
	}
	if ActivityLabel("reasoning") != "" {
		t.Fatal("reasoning should be ignored")
	}
}

func TestDispatchActivityDetails(t *testing.T) {
	for _, tc := range []struct {
		line string
		kind agent.EventType
		text string
	}{
		{`{"type":"item.started","item":{"type":"command_execution","command":"go test ./..."}}`, agent.EventActivity, "執行指令：go test ./..."},
		{`{"type":"item.updated","item":{"type":"web_search","query":"Go docs"}}`, agent.EventActivity, "搜尋：Go docs"},
		{`{"type":"item.completed","item":{"type":"file_change","changes":[{"path":"a.go"},{"path":"b.go"}],"status":"completed"}}`, agent.EventActivity, "已完成：修改檔案：a.go, b.go"},
		{`{"type":"item.started","item":{"type":"mcp_tool_call","server":"docs","tool":"search","arguments":{"query":"Go docs"}}}`, agent.EventActivity, "docs/search：Go docs"},
		{`{"type":"item.completed","item":{"type":"command_execution","command":"exit 1","status":"completed","exit_code":1}}`, agent.EventActivity, "執行失敗：執行指令：exit 1"},
		{`{"type":"item.started","item":{"type":"reasoning"}}`, agent.EventThinking, "思考中…"},
		{`{"type":"item.completed","item":{"type":"reasoning","text":"檢查現有測試"}}`, agent.EventThinking, "檢查現有測試"},
		{`{"type":"item.updated","item":{"type":"todo_list","items":[{"text":"讀取檔案","completed":true},{"text":"執行測試","completed":false}]}}`, agent.EventActivity, "進行中：執行測試"},
	} {
		e, err := ParseEvent([]byte(tc.line))
		if err != nil {
			t.Fatal(err)
		}
		var events []agent.Event
		(&Runner{}).dispatch(e, func(e agent.Event) { events = append(events, e) }, &dispatchState{})
		if len(events) != 1 || events[0].Type != tc.kind || events[0].Text != tc.text {
			t.Errorf("%s: got %+v, want %s %q", tc.line, events, tc.kind, tc.text)
		}
	}
}
