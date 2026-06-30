package cursor

import (
	"strings"
	"testing"

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
