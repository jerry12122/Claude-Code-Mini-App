package kiroacp

import (
	"encoding/json"
	"testing"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
)

func TestExtractAgentText(t *testing.T) {
	body := sessionUpdateBody{
		SessionUpdate: "agent_message_chunk",
		Content:       json.RawMessage(`{"type":"text","text":"hello"}`),
	}
	if got := extractAgentText(body); got != "hello" {
		t.Fatalf("got %q", got)
	}
	if extractAgentText(sessionUpdateBody{SessionUpdate: "tool_call"}) != "" {
		t.Fatal("tool_call should not yield text")
	}
}

func TestParseSessionResult(t *testing.T) {
	raw := json.RawMessage(`{"sessionId":"abc","models":{"currentModelId":"claude-sonnet-5"}}`)
	s, err := parseSessionResult(raw)
	if err != nil {
		t.Fatal(err)
	}
	if s.SessionID != "abc" || modelIDFrom(s) != "claude-sonnet-5" {
		t.Fatalf("%+v", s)
	}
	snap := modelSnapshot(s)
	if snap == nil || snap.Model != "claude-sonnet-5" {
		t.Fatalf("snapshot=%+v", snap)
	}
}

func TestBuildArgs(t *testing.T) {
	args := buildArgs(agent.RunOptions{
		ExtraArgs: map[string]string{agent.ArgModel: "claude-sonnet-5", "effort": "high"},
	})
	joined := ""
	for _, a := range args {
		joined += a + " "
	}
	if args[0] != "acp" {
		t.Fatalf("want acp first, got %v", args)
	}
	if !containsPair(args, "--model", "claude-sonnet-5") || !containsPair(args, "--effort", "high") {
		t.Fatalf("args=%v", args)
	}
}

func containsPair(args []string, flag, val string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == val {
			return true
		}
	}
	return false
}
