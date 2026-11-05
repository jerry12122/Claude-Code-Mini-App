package ws

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fasthttp/websocket"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/media"
)

func TestAttachmentInputQueueAndSync(t *testing.T) {
	database, wsURL := startTestServer(t)
	old := media.WorkspaceDir
	media.WorkspaceDir = t.TempDir()
	t.Cleanup(func() { media.WorkspaceDir = old })
	s, err := database.CreateSession("attachments", "", t.TempDir(), "default", "fakeq", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	p, err := media.SaveUpload(s.ID, "report.txt", strings.NewReader("report data"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AddAttachment(&db.Attachment{ID: "file", SessionID: s.ID, Name: "report.txt", StorageName: filepath.Base(p), MimeType: "text/plain", Size: 11}); err != nil {
		t.Fatal(err)
	}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(s.ID), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	events := make(chan serverMsg, 100)
	go func() {
		for {
			var msg serverMsg
			if conn.ReadJSON(&msg) != nil {
				return
			}
			events <- msg
		}
	}()
	waitEvent := func(kind, requestID string) serverMsg {
		t.Helper()
		timer := time.NewTimer(4 * time.Second)
		defer timer.Stop()
		for {
			select {
			case msg := <-events:
				if msg.Type == kind && (requestID == "" || msg.RequestID == requestID) {
					return msg
				}
			case <-timer.C:
				t.Fatalf("missing event %s/%s", kind, requestID)
				return serverMsg{}
			}
		}
	}
	write := func(msg clientMsg) {
		t.Helper()
		if err := conn.WriteJSON(msg); err != nil {
			t.Fatal(err)
		}
	}
	waitEvent("sync", "")
	write(clientMsg{Type: "input", RequestID: "invalid", Data: "bad", AttachmentIDs: []string{"unknown"}})
	waitEvent("input_rejected", "invalid")
	if messages, _ := database.ListMessages(s.ID); len(messages) != 0 {
		t.Fatal("invalid input was persisted")
	}

	// Attachments-only messages remain empty text in history, with paths only in the runner prompt.
	write(clientMsg{Type: "input", RequestID: "direct", AttachmentIDs: []string{"file"}})
	waitEvent("input_accepted", "direct")
	msg := waitEvent("user_message", "")
	if msg.Content != "" || msg.ID == 0 || msg.CreatedAt == "" || len(msg.Attachments) != 1 || msg.Attachments[0].Name != "report.txt" {
		t.Fatalf("message=%+v", msg)
	}
	waitFor(t, "direct attachment run", func() bool { return !taskIsActive(s.ID) && strings.Contains(lastReply(database, s.ID), p) })

	write(clientMsg{Type: "input", Data: "block-attachments"})
	waitFor(t, "blocked run", func() bool { return taskIsActive(s.ID) })
	write(clientMsg{Type: "input", RequestID: "queued", Data: "read report", AttachmentIDs: []string{"file"}})
	waitEvent("input_accepted", "queued")
	queue, _ := database.ListQueuedMessages(s.ID)
	if len(queue) != 1 || queue[0].Content != "read report" || len(queue[0].Attachments) != 1 {
		t.Fatalf("queue=%+v", queue)
	}
	fakeGate <- nil
	waitFor(t, "queued attachment run", func() bool {
		return queueLen(database, s.ID) == 0 && !taskIsActive(s.ID) && strings.Contains(lastReply(database, s.ID), p) && strings.Contains(lastReply(database, s.ID), "read report")
	})
	users := userMessages(database, s.ID)
	if len(users) != 3 || users[0] != "" || users[2] != "read report" {
		t.Fatalf("user text contains prompt paths: %v", users)
	}

	sync, err := buildSyncPayload(database, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	var history []db.Message
	if err := json.Unmarshal(sync.Messages, &history); err != nil {
		t.Fatal(err)
	}
	if len(history[0].Attachments) != 1 || history[0].Content != "" {
		t.Fatalf("sync lost attachments: %+v", history[0])
	}

	// A file disappearing while queued must pause without losing the item.
	write(clientMsg{Type: "input", Data: "block-missing-file"})
	waitFor(t, "second blocked run", func() bool { return taskIsActive(s.ID) })
	write(clientMsg{Type: "input", RequestID: "missing-later", Data: "keep this queued", AttachmentIDs: []string{"file"}})
	waitEvent("input_accepted", "missing-later")
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	fakeGate <- nil
	waitFor(t, "paused missing attachment", func() bool {
		paused, _ := database.QueuePaused(s.ID)
		return paused && !taskIsActive(s.ID) && queueLen(database, s.ID) == 1
	})
	write(clientMsg{Type: "input", RequestID: "missing-now", AttachmentIDs: []string{"file"}})
	waitEvent("input_rejected", "missing-now")
}
