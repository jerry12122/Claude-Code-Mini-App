package mcp

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

// listSessionsCaller 起一個 MCP HTTP server 並回傳呼叫 list_sessions 的 helper（走完整輸出 schema 驗證）。
func listSessionsCaller(t *testing.T, database *db.DB) func(args map[string]any) (map[string]map[string]any, error) {
	t.Helper()
	srv := httptest.NewServer(NewHTTPHandler(database, nil, nil, 0))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	client := gomcp.NewClient(&gomcp.Implementation{Name: "list-sessions-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &gomcp.StreamableClientTransport{Endpoint: srv.URL, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return func(args map[string]any) (map[string]map[string]any, error) {
		res, err := session.CallTool(ctx, &gomcp.CallToolParams{Name: "list_sessions", Arguments: args})
		if err != nil {
			return nil, err
		}
		if res.IsError {
			return nil, &toolError{res}
		}
		raw, err := json.Marshal(res.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		var out struct {
			Sessions []map[string]any `json:"sessions"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		byName := map[string]map[string]any{}
		for _, s := range out.Sessions {
			byName[s["name"].(string)] = s
		}
		return byName, nil
	}
}

type toolError struct{ res *gomcp.CallToolResult }

func (e *toolError) Error() string { return "tool error" }

func setFailure(t *testing.T, database *db.DB, id, text, at string) {
	t.Helper()
	if err := database.SetSessionLastError(id, text); err != nil {
		t.Fatal(err)
	}
	// 測試要固定時間；SetSessionLastError 用 datetime('now')，這裡直接覆寫成指定的 UTC 時間。
	if _, err := database.Exec(`UPDATE sessions SET last_error_at = ? WHERE id = ?`, at, id); err != nil {
		t.Fatal(err)
	}
}

func newNamedSession(t *testing.T, database *db.DB, name string) string {
	t.Helper()
	s, err := database.CreateSession(name, "", t.TempDir(), "default", "codex", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	return s.ID
}

func TestListSessions_ReportsFailureAndQueueState(t *testing.T) {
	database := setupTestDB(t)
	failed := newNamedSession(t, database, "cloudserver")
	healthy := newNamedSession(t, database, "healthy")
	setFailure(t, database, failed, "You've hit your usage limit, try again at 4:42 PM", "2026-11-02 08:17:43")
	if _, err := database.EnqueueMessage(failed, "q1", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := database.EnqueueMessage(failed, "q2", ""); err != nil {
		t.Fatal(err)
	}
	if err := database.SetQueuePaused(failed, true); err != nil {
		t.Fatal(err)
	}

	sessions, err := listSessionsCaller(t, database)(nil)
	if err != nil {
		t.Fatal(err)
	}

	f := sessions["cloudserver"]
	if f["last_error"] != "You've hit your usage limit, try again at 4:42 PM" {
		t.Fatalf("last_error=%v", f["last_error"])
	}
	if f["last_error_at"] != "2026-11-02T08:17:43Z" {
		t.Fatalf("last_error_at 應為 ISO8601 UTC: %v", f["last_error_at"])
	}
	if f["queue_paused"] != true || f["queue_length"] != float64(2) {
		t.Fatalf("queue_paused=%v queue_length=%v", f["queue_paused"], f["queue_length"])
	}

	h := sessions["healthy"]
	if _, ok := h["last_error"]; ok {
		t.Fatalf("無失敗時應省略 last_error: %v", h)
	}
	if _, ok := h["last_error_at"]; ok {
		t.Fatalf("無失敗時應省略 last_error_at: %v", h)
	}
	if h["queue_paused"] != false || h["queue_length"] != float64(0) {
		t.Fatalf("queue_paused=%v queue_length=%v", h["queue_paused"], h["queue_length"])
	}
	_ = healthy
}

func TestListSessions_FailedAfterFilter(t *testing.T) {
	database := setupTestDB(t)
	early := newNamedSession(t, database, "early")
	late := newNamedSession(t, database, "late")
	newNamedSession(t, database, "clean")
	setFailure(t, database, early, "e", "2026-11-02 08:00:00")
	setFailure(t, database, late, "l", "2026-11-02 09:00:00")
	call := listSessionsCaller(t, database)

	names := func(m map[string]map[string]any) []string {
		var out []string
		for n := range m {
			out = append(out, n)
		}
		return out
	}

	// 只回傳失敗時間晚於 failed_after 的 session；沒有失敗紀錄的不回傳。
	got, err := call(map[string]any{"failed_after": "2026-11-02T08:30:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got["late"] == nil {
		t.Fatalf("應只有 late: %v", names(got))
	}

	// 嚴格晚於：剛好等於失敗時間的不算。
	got, err = call(map[string]any{"failed_after": "2026-11-02T09:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("等於失敗時間不應回傳: %v", names(got))
	}

	// 帶時區的輸入要換算：+08:00 的 17:30 = 09:30 UTC，晚於 09:00。
	got, err = call(map[string]any{"failed_after": "2026-11-02T17:30:00+08:00"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("時區換算後沒有更晚的失敗: %v", names(got))
	}

	// 與 since 取交集：late 的 last_active 是建立當下（現在），since 設在未來就會濾掉。
	got, err = call(map[string]any{"failed_after": "2026-11-02T08:30:00Z", "since": "2999-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("since 與 failed_after 應取交集: %v", names(got))
	}

	// 不帶 failed_after 時行為不變，三個都回傳。
	got, err = call(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("不帶 failed_after 應回傳全部: %v", names(got))
	}

	// 非法時間回傳錯誤。
	if _, err := call(map[string]any{"failed_after": "not-a-time"}); err == nil {
		t.Fatal("非法 failed_after 應回錯誤")
	}
}
