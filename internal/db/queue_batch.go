package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

type QueueBatchItem struct {
	ID       int64   `json:"id"`
	Previous *string `json:"previous"`
}

var (
	ErrQueueBatchConflict  = errors.New("佇列或批次已變更，請重新載入；已安排的批次請先取消再編輯或移除")
	ErrQueueBatchForbidden = errors.New("此來源不可一起送")
	ErrQueueBatchInvalid   = errors.New("請提供 request_id 與 2–100 筆依序的 id／previous，總文字最多 16 MiB")
)

func migrateQueueBatches(tx *sql.Tx) error {
	// 舊版重建基線／測試可能保留新欄卻將 user_version 歸零，沿既有遷移模式檢查欄位。
	rows, err := tx.Query(`PRAGMA table_info(queued_messages)`)
	if err != nil {
		return err
	}
	hasColumn := false
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			rows.Close()
			return err
		}
		hasColumn = hasColumn || name == "batch_id"
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if !hasColumn {
		if _, err := tx.Exec(`ALTER TABLE queued_messages ADD COLUMN batch_id TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	_, err = tx.Exec(`CREATE TABLE IF NOT EXISTS queue_batches(id TEXT PRIMARY KEY, session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 room_id TEXT NOT NULL, request_id TEXT NOT NULL, digest TEXT NOT NULL, snapshot TEXT NOT NULL,
 state TEXT NOT NULL DEFAULT 'pending', UNIQUE(session_id,room_id,request_id));
 CREATE INDEX IF NOT EXISTS queued_batch ON queued_messages(batch_id);`)
	return err
}

type queueBatchRow struct {
	ID                                                  int64
	Content, Author, Source, RequestID, RawIDs, BatchID string
}

func queueBatchRows(ctx context.Context, tx *sql.Tx, sessionID string) ([]queueBatchRow, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id,content,author,source_session_id,input_request_id,attachment_ids,batch_id FROM queued_messages WHERE session_id=? ORDER BY id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []queueBatchRow
	for rows.Next() {
		var r queueBatchRow
		if err := rows.Scan(&r.ID, &r.Content, &r.Author, &r.Source, &r.RequestID, &r.RawIDs, &r.BatchID); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func queueBatchScope(ctx context.Context, tx *sql.Tx, roomID, sessionID string) (string, error) {
	if roomID == "" {
		return "", nil
	}
	pm, err := roomPM(tx.QueryRowContext(ctx, "SELECT session_id FROM ("+roomPMQuery+") WHERE room_id=?", roomID))
	if err != nil {
		return "", err
	}
	if pm != sessionID {
		return "", ErrRoomPMChanged
	}
	return RoomUserAuthorPrefix + roomID, nil
}

func queueBatchAttachmentIDs(ctx context.Context, tx *sql.Tx, sessionID string, rows []queueBatchRow) ([]string, error) {
	union := []string{}
	seen := map[string]bool{}
	for _, r := range rows {
		ids, err := decodeAttachmentIDs(r.RawIDs)
		if err != nil || ids == nil {
			return nil, ErrInvalidAttachment
		}
		local := map[string]bool{}
		for _, id := range ids {
			if local[id] {
				return nil, ErrInvalidAttachment
			}
			local[id] = true
			var accessible bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM attachments WHERE id=? AND (session_id=? OR EXISTS(SELECT 1 FROM attachment_access WHERE session_id=? AND attachment_id=attachments.id)))`, id, sessionID, sessionID).Scan(&accessible); err != nil {
				return nil, err
			}
			if !accessible {
				return nil, ErrInvalidAttachment
			}
			if !seen[id] {
				seen[id] = true
				union = append(union, id)
			}
		}
	}
	return union, nil
}

