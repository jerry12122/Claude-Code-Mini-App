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

func TestGuestSessionMembershipHidden(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/guest-member.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	sess, err := database.CreateSession("g", "", "A", "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE sessions SET last_active='2020-01-02 03:04:05' WHERE id=?`, sess.ID); err != nil {
		t.Fatal(err)
	}
	cat, err := database.CreateSessionCategory("A", "Review")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.SetSessionCategory(sess.ID, &cat.ID); err != nil {
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
	sh := api.NewSessionHandler(database)
	shareH := api.NewShareHandler(database, api.ShareHooks{}, os.DirFS("."))
	app.Get("/sessions", auth, sh.List)
	app.Patch("/sessions/:id", auth, sh.Patch)
	app.Get("/guest/me", auth, shareH.Me)

	code, body := guestCategoryDo(t, app, "GET", "/sessions", ga.Guest.Token, "")
	if code != 403 || strings.Contains(body, cat.ID) || strings.Contains(body, "category_id") {
		t.Fatalf("guest list %d %s", code, body)
	}
	code, body = guestCategoryDo(t, app, "PATCH", "/sessions/"+sess.ID, ga.Guest.Token, `{"category_id":null}`)
	if code != 403 || strings.Contains(body, cat.ID) {
		t.Fatalf("guest patch %d %s", code, body)
	}
	var still string
	var lastActive string
	if err := database.QueryRow(`SELECT category_id, last_active FROM sessions WHERE id=?`, sess.ID).Scan(&still, &lastActive); err != nil || still != cat.ID || lastActive != "2020-01-02 03:04:05" {
		t.Fatalf("訪客 PATCH 改到 category=%s active=%s err=%v", still, lastActive, err)
	}
	code, body = guestCategoryDo(t, app, "GET", "/guest/me", ga.Guest.Token, "")
	if code != 200 || strings.Contains(body, "category_id") || strings.Contains(body, cat.ID) {
		t.Fatalf("guest me %d %s", code, body)
	}
}
