package server

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/api"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

func TestGuestManualUnreadHidden(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/guest-marker.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	sess, err := database.CreateSession("g", "", "A", "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE sessions SET last_active='2020-01-02 03:04:05', manual_unread=1, manual_unread_revision=3 WHERE id=?`, sess.ID); err != nil {
		t.Fatalf("既有庫還沒有可觀察的 marker 欄，guest 測試無法確認資料不變：%v", err)
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
	if _, err := database.Exec(`INSERT INTO messages (session_id, role, content, status, is_read) VALUES (?, 'claude', 'guest-unread', 'done', 0)`, sess.ID); err != nil {
		t.Fatal(err)
	}
	sh := api.NewSessionHandler(database)
	shareH := api.NewShareHandler(database, api.ShareHooks{}, os.DirFS("."))
	app.Get("/sessions", auth, sh.List)
	app.Patch("/sessions/:id", auth, sh.Patch)
	app.Post("/sessions/read-all", auth, sh.ReadAll)
	app.Get("/guest/me", auth, shareH.Me)

	code, body := guestCategoryDo(t, app, "GET", "/sessions", ga.Guest.Token, "")
	if code != 403 || strings.Contains(body, "manual_unread") || strings.Contains(body, "read_message_ids") {
		t.Fatalf("guest list %d %s", code, body)
	}
	for _, raw := range []string{
		`{"manual_unread":true}`,
		`{"manual_unread":false}`,
		`{"manual_unread":false,"manual_unread_revision":3}`,
	} {
		code, body = guestCategoryDo(t, app, "PATCH", "/sessions/"+sess.ID, ga.Guest.Token, raw)
		if code != 403 || strings.Contains(body, "manual_unread") {
			t.Fatalf("guest patch %s → %d %s", raw, code, body)
		}
	}
	code, body = guestCategoryDo(t, app, "POST", "/sessions/read-all", ga.Guest.Token, `{"sessions":[{"id":"`+sess.ID+`","manual_unread_revision":3,"read_message_ids":[]}]}`)
	if code != 403 || strings.Contains(body, "manual_unread") || strings.Contains(body, "read_message_ids") {
		t.Fatalf("guest read-all %d %s", code, body)
	}
	var manual, revision, unread int
	var lastActive string
	if err := database.QueryRow(`SELECT manual_unread, manual_unread_revision, last_active FROM sessions WHERE id=?`, sess.ID).Scan(&manual, &revision, &lastActive); err != nil || manual != 1 || revision != 3 || lastActive != "2020-01-02 03:04:05" {
		t.Fatalf("訪客改到 marker=%d rev=%d active=%s err=%v", manual, revision, lastActive, err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM messages WHERE session_id=? AND is_read=0`, sess.ID).Scan(&unread); err != nil || unread != 1 {
		t.Fatalf("訪客不該確認訊息，未讀 %d err=%v", unread, err)
	}
	code, body = guestCategoryDo(t, app, "GET", "/guest/me", ga.Guest.Token, "")
	if code != 200 || strings.Contains(body, "manual_unread") || strings.Contains(body, "read_message_ids") {
		t.Fatalf("guest me %d %s", code, body)
	}
}

func TestOwnerSessionPatchAuthIsSourceOnly(t *testing.T) {
	b, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `app.Patch("/sessions/:id", authMiddleware, sh.Patch)`) {
		t.Fatal("PATCH /sessions/:id 沒有留在既有 authMiddleware 後面")
	}
	if strings.Contains(guestFile(t), "manual_unread") {
		t.Fatal("不該把 marker 加進 guest 白名單")
	}
}
