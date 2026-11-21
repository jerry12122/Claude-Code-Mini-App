package db

import (
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// openPublishedSchema 只跑已發布的前 5 段遷移，得到 user_version=5 的舊庫。
// 不用 DB.Open：Open 會升到程式內建的最新版本，不能拿來假裝舊資料。
func openPublishedSchema(t *testing.T, path string) *DB {
	t.Helper()
	if len(migrations) < 5 || migrations[4].name != "room_job_records" {
		t.Fatalf("前 5 段遷移前綴被改過：%d", len(migrations))
	}
	sqldb, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_txlock=immediate")
	if err != nil {
		t.Fatal(err)
	}
	d := &DB{DB: sqldb}
	if err := d.runMigrations(migrations[:5]); err != nil {
		d.Close()
		t.Fatal(err)
	}
	if got := userVersion(t, d); got != 5 {
		d.Close()
		t.Fatalf("舊 fixture user_version=%d，want 5", got)
	}
	if hasTable(t, d, "session_categories") {
		d.Close()
		t.Fatal("版本 5 不該已有 session_categories")
	}
	return d
}

func copyDBFile(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSessionCategoryUpgradeFromVersion5(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "v5.db")
	old := openPublishedSchema(t, src)
	_, err := old.Exec(`
		INSERT INTO sessions (id, name, work_dir, status, last_active, last_read_at)
		VALUES ('s1', 'keep', 'D:/Proj', 'idle', '2020-01-02 03:04:05', '2020-01-02 03:04:06');
		INSERT INTO messages (session_id, role, content, status, is_read, created_at, input_request_id)
		VALUES ('s1', 'claude', 'done-body', 'done', 1, '2020-01-02 03:04:07', 'req-m');
		INSERT INTO queued_messages (session_id, content, created_at, input_request_id)
		VALUES ('s1', 'queued-body', '2020-01-02 03:04:08', 'req-q');
		INSERT INTO input_receipts (session_id, actor, request_id, digest)
		VALUES ('s1', 'owner', 'req-1', 'digest-1');
		INSERT INTO work_dirs (path) VALUES ('D:/Proj');
	`)
	if err != nil {
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

	var categoryID sql.NullString
	if err := upgraded.QueryRow(`SELECT category_id FROM sessions WHERE id='s1'`).Scan(&categoryID); err != nil {
		t.Fatalf("舊資料升級後應有 nullable category_id，現況仍是沒有分類欄的 schema：%v", err)
	}
	if categoryID.Valid {
		t.Fatalf("舊 session 應維持未分類 NULL，got %q", categoryID.String)
	}
	if got := userVersion(t, upgraded); got != 7 {
		t.Fatalf("升級後 user_version=%d，want 7", got)
	}
	if !hasTable(t, upgraded, "session_categories") {
		t.Fatal("升級後應有 session_categories")
	}

	var name, workDir, status, lastActive, content, msgStatus string
	var isRead int
	var queue, digest string
	if err := upgraded.QueryRow(`SELECT name, work_dir, status, last_active FROM sessions WHERE id='s1'`).Scan(&name, &workDir, &status, &lastActive); err != nil {
		t.Fatal(err)
	}
	if name != "keep" || workDir != "D:/Proj" || status != "idle" || lastActive != "2020-01-02 03:04:05" {
		t.Fatalf("session 被遷移改動：%s %s %s %s", name, workDir, status, lastActive)
	}
	if err := upgraded.QueryRow(`SELECT content, status, is_read FROM messages WHERE session_id='s1'`).Scan(&content, &msgStatus, &isRead); err != nil {
		t.Fatal(err)
	}
	if content != "done-body" || msgStatus != "done" || isRead != 1 {
		t.Fatalf("歷史被遷移改動：%s %s read=%d", content, msgStatus, isRead)
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
	if err := reopened.QueryRow(`SELECT content FROM messages WHERE session_id='s1'`).Scan(&content); err != nil || content != "done-body" {
		t.Fatalf("重開後歷史=%q err=%v", content, err)
	}
}

func openCategoryDB(t *testing.T) (*DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cat.db")
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d, path
}

func categoryCount(t *testing.T, d *DB) int {
	t.Helper()
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM session_categories`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSessionCategoryCRUD(t *testing.T) {
	d, path := openCategoryDB(t)
	if _, err := d.Exec(`INSERT INTO work_dirs (path) VALUES ('A'), ('B')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO sessions (id, name, work_dir, status, last_active) VALUES ('s', 'keep', 'C:/Real', 'idle', '2020-01-02 03:04:05')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO messages (session_id, role, content, status, is_read) VALUES ('s', 'claude', 'hist', 'done', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO queued_messages (session_id, content) VALUES ('s', 'q')`); err != nil {
		t.Fatal(err)
	}

	reviewA, err := d.CreateSessionCategory(" A ", " Review ")
	if err != nil {
		t.Fatal(err)
	}
	reviewB, err := d.CreateSessionCategory("B", "Review")
	if err != nil {
		t.Fatal(err)
	}
	if reviewA.ID == "" || reviewA.ID == reviewB.ID || reviewA.WorkDir != "A" || reviewA.Name != "Review" || reviewB.WorkDir != "B" {
		t.Fatalf("建立結果 %+v %+v", reviewA, reviewB)
	}
	renamed, err := d.RenameSessionCategory(reviewA.ID, " Done ")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.ID != reviewA.ID || renamed.Name != "Done" || renamed.WorkDir != "A" {
		t.Fatalf("改名後 id 或目錄變了：%+v", renamed)
	}
	if got, err := d.GetSessionCategory(reviewB.ID); err != nil || got.Name != "Review" || got.WorkDir != "B" {
		t.Fatalf("目錄 B 被改到：%+v %v", got, err)
	}

	name80 := strings.Repeat("你", 80)
	wide80 := strings.Repeat("\U00010348", 80)
	if utf8.RuneCountInString(name80) != 80 || len(name80) <= 80 || utf8.RuneCountInString(wide80) != 80 || len(wide80) != 320 {
		t.Fatalf("邊界 fixture rune/byte 不對：你=%d/%d wide=%d/%d", utf8.RuneCountInString(name80), len(name80), utf8.RuneCountInString(wide80), len(wide80))
	}
	long, err := d.CreateSessionCategory("A", name80)
	if err != nil || long.Name != name80 {
		t.Fatalf("80 code points 應接受：%v %+v", err, long)
	}
	if _, err := d.CreateSessionCategory("B", wide80); err != nil {
		t.Fatal(err)
	}

	before := categoryCount(t, d)
	reject := []struct {
		dir, name string
		want      error
	}{
		{"A", "   ", ErrSessionCategoryInvalid},
		{"A", strings.Repeat("你", 81), ErrSessionCategoryInvalid},
		{"A", "Done", ErrSessionCategoryDuplicate},
		{"A", " Done ", ErrSessionCategoryDuplicate},
		{"nope", "X", ErrSessionCategoryInvalid},
		{"a", "X", ErrSessionCategoryInvalid},
		{"", "X", ErrSessionCategoryInvalid},
		{"Telegram 聊天室", "X", ErrSessionCategoryInvalid},
	}
	for _, c := range reject {
		if _, err := d.CreateSessionCategory(c.dir, c.name); !errors.Is(err, c.want) {
			t.Fatalf("Create(%q,%q)=%v，want %v", c.dir, c.name, err, c.want)
		}
	}
	if _, err := d.RenameSessionCategory(reviewA.ID, strings.Repeat("你", 81)); !errors.Is(err, ErrSessionCategoryInvalid) {
		t.Fatalf("過長改名=%v", err)
	}
	if _, err := d.RenameSessionCategory(reviewA.ID, name80); !errors.Is(err, ErrSessionCategoryDuplicate) {
		t.Fatalf("同目錄改成重名=%v", err)
	}
	if _, err := d.CreateSessionCategory("A", "done"); err != nil {
		t.Fatalf("大小寫不同應可並存：%v", err)
	}
	if categoryCount(t, d) != before+1 {
		t.Fatalf("拒絕案例不該增列，成功的大小寫案例應只多 1：before=%d now=%d", before, categoryCount(t, d))
	}
	if got, err := d.GetSessionCategory(reviewA.ID); err != nil || got.Name != "Done" {
		t.Fatalf("失敗改名仍改了儲存：%+v %v", got, err)
	}

	fromSession, err := d.CreateSessionCategory("C:/Real", "Kept")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`UPDATE sessions SET category_id = ? WHERE id = 's'`, fromSession.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteSessionCategory(fromSession.ID); err != nil {
		t.Fatal(err)
	}
	var gotID sql.NullString
	var lastActive, status, workDir, content, queue string
	var isRead int
	if err := d.QueryRow(`SELECT category_id, last_active, status, work_dir FROM sessions WHERE id='s'`).Scan(&gotID, &lastActive, &status, &workDir); err != nil {
		t.Fatal(err)
	}
	if gotID.Valid || lastActive != "2020-01-02 03:04:05" || status != "idle" || workDir != "C:/Real" {
		t.Fatalf("刪分類後 session 不該被清掉或改時間：id=%v active=%s status=%s dir=%s", gotID, lastActive, status, workDir)
	}
	if err := d.QueryRow(`SELECT content, is_read FROM messages WHERE session_id='s'`).Scan(&content, &isRead); err != nil || content != "hist" || isRead != 1 {
		t.Fatalf("歷史 content=%q read=%d err=%v", content, isRead, err)
	}
	if err := d.QueryRow(`SELECT content FROM queued_messages WHERE session_id='s'`).Scan(&queue); err != nil || queue != "q" {
		t.Fatalf("queue=%q err=%v", queue, err)
	}
	if _, err := d.GetSessionCategory(fromSession.ID); !errors.Is(err, ErrSessionCategoryNotFound) {
		t.Fatalf("刪除後仍可讀：%v", err)
	}
	if err := d.DeleteSessionCategory("missing"); !errors.Is(err, ErrSessionCategoryNotFound) {
		t.Fatalf("刪不存在=%v", err)
	}

	listed, err := d.ListSessionCategories("A")
	if err != nil || len(listed) != 3 {
		t.Fatalf("A 應剩 Done、80 字與 done：%v %+v", err, listed)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := reopened.GetSessionCategory(reviewA.ID)
	if err != nil || got.ID != reviewA.ID || got.Name != "Done" || got.WorkDir != "A" {
		t.Fatalf("重開後分類沒留住：%+v %v", got, err)
	}
}

func TestSessionCategoryForeignKeyNotEnforcedByPool(t *testing.T) {
	d, _ := openCategoryDB(t)
	var fk int
	if err := d.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil {
		t.Fatal(err)
	}
	if fk != 0 {
		t.Fatalf("foreign_keys=%d，連線池已強制外鍵，不能宣稱交易解綁是唯一手段", fk)
	}
	var fromCol, refTable, onDelete string
	if err := d.QueryRow(`SELECT "from", "table", on_delete FROM pragma_foreign_key_list('sessions') WHERE "from" = 'category_id'`).Scan(&fromCol, &refTable, &onDelete); err != nil {
		t.Fatalf("schema 應記下 nullable REFERENCES：%v", err)
	}
	if fromCol != "category_id" || refTable != "session_categories" || onDelete != "SET NULL" {
		t.Fatalf("外鍵紀錄 %s %s %s", fromCol, refTable, onDelete)
	}
	if _, err := d.Exec(`INSERT INTO work_dirs (path) VALUES ('A')`); err != nil {
		t.Fatal(err)
	}
	cat, err := d.CreateSessionCategory("A", "Review")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO sessions (id, name, work_dir, category_id) VALUES ('s', 'n', 'A', ?)`, cat.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`DELETE FROM session_categories WHERE id = ?`, cat.ID); err != nil {
		t.Fatal(err)
	}
	var still sql.NullString
	if err := d.QueryRow(`SELECT category_id FROM sessions WHERE id='s'`).Scan(&still); err != nil {
		t.Fatal(err)
	}
	if !still.Valid || still.String != cat.ID {
		t.Fatalf("foreign_keys 關閉時裸刪應留下懸空 id，got %+v；若已自動清空，代表池約束已生效，報告必須改口", still)
	}
}
