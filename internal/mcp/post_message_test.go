package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

func setupTestDB(t *testing.T) *db.DB {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open failed: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func TestPostMessageTool(t *testing.T) {
	database := setupTestDB(t)

	sess, err := database.CreateSession("test-session", "desc", t.TempDir(), "default", "claude", nil, "agent")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	var notifyCalled atomic.Int32
	var notifiedUser atomic.Int64
	var notifiedText string

	mockNotifier := func(botToken string, chatID int64, text string) error {
		notifyCalled.Add(1)
		notifiedUser.Store(chatID)
		notifiedText = text
		return nil
	}

	if err := database.AddUser(123456789, "testuser"); err != nil {
		t.Fatalf("AddUser failed: %v", err)
	}

	d := &deps{
		db:       database,
		botToken: "test_bot_token",
		notifier: mockNotifier,
	}

	// 測試正常發送 assistant 訊息
	in := postMessageIn{
		SessionID: sess.ID,
		Text:      "## 執行完成報告\n所有單元測試均已通過。",
		Role:      "assistant",
		Author:    "Worker Agent",
		Notify:    true,
	}

	_, out, err := d.postMessage(context.Background(), nil, in)
	if err != nil {
		t.Fatalf("postMessage failed: %v", err)
	}

	if !out.OK {
		t.Errorf("expected out.OK to be true, got false")
	}
	if out.MessageID <= 0 {
		t.Errorf("expected valid MessageID, got %d", out.MessageID)
	}

	// 檢查資料庫訊息
	msgs, err := database.ListMessages(sess.ID)
	if err != nil {
		t.Fatalf("ListMessages failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message in db, got %d", len(msgs))
	}
	m := msgs[0]
	if m.Role != "claude" {
		t.Errorf("expected role 'claude', got %q", m.Role)
	}
	if m.Content != in.Text {
		t.Errorf("content mismatch, expected %q, got %q", in.Text, m.Content)
	}
	if m.Author != "Worker Agent" {
		t.Errorf("author mismatch, expected 'Worker Agent', got %q", m.Author)
	}
	if m.Status != db.MessageStatusDone {
		t.Errorf("status mismatch, expected 'done', got %q", m.Status)
	}

	// 驗證目標 session 狀態仍為 idle
	refreshed, err := database.GetSession(sess.ID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if refreshed.Status != db.SessionStatusIdle {
		t.Errorf("expected session status to remain idle, got %q", refreshed.Status)
	}

	// 驗證 Telegram 通知有觸發
	if notifyCalled.Load() != 1 {
		t.Errorf("expected notifyCalled = 1, got %d", notifyCalled.Load())
	}
	if notifiedUser.Load() != 123456789 {
		t.Errorf("expected notifiedUser = 123456789, got %d", notifiedUser.Load())
	}
	if notifiedText != in.Text {
		t.Errorf("notifiedText mismatch: %q", notifiedText)
	}
}

func TestPostMessageWhileRunning(t *testing.T) {
	database := setupTestDB(t)

	sess, err := database.CreateSession("running-session", "desc", t.TempDir(), "default", "claude", nil, "agent")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// 模擬 session 正在 running 狀態
	if err := database.UpdateSessionStatus(sess.ID, db.SessionStatusRunning); err != nil {
		t.Fatalf("UpdateSessionStatus failed: %v", err)
	}

	d := &deps{db: database}

	in := postMessageIn{
		SessionID: sess.ID,
		Text:      "系統通知：記憶體監控正常",
		Role:      "system",
		Author:    "Monitor",
		Notify:    false,
	}

	_, out, err := d.postMessage(context.Background(), nil, in)
	if err != nil {
		t.Fatalf("postMessage while running failed: %v", err)
	}
	if !out.OK {
		t.Errorf("expected out.OK to be true")
	}

	// 驗證 session 狀態依然維持 running，不被干擾
	refreshed, err := database.GetSession(sess.ID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if refreshed.Status != db.SessionStatusRunning {
		t.Errorf("expected session status to remain running, got %q", refreshed.Status)
	}

	// 驗證訊息已成功寫入
	msgs, err := database.ListMessages(sess.ID)
	if err != nil {
		t.Fatalf("ListMessages failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message in db, got %d", len(msgs))
	}
	if msgs[0].Role != "system" {
		t.Errorf("expected role 'system', got %q", msgs[0].Role)
	}
}

func TestPostMessageInvalidSession(t *testing.T) {
	database := setupTestDB(t)
	d := &deps{db: database}

	_, _, err := d.postMessage(context.Background(), nil, postMessageIn{
		SessionID: "non-existent-id",
		Text:      "hello",
	})
	if err == nil {
		t.Fatalf("expected error for non-existent session, got nil")
	}
}

// 註冊 schema 與通知閘門：ok 只代表寫入；false、無 token、無使用者、通知錯誤都不啟動 agent。
func TestPostMessageSchemaAndNotifyGate(t *testing.T) {
	database := setupTestDB(t)
	sess, err := database.CreateSession("pm", "desc", t.TempDir(), "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AddUser(42, "owner"); err != nil {
		t.Fatal(err)
	}
	text := "自訂完成：登入已驗收。\n真機未測。"
	var calls atomic.Int32
	var got string
	d := &deps{
		db:       database,
		botToken: "test_bot_token",
		notifier: func(_ string, _ int64, body string) error {
			calls.Add(1)
			got = body
			return errors.New("telegram down")
		},
	}
	server := gomcp.NewServer(&gomcp.Implementation{Name: "post", Version: "1"}, nil)
	registerTools(server, d)
	ct, st := gomcp.NewInMemoryTransports()
	ss, err := server.Connect(t.Context(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	client, err := gomcp.NewClient(&gomcp.Implementation{Name: "c", Version: "1"}, nil).Connect(t.Context(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	var tool *gomcp.Tool
	for item, err := range client.Tools(t.Context(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		if item.Name == "post_message" {
			tool = item
		}
	}
	if tool == nil {
		t.Fatal("post_message 未註冊")
	}
	raw, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	props, _ := schema["properties"].(map[string]any)
	for _, key := range []string{"session_id", "text", "author", "notify"} {
		if _, ok := props[key]; !ok {
			t.Fatalf("schema 缺 %s", key)
		}
	}

	call := func(notify bool) *gomcp.CallToolResult {
		t.Helper()
		res, err := client.CallTool(t.Context(), &gomcp.CallToolParams{
			Name: "post_message",
			Arguments: map[string]any{
				"session_id": sess.ID,
				"text":       text,
				"author":     "PM",
				"notify":     notify,
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if res.IsError {
			t.Fatalf("notify=%v 呼叫失敗: %+v", notify, res)
		}
		return res
	}

	call(false)
	if calls.Load() != 0 {
		t.Fatalf("notify false 仍推播 %d 次", calls.Load())
	}

	call(true)
	if calls.Load() != 1 || got != text {
		t.Fatalf("通知文字=%q 次數=%d", got, calls.Load())
	}
	refreshed, err := database.GetSession(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Status != db.SessionStatusIdle {
		t.Fatalf("狀態被改成 %s", refreshed.Status)
	}
	msgs, err := database.ListMessages(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || msgs[0].Content != text || msgs[1].Content != text {
		t.Fatalf("寫入內容不符: %+v", msgs)
	}

	d.botToken = ""
	call(true)
	if calls.Load() != 1 {
		t.Fatalf("無 token 仍推播，次數 %d", calls.Load())
	}

	bare := setupTestDB(t)
	quiet, err := bare.CreateSession("quiet", "desc", t.TempDir(), "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	var bareCalls atomic.Int32
	bareDeps := &deps{
		db:       bare,
		botToken: "test_bot_token",
		notifier: func(string, int64, string) error {
			bareCalls.Add(1)
			return nil
		},
	}
	if _, _, err := bareDeps.postMessage(context.Background(), nil, postMessageIn{
		SessionID: quiet.ID, Text: text, Notify: true,
	}); err != nil {
		t.Fatal(err)
	}
	if bareCalls.Load() != 0 {
		t.Fatalf("沒有註冊使用者仍推播 %d 次", bareCalls.Load())
	}
}

// 沒有 loopback 連線時 get_status 要反映 DB 的實際狀態，不能一律回 idle。
func TestGetStatusFallsBackToDB(t *testing.T) {
	database := setupTestDB(t)
	sess, err := database.CreateSession("s", "", t.TempDir(), "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	d := &deps{db: database, reg: NewRegistry(func(string) string { return "" }, nil)}

	_ = database.UpdateSessionStatus(sess.ID, db.SessionStatusRunning)
	_, out, _ := d.getStatus(context.Background(), nil, sessionIDIn{SessionID: sess.ID})
	if out.State != StateRunning || out.Connected {
		t.Fatalf("want running/unconnected, got %+v", out)
	}
	_ = database.UpdateSessionStatus(sess.ID, db.SessionStatusIdle)
	_, out, _ = d.getStatus(context.Background(), nil, sessionIDIn{SessionID: sess.ID})
	if out.State != StateIdle {
		t.Fatalf("want idle, got %+v", out)
	}
}
