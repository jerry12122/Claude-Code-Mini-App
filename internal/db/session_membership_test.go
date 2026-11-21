package db

import (
	"database/sql"
	"fmt"
	"sync"
	"testing"
)

func TestSessionCategoryAssignRacesDelete(t *testing.T) {
	d, err := Open(t.TempDir() + "/race.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := d.Exec(`INSERT INTO sessions (id, name, work_dir, status, last_active) VALUES ('s', 'keep', 'A', 'idle', '2020-01-02 03:04:05')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO messages (session_id, role, content, status, is_read) VALUES ('s', 'claude', 'hist', 'done', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO queued_messages (session_id, content) VALUES ('s', 'q')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO work_dirs (path) VALUES ('A')`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		cat, err := d.CreateSessionCategory("A", fmt.Sprintf("N%d", i))
		if err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_ = d.SetSessionCategory("s", &cat.ID)
		}()
		go func() {
			defer wg.Done()
			<-start
			_ = d.DeleteSessionCategory(cat.ID)
		}()
		close(start)
		wg.Wait()
		var categoryID sql.NullString
		if err := d.QueryRow(`SELECT category_id FROM sessions WHERE id='s'`).Scan(&categoryID); err != nil {
			t.Fatal(err)
		}
		var exists int
		if err := d.QueryRow(`SELECT COUNT(*) FROM session_categories WHERE id=?`, cat.ID).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists != 0 || categoryID.Valid {
			t.Fatalf("第 %d 次並行後懸空 exists=%d category=%v", i, exists, categoryID)
		}
	}
	var lastActive, status, content, queue string
	var isRead int
	if err := d.QueryRow(`SELECT last_active, status FROM sessions WHERE id='s'`).Scan(&lastActive, &status); err != nil {
		t.Fatal(err)
	}
	if lastActive != "2020-01-02 03:04:05" || status != "idle" {
		t.Fatalf("並行改到 active=%s status=%s", lastActive, status)
	}
	if err := d.QueryRow(`SELECT content, is_read FROM messages WHERE session_id='s'`).Scan(&content, &isRead); err != nil || content != "hist" || isRead != 1 {
		t.Fatalf("歷史 %q read=%d %v", content, isRead, err)
	}
	if err := d.QueryRow(`SELECT content FROM queued_messages WHERE session_id='s'`).Scan(&queue); err != nil || queue != "q" {
		t.Fatalf("queue %q %v", queue, err)
	}
}
