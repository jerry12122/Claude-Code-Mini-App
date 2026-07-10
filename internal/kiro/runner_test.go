package kiro

import (
	"strings"
	"testing"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
)

func TestStripAnsi(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"\x1b[mhello\x1b[0m", "hello"},
		{"\x1b[32mgreen\x1b[0m text", "green text"},
		{"\x1b[38;5;141murl\x1b[0m", "url"},
		{"\x1b[?25lhidden\x1b[?25h", "hidden"},
		{"no ansi", "no ansi"},
	}
	for _, c := range cases {
		got := stripAnsi(c.input)
		if got != c.want {
			t.Errorf("stripAnsi(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestStripKiroPrefix(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"\x1b[m> \x1b[0mHello.", "Hello."},
		{"> Goodbye.", "Goodbye."},
		{"  > not a prefix", "  > not a prefix"},
		{"", ""},
		{"\x1b[m\x1b[0m", ""},
	}
	for _, c := range cases {
		got := stripKiroPrefix(c.input)
		if got != c.want {
			t.Errorf("stripKiroPrefix(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestSessionIDParse(t *testing.T) {
	raw := `Chat SessionId: a6dd5cab-245a-46b5-9f6a-0d01c6bd21c2
  2 seconds ago | hello | 2 msgs | classic`
	sessions := parseListSessions(raw)
	if len(sessions) != 1 || sessions[0].ID != "a6dd5cab-245a-46b5-9f6a-0d01c6bd21c2" {
		t.Fatalf("parse failed: %+v", sessions)
	}
}

func TestIsKiroResponseLine(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"\x1b[m> \x1b[0mHello.", true},
		{"> main", true},
		{"I will run the following command: git branch", false},
		{" - Completed in 1.5s", false},
		{"main", false},
		{"  > not at start", false},
	}
	for _, c := range cases {
		got := isKiroResponseLine(c.input)
		if got != c.want {
			t.Errorf("isKiroResponseLine(%q) = %v, want %v", c.input, got, c.want)
		}
	}
}

func TestDispatchLineThinkingThenResponse(t *testing.T) {
	var events []agent.Event
	cb := func(e agent.Event) { events = append(events, e) }

	st := &kiroStreamState{}
	st.dispatchLine("I will run the following command: git branch (using tool: shell)", cb)
	st.dispatchLine("main", cb)
	st.dispatchLine(" - Completed in 1.5s", cb)
	st.dispatchLine("\x1b[m> \x1b[0mmain", cb)
	st.dispatchLine("- second bullet without prefix", cb)

	// 應有 thinking + stream_start + 2 deltas
	if len(events) < 4 {
		t.Fatalf("expected at least 4 events, got %d: %+v", len(events), events)
	}

	thinkingCount := 0
	deltaCount := 0
	streamStart := false
	for _, e := range events {
		switch e.Type {
		case agent.EventThinking:
			thinkingCount++
		case agent.EventStreamStart:
			streamStart = true
		case agent.EventDelta:
			deltaCount++
		}
	}
	if thinkingCount == 0 {
		t.Error("expected EventThinking for tool lines")
	}
	if !streamStart {
		t.Error("expected EventStreamStart before response")
	}
	if deltaCount != 2 {
		t.Errorf("expected 2 EventDelta (response lines), got %d", deltaCount)
	}

	// 最後一則 thinking 應含工具相關文字
	lastThinking := ""
	for _, e := range events {
		if e.Type == agent.EventThinking {
			lastThinking = e.Text
		}
	}
	if !strings.Contains(lastThinking, "Completed in 1.5s") {
		t.Errorf("thinking should contain tool output, got %q", lastThinking)
	}
}

func TestDispatchLineTextOnly(t *testing.T) {
	var events []agent.Event
	cb := func(e agent.Event) { events = append(events, e) }

	st := &kiroStreamState{}
	st.dispatchLine("\x1b[m> \x1b[0mHello.", cb)

	hasThinking := false
	hasDelta := false
	for _, e := range events {
		if e.Type == agent.EventThinking {
			hasThinking = true
		}
		if e.Type == agent.EventDelta && strings.Contains(e.Text, "Hello.") {
			hasDelta = true
		}
	}
	if hasThinking {
		t.Error("text-only response should not emit thinking")
	}
	if !hasDelta {
		t.Error("expected EventDelta with Hello.")
	}
}

func TestFlushFallback(t *testing.T) {
	var events []agent.Event
	cb := func(e agent.Event) { events = append(events, e) }

	st := &kiroStreamState{}
	st.dispatchLine("plain line without prefix", cb)
	st.flushFallback(cb)

	hasDelta := false
	for _, e := range events {
		if e.Type == agent.EventDelta {
			hasDelta = true
		}
	}
	if !hasDelta {
		t.Error("flushFallback should emit delta when no > line seen")
	}
}

func TestBuildArgs(t *testing.T) {
	t.Run("new session", func(t *testing.T) {
		opts := buildArgsForTest("hello world", "", nil)
		expectContains(t, opts, "chat")
		expectContains(t, opts, "--no-interactive")
		expectContains(t, opts, "--trust-all-tools")
		expectNotContains(t, opts, "--resume-id")
		if opts[len(opts)-1] != "hello world" {
			t.Errorf("last arg should be prompt, got %q", opts[len(opts)-1])
		}
	})
	t.Run("resume session", func(t *testing.T) {
		opts := buildArgsForTest("hello", "some-session-id", nil)
		expectContains(t, opts, "--resume-id")
		idx := indexOf(opts, "--resume-id")
		if idx < 0 || opts[idx+1] != "some-session-id" {
			t.Error("--resume-id should be followed by session id")
		}
	})
	t.Run("effort extra arg", func(t *testing.T) {
		extra := map[string]string{"effort": "high"}
		opts := buildArgsForTest("hello", "", extra)
		expectContains(t, opts, "--effort")
		idx := indexOf(opts, "--effort")
		if idx < 0 || opts[idx+1] != "high" {
			t.Error("--effort should be followed by effort value")
		}
	})
	t.Run("model extra arg", func(t *testing.T) {
		extra := map[string]string{agent.ArgModel: "claude-haiku-4.5"}
		opts := buildArgsForTest("hello", "", extra)
		expectContains(t, opts, "--model")
		idx := indexOf(opts, "--model")
		if idx < 0 || opts[idx+1] != "claude-haiku-4.5" {
			t.Error("--model should be followed by model value")
		}
	})
}

// buildArgsForTest 建構 RunOptions 並呼叫 buildArgs 的測試輔助函式。
func buildArgsForTest(prompt, sessionID string, extra map[string]string) []string {
	return buildArgs(agent.RunOptions{
		Prompt:    prompt,
		SessionID: sessionID,
		ExtraArgs: extra,
	})
}

func expectContains(t *testing.T, args []string, val string) {
	t.Helper()
	for _, a := range args {
		if a == val {
			return
		}
	}
	t.Errorf("expected %q in args %v", val, args)
}

func expectNotContains(t *testing.T, args []string, val string) {
	t.Helper()
	for _, a := range args {
		if a == val {
			t.Errorf("did not expect %q in args %v", val, args)
			return
		}
	}
}

func indexOf(args []string, val string) int {
	for i, a := range args {
		if a == val {
			return i
		}
	}
	return -1
}
