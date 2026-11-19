package db

import (
	"database/sql"
	"errors"
	"fmt"
)

// QueuedMessage 是 session 執行中時使用者先送出、等前一個任務完成才執行的訊息。
type QueuedMessage struct {
	ID              int64        `json:"id"`
	Content         string       `json:"content"`
	Author          string       `json:"author,omitempty"` // 送出者暱稱；空字串＝擁有者
	SourceSessionID string       `json:"source_session_id,omitempty"`
	CreatedAt       string       `json:"created_at"`
	AttachmentIDs   []string     `json:"-"`
	Attachments     []Attachment `json:"attachments,omitempty"`
}

// EnqueueMessage 把訊息排到佇列尾端。佇列原本為空時順便解除暫停：
// 暫停只針對「失敗當下還留著的那批」，新的一批重新開始。
func (db *DB) EnqueueMessage(sessionID, content, author string, ids ...string) (*QueuedMessage, error) {
	return db.enqueueMessage(sessionID, content, author, "", ids)
}

// EnqueueMessageFromCancelQuota 把新佇列訊息與取消舊自動復原放在同一筆交易。
// 取消失敗時不留下新訊息，也不解除暫停。source 不是既存 session 時不取消。
func (db *DB) EnqueueMessageFromCancelQuota(sessionID, content, author, source string, ids ...string) (*QueuedMessage, error) {
	if source != "" {
		var one int
		if err := db.QueryRow(`SELECT 1 FROM sessions WHERE id = ?`, source).Scan(&one); err != nil {
			return nil, fmt.Errorf("來源 session 不存在")
		}
	}
	attachments, err := db.ResolveAttachments(sessionID, ids)
	if err != nil {
		return nil, err
	}
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM queued_messages WHERE session_id = ?`, sessionID).Scan(&n); err != nil {
		return nil, err
	}
	if n == 0 {
		if _, err := tx.Exec(`UPDATE sessions SET queue_paused = 0 WHERE id = ?`, sessionID); err != nil {
			return nil, err
		}
	}
	res, err := tx.Exec(`INSERT INTO queued_messages (session_id, content, attachment_ids, author, source_session_id) VALUES (?, ?, ?, ?, ?)`, sessionID, content, attachmentIDsJSON(ids), author, source)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	if err := execCancelQuotaResume(tx, sessionID); err != nil {
		return nil, err
	}
	var createdAt string
	if err := tx.QueryRow(`SELECT created_at FROM queued_messages WHERE id = ?`, id).Scan(&createdAt); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &QueuedMessage{
		ID: id, Content: content, Author: author, SourceSessionID: source, CreatedAt: createdAt,
		AttachmentIDs: ids, Attachments: attachments,
	}, nil
}

func (db *DB) enqueueMessage(sessionID, content, author, source string, ids []string) (*QueuedMessage, error) {
	attachments, err := db.ResolveAttachments(sessionID, ids)
	if err != nil {
		return nil, err
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM queued_messages WHERE session_id = ?`, sessionID).Scan(&n); err != nil {
		return nil, err
	}
	if n == 0 {
		if err := db.SetQueuePaused(sessionID, false); err != nil {
			return nil, err
		}
	}
	res, err := db.Exec(`INSERT INTO queued_messages (session_id, content, attachment_ids, author, source_session_id) VALUES (?, ?, ?, ?, ?)`, sessionID, content, attachmentIDsJSON(ids), author, source)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	q := &QueuedMessage{ID: id, Content: content, Author: author, SourceSessionID: source, AttachmentIDs: ids, Attachments: attachments}
	err = db.QueryRow(`SELECT created_at FROM queued_messages WHERE id = ?`, id).Scan(&q.CreatedAt)
	return q, err
}

