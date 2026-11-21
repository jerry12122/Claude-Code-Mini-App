package ws

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/media"
)

func TestIdleDrainDispatchesPhotoOwnership(t *testing.T) {
	database, s := openDeliverySession(t)
	old := media.WorkspaceDir
	media.WorkspaceDir = t.TempDir()
	t.Cleanup(func() { media.WorkspaceDir = old })
	writeUpload(t, s.ID, "a.png", "a")
	writeUpload(t, s.ID, "b.png", "b")
	writeUpload(t, s.ID, "c.png", "c")
	writeUpload(t, s.ID, "note.txt", "n")
	addUpload(t, database, s.ID, "img-a", "photo.png", "a.png", "image/png")
	addUpload(t, database, s.ID, "img-b", "photo.png", "b.png", "image/png")
	addUpload(t, database, s.ID, "img-c", "photo.png", "c.png", "image/png")
	addUpload(t, database, s.ID, "file-d", "note.txt", "note.txt", "text/plain")
	if _, err := database.PutGeneralSettings([]byte(`{"queueDelivery":"batch"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.EnqueueMessage(s.ID, "訊息1", "", "img-a", "img-b"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.EnqueueMessage(s.ID, "純文字", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := database.EnqueueMessage(s.ID, "訊息3", "", "img-c", "file-d"); err != nil {
		t.Fatal(err)
	}
	var prompt string
	var calls int
	d := deliveryDispatcher(database, s.ID, func(msg clientMsg, q *db.QueuedMessage) error {
		calls++
		rows, err := database.ListQueuedMessages(s.ID)
		if err != nil {
			return err
		}
		var batch []db.QueuedMessage
		for _, row := range rows {
			if row.BatchID == q.BatchID {
				batch = append(batch, row)
			}
		}
		prompt, err = batchDispatchPrompt(database, s.ID, batch)
		return err
	})
	d.drainLocked(true)
	if calls != 1 || !strings.Contains(prompt, "訊息1\n訊息1\n") || !strings.Contains(prompt, "照片1-1 id=img-a") || !strings.Contains(prompt, "照片1-2 id=img-b") {
		t.Fatalf("第一則照片應跟第一則: calls=%d %q", calls, prompt)
	}
	if strings.Contains(prompt, "照片2-") || !strings.Contains(prompt, "訊息2\n純文字\n") || !strings.Contains(prompt, "照片3-1 id=img-c") || !strings.Contains(prompt, "附件3-1 id=file-d") || strings.Contains(prompt, "照片3-2") {
		t.Fatalf("無圖不可位移，一般檔案不可標成照片: %q", prompt)
	}
	if strings.Count(prompt, "a.png") != 1 || strings.Count(prompt, "b.png") != 1 || strings.Count(prompt, "c.png") != 1 {
		t.Fatalf("路徑應各出現一次、不複製: %q", prompt)
	}
	if _, err := database.EnqueueMessage(s.ID, "晚到圖", "", "img-c"); err != nil {
		t.Fatal(err)
	}
	rows, _ := database.ListQueuedMessages(s.ID)
	if len(rows) != 4 || rows[3].BatchID != "" || strings.Contains(prompt, "晚到圖") {
		t.Fatalf("晚到不可入舊快照: %+v", rows)
	}
	if _, err := database.PutGeneralSettings([]byte(`{"queueDelivery":"each"}`)); err != nil {
		t.Fatal(err)
	}
	d.drainLocked(true)
	rows, _ = database.ListQueuedMessages(s.ID)
	if calls != 2 || rows[0].BatchID == "" || rows[3].BatchID != "" {
		t.Fatalf("切回逐筆不可拆舊批或吃進晚到: calls=%d %+v", calls, rows)
	}

	paused, err := database.CreateSession("paused", "", t.TempDir(), "default", "codex", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.EnqueueMessage(paused.ID, "停", ""); err != nil || database.SetQueuePaused(paused.ID, true) != nil {
		t.Fatal(err)
	}
	started := false
	deliveryDispatcher(database, paused.ID, func(clientMsg, *db.QueuedMessage) error { started = true; return nil }).drainLocked(true)
	if started {
		t.Fatal("暫停不可送出")
	}

	failed, err := database.CreateSession("failed", "", t.TempDir(), "default", "codex", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.PutGeneralSettings([]byte(`{"queueDelivery":"batch"}`)); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"甲", "乙"} {
		if _, err := database.EnqueueMessage(failed.ID, text, ""); err != nil {
			t.Fatal(err)
		}
	}
	deliveryDispatcher(database, failed.ID, func(clientMsg, *db.QueuedMessage) error { return os.ErrClosed }).drainLocked(true)
	left, _ := database.ListQueuedMessages(failed.ID)
	on, _ := database.QueuePaused(failed.ID)
	if !on || len(left) != 2 || left[0].BatchID == "" || left[1].BatchID != left[0].BatchID || left[0].Content != "甲" {
		t.Fatalf("失敗應暫停且保留原文與批次: paused=%v %+v", on, left)
	}
	again := 0
	deliveryDispatcher(database, failed.ID, func(clientMsg, *db.QueuedMessage) error { again++; return nil }).drainLocked(true)
	if again != 0 {
		t.Fatal("失敗後不可自動重送")
	}
	over, err := database.CreateSession("over", "", t.TempDir(), "default", "codex", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 101; i++ {
		if _, err := database.EnqueueMessage(over.ID, "x", ""); err != nil {
			t.Fatal(err)
		}
	}
	notice := ""
	overDispatcher := deliveryDispatcher(database, over.ID, func(clientMsg, *db.QueuedMessage) error {
		t.Fatal("超限不可送出")
		return nil
	})
	overDispatcher.broadcast = func(m serverMsg) { notice = m.Content }
	overDispatcher.drainLocked(true)
	overRows, _ := database.ListQueuedMessages(over.ID)
	overPaused, _ := database.QueuePaused(over.ID)
	if !overPaused || len(overRows) != 101 || overRows[0].BatchID != "" || !strings.Contains(notice, "100") {
		t.Fatalf("超限應暫停且不消耗: paused=%v notice=%q rows=%d", overPaused, notice, len(overRows))
	}
}

func openDeliverySession(t *testing.T) (*db.DB, *db.Session) {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/delivery.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	s, err := database.CreateSession("delivery", "", t.TempDir(), "default", "codex", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	return database, s
}

func deliveryDispatcher(database *db.DB, sessionID string, start func(clientMsg, *db.QueuedMessage) error) *sessionDispatcher {
	return &sessionDispatcher{database: database, sessionID: sessionID, start: start, reply: func(serverMsg) bool { return true }, broadcastQueue: func() {}, broadcast: func(serverMsg) {}}
}

func writeUpload(t *testing.T, sessionID, name, body string) {
	t.Helper()
	dir := filepath.Join(media.WorkspaceDir, "uploads", sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAutomaticDispatchPhotos(t *testing.T) {
	t.Setenv("MINIAPP_TEST_LOOPBACK", "1")
	x, dial := newRunNowWSRig(t)
	base := x.file(t)
	for _, id := range []string{"img-a", "img-b", "img-c"} {
		name := id + ".png"
		if err := os.WriteFile(filepath.Join(filepath.Dir(base), name), []byte("synthetic"), 0o600); err != nil {
			t.Fatal(err)
		}
		addUpload(t, x.database, x.s.ID, id, "same.png", name, "image/png")
	}
	if _, err := x.database.PutGeneralSettings([]byte(`{"queueDelivery":"batch"}`)); err != nil {
		t.Fatal(err)
	}
	owner := dialClient(t, dial(x.s.ID, ""))
	owner.barrier(t)
	a := x.plan("block")
	owner.send(t, map[string]any{"type": "input", "data": "running", "request_id": "pm159-running"})
	readyRunNow(t, a)
	for i, txt := range []string{"one", "no photo", "three"} {
		ids := []string{}
		if i == 0 {
			ids = []string{"img-a", "img-b"}
		}
		if i == 2 {
			ids = []string{"run-now-file", "img-c"}
		}
		owner.send(t, map[string]any{"type": "input", "data": txt, "request_id": "pm159-" + txt, "attachment_ids": ids})
	}
	owner.barrier(t)
	before := x.queue(t)
	if len(before) != 3 {
		t.Fatalf("queue=%+v", before)
	}
	for _, q := range before {
		if q.BatchID != "" {
			t.Fatal("snapshot created while running")
		}
	}
	batch := x.plan("block")
	a.release()
	readyRunNow(t, batch)
	prompt := batch.opts.Prompt
	for _, text := range []string{"訊息1\none\n", "照片1-1 id=img-a", "照片1-2 id=img-b", "訊息2\nno photo\n", "訊息3\nthree\n", "附件3-1 id=run-now-file", "照片3-1 id=img-c"} {
		if !strings.Contains(prompt, text) {
			t.Fatalf("missing %q: %s", text, prompt)
		}
	}
	if strings.Contains(prompt, "照片2-") {
		t.Fatal("text-only item shifted photo ownership")
	}
	if strings.Index(prompt, "附件3-1") > strings.Index(prompt, "照片3-1") {
		t.Fatal("attachment order changed")
	}
	history, err := x.database.ListMessages(x.s.ID)
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, m := range history {
		if m.Role != "user" {
			continue
		}
		texts = append(texts, m.Content)
		if m.Content == "one" && (len(m.Attachments) != 2 || m.Attachments[0].ID != "img-a" || m.Attachments[1].ID != "img-b") {
			t.Fatalf("history mapping %+v", m)
		}
	}
	if strings.Join(texts, "|") != "running|one|no photo|three" {
		t.Fatalf("history=%+v", texts)
	}
	if len(x.queue(t)) != 0 {
		t.Fatal("batch not promoted")
	}
	batch.release()
}

func TestAutoDrainUnsupportedSourceStaysFIFO(t *testing.T) {
	for _, typ := range []string{"shell", "telegram", "kiro"} {
		t.Run(typ, func(t *testing.T) {
			database, err := db.Open(t.TempDir() + "/source.db")
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			mode, agentType := "agent", "codex"
			switch typ {
			case "shell":
				mode = "shell"
			case "telegram":
				agentType = "telegram"
			default:
				agentType = "kiro"
			}
			s, err := database.CreateSession("unsupported", "", t.TempDir(), "default", agentType, nil, mode)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = database.PutGeneralSettings([]byte(`{"queueDelivery":"batch"}`)); err != nil {
				t.Fatal(err)
			}
			if err := database.AddAttachment(&db.Attachment{ID: "keep-file", SessionID: s.ID, Name: "note.txt", StorageName: "note.txt", MimeType: "text/plain", Size: 1}); err != nil {
				t.Fatal(err)
			}
			if _, err = database.EnqueueMessage(s.ID, "first", "", "keep-file"); err != nil {
				t.Fatal(err)
			}
			if _, err = database.EnqueueMessage(s.ID, "second", ""); err != nil {
				t.Fatal(err)
			}
			var seen db.QueuedMessage
			d := deliveryDispatcher(database, s.ID, func(_ clientMsg, q *db.QueuedMessage) error {
				seen = *q
				return nil
			})
			d.drain(true)
			if seen.BatchID != "" || seen.Content != "first" || len(seen.AttachmentIDs) != 1 || seen.AttachmentIDs[0] != "keep-file" {
				t.Fatalf("production drain bypassed existing source guard: %s; batch=%s content=%q ids=%v", typ, seen.BatchID, seen.Content, seen.AttachmentIDs)
			}
			rows, err := database.ListQueuedMessages(s.ID)
			if err != nil || len(rows) != 2 || rows[0].BatchID != "" || rows[1].BatchID != "" || rows[1].Content != "second" || len(rows[0].AttachmentIDs) != 1 {
				t.Fatalf("非支援來源應留原逐筆與附件: %+v %v", rows, err)
			}
		})
	}
}

func addUpload(t *testing.T, database *db.DB, sessionID, id, name, storage, mime string) {
	t.Helper()
	if err := database.AddAttachment(&db.Attachment{ID: id, SessionID: sessionID, Name: name, StorageName: storage, MimeType: mime, Size: 1}); err != nil {
		t.Fatal(err)
	}
}
