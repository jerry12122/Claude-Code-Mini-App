package db

import (
	"database/sql"
	"errors"
)

// RoomCursor 標記時間軸上的一個位置。排序鍵是 (created_at, id)：created_at 只到秒，
// 同秒的訊息靠 id（全域遞增）決勝，所以這個鍵是全序，翻頁不會漏也不會重。
type RoomCursor struct {
	CreatedAt string
	ID        int64
}

type RoomTimelineItem struct {
	MessageID   int64
	SessionID   string
	SessionName string
	MemberRole  string
	AgentType   string
	Role        string
	Author      string
	Content     string
	Status      string
	CreatedAt   string
}

type RoomTimelinePage struct {
	Items   []RoomTimelineItem // 新到舊
	HasMore bool
	Next    *RoomCursor // HasMore 時才有；用來取更早的一頁
}

// RoomTimeline 把所有成員 session 的文字訊息合併成一條新到舊的時間軸。
//
// 只收 user／claude／system 的文字：shell 輸出不算；沒有文字的已完成訊息（只有工具呼叫的回合）略過。
// pending 一律保留（內容可能還是空的），因為前端要靠同一個 id 在串流時就地更新。
// 以 JOIN sessions 取成員，session 被刪後即使殘留成員列，也不會有幽靈訊息或查詢錯誤。
func (db *DB) RoomTimeline(roomID string, before *RoomCursor, limit int) (*RoomTimelinePage, error) {
	var one int
	if err := db.QueryRow(`SELECT 1 FROM rooms WHERE id = ?`, roomID).Scan(&one); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRoomNotFound
		}
		return nil, err
	}
	if limit <= 0 {
		limit = RoomTimelineDefaultLimit
	}
	if limit > RoomTimelineMaxLimit {
		limit = RoomTimelineMaxLimit
	}

	q := `SELECT m.id, m.session_id, s.name, rm.role, s.agent_type, m.role, m.author, m.content, m.status, m.created_at
		FROM room_members rm
		JOIN sessions s ON s.id = rm.session_id
		JOIN messages m ON m.session_id = rm.session_id
		WHERE rm.room_id = ?
		  AND m.role IN ('user', 'claude', 'system')
		  AND (m.status = 'pending' OR TRIM(COALESCE(m.content, '')) != '')`
	args := []any{roomID}
	if before != nil {
		q += ` AND (m.created_at < ? OR (m.created_at = ? AND m.id < ?))`
		args = append(args, before.CreatedAt, before.CreatedAt, before.ID)
	}
	// 多取一筆只為了知道還有沒有更早的訊息。
	q += ` ORDER BY m.created_at DESC, m.id DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	page := &RoomTimelinePage{Items: []RoomTimelineItem{}}
	for rows.Next() {
		var it RoomTimelineItem
		if err := rows.Scan(&it.MessageID, &it.SessionID, &it.SessionName, &it.MemberRole, &it.AgentType,
			&it.Role, &it.Author, &it.Content, &it.Status, &it.CreatedAt); err != nil {
			return nil, err
		}
		if it.Status == "" {
			it.Status = MessageStatusDone
		}
		page.Items = append(page.Items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		page.HasMore = true
		last := page.Items[limit-1]
		page.Next = &RoomCursor{CreatedAt: last.CreatedAt, ID: last.MessageID}
	}
	return page, nil
}
