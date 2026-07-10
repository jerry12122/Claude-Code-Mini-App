package ws

import (
	"path/filepath"
	"testing"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

func TestSessionModelPayload_AntigravityNil(t *testing.T) {
	sess := &db.Session{AgentType: agent.TypeAntigravity}
	if sessionModelPayload(sess) != nil {
		t.Fatal("antigravity should not expose model payload")
	}
}

func TestSessionModelPayload_CursorDefault(t *testing.T) {
	sess := &db.Session{AgentType: agent.TypeCursor, CliExtraArgs: []string{}}
	p := sessionModelPayload(sess)
	if p == nil || p.DisplayText != "auto" {
		t.Fatalf("got %+v", p)
	}
}

func TestPersistModelUpdate(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	sess, err := database.CreateSession("m", "", "", "default", agent.TypeClaude, nil, "agent")
	if err != nil {
		t.Fatal(err)
	}

	p := persistModelUpdate(database, sess.ID, &agent.ModelSnapshot{
		Model:       "claude-sonnet-5",
		DisplayText: "claude-sonnet-5",
		Source:      "init_event",
	})
	if p.DisplayText != "claude-sonnet-5" || p.Source != "init_event" {
		t.Fatalf("got %+v", p)
	}

	updated, err := database.GetSession(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ActiveModel != "claude-sonnet-5" || updated.ActiveModelSource != "init_event" {
		t.Fatalf("db: %+v", updated)
	}
}
