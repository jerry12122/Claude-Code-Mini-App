package db

import (
	"database/sql"
	"log"
	"strings"

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
	tryAlter := func(q string) {
		if _, err := db.Exec(q); err != nil {
			if !strings.Contains(err.Error(), "duplicate column") {
				log.Printf("[migrate] warning: %s: %v", q, err)
			}
		}
	}
	tryAlter(`ALTER TABLE sessions ADD COLUMN pending_denials TEXT NOT NULL DEFAULT ''`)
	tryAlter(`ALTER TABLE sessions ADD COLUMN agent_type TEXT NOT NULL DEFAULT 'claude'`)
	// 舊 DB 的 claude_id 欄位更名為 agent_session_id（SQLite 3.25+ 支援）
	tryAlter(`ALTER TABLE sessions RENAME COLUMN claude_id TO agent_session_id`)
	// 保險：若上述 RENAME 在極舊 DB 失敗，仍可直接補上欄位
	tryAlter(`ALTER TABLE sessions ADD COLUMN agent_session_id TEXT NOT NULL DEFAULT ''`)
	tryAlter(`ALTER TABLE sessions ADD COLUMN status TEXT NOT NULL DEFAULT 'idle'`)
	tryAlter(`ALTER TABLE sessions ADD COLUMN cli_extra_args TEXT NOT NULL DEFAULT '[]'`)
	tryAlter(`ALTER TABLE sessions ADD COLUMN input_mode TEXT NOT NULL DEFAULT 'agent'`)
	tryAlter(`ALTER TABLE messages ADD COLUMN status TEXT NOT NULL DEFAULT 'done'`)
	// 重啟後孤兒 pending：標記為 done，保留已累積內容
	if _, err := db.Exec(`UPDATE messages SET status = 'done' WHERE status = 'pending'`); err != nil {
		return err
	}

	// 新增 shell_workdir_commands 表
	if _, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS shell_workdir_commands (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			work_dir_key TEXT    NOT NULL,
			command      TEXT    NOT NULL,
			created_at   TEXT    NOT NULL DEFAULT (datetime('now')),
			UNIQUE (work_dir_key, command)
		)
	`); err != nil {
		return err
	}
	if _, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_shell_wd_cmd_dir ON shell_workdir_commands (work_dir_key)`); err != nil {
		return err
	}
	// 新增 shell_pending 欄位
	db.Exec(`ALTER TABLE sessions ADD COLUMN shell_pending TEXT NOT NULL DEFAULT ''`)

	return nil
}
