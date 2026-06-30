package antigravity

import "testing"

func TestParseEventInit(t *testing.T) {
	line := []byte(`{"type":"init","session_id":"abc-123","model":"gemini-3.1-pro"}`)
	e, err := ParseEvent(line)
	if err != nil {
		t.Fatal(err)
	}
	if e.Type != "init" || e.SessionID != "abc-123" {
		t.Fatalf("got %+v", e)
	}
}

func TestIsAssistantDelta(t *testing.T) {
	trueVal := true
	e := &StreamEvent{Type: "message", Role: "assistant", Delta: &trueVal, Content: "hi"}
	if !e.IsAssistantDelta() {
		t.Fatal("expected delta")
	}
}

func TestMapSkipPermissions(t *testing.T) {
	if mapSkipPermissions(map[string]string{"permission_mode": "default"}) {
		t.Fatal("default should not skip")
	}
	if !mapSkipPermissions(map[string]string{"permission_mode": "bypassPermissions"}) {
		t.Fatal("bypass should skip")
	}
}
