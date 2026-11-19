package db

import (
	"database/sql"

	"strings"

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
	sqldb, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_txlock=immediate")
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
	if err := db.runMigrations(migrations); err != nil {
		return err
	}
	// 以下不是 schema 版本，每次啟動都要做：把 session 的目錄補進清單、收回上次異常中斷留下的 pending 訊息。
	if err := db.seedWorkDirsFromSessions(); err != nil {
		return err
	}
	return db.ResetPendingMessages()
}

// Preserve existing read receipts once; new pending replies remain unread until completion.
func (db *DB) migrateMessageReadState() error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`ALTER TABLE messages ADD COLUMN is_read INTEGER NOT NULL DEFAULT 0`); err != nil {
		if strings.Contains(err.Error(), "duplicate column") {
			return nil
		}
		return err
	}
	if _, err := tx.Exec(`UPDATE messages SET is_read = 1 WHERE status != 'pending' AND created_at <=
		(SELECT last_read_at FROM sessions WHERE sessions.id = messages.session_id)`); err != nil {
		return err
	}
	return tx.Commit()
}
