package db

import "testing"

func TestUnreadCompletedAfterReadingPending(t *testing.T) {
	database, err := Open(t.TempDir() + "/completion.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	s, err := database.CreateSession("completion", "", "", "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	id, err := database.CreatePendingMessage(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.MarkSessionRead(s.ID); err != nil {
		t.Fatal(err)
	}
	if err := database.AppendMessageContent(id, "finished after you left"); err != nil {
		t.Fatal(err)
	}
	if err := database.FinalizeMessage(id); err != nil {
		t.Fatal(err)
	}
	got, err := database.GetSession(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.UnreadCount != 1 {
		t.Fatalf("completion after reading pending must be unread, got %d", got.UnreadCount)
	}
}

func TestUnreadPendingAndSameSecondIncoming(t *testing.T) {
	database, err := Open(t.TempDir() + "/pending.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	s, err := database.CreateSession("pending", "", "", "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE sessions SET last_read_at = '2000-01-01 00:00:00' WHERE id = ?`, s.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreatePendingMessage(s.ID); err != nil {
		t.Fatal(err)
	}
	got, err := database.GetSession(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.UnreadCount != 0 {
		t.Fatalf("unfinished reply must not count, got %d", got.UnreadCount)
	}
	if err := database.MarkSessionRead(s.ID); err != nil {
		t.Fatal(err)
	}
	if err := database.AddMessage(s.ID, "claude", "instant incoming", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE messages SET created_at = (SELECT last_read_at FROM sessions WHERE id = ?) WHERE session_id = ? AND status = 'done'`, s.ID, s.ID); err != nil {
		t.Fatal(err)
	}
	got, err = database.GetSession(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.UnreadCount != 1 {
		t.Fatalf("same-second incoming must be unread, got %d", got.UnreadCount)
	}
}

func TestUnreadMigrationAndReadReceiptsPersist(t *testing.T) {
	path := t.TempDir() + "/migration.db"
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if database != nil {
			database.Close()
		}
	}()
	s, err := database.CreateSession("legacy", "", "", "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	tg, err := database.CreateSession("telegram", "", "", "default", "telegram", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{s.ID, tg.ID} {
		if _, err := database.Exec(`UPDATE sessions SET last_read_at = '2021-01-01 00:00:00' WHERE id = ?`, id); err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec(`INSERT INTO messages (session_id, role, content, status, created_at) VALUES
			(?, 'claude', 'old read', 'done', '2020-01-01 00:00:00'),
			(?, 'claude', 'old unread', 'done', '2022-01-01 00:00:00')`, id, id); err != nil {
			t.Fatal(err)
		}
	}
	pending, err := database.CreatePendingMessage(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Rebuild the exact pre-migration state, including a pending reply created before the read time.
	if _, err := database.Exec(`UPDATE messages SET created_at = '2020-01-01 00:00:00' WHERE id = ?`, pending); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`ALTER TABLE messages DROP COLUMN is_read`); err != nil {
		t.Fatal(err)
	}
	database.Close()
	database, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]int{s.ID: 2, tg.ID: 0} {
		// Restart finalizes the old pending reply as interrupted, which also requires reading.
		got, err := database.GetSession(id)
		if err != nil {
			t.Fatal(err)
		}
		if got.UnreadCount != want {
			t.Fatalf("migrated unread=%d want=%d", got.UnreadCount, want)
		}
	}
	changes := 0
	database.OnSessionChange = func() { changes++ }
	if err := database.MarkSessionRead(s.ID, pending); err != nil {
		t.Fatal(err)
	}
	if changes != 1 {
		t.Fatalf("read receipt must refresh the sidebar, got %d notifications", changes)
	}
	database.Close()
	database, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := database.GetSession(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.UnreadCount != 1 {
		t.Fatalf("reopening must retain acknowledged/unseen results, got %d", got.UnreadCount)
	}
}

func TestReadAllLeavesRunningResultsUnreadAndRollsBackFailure(t *testing.T) {
	database, err := Open(t.TempDir() + "/read-all.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	s, err := database.CreateSession("read-all", "", "", "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AddMessage(s.ID, "claude", "completed", ""); err != nil {
		t.Fatal(err)
	}
	pending, err := database.CreatePendingMessage(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE sessions SET last_read_at = '2000-01-01 00:00:00' WHERE id = ?`, s.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TRIGGER fail_read BEFORE UPDATE OF is_read ON messages BEGIN SELECT RAISE(ABORT, 'read failed'); END`); err != nil {
		t.Fatal(err)
	}
	if err := database.MarkSessionRead(s.ID); err == nil {
		t.Fatal("receipt failure must be returned")
	}
	got, err := database.GetSession(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastReadAt != "2000-01-01 00:00:00" || got.UnreadCount != 1 {
		t.Fatalf("failed read must roll back: %+v", got)
	}
	if _, err := database.Exec(`DROP TRIGGER fail_read`); err != nil {
		t.Fatal(err)
	}
	if err := database.MarkAllSessionsRead(); err != nil {
		t.Fatal(err)
	}
	if err := database.FinalizeMessage(pending); err != nil {
		t.Fatal(err)
	}
	got, err = database.GetSession(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.UnreadCount != 1 {
		t.Fatalf("read all must not acknowledge a future completion, got %d", got.UnreadCount)
	}
}

func TestMarkSessionRead_DoesNotTouchLastActive(t *testing.T) {
	path := t.TempDir() + "/smoke.db"
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	s, err := database.CreateSession("t", "", "", "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	if s.LastReadAt == "" {
		t.Fatal("CreateSession 應寫入 last_read_at")
	}
	beforeActive := s.LastActive

	if err := database.MarkSessionRead(s.ID); err != nil {
		t.Fatal(err)
	}
	got, err := database.GetSession(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastActive != beforeActive {
		t.Fatalf("MarkSessionRead 不應動到 last_active: before=%q after=%q", beforeActive, got.LastActive)
	}
	if got.LastReadAt == "" {
		t.Fatal("MarkSessionRead 後 last_read_at 不應為空")
	}
}

func TestMarkAllSessionsRead(t *testing.T) {
	path := t.TempDir() + "/smoke_all.db"
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	a, err := database.CreateSession("a", "", "", "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	b, err := database.CreateSession("b", "", "", "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	// 模擬未讀：把 a、b 的 last_read_at 清空，last_active 維持建立時的時間。
	if _, err := database.Exec(`UPDATE sessions SET last_read_at = '' WHERE id IN (?, ?)`, a.ID, b.ID); err != nil {
		t.Fatal(err)
	}

	if err := database.MarkAllSessionsRead(); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{a.ID, b.ID} {
		got, err := database.GetSession(id)
		if err != nil {
			t.Fatal(err)
		}
		if got.LastReadAt == "" {
			t.Fatalf("session %s: MarkAllSessionsRead 後 last_read_at 不應為空", id)
		}
	}
}

func TestUnreadCountCalculation(t *testing.T) {
	path := t.TempDir() + "/unread_count.db"
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	sess, err := database.CreateSession("test_unread", "", "", "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}

	// 初始無訊息，未讀應為 0
	got, err := database.GetSession(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.UnreadCount != 0 {
		t.Fatalf("expected initial unread_count=0, got %d", got.UnreadCount)
	}

	// 使用者發送訊息，role='user' 不應計入未讀
	if err := database.AddMessage(sess.ID, "user", "hello", ""); err != nil {
		t.Fatal(err)
	}
	got, err = database.GetSession(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.UnreadCount != 0 {
		t.Fatalf("user message should not count as unread, got %d", got.UnreadCount)
	}

	// 寫入兩則 assistant 訊息與一則 shell 訊息（created_at > last_read_at）
	// 將 last_read_at 設為稍微過去的時間，確保後續訊息大於 last_read_at
	if _, err := database.Exec(`UPDATE sessions SET last_read_at = '2020-01-01 00:00:00' WHERE id = ?`, sess.ID); err != nil {
		t.Fatal(err)
	}

	_, _, err = database.AddMessageWithID(sess.ID, "claude", "response 1", "")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = database.AddMessageWithID(sess.ID, "claude", "response 2", "Worker")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = database.AddMessageWithID(sess.ID, "shell", "echo done", "")
	if err != nil {
		t.Fatal(err)
	}

	// 查詢未讀數應為 3
	got, err = database.GetSession(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.UnreadCount != 3 {
		t.Fatalf("expected unread_count=3, got %d", got.UnreadCount)
	}

	// 驗證 ListSessions 亦正確統計 unread_count
	list, err := database.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) == 0 || list[0].UnreadCount != 3 {
		t.Fatalf("ListSessions expected unread_count=3, got %d", list[0].UnreadCount)
	}

	// 標記已讀後，未讀數應歸零
	if err := database.MarkSessionRead(sess.ID); err != nil {
		t.Fatal(err)
	}
	got, err = database.GetSession(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.UnreadCount != 0 {
		t.Fatalf("after MarkSessionRead expected unread_count=0, got %d", got.UnreadCount)
	}
}
