package db

import (
	"errors"
	"strings"
)

// ResetRoomMemberSession 在同一個交易內確認「閒置、無待處理、僅屬本工作室、PM 仍是 pmID」，
// 再清掉續接、訊息、佇列與 quota 續跑。連線設定為 _txlock=immediate，交易一開始就持有寫鎖，
// 所以檢查與清除之間沒有別的寫入（換 PM、加入別間）能插進來。任一前提不成立就回錯、資料不動。
func (db *DB) ResetRoomMemberSession(sessionID, roomID, pmID string) error {
	if strings.TrimSpace(roomID) == "" || strings.TrimSpace(pmID) == "" {
		return errors.New("重置條件不足，未重置")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status, denials, shell string
	if err := tx.QueryRow(`SELECT status, pending_denials, shell_pending FROM sessions WHERE id = ?`, sessionID).Scan(&status, &denials, &shell); err != nil {
		return err
	}
	if status != SessionStatusIdle {
		return errors.New("session 忙碌（" + status + "），未重置")
	}
	if strings.TrimSpace(denials) != "" || strings.TrimSpace(shell) != "" {
		return errors.New("session 有待授權或待確認，未重置")
	}
	rows, err := tx.Query(`SELECT room_id FROM room_members WHERE session_id = ?`, sessionID)
	if err != nil {
		return err
	}
	var rooms []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		rooms = append(rooms, id)
	}
	rows.Close()
	if len(rooms) != 1 || rooms[0] != roomID {
		return errors.New("只重置僅屬於這個工作室的會話；共用、其他工作室或不在工作室的會話未重置")
	}
	var pmRoom string
	if err := tx.QueryRow("SELECT room_id FROM ("+roomPMQuery+") WHERE session_id = ?", pmID).Scan(&pmRoom); err != nil || pmRoom != roomID {
		return errors.New("已不是這個工作室的 PM，未重置")
	}
	for _, q := range []string{
		`UPDATE queue_batches SET state='cancelled' WHERE session_id=? AND state='pending'`,
		`DELETE FROM messages WHERE session_id = ?`,
		`DELETE FROM queued_messages WHERE session_id = ?`,
		`UPDATE sessions SET agent_session_id = '', pending_denials = '', shell_pending = '', status = 'idle', last_active = datetime('now') WHERE id = ?`,
	} {
		if _, err := tx.Exec(q, sessionID); err != nil {
			return err
		}
	}
	if err := execCancelQuotaResume(tx, sessionID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if db.OnSessionChange != nil {
		db.OnSessionChange()
	}
	return nil
}
