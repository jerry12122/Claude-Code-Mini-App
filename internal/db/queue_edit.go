package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

var (
	ErrQueueEditConflict  = errors.New("佇列訊息已變更、已取走或不存在，請重新載入")
	ErrQueueEditForbidden = errors.New("此來源的佇列訊息不可編輯")
	ErrQueueEditInvalid   = errors.New("佇列文字無效：須為有效 UTF-8、不含 NUL、不超過 16 MiB，且文字或原附件不可皆空")
)

// EditQueuedMessage 只編輯私人擁有者輸入；空 author 是精確來源，不是萬用比對。
// 呼叫端仍須驗證 owner 權限，並以 session dispatchLock 序列化編輯與取出。
func (db *DB) EditQueuedMessage(ctx context.Context, sessionID string, id int64, previous, content string) error {
	return db.editQueuedMessage(ctx, "", sessionID, id, previous, content)
}

// EditRoomQueuedMessage 在同一交易核對現任 PM、room-user 來源與前值。
// expectedPM 不可由呼叫端悄悄替換；交接後須讓使用者重新載入再編輯。
func (db *DB) EditRoomQueuedMessage(ctx context.Context, roomID, expectedPM string, id int64, previous, content string) error {
	if roomID == "" {
		return ErrRoomPM
	}
	return db.editQueuedMessage(ctx, roomID, expectedPM, id, previous, content)
}

func (db *DB) editQueuedMessage(ctx context.Context, roomID, sessionID string, id int64, previous, content string) error {
	// 與現有 HTTP／WS 的 16 MiB 請求上限對齊，不另外縮短合法文字或改寫空白。
	if len(content) > 16<<20 || !utf8.ValidString(content) || strings.ContainsRune(content, 0) {
		return ErrQueueEditInvalid
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	author := ""
	if roomID != "" {
		pm, err := roomPM(tx.QueryRowContext(ctx, "SELECT session_id FROM ("+roomPMQuery+") WHERE room_id = ?", roomID))
		if err != nil {
			return err
		}
		if pm != sessionID {
			return ErrRoomPMChanged
		}
		author = RoomUserAuthorPrefix + roomID
	}
	var current, actualAuthor, source, rawIDs string
	var batchID string
	err = tx.QueryRowContext(ctx, `SELECT content, author, source_session_id, attachment_ids,batch_id FROM queued_messages WHERE session_id = ? AND id = ?`, sessionID, id).
		Scan(&current, &actualAuthor, &source, &rawIDs, &batchID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrQueueEditConflict
	}
	if err != nil {
		return err
	}
	if actualAuthor != author || source != "" {
		return ErrQueueEditForbidden
	}
	if current != previous {
		return ErrQueueEditConflict
	}
	if batchID != "" {
		return ErrQueueEditConflict
	}
	ids, err := decodeAttachmentIDs(rawIDs)
	if err != nil || ids == nil {
		return ErrInvalidAttachment
	}
	if strings.TrimSpace(content) == "" && len(ids) == 0 {
		return ErrQueueEditInvalid
	}
	seen := make(map[string]bool, len(ids))
	for _, attachmentID := range ids {
		if seen[attachmentID] {
			return ErrInvalidAttachment
		}
		seen[attachmentID] = true
		var accessible bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM attachments a WHERE a.id = ? AND
			(a.session_id = ? OR EXISTS(SELECT 1 FROM attachment_access g WHERE g.session_id = ? AND g.attachment_id = a.id)))`,
			attachmentID, sessionID, sessionID).Scan(&accessible); err != nil {
			return err
		}
		if !accessible {
			return ErrInvalidAttachment
		}
	}
	// receipt 是原始接受契約，絕不隨編輯更新 digest；只變更這筆尚未提升的文字。
	res, err := tx.ExecContext(ctx, `UPDATE queued_messages SET content = ? WHERE session_id = ? AND id = ? AND content = ? AND author = ? AND source_session_id = ''`,
		content, sessionID, id, previous, author)
	if err != nil {
		return fmt.Errorf("編輯佇列文字: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrQueueEditConflict
	}
	return tx.Commit()
}
