package mcp

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

// 每次 tools/call 寫一行含工具名、client、參數與結果大小的 log。
func TestToolCallLogMiddlewareLogsCall(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/log.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	call := listSessionsCaller(t, database)
	if _, err := call(map[string]any{"since": "2026-01-01T00:00:00+08:00"}); err != nil {
		t.Fatal(err)
	}

	var line string
	for _, l := range strings.Split(buf.String(), "\n") {
		if strings.Contains(l, "[mcp] tool call") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("找不到 tool call log：\n%s", buf.String())
	}
	for _, want := range []string{"tool=list_sessions", "client=list-sessions-test/1", "2026-01-01T00:00:00+08:00", "result_bytes=", "mcp_session="} {
		if !strings.Contains(line, want) {
			t.Errorf("log 缺少 %q：%s", want, line)
		}
	}
}

// 白名單工具記原始參數值；其餘工具只記鍵名與位元組數。
func TestToolCallArgs(t *testing.T) {
	if got := toolCallArgs("list_sessions", []byte(`{"since":"2026-01-01"}`)); got != `{"since":"2026-01-01"}` {
		t.Errorf("list_sessions args = %s", got)
	}
	got := toolCallArgs("send_message", []byte(`{"session_id":"abc","message":"秘密內容"}`))
	if strings.Contains(got, "秘密內容") || strings.Contains(got, "abc") {
		t.Errorf("send_message 參數值外洩：%s", got)
	}
	if got != "{message:14B session_id:5B}" {
		t.Errorf("send_message args = %s", got)
	}
	if got := toolCallArgs("shell_exec", []byte(`not-json`)); got != "<redacted 8 bytes>" {
		t.Errorf("非 JSON args = %s", got)
	}
}
