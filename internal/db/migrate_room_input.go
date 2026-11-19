package db

import (
	"database/sql"
	"fmt"
)

// input_request_id：這次送出的穩定 ID。佇列提升成歷史後仍帶著，前端用它合併本地氣泡，不用文字比對。
// 測試會把 user_version 退回 0 重跑遷移，此時欄位已存在，所以先查再加。
func migrateInputRequestID(tx *sql.Tx) error {
	for _, table := range []string{"messages", "queued_messages"} {
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = 'input_request_id'`, table).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		if _, err := tx.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN input_request_id TEXT NOT NULL DEFAULT ''`, table)); err != nil {
			return err
		}
	}
	return nil
}
