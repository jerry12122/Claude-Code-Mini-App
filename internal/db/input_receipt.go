package db

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

var ErrInputConflict = errors.New("訊息 ID 已用於不同內容")

func inputDigest(content, author string, ids []string) string {
	if ids == nil {
		ids = []string{}
	}
	b, _ := json.Marshal([]any{content, author, ids})
	hash := sha256.Sum256(b)
	return hex.EncodeToString(hash[:])
}

// InputAccepted also rejects reuse of an ID for a different payload.
// Receipts outlive queue/history deletion, so a delayed retry cannot rerun old work.
func (db *DB) InputAccepted(sessionID, actor, requestID, content, author string, ids []string) (bool, error) {
	var digest string
	err := db.QueryRow(`SELECT digest FROM input_receipts WHERE session_id = ? AND actor = ? AND request_id = ?`, sessionID, actor, requestID).Scan(&digest)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if digest != inputDigest(content, author, ids) {
		return false, ErrInputConflict
	}
	return true, nil
}

// AcceptInput atomically records acceptance and queues the work. A crash between
// commit and acknowledgement leaves one recoverable queue item, never a receipt alone.
func (db *DB) AcceptInput(sessionID, actor, requestID, content, author string, ids []string) (duplicate bool, err error) {
	if requestID == "" || len(requestID) > 128 {
		return false, fmt.Errorf("訊息 ID 必須介於 1 到 128 bytes")
	}
	if _, err := db.ResolveAttachments(sessionID, ids); err != nil {
		return false, err
	}
	tx, err := db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	digest := inputDigest(content, author, ids)
	res, err := tx.Exec(`INSERT INTO input_receipts (session_id, actor, request_id, digest)
		SELECT id, ?, ?, ? FROM sessions WHERE id = ?
		ON CONFLICT(session_id, actor, request_id) DO NOTHING`, actor, requestID, digest, sessionID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 0 {
		var previous string
		if err := tx.QueryRow(`SELECT digest FROM input_receipts WHERE session_id = ? AND actor = ? AND request_id = ?`, sessionID, actor, requestID).Scan(&previous); err != nil {
			return false, err
		}
		if previous != digest {
			return false, ErrInputConflict
		}
		return true, nil
	}
	if _, err := tx.Exec(`UPDATE sessions SET queue_paused = 0 WHERE id = ? AND NOT EXISTS (SELECT 1 FROM queued_messages WHERE session_id = ?)`, sessionID, sessionID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`INSERT INTO queued_messages (session_id, content, attachment_ids, author) VALUES (?, ?, ?, ?)`, sessionID, content, attachmentIDsJSON(ids), author); err != nil {
		return false, err
	}
	return false, tx.Commit()
}
