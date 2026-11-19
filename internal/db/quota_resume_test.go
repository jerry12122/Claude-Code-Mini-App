package db

import (
	"database/sql"
	"sync"
	"testing"
	"time"
)

func openQuotaDB(t *testing.T) *DB {
	t.Helper()
	database, err := Open(t.TempDir() + "/quota.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func newQuotaSession(t *testing.T, database *DB, name string) *Session {
	t.Helper()
	s, err := database.CreateSession(name, "", t.TempDir(), "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func setQuotaResume(t *testing.T, database *DB, sessionID, state, at string, attempts int) {
	t.Helper()
	_, err := database.Exec(
		`UPDATE sessions SET quota_resume_state = ?, quota_resume_at = ?, quota_resume_attempts = ? WHERE id = ?`,
		state, at, attempts, sessionID,
	)
	if err != nil {
		t.Fatal(err)
	}
}

func requireQuotaResume(t *testing.T, database *DB, sessionID, state, at string, attempts int) {
	t.Helper()
	gotState, gotAt, gotAttempts, err := database.GetQuotaResume(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if gotState != state || gotAt != at || gotAttempts != attempts {
		t.Fatalf("session %s: got state=%q at=%q attempts=%d, want state=%q at=%q attempts=%d",
			sessionID, gotState, gotAt, gotAttempts, state, at, attempts)
	}
}

func TestQuotaResumeTransitions(t *testing.T) {
	database := openQuotaDB(t)
	const prevAt = "2026-11-18T09:00:00Z"
	const nextAt = "2026-11-18T10:00:00Z"

	allowed := []struct {
		name              string
		from, to, at      string
		restore           bool
		startAttempts     int
		wantState, wantAt string
		wantAttempts      int
	}{
		{"none→waiting", QuotaResumeNone, QuotaResumeWaiting, nextAt, false, 0, QuotaResumeWaiting, nextAt, 0},
		{"none→exhausted", QuotaResumeNone, QuotaResumeExhausted, "", false, 2, QuotaResumeExhausted, prevAt, 2},
		{"waiting→waiting", QuotaResumeWaiting, QuotaResumeWaiting, nextAt, false, 2, QuotaResumeWaiting, nextAt, 2},
		{"waiting→resuming", QuotaResumeWaiting, QuotaResumeResuming, "", false, 0, QuotaResumeResuming, prevAt, 1},
		{"resuming→waiting 保留次數", QuotaResumeResuming, QuotaResumeWaiting, nextAt, false, 2, QuotaResumeWaiting, nextAt, 2},
		{"resuming→exhausted", QuotaResumeResuming, QuotaResumeExhausted, "", false, 3, QuotaResumeExhausted, prevAt, 3},
		{"resuming→none", QuotaResumeResuming, QuotaResumeNone, "should-clear", false, 4, QuotaResumeNone, "", 0},
		{"waiting→none", QuotaResumeWaiting, QuotaResumeNone, nextAt, false, 4, QuotaResumeNone, "", 0},
		{"exhausted→none", QuotaResumeExhausted, QuotaResumeNone, "", false, 3, QuotaResumeNone, "", 0},
	}
	if len(allowed) != 9 {
		t.Fatalf("允許轉移應為 9 條，got %d", len(allowed))
	}
	for _, tc := range allowed {
		t.Run(tc.name, func(t *testing.T) {
			s := newQuotaSession(t, database, tc.name)
			setQuotaResume(t, database, s.ID, tc.from, prevAt, tc.startAttempts)
			ok, err := database.TransitionQuotaResume(s.ID, tc.from, tc.to, tc.at, tc.restore)
			if err != nil || !ok {
				t.Fatalf("ok=%v err=%v", ok, err)
			}
			requireQuotaResume(t, database, s.ID, tc.wantState, tc.wantAt, tc.wantAttempts)
		})
	}

	denied := []struct{ from, to string }{
		{QuotaResumeNone, QuotaResumeResuming},
		{QuotaResumeNone, QuotaResumeNone},
		{QuotaResumeExhausted, QuotaResumeWaiting},
		{QuotaResumeWaiting, QuotaResumeExhausted},
	}
	for _, tc := range denied {
		t.Run("拒絕 "+tc.from+"→"+tc.to, func(t *testing.T) {
			s := newQuotaSession(t, database, tc.from+"-"+tc.to)
			setQuotaResume(t, database, s.ID, tc.from, prevAt, 5)
			ok, err := database.TransitionQuotaResume(s.ID, tc.from, tc.to, nextAt, true)
			if err != nil || ok {
				t.Fatalf("表外轉移應回 false, nil，got ok=%v err=%v", ok, err)
			}
			requireQuotaResume(t, database, s.ID, tc.from, prevAt, 5)
		})
	}

	t.Run("from 與實際狀態不符", func(t *testing.T) {
		s := newQuotaSession(t, database, "mismatch")
		setQuotaResume(t, database, s.ID, QuotaResumeWaiting, prevAt, 2)
		ok, err := database.TransitionQuotaResume(s.ID, QuotaResumeNone, QuotaResumeWaiting, nextAt, false)
		if err != nil || ok {
			t.Fatalf("狀態不符應回 false, nil，got ok=%v err=%v", ok, err)
		}
		requireQuotaResume(t, database, s.ID, QuotaResumeWaiting, prevAt, 2)
	})
}

func TestQuotaResumeAttempts(t *testing.T) {
	database := openQuotaDB(t)
	s := newQuotaSession(t, database, "attempts")
	const first = "2026-11-18T10:00:00Z"
	const second = "2026-11-18T11:00:00Z"

	ok, err := database.TransitionQuotaResume(s.ID, QuotaResumeNone, QuotaResumeWaiting, first, false)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	ok, err = database.TransitionQuotaResume(s.ID, QuotaResumeWaiting, QuotaResumeResuming, "", false)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	requireQuotaResume(t, database, s.ID, QuotaResumeResuming, first, 1)

	ok, err = database.TransitionQuotaResume(s.ID, QuotaResumeResuming, QuotaResumeWaiting, second, false)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	requireQuotaResume(t, database, s.ID, QuotaResumeWaiting, second, 1)

	ok, err = database.TransitionQuotaResume(s.ID, QuotaResumeWaiting, QuotaResumeResuming, "", false)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	requireQuotaResume(t, database, s.ID, QuotaResumeResuming, second, 2)

	ok, err = database.TransitionQuotaResume(s.ID, QuotaResumeResuming, QuotaResumeWaiting, first, true)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	requireQuotaResume(t, database, s.ID, QuotaResumeWaiting, first, 1)

	ok, err = database.TransitionQuotaResume(s.ID, QuotaResumeWaiting, QuotaResumeNone, "ignored", false)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	requireQuotaResume(t, database, s.ID, QuotaResumeNone, "", 0)

	// 次數已是 0 時，投遞失敗還原不得變成負數。
	setQuotaResume(t, database, s.ID, QuotaResumeResuming, first, 0)
	ok, err = database.TransitionQuotaResume(s.ID, QuotaResumeResuming, QuotaResumeWaiting, second, true)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	requireQuotaResume(t, database, s.ID, QuotaResumeWaiting, second, 0)
}

func TestQuotaResumeConcurrent(t *testing.T) {
	database := openQuotaDB(t)
	s := newQuotaSession(t, database, "race")
	setQuotaResume(t, database, s.ID, QuotaResumeWaiting, "2026-11-18T10:00:00Z", 0)

	const n = 8
	var wg sync.WaitGroup
	start := make(chan struct{})
	oks := make([]bool, n)
	errs := make([]error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			oks[i], errs[i] = database.TransitionQuotaResume(s.ID, QuotaResumeWaiting, QuotaResumeResuming, "", false)
		}(i)
	}
	close(start)
	wg.Wait()

	won := 0
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: %v", i, errs[i])
		}
		if oks[i] {
			won++
		}
	}
	if won != 1 {
		t.Fatalf("恰好一個成功，got %d", won)
	}
	requireQuotaResume(t, database, s.ID, QuotaResumeResuming, "2026-11-18T10:00:00Z", 1)
}

