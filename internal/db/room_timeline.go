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
	MessageID       int64
	SessionID       string
	SessionName     string
	MemberRole      string
	AgentType       string
	Role            string
	Author          string
	Sender          string
	Recipient       string
	Mine            bool
	SourceKind      string
	SourceSessionID string
	SourceName      string
	SourceRoom      string
	Body            string
	Transfer        string
	Attachments     []Attachment
	attachmentIDs   []string
	Content         string
	Status          string
	CreatedAt       string
	InputRequestID  string
	Queued          bool
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
		COALESCE(input.author, ''), COALESCE(m.source_session_id, ''), COALESCE(src.name, ''),
		COALESCE((SELECT r.name FROM room_members other JOIN rooms r ON r.id = other.room_id
			WHERE other.session_id = m.source_session_id AND m.source_session_id != ''
			AND NOT EXISTS (SELECT 1 FROM room_members here WHERE here.room_id = rm.room_id AND here.session_id = m.source_session_id)
			ORDER BY r.name LIMIT 1), ''), COALESCE(m.input_request_id, '')
		FROM room_members rm
		JOIN sessions s ON s.id = rm.session_id
		JOIN messages m ON m.session_id = rm.session_id
		LEFT JOIN messages input ON m.role = 'claude' AND input.id = (
			SELECT last.id FROM messages last WHERE last.session_id = m.session_id AND last.id < m.id AND last.role = 'user'
			ORDER BY last.id DESC LIMIT 1)
		LEFT JOIN room_jobs reply ON reply.assistant_id = m.id AND reply.assistant_id > 0 AND reply.worker_id = m.session_id
		LEFT JOIN room_jobs job ON job.id = CASE
			WHEN m.author LIKE 'room-job:%' THEN CAST(substr(m.author, 10) AS INTEGER)
			WHEN m.author LIKE 'room-report:%' THEN CAST(substr(m.content, 5) AS INTEGER)
			WHEN input.author LIKE 'room-job:%' THEN CAST(substr(input.author, 10) AS INTEGER)
		END
		LEFT JOIN sessions pm ON pm.id = job.pm_id
		LEFT JOIN sessions worker ON worker.id = job.worker_id
		LEFT JOIN sessions src ON src.id = m.source_session_id
		WHERE rm.room_id = ?
		  AND m.role IN ('user', 'claude', 'system')
		  AND (m.status = 'pending' OR TRIM(COALESCE(m.content, '')) != '' OR m.attachment_ids != '[]')
		  AND (m.author NOT LIKE 'room-user:%' OR m.author = 'room-user:' || rm.room_id)
		  AND (m.author NOT LIKE 'room-report:%' OR m.author = 'room-report:' || rm.room_id)
		  AND (COALESCE(input.author, '') NOT LIKE 'room-user:%' OR input.author = 'room-user:' || rm.room_id)
		  AND (COALESCE(input.author, '') NOT LIKE 'room-report:%' OR input.author = 'room-report:' || rm.room_id)
		  AND (m.author NOT LIKE 'room-job:%' OR job.room_id = rm.room_id)
		  AND (reply.id IS NULL OR reply.room_id = rm.room_id)
		  AND (reply.id IS NOT NULL OR COALESCE(input.author, '') NOT LIKE 'room-job:%' OR job.room_id = rm.room_id)`
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
			&it.Role, &it.Author, &it.Content, &it.Status, &it.CreatedAt, &attachmentJSON, &jobRoom, &pmName, &workerName, &inputAuthor,
			&it.SourceSessionID, &it.SourceName, &it.SourceRoom, &it.InputRequestID); err != nil {
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
	if before == nil {
		queued, err := db.roomQueuedInputs(roomID)
		if err != nil {
			return nil, err
		}
		confirmed := map[string]struct{}{}
		for _, it := range page.Items {
			if it.InputRequestID != "" {
				confirmed[it.InputRequestID] = struct{}{}
			}
		}
		for _, it := range queued {
			if it.InputRequestID != "" {
				if _, ok := confirmed[it.InputRequestID]; ok {
					continue
				}
			}
			page.Items = append(page.Items, it)
		}
		sortRoomTimeline(page.Items)
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

// roomQueuedInputs 只投影這個工作室已接受、尚未提升的輸入。不讀其他工作室或私人佇列，也不會啟動 session。
func (db *DB) roomQueuedInputs(roomID string) ([]RoomTimelineItem, error) {
	rows, err := db.Query(`
		SELECT q.id, q.session_id, s.name, rm.role, s.agent_type, q.author, q.content,
			q.created_at, q.attachment_ids, COALESCE(q.input_request_id, '')
		FROM room_members rm
		JOIN sessions s ON s.id = rm.session_id
		JOIN queued_messages q ON q.session_id = s.id
		WHERE rm.room_id = ? AND q.author = ?
		ORDER BY q.created_at DESC, q.id DESC`, roomID, RoomUserAuthorPrefix+roomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RoomTimelineItem{}
	for rows.Next() {
		var it RoomTimelineItem
		var queueID int64
		var attachmentJSON string
		if err := rows.Scan(&queueID, &it.SessionID, &it.SessionName, &it.MemberRole, &it.AgentType,
			&it.Author, &it.Content, &it.CreatedAt, &attachmentJSON, &it.InputRequestID); err != nil {
			return nil, err
		}
		it.attachmentIDs, err = decodeAttachmentIDs(attachmentJSON)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(it.Content) == "" && len(it.attachmentIDs) == 0 {
			continue
		}
		it.MessageID = -queueID
		it.Role = "user"
		it.Status = MessageStatusDone
		it.Queued = true
		it.setRoute("", "", false, "")
		out = append(out, it)
	}
	return out, rows.Err()
}

func sortRoomTimeline(items []RoomTimelineItem) {
	for i := 1; i < len(items); i++ {
		item := items[i]
		j := i
		for j > 0 && roomTimelineLater(item, items[j-1]) {
			items[j] = items[j-1]
			j--
		}
		items[j] = item
	}
}

func roomTimelineLater(a, b RoomTimelineItem) bool {
	if a.CreatedAt != b.CreatedAt {
		return a.CreatedAt > b.CreatedAt
	}
	return a.MessageID > b.MessageID
}

func (it *RoomTimelineItem) setRoute(pmName, workerName string, sameRoom bool, inputAuthor string) {
	body, transfer, envelope := consultDisplay(it.Content)
	if envelope {
		it.Body = body
		it.Transfer = transfer
	}
	switch it.Role {
	case "system":
		if strings.HasPrefix(it.Author, "room-handoff:") {
			it.Sender = "交接"
			it.SourceKind = "handoff"
			return
		}
		it.Sender = "系統"
		it.SourceKind = "system"
	case "user":
		it.Recipient = it.SessionName
		switch {
		case it.SourceSessionID != "":
			it.Mine = false
			it.SourceKind = "agent"
			if it.SourceName != "" {
				it.Sender = it.SourceName
			} else {
				it.Sender = "來源未辨識"
				it.SourceRoom = ""
			}
		case strings.HasPrefix(it.Author, "room-job:"):
			it.Sender = "PM"
			if sameRoom && pmName != "" {
				it.Sender = pmName
			}
			it.SourceKind = "job"
		case strings.HasPrefix(it.Author, "room-report:"):
			it.Sender = "執行者"
			if sameRoom && workerName != "" {
				it.Sender = workerName
			}
			it.SourceKind = "report"
		case it.Author == "" && envelope:
			it.Mine = false
			it.Sender = "來源未辨識"
			it.SourceKind = "unknown"
			it.SourceSessionID = ""
			it.SourceRoom = ""
		case it.Author == "" || strings.HasPrefix(it.Author, RoomUserAuthorPrefix):
			it.Mine = true
			it.Sender = "你"
			it.SourceKind = "user"
		default:
			it.Sender = it.Author
			it.SourceKind = "user"
		}
	default:
		it.SourceKind = "assistant"
		it.Sender = it.SessionName
		if it.Author != "" {
			it.Sender = it.Author
		}
		if it.MemberRole == RoomRolePM || strings.HasPrefix(inputAuthor, RoomUserAuthorPrefix) || strings.HasPrefix(inputAuthor, "room-report:") {
			it.Recipient = "工作室"
		}
	}
}

// consultDisplay 只把伺服器信封的原文拆成正文與傳遞資訊。解析結果不能當作身分。
func consultDisplay(content string) (body, transfer string, ok bool) {
	const lead = "來自 Mini-App session"
	if !strings.HasPrefix(content, lead) {
		return "", "", false
	}
	hop := strings.Index(content, "\nhop:")
	if hop < 0 || !strings.Contains(content[:hop], "from_session_id:") {
		return "", "", false
	}
	rest := content[hop:]
	blank := strings.Index(rest, "\n\n")
	if blank < 0 {
		return "", "", false
	}
	tail := rest[blank+2:]
	mark := strings.Index(tail, "\n\n--\n")
	if mark < 0 {
		return "", "", false
	}
	body = strings.TrimSpace(tail[:mark])
	if body == "" {
		return "", "", false
	}
	header := strings.TrimSpace(content[:hop+blank])
	footer := strings.TrimSpace(tail[mark+len("\n\n--\n"):])
	return body, header + "\n\n" + footer, true
}
