package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

func TestSessionMembershipHTTP(t *testing.T) {
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
	catA, err := database.CreateSessionCategory("A", "Review")
	if err != nil {
		t.Fatal(err)
	}
	catB, err := database.CreateSessionCategory("B", "Review")
	if err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	h := NewSessionHandler(database)
	app.Get("/sessions", h.List)
	app.Patch("/sessions/:id", h.Patch)

	code, body := patchSession(t, app, sess.ID, `{"mark_read":false}`)
	if code != 200 {
		t.Fatalf("省略 category_id 的既有 PATCH %d %s", code, body)
	}
	if got := membershipByID(t, app, sess.ID); got.CategoryID != nil || notes != 0 {
		t.Fatalf("省略不該歸類或通知 %+v notes=%d", got.CategoryID, notes)
	}
	assertMembershipUntouched(t, database, sess.ID)

	code, body = patchSession(t, app, sess.ID, `{"category_id":"`+catA.ID+`","work_dir":"B"}`)
	if code != 200 {
		t.Fatalf("指派 A %d %s", code, body)
	}
	assigned := decodeMembership(t, body)
	if assigned.CategoryID == nil || *assigned.CategoryID != catA.ID || assigned.WorkDir != "A" || notes != 1 {
		t.Fatalf("指派結果 %+v notes=%d work=%s", assigned.CategoryID, notes, assigned.WorkDir)
	}
	assertMembershipUntouched(t, database, sess.ID)
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	reApp := fiber.New()
	reApp.Get("/sessions", NewSessionHandler(reopened).List)
	if got := membershipByID(t, reApp, sess.ID); got.CategoryID == nil || *got.CategoryID != catA.ID {
		t.Fatalf("重開後歸屬 %+v", got.CategoryID)
	}

	app = fiber.New()
	h = NewSessionHandler(reopened)
	reopened.OnSessionChange = func() { notes++ }
	app.Patch("/sessions/:id", h.Patch)
	app.Get("/sessions", h.List)
	code, body = patchSession(t, app, sess.ID, `{"category_id":null}`)
	cleared := decodeMembership(t, body)
	if code != 200 || cleared.CategoryID != nil || notes != 2 {
		t.Fatalf("解除 %d %+v notes=%d %s", code, cleared.CategoryID, notes, body)
	}
	code, body = patchSession(t, app, sess.ID, `{"category_id":"`+catB.ID+`"}`)
	if code != 400 || notes != 2 {
		t.Fatalf("跨目錄 %d notes=%d %s", code, notes, body)
	}
	if got := membershipByID(t, app, sess.ID); got.CategoryID != nil {
		t.Fatalf("跨目錄仍寫入 %+v", got.CategoryID)
	}
	code, _ = patchSession(t, app, sess.ID, `{"category_id":"`+catA.ID+`"}`)
	if code != 200 || notes != 3 {
		t.Fatalf("再次指派 %d notes=%d", code, notes)
	}
	if err := reopened.DeleteSessionCategory(catA.ID); err != nil {
		t.Fatal(err)
	}
	notesBefore := notes
	code, body = patchSession(t, app, sess.ID, `{"category_id":"`+catA.ID+`"}`)
	if code != 404 || notes != notesBefore {
		t.Fatalf("已刪 id %d notes=%d %s", code, notes, body)
	}
	if got := membershipByID(t, app, sess.ID); got.CategoryID != nil {
		t.Fatalf("已刪 id 仍留下 %+v", got.CategoryID)
	}
	for _, raw := range []string{`{"category_id":1}`, `{"category_id":true}`, `{"category_id":{}}`, `{"category_id":""}`} {
		code, body = patchSession(t, app, sess.ID, raw)
		if code != 400 || notes != notesBefore {
			t.Fatalf("壞 category_id %s → %d notes=%d %s", raw, code, notes, body)
		}
	}
	code, _ = patchSession(t, app, "missing", `{"category_id":"`+catB.ID+`"}`)
	if code != 404 {
		t.Fatalf("缺少 session %d", code)
	}
	assertMembershipUntouched(t, reopened, sess.ID)
	fresh, err := reopened.CreateSession("new", "", "A", "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	if got := membershipByID(t, app, fresh.ID); got.CategoryID != nil {
		t.Fatalf("新會話應未分類 %+v", got.CategoryID)
	}
}

