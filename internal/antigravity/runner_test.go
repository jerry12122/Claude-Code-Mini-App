package antigravity

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
)

// 測試執行檔扮演 agy，不需要安裝或登入真實 CLI。
func TestMain(m *testing.M) {
	if mode := os.Getenv("FAKE_AGY_ACP"); mode != "" {
		fakeACPProcess(mode)
		os.Exit(0)
	}
	if mode := os.Getenv("FAKE_AGY_PRINT"); mode != "" {
		fmt.Fprint(os.Stderr, "fake diagnostic")
		switch mode {
		case "models":
			if len(os.Args) != 2 || os.Args[1] != "models" {
				os.Exit(2)
			}
			fmt.Fprint(os.Stdout, os.Getenv("FAKE_AGY_MODELS"))
		case "text":
			fmt.Fprint(os.Stdout, "hello\n")
		case "stream":
			fmt.Fprintln(os.Stdout, `{"event":"result","result":{"status":"SUCCESS","response":"hello"}}`)
		case "whitespace":
			fmt.Fprint(os.Stdout, " \n\t")
		case "failure":
			os.Exit(1)
		case "cancel":
			time.Sleep(time.Minute)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestPrintTextTerminalEvents(t *testing.T) {
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"empty", "whitespace", "text", "failure", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("CC_AGY_BIN", bin)
			t.Setenv("CC_AGY_FORCE_PRINT_TEXT", "1")
			t.Setenv("FAKE_AGY_PRINT", mode)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			opts := agent.RunOptions{Prompt: "hello", SessionID: "print-session"}
			if mode == "cancel" {
				opts.OnStart = func(int) { cancel() }
			}
			var done, failed int
			err := (&Runner{}).Run(ctx, opts, func(e agent.Event) {
				switch e.Type {
				case agent.EventDone:
					done++
					if e.ResultText != "hello" || e.SessionID != opts.SessionID {
						t.Errorf("unexpected result: %+v", e)
					}
				case agent.EventError:
					failed++
					if e.Err == nil || !strings.Contains(e.Err.Error(), "fake diagnostic") || e.SessionID != opts.SessionID {
						t.Errorf("missing diagnostic/session: %+v", e)
					}
					if (mode == "empty" || mode == "whitespace") && !strings.Contains(e.Err.Error(), "沒有回傳內容") {
						t.Errorf("missing empty-output explanation: %v", e.Err)
					}
				}
			})
			wantDone, wantFailed := 0, 1
			if mode == "text" {
				wantDone, wantFailed = 1, 0
			} else if mode == "cancel" {
				wantFailed = 0
			}
			if done != wantDone || failed != wantFailed {
				t.Fatalf("done=%d error=%d; want done=%d error=%d", done, failed, wantDone, wantFailed)
			}
			if (err != nil) != (mode == "failure" || mode == "cancel") {
				t.Fatalf("unexpected Run error: %v", err)
			}
		})
	}
}

func TestParseEventInit(t *testing.T) {
	line := []byte(`{"event":"init","conversation_id":"abc-123","init":{"cwd":"C:\\work","tools":["run_command"],"permission_mode":"always-proceed"}}`)
	e, err := ParseEvent(line)
	if err != nil {
		t.Fatal(err)
	}
	if e.Event != "init" || e.ConversationID != "abc-123" {
		t.Fatalf("unexpected init event: %+v", e)
	}
	if e.Init == nil || e.Init.PermissionMode != "always-proceed" {
		t.Fatalf("unexpected init payload: %+v", e.Init)
	}
}

func TestParseEventStepUpdate(t *testing.T) {
	line := []byte(`{"event":"step_update","step_update":{"conversation_id":"abc-123","step_index":1,"state":"ACTIVE","step_type":"agent_response","text_delta":"hi there"}}`)
	e, err := ParseEvent(line)
	if err != nil {
		t.Fatal(err)
	}
	if e.Event != "step_update" || e.StepUpdate == nil {
		t.Fatalf("unexpected step_update event: %+v", e)
	}
	if e.StepUpdate.StepType != "agent_response" || e.StepUpdate.TextDelta != "hi there" {
		t.Fatalf("unexpected step_update payload: %+v", e.StepUpdate)
	}
}

func TestParseEventTool(t *testing.T) {
	line := []byte(`{"event":"step_update","step_update":{"conversation_id":"abc-123","step_index":2,"state":"DONE","step_type":"tool","tool_name":"view_file","tool_info":{"name":"view_file","parameters":{"AbsolutePath":"README.md"},"output":"content text"}}}`)
	e, err := ParseEvent(line)
	if err != nil {
		t.Fatal(err)
	}
	if e.StepUpdate.ToolName != "view_file" || e.StepUpdate.ToolInfo == nil {
		t.Fatalf("unexpected tool payload: %+v", e.StepUpdate)
	}
	if e.StepUpdate.ToolInfo.OutputText() != "content text" {
		t.Fatalf("expected output 'content text', got %q", e.StepUpdate.ToolInfo.OutputText())
	}
}

