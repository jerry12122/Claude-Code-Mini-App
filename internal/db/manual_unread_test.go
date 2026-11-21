package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func openVersionedSchema(t *testing.T, path string, n int, lastName string) *DB {
	t.Helper()
	if len(migrations) < n || migrations[n-1].name != lastName {
		t.Fatalf("遷移前綴被改過：len=%d 第 %d 段=%q", len(migrations), n, func() string {
			if len(migrations) >= n {
				return migrations[n-1].name
			}
			return ""
		}())
	}
	sqldb, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_txlock=immediate")
	if err != nil {
		t.Fatal(err)
	}
	d := &DB{DB: sqldb}
	if err := d.runMigrations(migrations[:n]); err != nil {
		d.Close()
		t.Fatal(err)
	}
	if got := userVersion(t, d); got != n {
		d.Close()
		t.Fatalf("fixture user_version=%d，want %d", got, n)
	}
	return d
}

func TestManualUnreadUpgradeFromVersion6(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "v6.db")
	old := openVersionedSchema(t, src, 6, "session_categories")
	if _, err := old.Exec(`
		INSERT INTO sessions (id, name, work_dir, agent_type, status, last_active, last_read_at, pinned)
		VALUES ('s1', 'keep', 'D:/Proj', 'claude', 'idle', '2020-01-02 03:04:05', '2020-01-02 03:04:06', 1);
		INSERT INTO messages (session_id, role, content, status, is_read, created_at, input_request_id)
		VALUES ('s1', 'claude', 'done-body', 'done', 1, '2020-01-02 03:04:07', 'req-m');
		INSERT INTO queued_messages (session_id, content, created_at, input_request_id)
		VALUES ('s1', 'queued-body', '2020-01-02 03:04:08', 'req-q');
		INSERT INTO input_receipts (session_id, actor, request_id, digest)
		VALUES ('s1', 'owner', 'req-1', 'digest-1');
		INSERT INTO work_dirs (path) VALUES ('D:/Proj');
	`); err != nil {
		t.Fatal(err)
	}
	cat, err := old.CreateSessionCategory("D:/Proj", "Review")
	if err != nil {
		t.Fatal(err)
	}
	if err := old.SetSessionCategory("s1", &cat.ID); err != nil {
		t.Fatal(err)
	}
	var busy, logFrames, checkpointed int
	if err := old.QueryRow(`PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &logFrames, &checkpointed); err != nil {
		t.Fatal(err)
	}
	if busy != 0 {
		t.Fatalf("checkpoint busy=%d log=%d ckpt=%d", busy, logFrames, checkpointed)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dir, "copy.db")
	copyDBFile(t, src, dst)
	upgraded, err := Open(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()

	var manual, revision, pinned, isRead int
	var categoryID, name, workDir, status, lastActive, lastRead, content, msgStatus, queue, digest string
	if err := upgraded.QueryRow(`SELECT manual_unread, manual_unread_revision FROM sessions WHERE id='s1'`).Scan(&manual, &revision); err != nil {
		t.Fatalf("舊 v6 升級後應有 manual_unread 持久欄，現況仍缺：%v", err)
	}
	if manual != 0 || revision != 0 {
		t.Fatalf("舊資料預設 manual=%d revision=%d，want 0/0", manual, revision)
	}
	if got := userVersion(t, upgraded); got != 7 {
		t.Fatalf("升級後 user_version=%d，want 7", got)
	}
	var fk int
	if err := upgraded.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil || fk != 0 {
		t.Fatalf("foreign_keys=%d err=%v", fk, err)
	}
	if err := upgraded.QueryRow(`SELECT name, work_dir, status, last_active, last_read_at, pinned, category_id FROM sessions WHERE id='s1'`).Scan(&name, &workDir, &status, &lastActive, &lastRead, &pinned, &categoryID); err != nil {
		t.Fatal(err)
	}
	if name != "keep" || workDir != "D:/Proj" || status != "idle" || lastActive != "2020-01-02 03:04:05" || lastRead != "2020-01-02 03:04:06" || pinned != 1 || categoryID != cat.ID {
		t.Fatalf("舊列被遷移改動 name=%s dir=%s status=%s active=%s read=%s pin=%d cat=%s", name, workDir, status, lastActive, lastRead, pinned, categoryID)
	}
	if err := upgraded.QueryRow(`SELECT content, status, is_read FROM messages WHERE session_id='s1'`).Scan(&content, &msgStatus, &isRead); err != nil || content != "done-body" || msgStatus != "done" || isRead != 1 {
		t.Fatalf("歷史 content=%s status=%s read=%d err=%v", content, msgStatus, isRead, err)
	}
	if err := upgraded.QueryRow(`SELECT content FROM queued_messages WHERE session_id='s1'`).Scan(&queue); err != nil || queue != "queued-body" {
		t.Fatalf("queue=%q err=%v", queue, err)
	}
	if err := upgraded.QueryRow(`SELECT digest FROM input_receipts WHERE session_id='s1'`).Scan(&digest); err != nil || digest != "digest-1" {
		t.Fatalf("receipt=%q err=%v", digest, err)
	}

	if err := upgraded.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got := userVersion(t, reopened); got != 7 {
		t.Fatalf("重開 user_version=%d，want 7", got)
	}
	if err := reopened.QueryRow(`SELECT manual_unread, manual_unread_revision, category_id, content FROM sessions s JOIN messages m ON m.session_id=s.id WHERE s.id='s1'`).Scan(&manual, &revision, &categoryID, &content); err != nil || manual != 0 || revision != 0 || categoryID != cat.ID || content != "done-body" {
		t.Fatalf("重開 manual=%d rev=%d cat=%s content=%s err=%v", manual, revision, categoryID, content, err)
	}
}
