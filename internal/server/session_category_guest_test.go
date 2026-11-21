package server

import (
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/api"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

// 只掛訪客分支與分類路由，用 Fiber.App.Test，不 Listen、不叫 Start。
func TestGuestCannotReadOrWriteSessionCategories(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/guest-cat.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	sess, err := database.CreateSession("g", "", "A", "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := database.CreateSessionCategory("A", "SecretCat")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE sessions SET category_id=? WHERE id=?`, cat.ID, sess.ID); err != nil {
		t.Fatal(err)
	}
	share, err := database.CreateShare(sess.ID, db.ShareRoleEditor, db.ShareModeLive, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ga, err := database.TryJoin(share.Token, share.PIN, "訪客")
	if err != nil {
		t.Fatal(err)
	}

	app := fiber.New()
	auth := func(c *fiber.Ctx) error {
		if tok := guestToken(c); tok != "" {
			return guestAuth(database, c, tok)
		}
		return c.Next()
	}
	h := api.NewSessionCategoryHandler(database)
	app.Get("/session-categories", auth, h.List)
	app.Post("/session-categories", auth, h.Create)
	app.Patch("/session-categories/:id", auth, h.Rename)
	app.Delete("/session-categories/:id", auth, h.Delete)

	calls := []struct{ method, path, body string }{
		{"GET", "/session-categories?work_dir=A", ""},
		{"POST", "/session-categories", `{"name":"Nope","work_dir":"A"}`},
		{"PATCH", "/session-categories/" + cat.ID, `{"name":"Hijack"}`},
		{"DELETE", "/session-categories/" + cat.ID, ""},
	}
	for _, call := range calls {
		code, body := guestCategoryDo(t, app, call.method, call.path, ga.Guest.Token, call.body)
		if code != 403 || !strings.Contains(body, "share_forbidden") || strings.Contains(body, "SecretCat") || strings.Contains(body, cat.ID) {
			t.Fatalf("guest %s %s = %d %s", call.method, call.path, code, body)
		}
	}
	code, body := guestCategoryDo(t, app, "GET", "/session-categories?work_dir=A", "not-a-token", "")
	if code != 401 || !strings.Contains(body, "share_invalid") || strings.Contains(body, "SecretCat") {
		t.Fatalf("壞 token %d %s", code, body)
	}
	got, err := database.GetSessionCategory(cat.ID)
	if err != nil || got.Name != "SecretCat" {
		t.Fatalf("訪客不該改到分類 %+v %v", got, err)
	}
	var still sql.NullString
	if err := database.QueryRow(`SELECT category_id FROM sessions WHERE id=?`, sess.ID).Scan(&still); err != nil || !still.Valid || still.String != cat.ID {
		t.Fatalf("訪客 DELETE 不該解綁 %+v %v", still, err)
	}
	var n int
	if err := database.QueryRow(`SELECT COUNT(*) FROM session_categories`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("分類列 %d %v", n, err)
	}
}

func TestSessionCategoryRoutesUseOwnerMiddleware(t *testing.T) {
	b, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, line := range []string{
		`app.Get("/session-categories", authMiddleware, ch.List)`,
		`app.Post("/session-categories", authMiddleware, ch.Create)`,
		`app.Patch("/session-categories/:id", authMiddleware, ch.Rename)`,
		`app.Delete("/session-categories/:id", authMiddleware, ch.Delete)`,
	} {
		if !strings.Contains(src, line) {
			t.Fatalf("server.go 缺少擁有者路由：%s", line)
		}
	}
	if strings.Contains(guestFile(t), "/session-categories") {
		t.Fatal("不該把分類路由加進 guest 白名單")
	}
}

func guestFile(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("guest.go")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func guestCategoryDo(t *testing.T, app *fiber.App, method, path, token, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if method == http.MethodPost || method == http.MethodPatch {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("X-Share-Token", token)
	}
	res, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	buf, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, string(buf)
}
