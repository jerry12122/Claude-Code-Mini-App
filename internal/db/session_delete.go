package db

func (db *DB) DeleteSessionApproved(id, roomID, pmID, approval string) error {
	return db.deleteSession(id, roomID, pmID, &approval)
}

func (db *DB) deleteSession(id, roomID, pmID string, approval *string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if approval != nil {
		if err := consumeOperation(tx, *approval, "delete_session", roomID, pmID, id); err != nil {
			return err
		}
		if err := idleCleanupTarget(tx, id); err != nil {
			return err
		}
	}
	// 與原本 UI 刪除共用清理清單；任何錯誤連批准的消耗一起回滾。
	for _, q := range []string{
		`DELETE FROM input_receipts WHERE session_id=?`,
		`DELETE FROM messages WHERE session_id=?`,
		`DELETE FROM queued_messages WHERE session_id=?`,
		`DELETE FROM queue_batches WHERE session_id=?`,
		`DELETE FROM attachment_access WHERE session_id=?`,
		`DELETE FROM attachments WHERE session_id=? AND NOT EXISTS(SELECT 1 FROM attachment_access WHERE attachment_id=attachments.id)`,
		`DELETE FROM room_members WHERE session_id=?`,
		`UPDATE shares SET revoked=1 WHERE session_id=?`,
		`DELETE FROM sessions WHERE id=?`,
	} {
		if _, err := tx.Exec(q, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
