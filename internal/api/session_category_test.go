package api

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/gofiber/fiber/v2"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

// categoryApp 直接掛 handler，不經過 server.Start 的 authMiddleware。
// 擁有者登入邊界見 server.go 的路由註冊；這裡只驗 HTTP 契約。
func categoryApp(database *db.DB) *fiber.App {
	app := fiber.New()
	h := NewSessionCategoryHandler(database)
	app.Get("/session-categories", h.List)
	app.Post("/session-categories", h.Create)
	app.Patch("/session-categories/:id", h.Rename)
	app.Delete("/session-categories/:id", h.Delete)
	return app
}

func doCategory(t *testing.T, app *fiber.App, method, path, body string) (int, []byte) {
	t.Helper()
	var r io.Reader
	if method == http.MethodPost || method == http.MethodPatch {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	if method == http.MethodPost || method == http.MethodPatch {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, b
}

func categoryCount(t *testing.T, database *db.DB) int {
	t.Helper()
	var n int
	if err := database.QueryRow(`SELECT COUNT(*) FROM session_categories`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSessionCategoryHTTP(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "open.db")
	database, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	notes := 0
	database.OnSessionChange = func() { notes++ }
	sess, err := database.CreateSession("keep", "", "A", "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE sessions SET last_active='2020-01-02 03:04:05', status='idle' WHERE id=?`, sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO messages (session_id, role, content, status, is_read) VALUES (?, 'claude', 'hist', 'done', 1)`, sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO queued_messages (session_id, content) VALUES (?, 'q')`, sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO input_receipts (session_id, actor, request_id, digest) VALUES (?, 'owner', 'req-1', 'digest-1')`, sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO work_dirs (path) VALUES ('B')`); err != nil {
		t.Fatal(err)
	}
	app := categoryApp(database)
	assertSessionUntouched(t, database, sess.ID)

	code, body := doCategory(t, app, http.MethodGet, "/session-categories?work_dir=A", "")
	if code != 200 || string(body) != "[]" {
		t.Fatalf("空清單 %d %s", code, body)
	}
	if notes != 0 {
		t.Fatalf("GET 不該通知，got %d", notes)
	}

	rejects := []string{
		"",
		"null",
		"[]",
		`{"name":1,"work_dir":"A"}`,
		`{"work_dir":"A"}`,
		`{"name":"Review"}`,
		`{"name":"Review","work_dir":"  "}`,
		`{"name":"Review","work_dir":"nope"}`,
		`{"name":"Review","work_dir":"a"}`,
		`{"name":"Review","work_dir":"Telegram 聊天室"}`,
		`{"name":"` + strings.Repeat("你", 81) + `","work_dir":"A"}`,
	}
	for _, raw := range rejects {
		code, body = doCategory(t, app, http.MethodPost, "/session-categories", raw)
		if code != 400 {
			t.Fatalf("POST %q → %d %s", raw, code, body)
		}
	}
	if categoryCount(t, database) != 0 || notes != 0 {
		t.Fatalf("無效 POST 不該寫入或通知 count=%d notes=%d", categoryCount(t, database), notes)
	}
	assertSessionUntouched(t, database, sess.ID)

	code, body = doCategory(t, app, http.MethodPost, "/session-categories", `{"name":" Review ","work_dir":" A "}`)
	if code != 201 {
		t.Fatalf("建立 A %d %s", code, body)
	}
	reviewA := decodeCategory(t, body)
	if reviewA.WorkDir != "A" || reviewA.Name != "Review" || reviewA.ID == "" || len(mustObj(t, body)) != 3 {
		t.Fatalf("DTO %+v %s", reviewA, body)
	}
	code, body = doCategory(t, app, http.MethodPost, "/session-categories", `{"name":"Review","work_dir":"B"}`)
	if code != 201 {
		t.Fatalf("跨目錄同名 %d %s", code, body)
	}
	reviewB := decodeCategory(t, body)
	if reviewB.ID == reviewA.ID || reviewB.WorkDir != "B" {
		t.Fatalf("B %+v", reviewB)
	}
	code, body = doCategory(t, app, http.MethodPost, "/session-categories", `{"name":"Review","work_dir":"A"}`)
	if code != 409 || notes != 2 {
		t.Fatalf("重名 %d notes=%d %s", code, notes, body)
	}

	name80 := strings.Repeat("你", 80)
	if utf8.RuneCountInString(name80) != 80 || len(name80) <= 80 {
		t.Fatal("80 fixture")
	}
	code, body = doCategory(t, app, http.MethodPost, "/session-categories", `{"name":"`+name80+`","work_dir":"A"}`)
	if code != 201 || decodeCategory(t, body).Name != name80 {
		t.Fatalf("80 code points %d %s", code, body)
	}
	if notes != 3 {
		t.Fatalf("成功建立應通知 3 次，got %d", notes)
	}

	code, body = doCategory(t, app, http.MethodGet, "/session-categories", "")
	if code != 400 || strings.Contains(string(body), reviewA.ID) || strings.Contains(string(body), reviewB.ID) {
		t.Fatalf("缺 work_dir 不該倒出全部 %d %s", code, body)
	}
	code, body = doCategory(t, app, http.MethodGet, "/session-categories?work_dir=A", "")
	if code != 200 || strings.Contains(string(body), reviewB.ID) || !strings.Contains(string(body), reviewA.ID) {
		t.Fatalf("GET A %d %s", code, body)
	}

	code, body = doCategory(t, app, http.MethodPatch, "/session-categories/"+reviewA.ID, `{"name":" Done ","work_dir":"B","id":"other"}`)
	renamed := decodeCategory(t, body)
	if code != 200 || renamed.ID != reviewA.ID || renamed.Name != "Done" || renamed.WorkDir != "A" || notes != 4 {
		t.Fatalf("改名 %d notes=%d %+v %s", code, notes, renamed, body)
	}
	code, _ = doCategory(t, app, http.MethodPatch, "/session-categories/"+reviewA.ID, `{"name":"Done"}`)
	if code != 200 || notes != 5 {
		t.Fatalf("成功改名都要通知，同名也算一次 %d notes=%d", code, notes)
	}
	code, _ = doCategory(t, app, http.MethodPatch, "/session-categories/"+reviewA.ID, `{"name":"`+strings.Repeat("你", 81)+`"}`)
	if code != 400 || notes != 5 {
		t.Fatalf("過長改名不該通知 %d notes=%d", code, notes)
	}
	if got, err := database.GetSessionCategory(reviewA.ID); err != nil || got.Name != "Done" || got.WorkDir != "A" {
		t.Fatalf("失敗改名仍改了儲存 %+v %v", got, err)
	}
	code, _ = doCategory(t, app, http.MethodPatch, "/session-categories/missing", `{"name":"Z"}`)
	if code != 404 {
		t.Fatalf("找不到 %d", code)
	}

	if _, err := database.Exec(`UPDATE sessions SET category_id=? WHERE id=?`, reviewA.ID, sess.ID); err != nil {
		t.Fatal(err)
	}
	code, body = doCategory(t, app, http.MethodDelete, "/session-categories/"+reviewA.ID, "")
	if code != 204 || len(body) != 0 || notes != 6 {
		t.Fatalf("刪除 %d notes=%d %q", code, notes, body)
	}
	assertSessionUntouched(t, database, sess.ID)
	if _, err := database.GetSessionCategory(reviewA.ID); err == nil {
		t.Fatal("分類還在")
	}
	code, _ = doCategory(t, app, http.MethodDelete, "/session-categories/missing", "")
	if code != 404 || notes != 6 {
		t.Fatalf("刪不存在 %d notes=%d", code, notes)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	code, body = doCategory(t, categoryApp(reopened), http.MethodGet, "/session-categories?work_dir=B", "")
	if code != 200 || !strings.Contains(string(body), reviewB.ID) {
		t.Fatalf("重開 %d %s", code, body)
	}
}

func TestSuccessfulRenameAlwaysNotifies(t *testing.T) {
	database := testDB(t)
	if _, err := database.CreateSession("s", "", "A", "default", "claude", nil, "agent"); err != nil {
		t.Fatal(err)
	}
	cat, err := database.CreateSessionCategory("A", "Original")
	if err != nil {
		t.Fatal(err)
	}
	notes := 0
	database.OnSessionChange = func() { notes++ }
	app := categoryApp(database)
	code, body := doCategory(t, app, http.MethodPatch, "/session-categories/"+cat.ID, `{"name":"Original"}`)
	got := decodeCategory(t, body)
	if code != 200 || got.ID != cat.ID || got.Name != "Original" || got.WorkDir != "A" || notes != 1 {
		t.Fatalf("成功的改名請求都要通知，同名也算一次：%d notes=%d %+v %s", code, notes, got, body)
	}
	code, _ = doCategory(t, app, http.MethodPatch, "/session-categories/"+cat.ID, `{"name":"`+strings.Repeat("你", 81)+`"}`)
	if code != 400 || notes != 1 {
		t.Fatalf("無效改名不該通知 %d notes=%d", code, notes)
	}
	code, _ = doCategory(t, app, http.MethodGet, "/session-categories?work_dir=A", "")
	if code != 200 || notes != 1 {
		t.Fatalf("GET 不該通知 %d notes=%d", code, notes)
	}
}

func TestSessionCategoryClosedDatabaseIs500(t *testing.T) {
	database := testDB(t)
	app := categoryApp(database)
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	code, body := doCategory(t, app, http.MethodGet, "/session-categories?work_dir=A", "")
	if code != 500 || string(body) == "[]" || !strings.Contains(string(body), "分類資料庫錯誤") {
		t.Fatalf("關閉的 DB 應 500，got %d %s", code, body)
	}
	var payload struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &payload) != nil || payload.Error == "" || payload.Error == db.ErrSessionCategoryInvalid.Error() || payload.Error == db.ErrSessionCategoryNotFound.Error() {
		t.Fatalf("500 不該冒充 400／404 或空成功：%s", body)
	}
}

func decodeCategory(t *testing.T, body []byte) db.SessionCategory {
	t.Helper()
	var cat db.SessionCategory
	if err := json.Unmarshal(body, &cat); err != nil {
		t.Fatal(err)
	}
	return cat
}

func mustObj(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	return raw
}

func assertSessionUntouched(t *testing.T, database *db.DB, id string) {
	t.Helper()
	var categoryID sql.NullString
	var lastActive, status, workDir, content, queue, digest string
	var isRead int
	if err := database.QueryRow(`SELECT category_id, last_active, status, work_dir FROM sessions WHERE id=?`, id).Scan(&categoryID, &lastActive, &status, &workDir); err != nil {
		t.Fatal(err)
	}
	if categoryID.Valid || lastActive != "2020-01-02 03:04:05" || status != "idle" || workDir != "A" {
		t.Fatalf("session 被改到 category=%v active=%s status=%s dir=%s", categoryID, lastActive, status, workDir)
	}
	if err := database.QueryRow(`SELECT content, is_read FROM messages WHERE session_id=?`, id).Scan(&content, &isRead); err != nil || content != "hist" || isRead != 1 {
		t.Fatalf("歷史 content=%q read=%d err=%v", content, isRead, err)
	}
	if err := database.QueryRow(`SELECT content FROM queued_messages WHERE session_id=?`, id).Scan(&queue); err != nil || queue != "q" {
		t.Fatalf("queue %q err=%v", queue, err)
	}
	if err := database.QueryRow(`SELECT digest FROM input_receipts WHERE session_id=?`, id).Scan(&digest); err != nil || digest != "digest-1" {
		t.Fatalf("receipt %q err=%v", digest, err)
	}
}
