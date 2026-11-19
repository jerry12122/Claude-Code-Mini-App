package db

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
)

func userVersion(t *testing.T, d *DB) int {
	t.Helper()
	var v int
	if err := d.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func hasTable(t *testing.T, d *DB, name string) bool {
	t.Helper()
	var n int
	if err := d.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n == 1
}

func TestMigrationsFreshDatabaseIsAtLatestVersion(t *testing.T) {
	d, err := Open(t.TempDir() + "/fresh.db")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if got := userVersion(t, d); got != len(migrations) {
		t.Fatalf("user_version = %d，want %d", got, len(migrations))
	}
}

func TestMigrationsLegacyDatabaseGetsVersioned(t *testing.T) {
	path := t.TempDir() + "/legacy.db"
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE sessions (id TEXT PRIMARY KEY, name TEXT NOT NULL DEFAULT '', work_dir TEXT NOT NULL DEFAULT ''); INSERT INTO sessions (id, name) VALUES ('old', 'x')`); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if got := userVersion(t, d); got != len(migrations) {
		t.Fatalf("舊資料庫遷移後 user_version = %d，want %d", got, len(migrations))
	}
	// 舊資料沒被動到，缺的欄位補上了。
	var name, status string
	if err := d.QueryRow(`SELECT name, status FROM sessions WHERE id = 'old'`).Scan(&name, &status); err != nil || name != "x" || status != "idle" {
		t.Fatalf("name=%q status=%q err=%v", name, status, err)
	}
}

func TestMigrationsRunEachOnlyOnce(t *testing.T) {
	d, err := Open(t.TempDir() + "/once.db")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	runs := 0
	list := append(append([]migration{}, migrations...), migration{name: "count", upTx: func(tx *sql.Tx) error {
		runs++
		_, err := tx.Exec(`CREATE TABLE migration_probe (x INTEGER)`)
		return err
	}})
	for i := 0; i < 3; i++ {
		if err := d.runMigrations(list); err != nil {
			t.Fatal(err)
		}
	}
	if runs != 1 || userVersion(t, d) != len(list) || !hasTable(t, d, "migration_probe") {
		t.Fatalf("runs=%d version=%d", runs, userVersion(t, d))
	}
}

func TestMigrationsFailureRollsBackAndKeepsVersion(t *testing.T) {
	d, err := Open(t.TempDir() + "/fail.db")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	before := userVersion(t, d)
	boom := errors.New("boom")
	list := append(append([]migration{}, migrations...), migration{name: "bad", upTx: func(tx *sql.Tx) error {
		if _, err := tx.Exec(`CREATE TABLE half_done (x INTEGER)`); err != nil {
			return err
		}
		return boom
	}})
	err = d.runMigrations(list)
	if err == nil || !errors.Is(err, boom) || !strings.Contains(err.Error(), "bad") {
		t.Fatalf("應回傳含遷移名稱並包住原錯誤的錯誤，got %v", err)
	}
	if got := userVersion(t, d); got != before {
		t.Fatalf("失敗後 user_version 不該前進：%d → %d", before, got)
	}
	if hasTable(t, d, "half_done") {
		t.Fatal("失敗的遷移應整個回滾，卻留下了 half_done")
	}
}

func TestMigrationsRefuseNewerDatabase(t *testing.T) {
	path := t.TempDir() + "/newer.db"
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`PRAGMA user_version = 999`); err != nil {
		t.Fatal(err)
	}
	d.Close()
	if d2, err := Open(path); err == nil {
		d2.Close()
		t.Fatal("比程式新的資料庫版本應拒絕開啟")
	} else if !strings.Contains(err.Error(), "999") {
		t.Fatalf("錯誤訊息應包含版本號：%v", err)
	}
}
