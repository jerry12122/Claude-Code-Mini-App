package db

import (
	"database/sql"
	"errors"
	"strings"
)

// RoomCursor 標記時間軸上的一個位置。排序鍵是 (created_at, id)：created_at 只到秒，
// 同秒的訊息靠 id（全域遞增）決勝，所以這個鍵是全序，翻頁不會漏也不會重。
type RoomCursor struct {
	CreatedAt string
	ID        int64
}

type RoomTimelineItem struct {
	MessageID     int64
	SessionID     string
	SessionName   string
	MemberRole    string
	AgentType     string
	Role          string
	Author        string
	Sender        string
	Recipient     string
	Mine          bool
	Attachments   []Attachment
	attachmentIDs []string
	Content       string
	Status        string
	CreatedAt     string
}

type RoomTimelinePage struct {
	Items   []RoomTimelineItem // 新到舊
	HasMore bool
	Next    *RoomCursor // HasMore 時才有；用來取更早的一頁
}

// RoomTimeline 合併成員訊息、傳送方向及附件，依時間由新到舊。
//
// 收 user／claude／system 訊息，含只有附件的訊息。沒有內容的已完成回合略過。
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

	q := `SELECT m.id, m.session_id, s.name, rm.role, s.agent_type, m.role, m.author, m.content, m.status,
		m.created_at, m.attachment_ids, COALESCE(job.room_id, ''), COALESCE(pm.name, ''), COALESCE(worker.name, ''),
		COALESCE(input.author, '')
		FROM room_members rm
		JOIN sessions s ON s.id = rm.session_id
		JOIN messages m ON m.session_id = rm.session_id
		LEFT JOIN messages input ON m.role = 'claude' AND input.id = (
			SELECT last.id FROM messages last WHERE last.session_id = m.session_id AND last.id < m.id AND last.role = 'user'
			ORDER BY last.id DESC LIMIT 1)
		LEFT JOIN room_jobs job ON job.id = CASE
			WHEN m.author LIKE 'room-job:%' THEN CAST(substr(m.author, 10) AS INTEGER)
			WHEN m.author LIKE 'room-report:%' THEN CAST(substr(m.content, 5) AS INTEGER)
		END
		LEFT JOIN sessions pm ON pm.id = job.pm_id
		LEFT JOIN sessions worker ON worker.id = job.worker_id
		WHERE rm.room_id = ?
		  AND m.role IN ('user', 'claude', 'system')
		  AND (m.status = 'pending' OR TRIM(COALESCE(m.content, '')) != '' OR m.attachment_ids != '[]')
		  AND (m.author NOT LIKE 'room-user:%' OR m.author = 'room-user:' || rm.room_id)
		  AND (m.author NOT LIKE 'room-report:%' OR m.author = 'room-report:' || rm.room_id)
		  AND (COALESCE(input.author, '') NOT LIKE 'room-user:%' OR input.author = 'room-user:' || rm.room_id)
		  AND (COALESCE(input.author, '') NOT LIKE 'room-report:%' OR input.author = 'room-report:' || rm.room_id)
		  AND (m.author NOT LIKE 'room-job:%' OR job.room_id = rm.room_id)`
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
		var attachmentJSON, jobRoom, pmName, workerName, inputAuthor string
		if err := rows.Scan(&it.MessageID, &it.SessionID, &it.SessionName, &it.MemberRole, &it.AgentType,
			&it.Role, &it.Author, &it.Content, &it.Status, &it.CreatedAt, &attachmentJSON, &jobRoom, &pmName, &workerName, &inputAuthor); err != nil {
			return nil, err
		}
		it.attachmentIDs, err = decodeAttachmentIDs(attachmentJSON)
		if err != nil {
			return nil, err
		}
		it.setRoute(pmName, workerName, jobRoom == roomID, inputAuthor)
		if it.Status == "" {
			it.Status = MessageStatusDone
		}
		page.Items = append(page.Items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		page.HasMore = true
		last := page.Items[limit-1]
		page.Next = &RoomCursor{CreatedAt: last.CreatedAt, ID: last.MessageID}
	}
	idsBySession := map[string][]string{}
	for _, it := range page.Items {
		if len(it.attachmentIDs) > 0 {
			idsBySession[it.SessionID] = append(idsBySession[it.SessionID], it.attachmentIDs...)
		}
	}
	files := map[string]map[string]Attachment{}
	for id, ids := range idsBySession {
		if files[id], err = db.attachmentMap(id, ids); err != nil {
			return nil, err
		}
	}
	for i := range page.Items {
		it := &page.Items[i]
		it.Attachments = hydrateAttachments(it.attachmentIDs, files[it.SessionID])
	}
	return page, nil
}

func (it *RoomTimelineItem) setRoute(pmName, workerName string, sameRoom bool, inputAuthor string) {
	switch it.Role {
	case "system":
		it.Sender = "系統"
	case "user":
		it.Recipient = it.SessionName
		it.Mine = it.Author == "" || strings.HasPrefix(it.Author, RoomUserAuthorPrefix)
		switch {
		case it.Mine:
			it.Sender = "你"
		case strings.HasPrefix(it.Author, "room-job:"):
			it.Sender = "PM"
			if sameRoom && pmName != "" {
				it.Sender = pmName
			}
		case strings.HasPrefix(it.Author, "room-report:"):
			it.Sender = "執行者"
			if sameRoom && workerName != "" {
				it.Sender = workerName
			}
		default:
			it.Sender = it.Author
		}
	default:
		it.Sender = it.SessionName
		if it.Author != "" {
			it.Sender = it.Author
		}
		if it.MemberRole == RoomRolePM || strings.HasPrefix(inputAuthor, RoomUserAuthorPrefix) || strings.HasPrefix(inputAuthor, "room-report:") {
			it.Recipient = "工作室"
		}
	}
}
