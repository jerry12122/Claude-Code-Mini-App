package db

import (
	"database/sql"
	"fmt"
)

// migration 是一次 schema 變更。upTx 在交易內執行，版本號在同一個交易更新（失敗整個回滾）；
// up 供舊的基線使用，它含多段 DDL 與資料修正且自己管理交易，成功後才更新版本。兩者擇一。
type migration struct {
	name string
	up   func(db *DB) error
	upTx func(tx *sql.Tx) error
}

// migrations 依序執行；已完成的版本記在 PRAGMA user_version。
//
// 規則：只能在尾端追加，不得修改或重排已發佈的項目。新項目用 upTx，失敗要回傳錯誤讓啟動中止，
// 不要像舊的 tryAlter 那樣吞掉錯誤。
var migrations = []migration{
	{name: "baseline", up: migrateBaseline},
	{name: "input_request_id", upTx: migrateInputRequestID},
	{name: "queue_batches", upTx: migrateQueueBatches},
}

// runMigrations 把資料庫從目前的 user_version 升到 len(list)。
func (db *DB) runMigrations(list []migration) error {
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("讀取資料庫版本失敗: %w", err)
	}
	if version > len(list) {
		return fmt.Errorf("資料庫版本 %d 比這個程式支援的 %d 新，請改用較新的版本", version, len(list))
	}
	for i := version; i < len(list); i++ {
		m := list[i]
		if err := db.applyMigration(i+1, m); err != nil {
			return fmt.Errorf("遷移 %d（%s）失敗: %w", i+1, m.name, err)
		}
	}
	return nil
}

func (db *DB) applyMigration(version int, m migration) error {
	if m.upTx == nil {
		if err := m.up(db); err != nil {
			return err
		}
		_, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, version))
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := m.upTx(tx); err != nil {
		return err
	}
	// PRAGMA 不接受參數；version 是我們自己的整數，沒有注入問題。
	if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, version)); err != nil {
		return err
	}
	return tx.Commit()
}