func TestParseEventResult(t *testing.T) {
	line := []byte(`{"event":"result","result":{"conversation_id":"abc-123","status":"SUCCESS","response":"hi there\n","num_turns":1,"usage":{"input_tokens":100,"output_tokens":20}}}`)
	e, err := ParseEvent(line)
	if err != nil {
		t.Fatal(err)
	}
	if e.Event != "result" || e.Result == nil {
		t.Fatalf("unexpected result: %+v", e)
	}
	if e.Result.Status != "SUCCESS" || e.Result.Usage.InputTokens != 100 {
		t.Fatalf("unexpected result payload: %+v", e.Result)
	}
}

func TestMapPermissionArgs(t *testing.T) {
	tests := []struct {
		mode     string
		expected []string
	}{
		{"default", nil},
		{"acceptEdits", []string{"--mode", "accept-edits"}},
		{"plan", []string{"--mode", "plan"}},
		{"bypassPermissions", []string{"--dangerously-skip-permissions"}},
		{"yolo", []string{"--dangerously-skip-permissions"}},
	}

	for _, tt := range tests {
		var args []string
		extra := map[string]string{agent.ArgPermissionMode: tt.mode}
		mapPermissionArgs(&args, extra)
		if !slices.Equal(args, tt.expected) {
			t.Fatalf("mode %q: expected %v, got %v", tt.mode, tt.expected, args)
		}
	}
}

func TestDispatchWorkflow(t *testing.T) {
	r := &Runner{}
	st := dispatchState{}
	var events []agent.Event
	cb := func(ev agent.Event) {
		events = append(events, ev)
	}

	initEv, _ := ParseEvent([]byte(`{"event":"init","conversation_id":"test-session-1"}`))
	r.dispatch(initEv, cb, &st)

	deltaEv, _ := ParseEvent([]byte(`{"event":"step_update","step_update":{"conversation_id":"test-session-1","step_index":1,"state":"ACTIVE","step_type":"agent_response","text_delta":"Hello World"}}`))
	r.dispatch(deltaEv, cb, &st)

	resultEv, _ := ParseEvent([]byte(`{"event":"result","result":{"conversation_id":"test-session-1","status":"SUCCESS","response":"Hello World"}}`))
	r.dispatch(resultEv, cb, &st)

	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d: %+v", len(events), events)
	}
	if events[0].Type != agent.EventSessionInit || events[0].SessionID != "test-session-1" {
		t.Errorf("expected EventSessionInit, got %+v", events[0])
	}
	if events[1].Type != agent.EventStreamStart {
		t.Errorf("expected EventStreamStart, got %+v", events[1])
	}
	if events[2].Type != agent.EventDelta || events[2].Text != "Hello World" {
		t.Errorf("expected EventDelta with 'Hello World', got %+v", events[2])
	}
	if events[3].Type != agent.EventDone || events[3].ResultText != "Hello World" {
		t.Errorf("expected EventDone, got %+v", events[3])
	}
}

func TestDispatchToolActivityLifecycle(t *testing.T) {
	var activities []string
	var tools []agent.EventType
	var deltas []string
	var st dispatchState
	for _, line := range []string{
		`{"event":"step_update","step_update":{"step_index":2,"step_type":"tool","state":"ACTIVE","tool_name":"run_command","tool_info":{"parameters":{"CommandLine":"go test ./..."}}}}`,
		`{"event":"step_update","step_update":{"step_index":2,"step_type":"tool","state":"DONE","tool_name":"run_command"}}`,
		`{"event":"step_update","step_update":{"step_index":3,"step_type":"tool","state":"ERROR","tool_info":{"name":"view_file","error":{"message":"not found"}}}}`,
	} {
		e, err := ParseEvent([]byte(line))
		if err != nil {
			t.Fatal(err)
		}
		(&Runner{}).dispatch(e, func(e agent.Event) {
			if e.Type == agent.EventActivity {
				activities = append(activities, e.Text)
			}
			if e.Type == agent.EventToolStarted || e.Type == agent.EventToolCompleted {
				tools = append(tools, e.Type)
			}
			if e.Type == agent.EventDelta {
				deltas = append(deltas, e.Text)
			}
		}, &st)
	}
	want := "執行工具：run_command：go test ./...|工具已完成：run_command|工具失敗：view_file：not found"
	if strings.Join(activities, "|") != want || len(tools) != 3 || len(deltas) != 0 {
		t.Fatalf("activities=%v tools=%v deltas=%v", activities, tools, deltas)
	}
}
