package db

import "testing"

func TestSetSessionPinned(t *testing.T) {
	database, err := Open(t.TempDir() + "/pin.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	s, err := database.CreateSession("t", "", "", "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	if s.Pinned {
		t.Fatal("新 session 預設不應釘選")
	}

	if err := database.SetSessionPinned(s.ID, true); err != nil {
		t.Fatal(err)
	}
	got, _ := database.GetSession(s.ID)
	if !got.Pinned {
		t.Fatal("釘選後 Pinned 應為 true")
	}
	if got.LastActive != s.LastActive {
		t.Fatalf("釘選不應動到 last_active: before=%q after=%q", s.LastActive, got.LastActive)
	}
	list, _ := database.ListSessions()
	if len(list) != 1 || !list[0].Pinned {
		t.Fatal("ListSessions 應帶出 pinned")
	}

	if err := database.SetSessionPinned(s.ID, false); err != nil {
		t.Fatal(err)
	}
	got, _ = database.GetSession(s.ID)
	if got.Pinned {
		t.Fatal("取消釘選後 Pinned 應為 false")
	}
}