func TestSessionMembershipMixedPatchRejectsBeforeWrite(t *testing.T) {
	for _, clear := range []bool{false, true} {
		label := "assign"
		if clear {
			label = "clear"
		}
		t.Run(label, func(t *testing.T) {
			database := testDB(t)
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
			cat, err := database.CreateSessionCategory("A", "Review")
			if err != nil {
				t.Fatal(err)
			}
			next := `"` + cat.ID + `"`
			if clear {
				if err := database.SetSessionCategory(sess.ID, &cat.ID); err != nil {
					t.Fatal(err)
				}
				next = "null"
			}
			notes := 0
			database.OnSessionChange = func() { notes++ }
			app := fiber.New()
			app.Patch("/sessions/:id", NewSessionHandler(database).Patch)
			code, body := patchSession(t, app, sess.ID, `{"name":" \t ","category_id":`+next+`}`)
			if code != 400 || !strings.Contains(string(body), "name 不可為空") {
				t.Fatalf("空白名稱應 400，got %d %s", code, body)
			}
			got, err := database.GetSession(sess.ID)
			if err != nil {
				t.Fatal(err)
			}
			same := got.CategoryID == nil
			if clear {
				same = got.CategoryID != nil && *got.CategoryID == cat.ID
			}
			if got.Name != "keep" || !same || notes != 0 {
				t.Fatalf("400 仍改歸屬或通知：clear=%v name=%s category=%v notes=%d", clear, got.Name, got.CategoryID, notes)
			}
			assertMembershipUntouched(t, database, sess.ID)
		})
	}

	t.Run("legal name", func(t *testing.T) {
		database := testDB(t)
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
		cat, err := database.CreateSessionCategory("A", "Review")
		if err != nil {
			t.Fatal(err)
		}
		notes := 0
		database.OnSessionChange = func() { notes++ }
		app := fiber.New()
		app.Patch("/sessions/:id", NewSessionHandler(database).Patch)
		code, body := patchSession(t, app, sess.ID, `{"name":" renamed ","category_id":"`+cat.ID+`"}`)
		if code != 200 {
			t.Fatalf("合法名稱與指派 %d %s", code, body)
		}
		got, err := database.GetSession(sess.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Name != "renamed" || got.CategoryID == nil || *got.CategoryID != cat.ID || notes != 1 {
			t.Fatalf("混合合法結果 name=%s category=%v notes=%d", got.Name, got.CategoryID, notes)
		}
		var status, workDir, content, queue, digest string
		var isRead int
		if err := database.QueryRow(`SELECT status, work_dir FROM sessions WHERE id=?`, sess.ID).Scan(&status, &workDir); err != nil {
			t.Fatal(err)
		}
		if status != "idle" || workDir != "A" {
			t.Fatalf("合法混合改到 status=%s dir=%s", status, workDir)
		}
		if err := database.QueryRow(`SELECT content, is_read FROM messages WHERE session_id=?`, sess.ID).Scan(&content, &isRead); err != nil || content != "hist" || isRead != 1 {
			t.Fatalf("歷史 %q read=%d err=%v", content, isRead, err)
		}
		if err := database.QueryRow(`SELECT content FROM queued_messages WHERE session_id=?`, sess.ID).Scan(&queue); err != nil || queue != "q" {
			t.Fatalf("queue %q %v", queue, err)
		}
		if err := database.QueryRow(`SELECT digest FROM input_receipts WHERE session_id=?`, sess.ID).Scan(&digest); err != nil || digest != "digest-1" {
			t.Fatalf("receipt %q %v", digest, err)
		}
	})
}

func TestSessionMembershipClosedDatabaseIs500(t *testing.T) {
	database := testDB(t)
	sess, err := database.CreateSession("s", "", "A", "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := database.CreateSessionCategory("A", "Review")
	if err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	app.Patch("/sessions/:id", NewSessionHandler(database).Patch)
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	code, body := patchSession(t, app, sess.ID, `{"category_id":"`+cat.ID+`"}`)
	if code != 500 || !strings.Contains(string(body), "分類資料庫錯誤") {
		t.Fatalf("關閉的 DB 應 500，got %d %s", code, body)
	}
}

func patchSession(t *testing.T, app *fiber.App, id, body string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, "/sessions/"+id, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
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

type membershipView struct {
	ID         string  `json:"id"`
	WorkDir    string  `json:"work_dir"`
	CategoryID *string `json:"category_id"`
	Status     string  `json:"status"`
}

func decodeMembership(t *testing.T, body []byte) membershipView {
	t.Helper()
	var s membershipView
	if err := json.Unmarshal(body, &s); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return s
}

func membershipByID(t *testing.T, app *fiber.App, id string) membershipView {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/sessions", nil)
	res, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 {
		t.Fatalf("list %d %s", res.StatusCode, b)
	}
	var list []membershipView
	if err := json.Unmarshal(b, &list); err != nil {
		t.Fatal(err)
	}
	for _, s := range list {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("list 沒有 %s：%s", id, b)
	return membershipView{}
}

func assertMembershipUntouched(t *testing.T, database *db.DB, id string) {
	t.Helper()
	var lastActive, status, workDir, content, queue, digest string
	var isRead int
	if err := database.QueryRow(`SELECT last_active, status, work_dir FROM sessions WHERE id=?`, id).Scan(&lastActive, &status, &workDir); err != nil {
		t.Fatal(err)
	}
	if lastActive != "2020-01-02 03:04:05" || status != "idle" || workDir != "A" {
		t.Fatalf("session 被改到 active=%s status=%s dir=%s", lastActive, status, workDir)
	}
	if err := database.QueryRow(`SELECT content, is_read FROM messages WHERE session_id=?`, id).Scan(&content, &isRead); err != nil || content != "hist" || isRead != 1 {
		t.Fatalf("歷史 %q read=%d err=%v", content, isRead, err)
	}
	if err := database.QueryRow(`SELECT content FROM queued_messages WHERE session_id=?`, id).Scan(&queue); err != nil || queue != "q" {
		t.Fatalf("queue %q %v", queue, err)
	}
	if err := database.QueryRow(`SELECT digest FROM input_receipts WHERE session_id=?`, id).Scan(&digest); err != nil || digest != "digest-1" {
		t.Fatalf("receipt %q %v", digest, err)
	}
}
