package db

import (
	"database/sql"
	"errors"
)

// QueuedMessage 是 session 執行中時使用者先送出、等前一個任務完成才執行的訊息。
type QueuedMessage struct {
	ID            int64        `json:"id"`
	Content       string       `json:"content"`
	Author        string       `json:"author,omitempty"` // 送出者暱稱；空字串＝擁有者
	CreatedAt     string       `json:"created_at"`
	AttachmentIDs []string     `json:"-"`
	Attachments   []Attachment `json:"attachments,omitempty"`
}

// EnqueueMessage 把訊息排到佇列尾端。佇列原本為空時順便解除暫停：
// 暫停只針對「失敗當下還留著的那批」，新的一批重新開始。
func (db *DB) EnqueueMessage(sessionID, content, author string, ids ...string) (*QueuedMessage, error) {
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
	res, err := db.Exec(`INSERT INTO queued_messages (session_id, content, attachment_ids, author) VALUES (?, ?, ?, ?)`, sessionID, content, attachmentIDsJSON(ids), author)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	q := &QueuedMessage{ID: id, Content: content, Author: author, AttachmentIDs: ids, Attachments: attachments}
	err = db.QueryRow(`SELECT created_at FROM queued_messages WHERE id = ?`, id).Scan(&q.CreatedAt)
	return q, err
}

// ListQueuedMessages 依送出順序（FIFO）回傳佇列。
func (db *DB) ListQueuedMessages(sessionID string) ([]QueuedMessage, error) {
	rows, err := db.Query(`SELECT id, content, created_at, attachment_ids, author FROM queued_messages WHERE session_id = ? ORDER BY id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []QueuedMessage{}
	for rows.Next() {
		var q QueuedMessage
		var ids string
		if err := rows.Scan(&q.ID, &q.Content, &q.CreatedAt, &ids, &q.Author); err != nil {
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
	err = tx.QueryRow(`SELECT id, content, created_at, attachment_ids, author FROM queued_messages WHERE session_id = ? ORDER BY id LIMIT 1`, sessionID).
		Scan(&q.ID, &q.Content, &q.CreatedAt, &ids, &q.Author)
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
func (db *DB) DeleteQueuedMessage(sessionID string, id int64) error {
	_, err := db.Exec(`DELETE FROM queued_messages WHERE session_id = ? AND id = ?`, sessionID, id)
	return err
}

func (db *DB) ClearQueuedMessages(sessionID string) error {
	_, err := db.Exec(`DELETE FROM queued_messages WHERE session_id = ?`, sessionID)
	return err
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
