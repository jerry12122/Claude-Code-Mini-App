package cursor

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
)

// 官方文件範例 NDJSON（精簡版）
// https://cursor.com/docs/cli/reference/output-format
const officialSampleNDJSON = `{"type":"system","subtype":"init","apiKeySource":"login","cwd":"/Users/user/project","session_id":"c6b62c6f-7ead-4fd6-9922-e952131177ff","model":"Claude 4 Sonnet","permissionMode":"default"}
{"type":"user","message":{"role":"user","content":[{"type":"text","text":"Read README.md"}]},"session_id":"c6b62c6f-7ead-4fd6-9922-e952131177ff"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"I'll read the README.md file"}]},"session_id":"c6b62c6f-7ead-4fd6-9922-e952131177ff"}
{"type":"result","subtype":"success","is_error":false,"result":"done","session_id":"c6b62c6f-7ead-4fd6-9922-e952131177ff"}`

func TestParseOfficialSampleEvents(t *testing.T) {
	for i, line := range strings.Split(officialSampleNDJSON, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if _, err := ParseEvent([]byte(line)); err != nil {
			t.Fatalf("line %d parse error: %v", i, err)
		}
	}
	init, _ := ParseEvent([]byte(strings.Split(officialSampleNDJSON, "\n")[0]))
	if init.SessionID != "c6b62c6f-7ead-4fd6-9922-e952131177ff" {
		t.Errorf("session_id = %q", init.SessionID)
	}
}

func TestDispatchNonPartialAssistantSegments(t *testing.T) {
	r := &Runner{}
	var events []agent.Event
	cb := func(e agent.Event) { events = append(events, e) }

	lines := []string{
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"segment one"}]},"session_id":"s1"}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"segment two"}]},"session_id":"s1"}`,
	}
	var st dispatchState
	for _, line := range lines {
		e, err := ParseEvent([]byte(line))
		if err != nil {
			t.Fatal(err)
		}
		r.dispatchAssistant(e, cb, &st)
	}

	deltas := filterEvents(events, agent.EventDelta)
	if len(deltas) != 2 {
		t.Fatalf("expected 2 deltas for non-partial mode, got %d", len(deltas))
	}
}

func TestDispatchPartialOutputFiltering(t *testing.T) {
	r := &Runner{}
	var events []agent.Event
	cb := func(e agent.Event) { events = append(events, e) }

	lines := []string{
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Hel"}]},"timestamp_ms":123,"session_id":"s1"}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Hello"}]},"timestamp_ms":123,"model_call_id":"mc-1","session_id":"s1"}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Hello"}]},"session_id":"s1"}`,
	}
	var st dispatchState
	for _, line := range lines {
		e, err := ParseEvent([]byte(line))
		if err != nil {
			t.Fatal(err)
		}
		r.dispatchAssistant(e, cb, &st)
	}

	deltas := filterEvents(events, agent.EventDelta)
	if len(deltas) != 1 || deltas[0].Text != "Hel" {
		t.Fatalf("expected single delta 'Hel', got %+v", deltas)
	}
}

func TestDispatchResultCarriesResultText(t *testing.T) {
	r := &Runner{}
	var events []agent.Event
	cb := func(e agent.Event) { events = append(events, e) }

	e := &StreamEvent{
		Type:      "result",
		Subtype:   "success",
		IsError:   false,
		Result:    "full answer text",
		SessionID: "s1",
	}
	r.dispatchResult(e, cb)

	done := filterEvents(events, agent.EventDone)
	if len(done) != 1 || done[0].ResultText != "full answer text" {
		t.Fatalf("EventDone.ResultText not set: %+v", done)
	}
}

func TestClassifyAuthError(t *testing.T) {
	err := classifyRunnerError("Error: Authentication required. Please run 'agent login' first.", nil)
	if _, ok := err.(*authError); !ok {
		t.Fatalf("expected authError, got %T: %v", err, err)
	}
}

