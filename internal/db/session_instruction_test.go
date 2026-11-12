package db

import (
	"path/filepath"
	"testing"
)

func TestSessionInstructionMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s, err := db.CreateSession("old", "", "", "", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`ALTER TABLE sessions DROP COLUMN system_instruction`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var value string
	if err := db.QueryRow(`SELECT system_instruction FROM sessions WHERE id = ?`, s.ID).Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != "" {
		t.Fatalf("migration instruction = %q", value)
	}
}

func TestSessionInstructionRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roundtrip.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	want := "回答使用繁體中文\n路徑 C:\\work \"quoted\""
	s, err := db.CreateSessionWithInstruction("new", "", "", "", "claude", nil, "agent", "", "  "+want+"\u3000")
	if err != nil {
		t.Fatal(err)
	}
	if s.SystemInstruction != want {
		t.Fatalf("create = %q", s.SystemInstruction)
	}
	db.Close()
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s, err = db.GetSession(s.ID)
	if err != nil || s.SystemInstruction != want {
		t.Fatalf("get = %+v, %v", s, err)
	}
	list, err := db.ListSessions()
	if err != nil || len(list) != 1 || list[0].SystemInstruction != want {
		t.Fatalf("list = %+v, %v", list, err)
	}
}

func TestSessionInstructionCreateFailure(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "failure.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TRIGGER fail_instruction BEFORE INSERT ON sessions WHEN NEW.system_instruction != '' BEGIN SELECT RAISE(ABORT, 'storage failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.CreateSessionWithInstruction("new", "", "", "", "claude", nil, "agent", "", "role"); err == nil {
		t.Fatal("expected storage error")
	}
	list, err := db.ListSessions()
	if err != nil || len(list) != 0 {
		t.Fatalf("partial session = %+v, %v", list, err)
	}
}
