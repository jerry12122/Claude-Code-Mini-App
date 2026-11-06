package server

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

func TestShareClearEndedOwnerOnly(t *testing.T) {
	e := newGuestEnv(t)
	session := e.session("keep")
	token, share := e.guest(session.ID, db.ShareRoleViewer, db.ShareModeLive, "guest")
	path := fmt.Sprintf("/shares/ended/%d", share.ID)
	if code := e.status("DELETE", path, token); code != 403 {
		t.Fatalf("guest clear got %d", code)
	}
	if code := e.status("DELETE", "/shares/ended", token); code != 403 {
		t.Fatalf("guest clear all got %d", code)
	}
	if code := e.status("DELETE", path, ""); code != 409 {
		t.Fatalf("active clear got %d", code)
	}
	for _, id := range []string{"bad", "0", "-1"} {
		if code := e.status("DELETE", "/shares/ended/"+id, ""); code != 400 {
			t.Fatalf("id=%s got %d", id, code)
		}
	}
	if code := e.status("DELETE", fmt.Sprintf("/shares/%d", share.ID), ""); code != 204 {
		t.Fatalf("revoke changed: %d", code)
	}
	for _, want := range []string{`"cleared":1`, `"cleared":0`} {
		code, body := e.do("DELETE", path, "", nil, nil)
		if code != 200 || !strings.Contains(string(body), want) {
			t.Fatalf("clear got %d %s, want %s", code, body, want)
		}
	}
	if code := e.status("GET", "/guest/me", token); code != 401 {
		t.Fatalf("cleared guest got %d", code)
	}
	_, ended := e.guest(session.ID, db.ShareRoleViewer, db.ShareModeLive, "ended")
	if err := e.database.RevokeShare(ended.ID); err != nil {
		t.Fatal(err)
	}
	_, active := e.guest(session.ID, db.ShareRoleViewer, db.ShareModeLive, "active")
	code, body := e.do("DELETE", "/shares/ended", "", nil, nil)
	if code != 200 || !strings.Contains(string(body), `"cleared":1`) {
		t.Fatalf("clear all got %d %s", code, body)
	}
	if _, err := e.database.GetShare(active.ID); err != nil {
		t.Fatalf("active removed: %v", err)
	}
	if _, err := e.database.GetSession(session.ID); err != nil {
		t.Fatalf("chat removed: %v", err)
	}
}
