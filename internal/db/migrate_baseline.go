package db

import (
	"fmt"
	"log/slog"
	"strings"
)

// migrateBaseline 是 PRAGMA user_version 機制出現之前的整個 schema（原 DB.migrate）。
// 全部是冪等的：舊資料庫（user_version 為 0）重跑一次不會改變任何東西，新資料庫則一次建齊。
// 凍結：不要修改這個檔案；新的 schema 變更請在 migrate.go 的 migrations 尾端追加。
func migrateBaseline(db *DB) error {
	if err := createBaseTables(db); err != nil {
		return err
	}
	if err := addLegacyColumns(db); err != nil {
		return err
	}
	if err := db.migrateRooms(); err != nil {
		return err
	}
	return db.migrateMessageReadState()
}

// createBaseTables 建立 v1 之前就存在的資料表（IF NOT EXISTS，可重複執行）。
func createBaseTables(db *DB) error {
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
		CREATE TABLE IF NOT EXISTS input_receipts (
			session_id TEXT NOT NULL,
			actor TEXT NOT NULL,
			request_id TEXT NOT NULL,
			digest TEXT NOT NULL,
			PRIMARY KEY (session_id, actor, request_id)
		);
		CREATE INDEX IF NOT EXISTS idx_messages_session_id ON messages (session_id, id);
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
		CREATE TABLE IF NOT EXISTS attachment_access (
			session_id TEXT NOT NULL,
			attachment_id TEXT NOT NULL,
			PRIMARY KEY (session_id, attachment_id)
		);
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
	return nil
}

// addLegacyColumns 補齊 v1 之前陸續加入的欄位。舊寫法：已存在或來源欄位不存在時忽略錯誤，只記警告。
// 凍結：不要再往這裡加欄位，新的 schema 變更請加在 migrations 尾端。
func addLegacyColumns(db *DB) error {
	// 新增／更名欄位（已存在或來源欄位不存在時忽略錯誤）
	for _, query := range []string{
		`ALTER TABLE sessions ADD COLUMN system_instruction TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN global_instruction_text TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN global_instruction_mode TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE messages ADD COLUMN global_instruction_sent INTEGER NOT NULL DEFAULT 0`,
	} {
		if _, err := db.Exec(query); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return err
		}
	}
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
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_shell_wd_cmd_dir ON shell_workdir_commands (work_dir_key)`); err != nil {
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
	// last_read_at：已讀操作時間；舊資料的未讀狀態以此遷移至 messages.is_read。
	tryAlter(`ALTER TABLE sessions ADD COLUMN last_read_at TEXT NOT NULL DEFAULT ''`)
	// queue_paused：上一個任務失敗／中斷時佇列暫停，需使用者手動繼續。
	tryAlter(`ALTER TABLE sessions ADD COLUMN queue_paused INTEGER NOT NULL DEFAULT 0`)
	// last_error_text／last_error_at：最近一次 run 失敗的原始錯誤文字與時間，供外部 agent（MCP list_sessions）查詢；成功後清空。
	tryAlter(`ALTER TABLE sessions ADD COLUMN last_error_text TEXT NOT NULL DEFAULT ''`)
	tryAlter(`ALTER TABLE sessions ADD COLUMN last_error_at TEXT NOT NULL DEFAULT ''`)
	// disguise_agent：Telegram 秘密會話的偽裝 agent（例如 claude、cursor、antigravity；空字串＝無偽裝）。
	tryAlter(`ALTER TABLE sessions ADD COLUMN disguise_agent TEXT NOT NULL DEFAULT ''`)
	// pinned：側欄釘選（1＝釘選，置頂顯示）。
	tryAlter(`ALTER TABLE sessions ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0`)
	// author：訊息說話者。空字串＝擁有者（含舊資料），訪客為其暱稱。
	tryAlter(`ALTER TABLE messages ADD COLUMN author TEXT NOT NULL DEFAULT ''`)
	tryAlter(`ALTER TABLE queued_messages ADD COLUMN author TEXT NOT NULL DEFAULT ''`)
	// source_session_id：伺服器已驗證的跨 session 來源。空字串代表沒有這份紀錄；不能由訊息本文推回。
	tryAlter(`ALTER TABLE messages ADD COLUMN source_session_id TEXT NOT NULL DEFAULT ''`)
	tryAlter(`ALTER TABLE queued_messages ADD COLUMN source_session_id TEXT NOT NULL DEFAULT ''`)
	// quota_resume_*：5 小時額度用盡後的自動復原狀態（none／waiting／resuming／exhausted）。
	tryAlter(`ALTER TABLE sessions ADD COLUMN quota_resume_state TEXT NOT NULL DEFAULT 'none'`)
	tryAlter(`ALTER TABLE sessions ADD COLUMN quota_resume_at TEXT NOT NULL DEFAULT ''`)
	tryAlter(`ALTER TABLE sessions ADD COLUMN quota_resume_attempts INTEGER NOT NULL DEFAULT 0`)
	// quota_resume_generation：waiting→resuming、取消與關閉開關都加 1，不歸零、不重用。舊的收尾只能比對自己的世代。
	tryAlter(`ALTER TABLE sessions ADD COLUMN quota_resume_generation INTEGER NOT NULL DEFAULT 0`)
	return nil
}
