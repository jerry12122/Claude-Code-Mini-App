package db

import (
	"database/sql"

	_ "modernc.org/sqlite"
)

type DB struct {
	*sql.DB
}

func Open(path string) (*DB, error) {
	sqldb, err := sql.Open("sqlite", path+"?_journal_mode=WAL")
	if err != nil {
		return nil, err
	}
	db := &DB{sqldb}
	if err := db.migrate(); err != nil {
		sqldb.Close()
		return nil, err
	}
	return db, nil
}

func (db *DB) migrate() error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			tg_id    INTEGER PRIMARY KEY,
			username TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE IF NOT EXISTS sessions (
			id                TEXT PRIMARY KEY,
			agent_type        TEXT NOT NULL DEFAULT 'claude',
			agent_session_id  TEXT NOT NULL DEFAULT '',
			name              TEXT NOT NULL DEFAULT '',
			description       TEXT NOT NULL DEFAULT '',
			work_dir          TEXT NOT NULL DEFAULT '',
			permission_mode   TEXT NOT NULL DEFAULT 'default',
			allowed_tools     TEXT NOT NULL DEFAULT '',
			last_active       TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE TABLE IF NOT EXISTS messages (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			role       TEXT NOT NULL,
			content    TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
	`)
	if err != nil {
		return err
	}
	// 新增欄位（已存在時忽略錯誤）
	db.Exec(`ALTER TABLE sessions ADD COLUMN pending_denials TEXT NOT NULL DEFAULT ''`)
	db.Exec(`ALTER TABLE sessions ADD COLUMN agent_type TEXT NOT NULL DEFAULT 'claude'`)
	// 舊 DB 的 claude_id 欄位更名為 agent_session_id（SQLite 3.25+ 支援）
	db.Exec(`ALTER TABLE sessions RENAME COLUMN claude_id TO agent_session_id`)
	// 保險：若上述 RENAME 在極舊 DB 失敗，仍可直接補上欄位
	db.Exec(`ALTER TABLE sessions ADD COLUMN agent_session_id TEXT NOT NULL DEFAULT ''`)
	return nil
}
