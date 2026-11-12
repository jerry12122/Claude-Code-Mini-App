package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

func TestShareDoesNotExposeInstruction(t *testing.T) {
	e := newGuestEnv(t)
	const instruction = "private-role-原文"
	s, err := e.database.CreateSessionWithInstruction("shared", "", "", "default", "claude", nil, "agent", "", instruction)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.database.AddMessage(s.ID, "user", "ordinary message", ""); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{db.ShareModeLive, db.ShareModeSnapshot} {
		t.Run(mode, func(t *testing.T) {
			token, _ := e.guest(s.ID, db.ShareRoleViewer, mode, "reader")
			for _, path := range []string{"/guest/me", "/sessions/" + s.ID + "/messages"} {
				code, body := e.do(http.MethodGet, path, token, nil, nil)
				if code != 200 {
					t.Fatalf("%s status=%d: %s", path, code, body)
				}
				if strings.Contains(string(body), instruction) || strings.Contains(string(body), "system_instruction") {
					t.Fatalf("share exposed instruction: %s", body)
				}
				if strings.HasSuffix(path, "/messages") && !strings.Contains(string(body), "ordinary message") {
					t.Fatalf("missing ordinary history: %s", body)
				}
			}
			if code := e.status(http.MethodGet, "/sessions", token); code != 403 {
				t.Fatalf("guest owner metadata status=%d", code)
			}
		})
	}
	code, body := e.do(http.MethodGet, "/sessions", "", nil, nil)
	var sessions []db.Session
	if code != 200 || json.Unmarshal(body, &sessions) != nil || len(sessions) != 1 || sessions[0].SystemInstruction != instruction {
		t.Fatalf("owner cannot read instruction: %d %s", code, body)
	}
	msgs, err := e.database.ListMessages(s.ID)
	if err != nil || len(msgs) != 1 || msgs[0].Content != "ordinary message" {
		t.Fatalf("synthetic message added: %+v %v", msgs, err)
	}
}