func TestListDueQuotaResume(t *testing.T) {
	database := openQuotaDB(t)
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	past := "2026-11-18T11:59:59Z"
	exact := "2026-11-18T12:00:00Z"
	future := "2026-11-18T12:00:01Z"

	duePast := newQuotaSession(t, database, "due-past")
	dueExact := newQuotaSession(t, database, "due-exact")
	notYet := newQuotaSession(t, database, "future")
	resuming := newQuotaSession(t, database, "resuming")
	exhausted := newQuotaSession(t, database, "exhausted")
	none := newQuotaSession(t, database, "none")
	setQuotaResume(t, database, duePast.ID, QuotaResumeWaiting, past, 1)
	setQuotaResume(t, database, dueExact.ID, QuotaResumeWaiting, exact, 1)
	setQuotaResume(t, database, notYet.ID, QuotaResumeWaiting, future, 1)
	setQuotaResume(t, database, resuming.ID, QuotaResumeResuming, past, 1)
	setQuotaResume(t, database, exhausted.ID, QuotaResumeExhausted, past, 3)
	setQuotaResume(t, database, none.ID, QuotaResumeNone, past, 0)

	got, err := database.ListDueQuotaResume(now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("只應回傳到期的 waiting，got %+v", got)
	}
	seen := map[string]string{}
	for _, item := range got {
		seen[item.SessionID] = item.At
	}
	if seen[duePast.ID] != past || seen[dueExact.ID] != exact {
		t.Fatalf("到期項目不符：%+v", got)
	}
}

