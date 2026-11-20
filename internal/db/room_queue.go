package db

import (
	"context"
	"database/sql"
	"net/url"
)

type RoomQueueItem struct {
	QueuedMessage
	InputRequestID string `json:"input_request_id,omitempty"`
}

type RoomQueuePage struct {
	PMID       string          `json:"pm_id"`
	Queue      []RoomQueueItem `json:"queue"`
	Paused     bool            `json:"queue_paused"`
	HasMore    bool            `json:"has_more"`
	Next       int64           `json:"next_after_id"`
	BatchReady bool            `json:"batch_ready"`
}

// RoomQueue 與 member/timeline 無關；同一讀交易固定 PM、FIFO、附件權限與 paused。
func (db *DB) RoomQueue(ctx context.Context, roomID, expectedPM string, limit int, afterID int64) (RoomQueuePage, error) {
	out := RoomQueuePage{PMID: expectedPM, Queue: []RoomQueueItem{}}
	if roomID == "" || expectedPM == "" || limit < 1 || limit > 100 || afterID < 0 {
		return out, ErrQueueBatchInvalid
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if _, err := queueBatchScope(ctx, tx, roomID, expectedPM); err != nil {
		return out, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT queue_paused FROM sessions WHERE id=?`, expectedPM).Scan(&out.Paused); err != nil {
		return out, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,content,created_at,attachment_ids,batch_id,input_request_id FROM queued_messages
 WHERE session_id=? AND author=? AND source_session_id='' AND id>? ORDER BY id LIMIT ?`, expectedPM, RoomUserAuthorPrefix+roomID, afterID, limit+1)
	if err != nil {
		return out, err
	}
	var files []string
	seen := map[string]bool{}
	for rows.Next() {
		var item RoomQueueItem
		var rawIDs string
		if err := rows.Scan(&item.ID, &item.Content, &item.CreatedAt, &rawIDs, &item.BatchID, &item.InputRequestID); err != nil {
			rows.Close()
			return out, err
		}
		if len(out.Queue) == limit {
			out.HasMore = true
			break
		}
		item.AttachmentIDs, err = decodeAttachmentIDs(rawIDs)
		if err != nil {
			rows.Close()
			return out, err
		}
		for _, id := range item.AttachmentIDs {
			if !seen[id] {
				seen[id] = true
				files = append(files, id)
			}
		}
		out.Queue = append(out.Queue, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	rows.Close()
	all := map[string]Attachment{}
	if len(files) > 0 {
		attachments, err := tx.QueryContext(ctx, `SELECT a.id,a.session_id,a.original_name,a.storage_name,a.mime_type,a.size,a.created_at
 FROM json_each(?) wanted CROSS JOIN attachments a ON a.id=wanted.value WHERE a.session_id=? OR EXISTS
 (SELECT 1 FROM attachment_access g WHERE g.session_id=? AND g.attachment_id=a.id)`, attachmentIDsJSON(files), expectedPM, expectedPM)
		if err != nil {
			return out, err
		}
		for attachments.Next() {
			var a Attachment
			if err := attachments.Scan(&a.ID, &a.SessionID, &a.Name, &a.StorageName, &a.MimeType, &a.Size, &a.CreatedAt); err != nil {
				attachments.Close()
				return out, err
			}
			a.URL = "/sessions/" + url.PathEscape(expectedPM) + "/uploads/" + url.PathEscape(a.ID)
			all[a.ID] = a
		}
		if err := attachments.Err(); err != nil {
			attachments.Close()
			return out, err
		}
		attachments.Close()
		if len(all) != len(files) {
			return out, ErrInvalidAttachment
		}
	}
	for i := range out.Queue {
		out.Queue[i].Attachments = hydrateAttachments(out.Queue[i].AttachmentIDs, all)
	}
	if out.HasMore {
		out.Next = out.Queue[len(out.Queue)-1].ID
	}
	ready, err := roomPageBatchReady(ctx, tx, expectedPM, RoomUserAuthorPrefix+roomID, out.Queue)
	if err != nil {
		return out, err
	}
	out.BatchReady = ready
	return out, tx.Commit()
}

// roomPageBatchReady 只回答這一頁可見項是否剛好是同一 session 的連續 Room user。
// 別的 session 造成的全域 ID 空隙不算；頁面以外的回報也不算。不回傳隱藏內容、作者或數量。
func roomPageBatchReady(ctx context.Context, tx *sql.Tx, sessionID, author string, items []RoomQueueItem) (bool, error) {
	if len(items) < 2 {
		return false, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, author, source_session_id FROM queued_messages
 WHERE session_id=? AND id>=? AND id<=? ORDER BY id`, sessionID, items[0].ID, items[len(items)-1].ID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	i := 0
	for rows.Next() {
		var id int64
		var rowAuthor, source string
		if err := rows.Scan(&id, &rowAuthor, &source); err != nil {
			return false, err
		}
		if i >= len(items) || id != items[i].ID || rowAuthor != author || source != "" {
			return false, nil
		}
		i++
	}
	return i == len(items), rows.Err()
}
