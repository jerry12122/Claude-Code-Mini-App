package db

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func newFailureTestSession(t *testing.T, path string) (*DB, *Session) {
	t.Helper()
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s, err := database.CreateSession("t", "", "/tmp/f", "default", "codex", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	return database, s
}

func TestSessionFailure_SetOverwriteClear(t *testing.T) {
	database, s := newFailureTestSession(t, t.TempDir()+"/f.db")
	defer database.Close()

	if s.LastErrorText != "" || s.LastErrorAt != "" {
		t.Fatalf("新 session 不應有失敗紀錄: %+v", s)
	}

	if err := database.SetSessionLastError(s.ID, "first"); err != nil {
		t.Fatal(err)
	}
	got, _ := database.GetSession(s.ID)
	if got.LastErrorText != "first" || got.LastErrorAt == "" {
		t.Fatalf("寫入後應有文字與時間: %+v", got)
	}

	if err := database.SetSessionLastError(s.ID, "second"); err != nil {
		t.Fatal(err)
	}
	got, _ = database.GetSession(s.ID)
	if got.LastErrorText != "second" {
		t.Fatalf("新失敗應覆寫舊的: %q", got.LastErrorText)
	}

	if err := database.ClearSessionLastError(s.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = database.GetSession(s.ID)
	if got.LastErrorText != "" || got.LastErrorAt != "" {
		t.Fatalf("清除後應為空: %+v", got)
	}
}

func TestSessionFailure_TruncatesTo4000Runes(t *testing.T) {
	database, s := newFailureTestSession(t, t.TempDir()+"/f.db")
	defer database.Close()

	// 用多位元組字元，確認是以字元（rune）而不是 byte 截斷，且不會切壞 UTF-8。
	if err := database.SetSessionLastError(s.ID, strings.Repeat("額", 4500)); err != nil {
		t.Fatal(err)
	}
	got, _ := database.GetSession(s.ID)
	if n := utf8.RuneCountInString(got.LastErrorText); n != 4000 {
		t.Fatalf("應截斷為 4000 字元，實際 %d", n)
	}
	if !utf8.ValidString(got.LastErrorText) {
		t.Fatal("截斷後不是合法 UTF-8")
	}
}

func TestSessionFailure_SurvivesReopen(t *testing.T) {
	path := t.TempDir() + "/f.db"
	database, s := newFailureTestSession(t, path)
	if err := database.SetSessionLastError(s.ID, "usage limit"); err != nil {
		t.Fatal(err)
	}
	database.Close()

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := reopened.GetSession(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastErrorText != "usage limit" || got.LastErrorAt == "" {
		t.Fatalf("重開資料庫後失敗紀錄應仍在: %+v", got)
	}

	list, err := reopened.ListSessions()
	if err != nil || len(list) != 1 || list[0].LastErrorText != "usage limit" {
		t.Fatalf("ListSessions 也應帶出失敗紀錄: %+v err=%v", list, err)
	}
}
