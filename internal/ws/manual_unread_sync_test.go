package ws

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

func TestSyncPayloadOmitsManualUnread(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/sync-marker.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	sess, err := database.CreateSession("sync", "", t.TempDir(), "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE sessions SET manual_unread=1, manual_unread_revision=4 WHERE id=?`, sess.ID); err != nil {
		t.Fatalf("sync 測試需要已持久的 marker 欄，現況沒有：%v", err)
	}
	payload, err := buildSyncPayload(database, sess.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(serverMsg{
		Type:      "sync",
		Value:     payload.UIState,
		Messages:  payload.Messages,
		InputMode: payload.InputMode,
		ShellType: payload.ShellType,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "manual_unread") || strings.Contains(string(raw), "read_message_ids") {
		t.Fatalf("guest sync 帶出 marker 或讀取快照：%s", raw)
	}
}
