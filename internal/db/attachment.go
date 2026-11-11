package db

import (
	"encoding/json"
	"fmt"
	"net/url"
)

// Attachment exposes display metadata; storage names never leave the server.
type Attachment struct {
	ID          string `json:"id"`
	SessionID   string `json:"-"`
	Name        string `json:"name"`
	StorageName string `json:"-"`
	MimeType    string `json:"mime_type"`
	Size        int64  `json:"size"`
	CreatedAt   string `json:"created_at"`
	URL         string `json:"url"`
}

func (db *DB) AddAttachment(a *Attachment) error {
	_, err := db.Exec(`INSERT INTO attachments (id, session_id, original_name, storage_name, mime_type, size) VALUES (?, ?, ?, ?, ?, ?)`,
		a.ID, a.SessionID, a.Name, a.StorageName, a.MimeType, a.Size)
	return err
}

func (db *DB) AttachmentMap(sessionID string) (map[string]Attachment, error) {
	return db.attachmentMap(sessionID, nil)
}

func (db *DB) attachmentMap(sessionID string, ids []string) (map[string]Attachment, error) {
	from := ` FROM attachments AS a`
	args := []any{sessionID}
	if ids != nil {
		if len(ids) == 0 {
			return map[string]Attachment{}, nil
		}
		// Start with the requested IDs, then use the attachment primary key;
		// otherwise SQLite may scan every attachment through the session index.
		from = ` FROM json_each(?) AS requested CROSS JOIN attachments AS a ON a.id = requested.value`
		args = []any{attachmentIDsJSON(ids), sessionID}
	}
	query := `SELECT a.id, a.original_name, a.storage_name, a.mime_type, a.size, a.created_at` + from + ` WHERE a.session_id = ?`
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]Attachment)
	for rows.Next() {
		a := Attachment{SessionID: sessionID}
		if err := rows.Scan(&a.ID, &a.Name, &a.StorageName, &a.MimeType, &a.Size, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.URL = "/sessions/" + url.PathEscape(sessionID) + "/uploads/" + url.PathEscape(a.ID)
		out[a.ID] = a
	}
	return out, rows.Err()
}

func (db *DB) GetAttachment(sessionID, id string) (*Attachment, error) {
	all, err := db.attachmentMap(sessionID, []string{id})
	if err != nil {
		return nil, err
	}
	a, ok := all[id]
	if !ok {
		return nil, fmt.Errorf("附件不存在或不屬於此會話")
	}
	return &a, nil
}

// ResolveAttachments preserves order and rejects unknown or duplicate IDs.
func (db *DB) ResolveAttachments(sessionID string, ids []string) ([]Attachment, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	all, err := db.attachmentMap(sessionID, ids)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	out := make([]Attachment, 0, len(ids))
	for _, id := range ids {
		a, ok := all[id]
		if !ok || seen[id] {
			return nil, fmt.Errorf("附件不存在、重複或不屬於此會話")
		}
		seen[id] = true
		out = append(out, a)
	}
	return out, nil
}

func attachmentIDsJSON(ids []string) string {
	if ids == nil {
		ids = []string{}
	}
	b, _ := json.Marshal(ids)
	return string(b)
}

func decodeAttachmentIDs(raw string) ([]string, error) {
	var ids []string
	err := json.Unmarshal([]byte(raw), &ids)
	return ids, err
}

func hydrateAttachments(ids []string, all map[string]Attachment) []Attachment {
	out := make([]Attachment, 0, len(ids))
	for _, id := range ids {
		if a, ok := all[id]; ok {
			out = append(out, a)
		}
	}
	return out
}

// AddUserMessageWithAttachments 寫入 user 訊息；author 為說話者暱稱，空字串代表擁有者。
func (db *DB) AddUserMessageWithAttachments(sessionID, content, author string, ids []string) (*Message, error) {
	return db.saveUserMessage(sessionID, content, author, ids, 0)
}

// PromoteQueuedMessage moves a queue item into history in one transaction.
func (db *DB) PromoteQueuedMessage(sessionID string, q QueuedMessage) (*Message, error) {
	return db.saveUserMessage(sessionID, q.Content, q.Author, q.AttachmentIDs, q.ID)
}

func (db *DB) saveUserMessage(sessionID, content, author string, ids []string, queueID int64) (*Message, error) {
	attachments, err := db.ResolveAttachments(sessionID, ids)
	if err != nil {
		return nil, err
	}
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if queueID != 0 {
		res, err := tx.Exec(`DELETE FROM queued_messages WHERE session_id = ? AND id = ? AND content = ? AND attachment_ids = ?`, sessionID, queueID, content, attachmentIDsJSON(ids))
		if err != nil {
			return nil, err
		}
		n, err := res.RowsAffected()
		if err != nil || n != 1 {
			return nil, fmt.Errorf("排隊訊息已變更或不存在")
		}
	}
	res, err := tx.Exec(`INSERT INTO messages (session_id, role, content, status, attachment_ids, author) VALUES (?, 'user', ?, ?, ?, ?)`,
		sessionID, content, MessageStatusDone, attachmentIDsJSON(ids), author)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	m := &Message{ID: id, SessionID: sessionID, Role: "user", Content: content, Status: MessageStatusDone, Author: author, AttachmentIDs: ids, Attachments: attachments}
	if err := tx.QueryRow(`SELECT created_at FROM messages WHERE id = ?`, id).Scan(&m.CreatedAt); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return m, nil
}

// AttachmentInSnapshot 回報附件是否被 id <= maxMsgID 的訊息引用（快照分享只能讀這些附件）。
func (db *DB) AttachmentInSnapshot(sessionID, attachmentID string, maxMsgID int64) (bool, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM messages m, json_each(m.attachment_ids) j
		WHERE m.session_id = ? AND m.id <= ? AND j.value = ?`, sessionID, maxMsgID, attachmentID).Scan(&n)
	return n > 0, err
}
