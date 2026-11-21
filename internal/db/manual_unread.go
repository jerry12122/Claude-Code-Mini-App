package db

import (
	"database/sql"
	"strings"
)

func migrateManualUnread(tx *sql.Tx) error {
	if err := addSessionColumn(tx, "manual_unread", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	return addSessionColumn(tx, "manual_unread_revision", "INTEGER NOT NULL DEFAULT 0")
}

func addSessionColumn(tx *sql.Tx, name, decl string) error {
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('sessions') WHERE name = ?`, name).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err := tx.Exec(`ALTER TABLE sessions ADD COLUMN ` + name + ` ` + decl)
	return err
}

// MarkSessionManualUnread 把手動提醒設為 true，並把伺服器 revision 加 1。
// 已是 true 也再加 1。不改已讀、last_active、status、釘選、分類或工作目錄。
func (db *DB) MarkSessionManualUnread(id string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE sessions SET manual_unread = 1, manual_unread_revision = manual_unread_revision + 1 WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

// ObservedRead 是客戶端在某個時刻看到的一筆會話：當時的 marker revision，以及當時已完成未讀的訊息 ID。
type ObservedRead struct {
	ID         string
	Revision   int64
	MessageIDs []int64
}

// MarkerState 是交易結束時伺服器上的手動標記。
type MarkerState struct {
	ID           string `json:"id"`
	ManualUnread bool   `json:"manual_unread"`
	Revision     int64  `json:"manual_unread_revision"`
}

// ObservedReadResult 是整批觀察套用的結果。Conflict 表示至少有一筆 revision 已經不是觀察值。
type ObservedReadResult struct {
	States   []MarkerState
	Conflict bool
	Changed  bool
}

// ApplyObservedReads 在同一個立即交易裡比對 revision、清除對上的標記，並只確認名單裡現在已完成的訊息。
// 空名單不讀任何訊息。revision 不符時不改那筆標記，但仍確認它觀察到且已完成的訊息。
func (db *DB) ApplyObservedReads(entries []ObservedRead) (ObservedReadResult, error) {
	var out ObservedReadResult
	if len(entries) == 0 {
		out.States = []MarkerState{}
		return out, nil
	}
	tx, err := db.Begin()
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	for _, entry := range entries {
		var one int
		err = tx.QueryRow(`SELECT 1 FROM sessions WHERE id = ?`, entry.ID).Scan(&one)
		if err != nil {
			return ObservedReadResult{}, err
		}
	}
	for _, entry := range entries {
		res, err := tx.Exec(`UPDATE sessions SET manual_unread = 0, manual_unread_revision = manual_unread_revision + 1 WHERE id = ? AND manual_unread_revision = ?`, entry.ID, entry.Revision)
		if err != nil {
			return ObservedReadResult{}, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return ObservedReadResult{}, err
		}
		if n == 0 {
			out.Conflict = true
		} else {
			out.Changed = true
		}
		acked, err := ackCompletedMessages(tx, entry.ID, entry.MessageIDs)
		if err != nil {
			return ObservedReadResult{}, err
		}
		if acked > 0 {
			out.Changed = true
		}
		state, err := readMarkerState(tx, entry.ID)
		if err != nil {
			return ObservedReadResult{}, err
		}
		out.States = append(out.States, state)
	}
	if err := tx.Commit(); err != nil {
		return ObservedReadResult{}, err
	}
	return out, nil
}

func readMarkerState(tx *sql.Tx, id string) (MarkerState, error) {
	var manual int
	var state MarkerState
	state.ID = id
	err := tx.QueryRow(`SELECT manual_unread, manual_unread_revision FROM sessions WHERE id = ?`, id).Scan(&manual, &state.Revision)
	state.ManualUnread = manual != 0
	return state, err
}

func ackCompletedMessages(tx *sql.Tx, sessionID string, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	args := make([]any, 0, len(ids)+1)
	args = append(args, sessionID)
	holders := make([]string, len(ids))
	for i, id := range ids {
		holders[i] = "?"
		args = append(args, id)
	}
	res, err := tx.Exec(`UPDATE messages SET is_read = 1 WHERE session_id = ? AND status != 'pending' AND role != 'user' AND id IN (`+strings.Join(holders, ",")+`)`, args...)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil || n == 0 {
		return n, err
	}
	_, err = tx.Exec(`UPDATE sessions SET last_read_at = datetime('now') WHERE id = ?`, sessionID)
	return n, err
}