func TestClearAllQuotaResume(t *testing.T) {
	database := openQuotaDB(t)
	waiting := newQuotaSession(t, database, "waiting")
	resuming := newQuotaSession(t, database, "resuming")
	exhausted := newQuotaSession(t, database, "exhausted")
	none := newQuotaSession(t, database, "none")
	setQuotaResume(t, database, waiting.ID, QuotaResumeWaiting, "2026-11-18T10:00:00Z", 1)
	setQuotaResume(t, database, resuming.ID, QuotaResumeResuming, "2026-11-18T10:00:00Z", 2)
	setQuotaResume(t, database, exhausted.ID, QuotaResumeExhausted, "2026-11-18T10:00:00Z", 3)
	setQuotaResume(t, database, none.ID, QuotaResumeNone, "keep", 7)

	n, err := database.ClearAllQuotaResume()
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("受影響列數 = %d，want 3", n)
	}
	requireQuotaResume(t, database, waiting.ID, QuotaResumeNone, "", 0)
	requireQuotaResume(t, database, resuming.ID, QuotaResumeNone, "", 0)
	requireQuotaResume(t, database, exhausted.ID, QuotaResumeNone, "", 0)
	requireQuotaResume(t, database, none.ID, QuotaResumeNone, "keep", 7)
}

func TestRecoverQuotaResume(t *testing.T) {
	database := openQuotaDB(t)
	now := time.Date(2026, 11, 18, 15, 4, 5, 900000000, time.UTC)
	wantAt := "2026-11-18T15:04:05Z"
	future := "2026-11-18T18:00:00Z"

	resuming := newQuotaSession(t, database, "resuming")
	waiting := newQuotaSession(t, database, "waiting")
	exhausted := newQuotaSession(t, database, "exhausted")
	none := newQuotaSession(t, database, "none")
	setQuotaResume(t, database, resuming.ID, QuotaResumeResuming, "2026-11-18T10:00:00Z", 2)
	setQuotaResume(t, database, waiting.ID, QuotaResumeWaiting, future, 1)
	setQuotaResume(t, database, exhausted.ID, QuotaResumeExhausted, "2026-11-18T09:00:00Z", 3)
	setQuotaResume(t, database, none.ID, QuotaResumeNone, "", 0)

	if err := database.RecoverQuotaResume(now); err != nil {
		t.Fatal(err)
	}
	requireQuotaResume(t, database, resuming.ID, QuotaResumeWaiting, wantAt, 2)
	requireQuotaResume(t, database, waiting.ID, QuotaResumeWaiting, future, 1)
	requireQuotaResume(t, database, exhausted.ID, QuotaResumeExhausted, "2026-11-18T09:00:00Z", 3)
	requireQuotaResume(t, database, none.ID, QuotaResumeNone, "", 0)
}