// ScheduleQueueBatch 保留原 queue 與 receipt；快照、CAS、PM 與批次 receipt 在同一交易提交。
func (db *DB) ScheduleQueueBatch(ctx context.Context, roomID, sessionID, requestID string, items []QueueBatchItem) (string, error) {
	if requestID == "" || len(requestID) > 128 || strings.ContainsRune(requestID, 0) || !utf8.ValidString(requestID) || len(items) < 2 || len(items) > 100 {
		return "", ErrQueueBatchInvalid
	}
	size := 0
	for i, item := range items {
		if item.Previous == nil || item.ID <= 0 || (i > 0 && item.ID <= items[i-1].ID) {
			return "", ErrQueueBatchInvalid
		}
		size += len(*item.Previous)
		if size > 16<<20 || !utf8.ValidString(*item.Previous) || strings.ContainsRune(*item.Previous, 0) {
			return "", ErrQueueBatchInvalid
		}
	}
	b, _ := json.Marshal(items)
	sum := sha256.Sum256(b)
	digest := hex.EncodeToString(sum[:])
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	author, err := queueBatchScope(ctx, tx, roomID, sessionID)
	if err != nil {
		return "", err
	}
	var id, oldDigest string
	err = tx.QueryRowContext(ctx, `SELECT id,digest FROM queue_batches WHERE session_id=? AND room_id=? AND request_id=?`, sessionID, roomID, requestID).Scan(&id, &oldDigest)
	if err == nil {
		if oldDigest != digest {
			return "", ErrQueueBatchConflict
		}
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	var pending bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM queue_batches WHERE session_id=? AND room_id=? AND state='pending')`, sessionID, roomID).Scan(&pending); err != nil {
		return "", err
	}
	if pending {
		return "", ErrQueueBatchConflict
	}
	rows, err := queueBatchRows(ctx, tx, sessionID)
	if err != nil {
		return "", err
	}
	start := -1
	for i, r := range rows {
		if r.ID == items[0].ID {
			start = i
			break
		}
	}
	if start < 0 || start+len(items) > len(rows) {
		return "", ErrQueueBatchConflict
	}
	for _, r := range rows[:start] {
		if r.Author == author && r.Source == "" {
			return "", ErrQueueBatchConflict
		}
	}
	snapshot := append([]queueBatchRow(nil), rows[start:start+len(items)]...)
	for i, r := range snapshot {
		if r.Author != author || r.Source != "" {
			return "", ErrQueueBatchForbidden
		}
		if r.ID != items[i].ID || r.Content != *items[i].Previous || r.BatchID != "" {
			return "", ErrQueueBatchConflict
		}
	}
	if _, err := queueBatchAttachmentIDs(ctx, tx, sessionID, snapshot); err != nil {
		return "", err
	}
	b, _ = json.Marshal(snapshot)
	id = uuid.NewString()
	if _, err := tx.ExecContext(ctx, `INSERT INTO queue_batches(id,session_id,room_id,request_id,digest,snapshot) VALUES(?,?,?,?,?,?)`, id, sessionID, roomID, requestID, digest, string(b)); err != nil {
		return "", err
	}
	for _, r := range snapshot {
		if _, err := tx.ExecContext(ctx, `UPDATE queued_messages SET batch_id=? WHERE session_id=? AND id=?`, id, sessionID, r.ID); err != nil {
			return "", err
		}
	}
	return id, tx.Commit()
}

func (db *DB) CancelQueueBatch(ctx context.Context, roomID, sessionID, id string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := queueBatchScope(ctx, tx, roomID, sessionID); err != nil {
		return err
	}
	var state string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM queue_batches WHERE id=? AND session_id=? AND room_id=?`, id, sessionID, roomID).Scan(&state); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrQueueBatchConflict
		}
		return err
	}
	if state != "pending" {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE queued_messages SET batch_id='' WHERE session_id=? AND batch_id=?`, sessionID, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE queue_batches SET state='cancelled' WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) QueueBatchPending(sessionID, id string) (bool, error) {
	var pending bool
	err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM queue_batches WHERE session_id=? AND id=? AND state='pending')`, sessionID, id).Scan(&pending)
	return pending, err
}

// PromoteQueuedBatch 一次寫入每筆原歷史及 request id；其中一筆失敗便全部回滾。
func (db *DB) PromoteQueuedBatch(ctx context.Context, sessionID string, q QueuedMessage) ([]*Message, error) {
	all, err := db.AttachmentMap(sessionID)
	if err != nil {
		return nil, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var roomID, raw, state string
	if err := tx.QueryRowContext(ctx, `SELECT room_id,snapshot,state FROM queue_batches WHERE id=? AND session_id=?`, q.BatchID, sessionID).Scan(&roomID, &raw, &state); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrQueueBatchConflict
		}
		return nil, err
	}
	if state != "pending" {
		return nil, ErrQueueBatchConflict
	}
	author, err := queueBatchScope(ctx, tx, roomID, sessionID)
	if err != nil {
		return nil, err
	}
	var snapshot []queueBatchRow
	if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
		return nil, err
	}
	rows, err := queueBatchRows(ctx, tx, sessionID)
	if err != nil {
		return nil, err
	}
	if len(snapshot) < 2 || len(rows) < len(snapshot) || rows[0].ID != q.ID || snapshot[0].ID != q.ID {
		return nil, ErrQueueBatchConflict
	}
	for i, r := range snapshot {
		current := rows[i]
		current.BatchID = ""
		if current != r || rows[i].BatchID != q.BatchID || r.Author != author || r.Source != "" {
			return nil, ErrQueueBatchConflict
		}
	}
	if _, err := queueBatchAttachmentIDs(ctx, tx, sessionID, snapshot); err != nil {
		return nil, err
	}
	var out []*Message
	for _, r := range snapshot {
		res, err := tx.ExecContext(ctx, `INSERT INTO messages(session_id,role,content,status,attachment_ids,author,source_session_id,input_request_id) VALUES(?,'user',?,?,?,?,?,?)`, sessionID, r.Content, MessageStatusDone, r.RawIDs, r.Author, r.Source, r.RequestID)
		if err != nil {
			return nil, err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return nil, err
		}
		m := &Message{ID: id, SessionID: sessionID, Role: "user", Content: r.Content, Status: MessageStatusDone, Author: r.Author}
		m.AttachmentIDs, _ = decodeAttachmentIDs(r.RawIDs)
		if err := tx.QueryRowContext(ctx, `SELECT created_at FROM messages WHERE id=?`, id).Scan(&m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM queued_messages WHERE session_id=? AND batch_id=?`, sessionID, q.BatchID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE queue_batches SET state='completed' WHERE id=?`, q.BatchID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	for _, m := range out {
		m.Attachments = hydrateAttachments(m.AttachmentIDs, all)
	}
	return out, nil
}

// QueueBatchInput 只供出列前準備 runner prompt；最後仍由 Promote 在交易內逐筆重驗。
func (db *DB) QueueBatchInput(sessionID string, q QueuedMessage) (string, []string, error) {
	if q.BatchID == "" {
		return "", nil, ErrQueueBatchConflict
	}
	rows, err := db.ListQueuedMessages(sessionID)
	if err != nil {
		return "", nil, err
	}
	var texts, ids []string
	seen := map[string]bool{}
	for _, r := range rows {
		if r.BatchID == q.BatchID {
			texts = append(texts, r.Content)
			for _, id := range r.AttachmentIDs {
				if !seen[id] {
					seen[id] = true
					ids = append(ids, id)
				}
			}
		}
	}
	if len(texts) < 2 {
		return "", nil, ErrQueueBatchConflict
	}
	return strings.Join(texts, "\n\n"), ids, nil
}
