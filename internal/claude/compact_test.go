package claude

import (
	"strings"
	"testing"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
)

// 原生 fixture 對應已驗證的 Claude stream-json：頂層 system compact_boundary，
// 接著 result subtype success。摘要文字、compacting 狀態、錯 session、失敗結果都不是成功。
func TestClaudeNativeCompactFixture(t *testing.T) {
	const secret = "summary secret"
	events := dispatchLines(t, "provider-session", []string{
		`{"type":"system","subtype":"status","status":"compacting","session_id":"provider-session"}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"I compacted the notes"}]}}`,
		`{"type":"system","subtype":"compact_boundary","session_id":"provider-session"}`,
		`{"type":"result","subtype":"success","is_error":false,"session_id":"provider-session","result":"` + secret + `"}`,
	})
	assertCompactNative(t, events, "provider-session", secret)

	noBoundary := dispatchLines(t, "provider-session", []string{
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"compacted the context"}]}}`,
		`{"type":"result","subtype":"success","is_error":false,"session_id":"provider-session","result":"` + secret + `"}`,
	})
	if nativeCompact(noBoundary) {
		t.Fatal("沒有頂層 compact_boundary 不得當成成功")
	}
	assertNoSecret(t, noBoundary, secret)

	wrong := dispatchLines(t, "provider-session", []string{
		`{"type":"system","subtype":"compact_boundary","session_id":"other-session"}`,
		`{"type":"result","subtype":"success","is_error":false,"session_id":"other-session","result":"` + secret + `"}`,
	})
	if nativeCompact(wrong) {
		t.Fatal("不同 provider session 不得當成成功")
	}

	failed := dispatchLines(t, "provider-session", []string{
		`{"type":"system","subtype":"compact_boundary","session_id":"provider-session"}`,
		`{"type":"result","subtype":"success","is_error":true,"session_id":"provider-session","result":"boom"}`,
	})
	if nativeCompact(failed) {
		t.Fatal("is_error 不得當成成功")
	}
	assertNoSecret(t, failed, "boom")
}

func dispatchLines(t *testing.T, sessionID string, lines []string) []agent.Event {
	t.Helper()
	r := &Runner{}
	st := &streamState{compact: true, expectSession: sessionID}
	var out []agent.Event
	for _, line := range lines {
		ev, err := ParseEvent([]byte(line))
		if err != nil {
			t.Fatal(err)
		}
		r.dispatch(ev, func(e agent.Event) { out = append(out, e) }, st)
	}
	return out
}

func nativeCompact(events []agent.Event) bool {
	var boundary string
	for _, e := range events {
		if e.Type == agent.EventCompactBoundary && boundary == "" {
			boundary = e.SessionID
		}
		if e.Type == agent.EventDone && e.CompactNative && boundary != "" && e.SessionID == boundary {
			return true
		}
	}
	return false
}

func assertCompactNative(t *testing.T, events []agent.Event, sessionID, secret string) {
	t.Helper()
	if !nativeCompact(events) {
		t.Fatalf("同一 session 的頂層 compact_boundary 與成功結果應標成原生成功: %+v", events)
	}
	for _, e := range events {
		if e.Type == agent.EventCompactBoundary && e.SessionID != sessionID {
			t.Fatalf("boundary session=%s", e.SessionID)
		}
	}
	assertNoSecret(t, events, secret)
}

func assertNoSecret(t *testing.T, events []agent.Event, secret string) {
	t.Helper()
	for _, e := range events {
		if strings.Contains(e.Text, secret) || strings.Contains(e.ResultText, secret) || strings.Contains(e.Text, "I compacted") {
			t.Fatalf("壓縮摘要不該進入可見事件: %+v", e)
		}
	}
}
