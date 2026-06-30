package usage

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFromClaudeResult(t *testing.T) {
	line := []byte(`{"type":"result","subtype":"success","total_cost_usd":0.0073953,"duration_ms":2280,"usage":{"input_tokens":3,"output_tokens":4,"cache_read_input_tokens":24421,"cache_creation_input_tokens":0}}`)
	info, err := FromClaudeResult(line)
	if err != nil {
		t.Fatal(err)
	}
	if info.Provider != "claude" || info.CostUSD == nil || *info.CostUSD != 0.0073953 {
		t.Fatalf("cost: %+v", info)
	}
	if info.InputTokens == nil || *info.InputTokens != 3 {
		t.Fatalf("input: %+v", info)
	}
}

func TestFromCursorResult(t *testing.T) {
	line := []byte(`{"type":"result","subtype":"success","duration_ms":9230,"usage":{"inputTokens":31076,"outputTokens":173,"cacheReadTokens":448,"cacheWriteTokens":0}}`)
	info, err := FromCursorResult(line)
	if err != nil {
		t.Fatal(err)
	}
	if info.Provider != "cursor" || info.CostUSD != nil {
		t.Fatalf("unexpected: %+v", info)
	}
	if info.InputTokens == nil || *info.InputTokens != 31076 {
		t.Fatalf("input: %+v", info)
	}
}

func TestFromKiroStderr(t *testing.T) {
	stderr := "All tools are now trusted.\n\nCredits: 0.05 \u22c5 Time: 2s\n"
	info := FromKiroStderr(stderr)
	if info.Credits == nil || *info.Credits != 0.05 {
		t.Fatalf("credits: %+v", info)
	}
	if info.DurationText != "2s" {
		t.Fatalf("duration: %q", info.DurationText)
	}
}

func TestFromCursorResultBrokenUTF8(t *testing.T) {
	line := []byte(`{"type":"result","duration_ms":7783,"result":"broken\xff","usage":{"inputTokens":25737,"outputTokens":127,"cacheReadTokens":5792,"cacheWriteTokens":0}}`)
	info, err := FromCursorResult(line)
	if err != nil {
		t.Fatal(err)
	}
	if info.InputTokens == nil || *info.InputTokens != 25737 {
		t.Fatalf("regex fallback failed: %+v", info)
	}
}

func readFixture(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF}), nil
}

func TestFixtureSamplesIfPresent(t *testing.T) {
	root := filepath.Join("..", "..", "poc", "usage-events", "samples")
	claudePath := filepath.Join(root, "claude-result.ndjson")
	if _, err := os.Stat(claudePath); err != nil {
		t.Skip("live samples not generated yet")
	}
	line, err := readFixture(claudePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := FromClaudeResult(line); err != nil {
		t.Fatalf("claude fixture: %v", err)
	}
	cursorLine, err := readFixture(filepath.Join(root, "cursor-result.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := FromCursorResult(cursorLine); err != nil {
		t.Fatalf("cursor fixture: %v", err)
	}
	kiroStderr, err := readFixture(filepath.Join(root, "kiro-stderr.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if FromKiroStderr(string(kiroStderr)).Credits == nil {
		t.Fatal("kiro fixture: no credits parsed")
	}
}