func TestQuotaResumePersistsReopen(t *testing.T) {
	path := t.TempDir() + "/reopen.db"
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s := newQuotaSession(t, database, "persist")
	ok, err := database.TransitionQuotaResume(s.ID, QuotaResumeNone, QuotaResumeWaiting, "2026-11-18T10:00:00Z", false)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	ok, err = database.TransitionQuotaResume(s.ID, QuotaResumeWaiting, QuotaResumeResuming, "", false)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	ok, err = database.TransitionQuotaResume(s.ID, QuotaResumeResuming, QuotaResumeWaiting, "2026-11-18T11:00:00Z", false)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	database, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	requireQuotaResume(t, database, s.ID, QuotaResumeWaiting, "2026-11-18T11:00:00Z", 1)
}

func TestEnqueueFront(t *testing.T) {
	database := openQuotaDB(t)

	t.Run("不同 session 不衝突", func(t *testing.T) {
		s1 := newQuotaSession(t, database, "s1")
		s2 := newQuotaSession(t, database, "s2")
		if _, err := database.EnqueueMessage(s1.ID, "a", ""); err != nil {
			t.Fatal(err)
		}
		front2, err := database.EnqueueFront(s2.ID, "from-s2", "甲")
		if err != nil {
			t.Fatal(err)
		}
		front1, err := database.EnqueueFront(s1.ID, "from-s1", "乙")
		if err != nil {
			t.Fatal(err)
		}
		if front1.ID >= 0 || front2.ID >= 0 || front1.ID == front2.ID {
			t.Fatalf("兩個 EnqueueFront 應得到不同負數 id，got %d 與 %d", front1.ID, front2.ID)
		}
		if front1.ID >= front2.ID {
			t.Fatalf("後呼叫的應更小，got s2=%d s1=%d", front2.ID, front1.ID)
		}
	})

	t.Run("空佇列不改暫停", func(t *testing.T) {
		paused := newQuotaSession(t, database, "paused")
		if err := database.SetQueuePaused(paused.ID, true); err != nil {
			t.Fatal(err)
		}
		q, err := database.EnqueueFront(paused.ID, "hold", "")
		if err != nil {
			t.Fatal(err)
		}
		if q.ID >= 0 || q.Content != "hold" || q.CreatedAt == "" {
			t.Fatalf("入列結果 %+v", q)
		}
		if p, err := database.QueuePaused(paused.ID); err != nil || !p {
			t.Fatalf("暫停應保持，paused=%v err=%v", p, err)
		}

		running := newQuotaSession(t, database, "running")
		if _, err := database.EnqueueFront(running.ID, "go", ""); err != nil {
			t.Fatal(err)
		}
		if p, err := database.QueuePaused(running.ID); err != nil || p {
			t.Fatalf("未暫停應保持，paused=%v err=%v", p, err)
		}
	})

	t.Run("插到最前面並保留附件與署名", func(t *testing.T) {
		s := newQuotaSession(t, database, "order")
		if err := database.AddAttachment(&Attachment{
			ID: "att1", SessionID: s.ID, Name: "note.txt", StorageName: "att1.txt", MimeType: "text/plain", Size: 4,
		}); err != nil {
			t.Fatal(err)
		}
		for _, content := range []string{"A", "B", "C"} {
			if _, err := database.EnqueueMessage(s.ID, content, ""); err != nil {
				t.Fatal(err)
			}
		}
		x, err := database.EnqueueFront(s.ID, "X", "訪客", "att1")
		if err != nil {
			t.Fatal(err)
		}
		if x.ID >= 0 || x.Author != "訪客" || len(x.AttachmentIDs) != 1 || x.AttachmentIDs[0] != "att1" || len(x.Attachments) != 1 || x.Attachments[0].ID != "att1" || x.CreatedAt == "" {
			t.Fatalf("X=%+v", x)
		}
		list, err := database.ListQueuedMessages(s.ID)
		if err != nil {
			t.Fatal(err)
		}
		if queuedContents(list) != "X,A,B,C" {
			t.Fatalf("list=%s", queuedContents(list))
		}
		y, err := database.EnqueueFront(s.ID, "Y", "另一人")
		if err != nil {
			t.Fatal(err)
		}
		if y.ID >= x.ID {
			t.Fatalf("第二次應排更前面，Y=%d X=%d", y.ID, x.ID)
		}
		list, err = database.ListQueuedMessages(s.ID)
		if err != nil {
			t.Fatal(err)
		}
		if queuedContents(list) != "Y,X,A,B,C" {
			t.Fatalf("list=%s", queuedContents(list))
		}
		popped, err := database.PopQueuedMessage(s.ID)
		if err != nil || popped == nil || popped.Content != "Y" || popped.ID != y.ID {
			t.Fatalf("pop=%+v err=%v", popped, err)
		}
		// X 仍是負數 id。晉升應刪除該列並寫入 user 訊息。
		promoted, err := database.PromoteQueuedMessage(s.ID, *x)
		if err != nil {
			t.Fatal(err)
		}
		if promoted.Content != "X" || promoted.Author != "訪客" || len(promoted.Attachments) != 1 || promoted.Attachments[0].ID != "att1" {
			t.Fatalf("promoted=%+v", promoted)
		}
		list, err = database.ListQueuedMessages(s.ID)
		if err != nil {
			t.Fatal(err)
		}
		if queuedContents(list) != "A,B,C" {
			t.Fatalf("晉升後佇列=%s", queuedContents(list))
		}
		messages, err := database.ListMessages(s.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(messages) != 1 || messages[0].Content != "X" || messages[0].Author != "訪客" {
			t.Fatalf("messages=%+v", messages)
		}
	})
}

func queuedContents(list []QueuedMessage) string {
	out := ""
	for i, q := range list {
		if i > 0 {
			out += ","
		}
		out += q.Content
	}
	return out
}

func TestQuotaResumeOldDatabase(t *testing.T) {
	path := t.TempDir() + "/old.db"
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Exec(`
		CREATE TABLE sessions (
			id                TEXT PRIMARY KEY,
			agent_type        TEXT NOT NULL DEFAULT 'claude',
			agent_session_id  TEXT NOT NULL DEFAULT '',
			name              TEXT NOT NULL DEFAULT '',
			description       TEXT NOT NULL DEFAULT '',
			work_dir          TEXT NOT NULL DEFAULT '',
			permission_mode   TEXT NOT NULL DEFAULT 'default',
			allowed_tools     TEXT NOT NULL DEFAULT '',
			last_active       TEXT NOT NULL DEFAULT (datetime('now')),
			disguise_agent    TEXT NOT NULL DEFAULT ''
		);
		INSERT INTO sessions (id, name, work_dir) VALUES ('legacy', 'old', '');
	`)
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	requireQuotaResume(t, database, "legacy", QuotaResumeNone, "", 0)

	s := newQuotaSession(t, database, "fresh")
	requireQuotaResume(t, database, s.ID, QuotaResumeNone, "", 0)
}

func TestQuotaResumeGenerationMigration(t *testing.T) {
	path := t.TempDir() + "/generation-old.db"
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Exec(`
		CREATE TABLE sessions (
			id                TEXT PRIMARY KEY,
			agent_type        TEXT NOT NULL DEFAULT 'claude',
			agent_session_id  TEXT NOT NULL DEFAULT '',
			name              TEXT NOT NULL DEFAULT '',
			description       TEXT NOT NULL DEFAULT '',
			work_dir          TEXT NOT NULL DEFAULT '',
			permission_mode   TEXT NOT NULL DEFAULT 'default',
			allowed_tools     TEXT NOT NULL DEFAULT '',
			last_active       TEXT NOT NULL DEFAULT (datetime('now')),
			disguise_agent    TEXT NOT NULL DEFAULT ''
		);
		INSERT INTO sessions (id, name, work_dir) VALUES ('legacy', 'old', '');
	`)
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	generation, err := database.GetQuotaResumeGeneration("legacy")
	if err != nil || generation != 0 {
		t.Fatalf("舊庫世代=%d err=%v", generation, err)
	}
	s := newQuotaSession(t, database, "fresh")
	step := func(from, to string) {
		t.Helper()
		ok, err := database.TransitionQuotaResume(s.ID, from, to, "2026-11-18T10:01:00Z", false)
		if err != nil || !ok {
			t.Fatalf("transition %s -> %s: %v %v", from, to, ok, err)
		}
	}
	step(QuotaResumeNone, QuotaResumeWaiting)
	step(QuotaResumeWaiting, QuotaResumeResuming)
	generation, err = database.GetQuotaResumeGeneration(s.ID)
	if err != nil || generation != 1 {
		t.Fatalf("第一次 resuming 世代=%d err=%v", generation, err)
	}
	step(QuotaResumeResuming, QuotaResumeNone)
	requireQuotaResume(t, database, s.ID, QuotaResumeNone, "", 0)
	generation, err = database.GetQuotaResumeGeneration(s.ID)
	if err != nil || generation != 1 {
		t.Fatalf("回到 none 後世代=%d err=%v", generation, err)
	}
	step(QuotaResumeNone, QuotaResumeWaiting)
	step(QuotaResumeWaiting, QuotaResumeResuming)
	generation, err = database.GetQuotaResumeGeneration(s.ID)
	if err != nil || generation != 2 {
		t.Fatalf("第二次 resuming 世代=%d err=%v", generation, err)
	}
	ok, err := database.ReleaseQuotaResumeGeneration(s.ID, 1)
	if err != nil || ok {
		t.Fatalf("舊世代不該清掉 ok=%v err=%v", ok, err)
	}
	requireQuotaResume(t, database, s.ID, QuotaResumeResuming, "2026-11-18T10:01:00Z", 1)
	if got, err := database.GetQuotaResumeGeneration(s.ID); err != nil || got != 2 {
		t.Fatalf("世代=%d err=%v", got, err)
	}
	ok, err = database.ReleaseQuotaResumeGeneration(s.ID, 2)
	if err != nil || !ok {
		t.Fatalf("這一世代應清掉 ok=%v err=%v", ok, err)
	}
	requireQuotaResume(t, database, s.ID, QuotaResumeNone, "", 0)
	if got, err := database.GetQuotaResumeGeneration(s.ID); err != nil || got != 2 {
		t.Fatalf("清掉後世代=%d err=%v", got, err)
	}
}

func TestQuotaResumeCancelKeepsGenerationAndDropsSession(t *testing.T) {
	database := openQuotaDB(t)
	s := newQuotaSession(t, database, "cancel")
	if _, err := database.TransitionQuotaResume(s.ID, QuotaResumeNone, QuotaResumeWaiting, "2026-11-18T10:01:00Z", false); err != nil {
		t.Fatal(err)
	}
	if _, err := database.TransitionQuotaResume(s.ID, QuotaResumeWaiting, QuotaResumeResuming, "", false); err != nil {
		t.Fatal(err)
	}
	if err := database.CancelQuotaResume(s.ID); err != nil {
		t.Fatal(err)
	}
	requireQuotaResume(t, database, s.ID, QuotaResumeNone, "", 0)
	generation, err := database.GetQuotaResumeGeneration(s.ID)
	if err != nil || generation != 2 {
		t.Fatalf("取消後世代=%d err=%v", generation, err)
	}
	if _, err := database.EnqueueMessage(s.ID, "A", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := database.TransitionQuotaResume(s.ID, QuotaResumeNone, QuotaResumeWaiting, "2026-11-18T11:00:00Z", false); err != nil {
		t.Fatal(err)
	}
	if err := database.ClearQueuedMessages(s.ID); err != nil {
		t.Fatal(err)
	}
	requireQuotaResume(t, database, s.ID, QuotaResumeNone, "", 0)
	list, err := database.ListQueuedMessages(s.ID)
	if err != nil || len(list) != 0 {
		t.Fatalf("queue=%v err=%v", list, err)
	}
	paused, err := database.QueuePaused(s.ID)
	if err != nil || paused {
		t.Fatalf("paused=%v err=%v", paused, err)
	}
	gone := newQuotaSession(t, database, "gone")
	if _, err := database.TransitionQuotaResume(gone.ID, QuotaResumeNone, QuotaResumeWaiting, "2026-11-18T10:01:00Z", false); err != nil {
		t.Fatal(err)
	}
	if err := database.DeleteSession(gone.ID); err != nil {
		t.Fatal(err)
	}
	due, err := database.ListDueQuotaResume(time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range due {
		if item.SessionID == gone.ID {
			t.Fatal("刪掉的 session 仍會被輪詢")
		}
	}
}