func filterEvents(events []agent.Event, typ agent.EventType) []agent.Event {
	var out []agent.Event
	for _, e := range events {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

func TestToolLabel(t *testing.T) {
	cases := []struct {
		name string
		line string
		want string
	}{
		{"read", `{"type":"tool_call","subtype":"started","call_id":"1","tool_call":{"readToolCall":{"args":{"path":"README.md"}}}}`, "讀取檔案中…"},
		{"write", `{"type":"tool_call","subtype":"started","call_id":"2","tool_call":{"writeToolCall":{"args":{"path":"a.go"}}}}`, "修改檔案中…"},
		{"function", `{"type":"tool_call","subtype":"started","call_id":"3","tool_call":{"function":{"name":"grep","arguments":"{}"}}}`, "呼叫工具 grep…"},
		{"unknown-suffix", `{"type":"tool_call","subtype":"started","call_id":"4","tool_call":{"searchToolCall":{"args":{}}}}`, "使用工具 search…"},
		{"empty", `{"type":"tool_call","subtype":"started","call_id":"5"}`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e, err := ParseEvent([]byte(c.line))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got := e.ToolLabel(); got != c.want {
				t.Errorf("ToolLabel() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestDispatch_mcpToolCallCompletedImageEmitsMarkdownDelta(t *testing.T) {
	t.Chdir(t.TempDir()) // media.SaveBase64Image 用相對路徑落地檔案，隔離避免污染原始碼樹

	r := &Runner{}
	var st dispatchState
	var deltas []string
	cb := func(e agent.Event) {
		if e.Type == agent.EventDelta {
			deltas = append(deltas, e.Text)
		}
	}
	// cursor-agent 實際 schema：{"image":{"data","mimeType"}}，不是標準 MCP 的 type 欄位。
	line := `{"type":"tool_call","subtype":"completed","call_id":"1","tool_call":{"mcpToolCall":{"result":{"success":{"content":[{"text":{"text":"desc"}},{"image":{"data":"aGVsbG8=","mimeType":"image/png"}}]}}}}}`
	e, err := ParseEvent([]byte(line))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	r.dispatch(e, cb, &st)

	if len(deltas) != 1 || !strings.Contains(deltas[0], "![screenshot](/uploads/") {
		t.Fatalf("預期收到一則 markdown image delta，got deltas=%v", deltas)
	}
}

func TestDispatch_mcpToolCallCompletedImageStandardMCPFallback(t *testing.T) {
	t.Chdir(t.TempDir())

	r := &Runner{}
	var st dispatchState
	var deltas []string
	cb := func(e agent.Event) {
		if e.Type == agent.EventDelta {
			deltas = append(deltas, e.Text)
		}
	}
	line := `{"type":"tool_call","subtype":"completed","call_id":"1","tool_call":{"mcpToolCall":{"result":{"success":{"content":[{"type":"text","text":"desc"},{"type":"image","data":"aGVsbG8=","mimeType":"image/png"}]}}}}}`
	e, err := ParseEvent([]byte(line))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	r.dispatch(e, cb, &st)

	if len(deltas) != 1 || !strings.Contains(deltas[0], "![screenshot](/uploads/") {
		t.Fatalf("預期標準 MCP fallback 也能吐 markdown image delta，got deltas=%v", deltas)
	}
}

// result 後若有孤兒子進程（cursor-agent 自我更新）占著 stdout，Run 仍須返回且不算失敗。
func TestRunReturnsWhenOrphanHoldsStdoutAfterResult(t *testing.T) {
	dir := t.TempDir()
	const result = `{"type":"result","subtype":"success","is_error":false,"result":"done","session_id":"s1"}`
	if runtime.GOOS == "windows" {
		script := "@echo " + result + "\r\n@start /b ping -n 8 127.0.0.1\r\n"
		if err := os.WriteFile(filepath.Join(dir, "cursor-agent.cmd"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	} else {
		script := "#!/bin/sh\necho '" + result + "'\nsleep 8 &\n"
		if err := os.WriteFile(filepath.Join(dir, "cursor-agent"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	old := resultExitGrace
	resultExitGrace = 300 * time.Millisecond
	defer func() { resultExitGrace = old }()

	done := make(chan error, 1)
	go func() {
		done <- (&Runner{}).Run(context.Background(), agent.RunOptions{Prompt: "x"}, func(agent.Event) {})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run err = %v, want nil", err)
		}
	case <-time.After(7 * time.Second):
		t.Fatal("Run 卡在孤兒進程占住的 stdout")
	}
}
