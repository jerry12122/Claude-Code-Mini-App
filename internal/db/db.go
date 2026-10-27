package db

import (
	"database/sql"

	"strings"

	"fmt"
	"log/slog"
	_ "modernc.org/sqlite"
)

type DB struct {
	*sql.DB
	// OnSessionChange：session 狀態／活動時間變動後呼叫（可為 nil），供上層推播列表更新。
	OnSessionChange func()
}

func Open(path string) (*DB, error) {
	// modernc.org/sqlite 只認 _pragma=...；舊的 _journal_mode/_busy_timeout（mattn 語法）會被靜默忽略，
	// 導致實際是 delete journal、busy_timeout=0，並行讀寫時直接 SQLITE_BUSY。
	// busy_timeout 放前面：切 WAL 需要鎖，先設好等待時間。
	sqldb, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db := &DB{DB: sqldb}
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
			last_active       TEXT NOT NULL DEFAULT (datetime('now')),
			disguise_agent    TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE IF NOT EXISTS messages (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			role       TEXT NOT NULL,
			content    TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE TABLE IF NOT EXISTS shell_workdir_commands (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			work_dir_key TEXT    NOT NULL,
			command      TEXT    NOT NULL,
			created_at   TEXT    NOT NULL DEFAULT (datetime('now')),
			UNIQUE (work_dir_key, command)
		);
		CREATE TABLE IF NOT EXISTS model_options (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			agent_type TEXT    NOT NULL,
			model_id   TEXT    NOT NULL,
			label      TEXT    NOT NULL,
			enabled    INTEGER NOT NULL DEFAULT 1,
			UNIQUE (agent_type, model_id)
		);
		CREATE TABLE IF NOT EXISTS settings (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL DEFAULT '{}'
		);
		CREATE TABLE IF NOT EXISTS work_dirs (
			path TEXT PRIMARY KEY
		);
		CREATE TABLE IF NOT EXISTS queued_messages (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			content    TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE INDEX IF NOT EXISTS idx_queued_messages_session ON queued_messages (session_id, id);
		CREATE TABLE IF NOT EXISTS attachments (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			original_name TEXT NOT NULL,
			storage_name TEXT NOT NULL,
			mime_type TEXT NOT NULL,
			size INTEGER NOT NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE INDEX IF NOT EXISTS idx_attachments_session ON attachments (session_id);
		CREATE TABLE IF NOT EXISTS shares (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id      TEXT    NOT NULL,
			token           TEXT    NOT NULL UNIQUE,
			pin             TEXT    NOT NULL,
			role            TEXT    NOT NULL,
			mode            TEXT    NOT NULL,
			snapshot_msg_id INTEGER,
			expires_at      TEXT    NOT NULL,
			revoked         INTEGER NOT NULL DEFAULT 0,
			failed_attempts INTEGER NOT NULL DEFAULT 0,
			created_at      TEXT    NOT NULL DEFAULT (datetime('now'))
		);
		CREATE INDEX IF NOT EXISTS idx_shares_session ON shares (session_id);
		CREATE TABLE IF NOT EXISTS share_guests (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			share_id   INTEGER NOT NULL,
			nickname   TEXT    NOT NULL,
			token      TEXT    NOT NULL UNIQUE,
			created_at TEXT    NOT NULL DEFAULT (datetime('now'))
		);
		CREATE INDEX IF NOT EXISTS idx_share_guests_share ON share_guests (share_id);
	`)
	if err != nil {
		return err
	}
	// 新增／更名欄位（已存在或來源欄位不存在時忽略錯誤）
	tryAlter := func(q string) {
		if _, err := db.Exec(q); err != nil {
			msg := err.Error()
			if strings.Contains(msg, "duplicate column") || strings.Contains(msg, "no such column") {
				return
			}
			slog.Info(fmt.Sprintf("[migrate] warning: %s: %v", q, err))
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
	if _, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_shell_wd_cmd_dir ON shell_workdir_commands (work_dir_key)`); err != nil {
		return err
	}
	// 新增 shell_pending 欄位（已存在時忽略）
	tryAlter(`ALTER TABLE sessions ADD COLUMN shell_pending TEXT NOT NULL DEFAULT ''`)
	tryAlter(`ALTER TABLE messages ADD COLUMN result_text TEXT NOT NULL DEFAULT ''`)
	// Attachment migrations are required: fail startup rather than silently lose metadata.
	for _, table := range []string{"messages", "queued_messages"} {
		if _, err := db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN attachment_ids TEXT NOT NULL DEFAULT '[]'`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return err
		}
	}
	tryAlter(`ALTER TABLE sessions ADD COLUMN active_model TEXT NOT NULL DEFAULT ''`)
	tryAlter(`ALTER TABLE sessions ADD COLUMN active_model_source TEXT NOT NULL DEFAULT ''`)
	tryAlter(`ALTER TABLE sessions ADD COLUMN active_model_at TEXT NOT NULL DEFAULT ''`)
	// model/effort：使用者要求值（空字串＝不指定，交給 CLI 用預設）
	tryAlter(`ALTER TABLE sessions ADD COLUMN model TEXT NOT NULL DEFAULT ''`)
	tryAlter(`ALTER TABLE sessions ADD COLUMN effort TEXT NOT NULL DEFAULT ''`)
	// last_read_at：使用者最後一次「正在看」此 session 的時間；配合 last_active 判斷未讀。
	tryAlter(`ALTER TABLE sessions ADD COLUMN last_read_at TEXT NOT NULL DEFAULT ''`)
	// queue_paused：上一個任務失敗／中斷時佇列暫停，需使用者手動繼續。
	tryAlter(`ALTER TABLE sessions ADD COLUMN queue_paused INTEGER NOT NULL DEFAULT 0`)
	// disguise_agent：Telegram 秘密會話的偽裝 agent（例如 claude、cursor、antigravity；空字串＝無偽裝）。
	tryAlter(`ALTER TABLE sessions ADD COLUMN disguise_agent TEXT NOT NULL DEFAULT ''`)
	// author：訊息說話者。空字串＝擁有者（含舊資料），訪客為其暱稱。
	tryAlter(`ALTER TABLE messages ADD COLUMN author TEXT NOT NULL DEFAULT ''`)
	tryAlter(`ALTER TABLE queued_messages ADD COLUMN author TEXT NOT NULL DEFAULT ''`)

	// 既有 session 的 work_dir 補進目錄清單
	if err := db.seedWorkDirsFromSessions(); err != nil {
		return err
	}

	return nil
}
