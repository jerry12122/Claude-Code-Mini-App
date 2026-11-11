package db

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestAttachmentHistoryQueueAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "attachments.db")
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s, err := database.CreateSession("files", "", t.TempDir(), "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"first", "second"} {
		if err := database.AddAttachment(&Attachment{ID: id, SessionID: s.ID, Name: id + "報告.txt", StorageName: id + ".txt", MimeType: "text/plain", Size: 123}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.AddUserMessageWithAttachments(s.ID, "read these", "", []string{"second", "first"}); err != nil {
		t.Fatal(err)
	}
	q, err := database.EnqueueMessage(s.ID, "", "", "first")
	if err != nil {
		t.Fatal(err)
	}
	database.Close()
	database, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	messages, err := database.ListMessages(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Content != "read these" || len(messages[0].Attachments) != 2 || messages[0].Attachments[0].ID != "second" {
		t.Fatalf("history=%+v", messages)
	}
	encoded, err := json.Marshal(messages)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "storage_name") || strings.Contains(string(encoded), "attachment_ids") {
		t.Fatalf("private metadata leaked: %s", encoded)
	}
	queue, err := database.ListQueuedMessages(s.ID)
	if err != nil || len(queue) != 1 || len(queue[0].Attachments) != 1 || queue[0].Attachments[0].Name != "first報告.txt" {
		t.Fatalf("queue=%+v err=%v", queue, err)
	}
	m, err := database.PromoteQueuedMessage(s.ID, queue[0])
	if err != nil || m.Content != "" || len(m.Attachments) != 1 {
		t.Fatalf("promoted=%+v err=%v", m, err)
	}
	if _, err := database.PromoteQueuedMessage(s.ID, *q); err == nil {
		t.Fatal("queue item was promoted twice")
	}
	queue, _ = database.ListQueuedMessages(s.ID)
	if len(queue) != 0 {
		t.Fatal("promoted queue item remains")
	}
	if err := database.DeleteSession(s.ID); err != nil {
		t.Fatal(err)
	}
	all, _ := database.AttachmentMap(s.ID)
	if len(all) != 0 {
		t.Fatal("deleted session retained attachment metadata")
	}
}

func TestAttachmentRejectsWrongSessionAndRollsBackPromotion(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "attachments.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	s, _ := database.CreateSession("files", "", t.TempDir(), "default", "claude", nil, "agent")
	if err := database.AddAttachment(&Attachment{ID: "file", SessionID: s.ID, Name: "a.txt", StorageName: "random.txt", MimeType: "text/plain", Size: 1}); err != nil {
		t.Fatal(err)
	}
	for _, ids := range [][]string{{"unknown"}, {"file", "file"}} {
		if _, err := database.AddUserMessageWithAttachments(s.ID, "text", "", ids); err == nil {
			t.Fatalf("accepted invalid IDs: %v", ids)
		}
	}
	if _, err := database.EnqueueMessage("other-session", "text", "", "file"); err == nil {
		t.Fatal("accepted cross-session attachment")
	}
	q, err := database.EnqueueMessage(s.ID, "queued", "", "file")
	if err != nil {
		t.Fatal(err)
	}
	// A failed history insert must leave the original queue item intact.
	if _, err := database.Exec(`CREATE TRIGGER fail_message BEFORE INSERT ON messages BEGIN SELECT RAISE(ABORT, 'simulated disk failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.PromoteQueuedMessage(s.ID, *q); err == nil {
		t.Fatal("expected persistence failure")
	}
	items, err := database.ListQueuedMessages(s.ID)
	if err != nil || len(items) != 1 || items[0].ID != q.ID {
		t.Fatalf("queue lost on failure: %+v %v", items, err)
	}
}

// Unrelated metadata must not be scanned when resolving one file or one page.
func TestAttachmentQueriesOnlyReadRequestedRows(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	_, err = database.Exec(`INSERT INTO attachments(id,session_id,original_name,storage_name,mime_type,size) VALUES ('good','s','good','good','text/plain',1),('unrelated','s','bad','bad','text/plain','invalid-size')`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.GetAttachment("s", "good"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.GetAttachment("other", "good"); err == nil {
		t.Fatal("cross-session file accepted")
	}
	if _, err := database.AddUserMessageWithAttachments("s", "file", "", []string{"good"}); err != nil {
		t.Fatal(err)
	}
	rows, err := database.ListMessagesQuery(MessageQuery{SessionID: "s", Limit: 1})
	if err != nil || len(rows) != 1 || len(rows[0].Attachments) != 1 {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
}

func BenchmarkGetAttachment(b *testing.B) {
	database, err := Open(":memory:")
	if err != nil {
		b.Fatal(err)
	}
	defer database.Close()
	_, err = database.Exec(`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<4999)
 INSERT INTO attachments(id,session_id,original_name,storage_name,mime_type,size)
 SELECT 'a'||i,'s','file','file','text/plain',1 FROM n`)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := database.GetAttachment("s", "a0"); err != nil {
			b.Fatal(err)
		}
	}
}
