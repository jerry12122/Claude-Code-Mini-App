package ws

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

func TestSyncPaginationKeepsLatestAndLegacyHistory(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	s, err := database.CreateSession("pages", "", "", "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	var lastID int64
	for i := 0; i < 3; i++ {
		m, err := database.AddUserMessageWithAttachments(s.ID, "message", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		lastID = m.ID
	}
	for _, limit := range []int{0, 2, 3} {
		payload, err := buildSyncPayload(database, s.ID, limit)
		if err != nil {
			t.Fatal(err)
		}
		var rows []db.Message
		if err := json.Unmarshal(payload.Messages, &rows); err != nil {
			t.Fatal(err)
		}
		want := 3
		if limit == 2 {
			want = 2
		}
		if len(rows) != want || rows[len(rows)-1].ID != lastID || payload.HistoryHasMore != (limit == 2) {
			t.Fatalf("limit=%d: rows=%+v hasMore=%v", limit, rows, payload.HistoryHasMore)
		}
	}
}