// ListQueuedMessages 依送出順序（FIFO）回傳佇列。
func (db *DB) ListQueuedMessages(sessionID string) ([]QueuedMessage, error) {
	rows, err := db.Query(`SELECT id, content, created_at, attachment_ids, author, source_session_id FROM queued_messages WHERE session_id = ? ORDER BY id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []QueuedMessage{}
	for rows.Next() {
		var q QueuedMessage
		var ids string
		if err := rows.Scan(&q.ID, &q.Content, &q.CreatedAt, &ids, &q.Author, &q.SourceSessionID); err != nil {
			return nil, err
		}
		q.AttachmentIDs, err = decodeAttachmentIDs(ids)
		if err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	all, err := db.AttachmentMap(sessionID)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Attachments = hydrateAttachments(out[i].AttachmentIDs, all)
	}
	return out, nil
}

// PopQueuedMessage 取出並刪除最早的一則；佇列為空時回傳 nil, nil。
func (db *DB) PopQueuedMessage(sessionID string) (*QueuedMessage, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var q QueuedMessage
	var ids string
	err = tx.QueryRow(`SELECT id, content, created_at, attachment_ids, author, source_session_id FROM queued_messages WHERE session_id = ? ORDER BY id LIMIT 1`, sessionID).
		Scan(&q.ID, &q.Content, &q.CreatedAt, &ids, &q.Author, &q.SourceSessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	q.AttachmentIDs, err = decodeAttachmentIDs(ids)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`DELETE FROM queued_messages WHERE id = ?`, q.ID); err != nil {
		return nil, err
	}
	return &q, tx.Commit()
}

// DeleteQueuedMessage 刪除指定一則；帶 sessionID 避免跨 session 誤刪。
// 不取消自動復原。Telegram 出列走這裡，避免把正在跑的排程清掉。
func (db *DB) DeleteQueuedMessage(sessionID string, id int64) error {
	_, err := db.Exec(`DELETE FROM queued_messages WHERE session_id = ? AND id = ?`, sessionID, id)
	return err
}

// DeleteQueuedMessageCancelQuota 在同一筆交易刪除指定列並取消自動復原。
// 任一失敗都回滾，佇列與狀態保持原樣。列已經不在時仍取消排程，與原本的冪等刪除一致。
func (db *DB) DeleteQueuedMessageCancelQuota(sessionID string, id int64) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM queued_messages WHERE session_id = ? AND id = ?`, sessionID, id); err != nil {
		return err
	}
	if err := execCancelQuotaResume(tx, sessionID); err != nil {
		return err
	}
	return tx.Commit()
}

// ClearQueuedMessages 刪掉這個 session 的佇列，並在同一筆交易把自動復原清成 none、世代加 1。
// 世代不歸零。刪除本身不解除暫停，也不啟動佇列。
func (db *DB) ClearQueuedMessages(sessionID string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM queued_messages WHERE session_id = ?`, sessionID); err != nil {
		return err
	}
	if err := execCancelQuotaResume(tx, sessionID); err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) SetQueuePaused(sessionID string, paused bool) error {
	v := 0
	if paused {
		v = 1
	}
	_, err := db.Exec(`UPDATE sessions SET queue_paused = ? WHERE id = ?`, v, sessionID)
	return err
}

// PauseQueueIfPending 只在佇列還有東西時才暫停，避免空佇列殘留暫停狀態卡住下一批。
func (db *DB) PauseQueueIfPending(sessionID string) error {
	_, err := db.Exec(`UPDATE sessions SET queue_paused = 1
		WHERE id = ? AND EXISTS (SELECT 1 FROM queued_messages WHERE session_id = ?)`, sessionID, sessionID)
	return err
}

func (db *DB) QueuePaused(sessionID string) (bool, error) {
	var v int
	err := db.QueryRow(`SELECT queue_paused FROM sessions WHERE id = ?`, sessionID).Scan(&v)
	return v != 0, err
}

// CountQueuedMessages 回傳 session 佇列中待執行的訊息數。
func (db *DB) CountQueuedMessages(sessionID string) (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM queued_messages WHERE session_id = ?`, sessionID).Scan(&n)
	return n, err
}
