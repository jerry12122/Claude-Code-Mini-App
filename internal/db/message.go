package db

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

const (
	MessageStatusPending     = "pending"
	MessageStatusDone        = "done"
	MessageStatusInterrupted = "interrupted"
	// RoleShell 為直連 shell 輸出訊息（非 agent）。
	RoleShell = "shell"
)

type Message struct {
	ID            int64        `json:"id"`
	SessionID     string       `json:"session_id"`
	Role          string       `json:"role"`
	Content       string       `json:"content"`
	ResultText    string       `json:"result_text,omitempty"` // stream-json 最終 result 行文字（若有）；複製時優先
	Status        string       `json:"status"`
	CreatedAt     string       `json:"created_at"`
	Author        string       `json:"author,omitempty"` // 說話者；空字串＝擁有者
	AttachmentIDs []string     `json:"-"`
	Attachments   []Attachment `json:"attachments,omitempty"`
	Telegram      *TelegramRef `json:"telegram,omitempty"`
	Delivery      string       `json:"delivery,omitempty"`
	// ClientMessageID 是尚未取得遠端識別的本機待傳項目的穩定識別；已有遠端識別的列為空。
	ClientMessageID string `json:"client_message_id,omitempty"`
}

// TelegramRef 是 Telegram 私聊訊息的遠端識別（message_id 以數字傳遞；超過 JS 安全整數時由前端以字串游標處理）。
type TelegramRef struct {
	AccountID string `json:"account_id"`
	PeerID    string `json:"peer_id"`
	MessageID int64  `json:"message_id"`
}

// AddMessage 寫入一則已完成的訊息；author 為說話者暱稱，空字串代表擁有者。
func (db *DB) AddMessage(sessionID, role, content, author string) error {
	_, err := db.Exec(
		`INSERT INTO messages (session_id, role, content, status, author) VALUES (?, ?, ?, ?, ?)`,
		sessionID, role, content, MessageStatusDone, author,
	)
	return err
}

// AddMessageWithID 寫入一則已完成的訊息並回傳其產生的 ID 與建立時間（UTC SQLite 格式）。
func (db *DB) AddMessageWithID(sessionID, role, content, author string) (int64, string, error) {
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	res, err := db.Exec(
		`INSERT INTO messages (session_id, role, content, status, author, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		sessionID, role, content, MessageStatusDone, author, now,
	)
	if err != nil {
		return 0, "", err
	}
	id, err := res.LastInsertId()
	return id, now, err
}

// LatestUserMessage 回傳 session 最近一則 user 訊息內容；沒有時回空字串。
func (db *DB) LatestUserMessage(sessionID string) (string, error) {
	var content string
	err := db.QueryRow(
		`SELECT content FROM messages WHERE session_id = ? AND role = 'user' ORDER BY id DESC LIMIT 1`,
		sessionID,
	).Scan(&content)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return content, err
}

// CreatePendingMessage inserts an empty assistant row with status=pending.
func (db *DB) CreatePendingMessage(sessionID string) (int64, error) {
	return db.CreatePendingMessageWithRole(sessionID, "claude")
}

// CreatePendingMessageWithRole 建立一筆 pending 訊息（role 為 claude 或 shell）。
func (db *DB) CreatePendingMessageWithRole(sessionID, role string) (int64, error) {
	res, err := db.Exec(
		`INSERT INTO messages (session_id, role, content, status) VALUES (?, ?, '', ?)`,
		sessionID, role, MessageStatusPending,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// SetShellMessageResult 寫入 shell 輸出並標記完成；若訊息已被他處 finalize（併發中止）則略過。
func (db *DB) SetShellMessageResult(msgID int64, sessionID, content string) error {
	res, err := db.Exec(
		`UPDATE messages SET content = ?, status = ? WHERE id = ? AND session_id = ? AND status = ?`,
		content, MessageStatusDone, msgID, sessionID, MessageStatusPending,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil
	}
	return nil
}

// AppendMessageContent 將 delta 累加到 pending 訊息。
func (db *DB) AppendMessageContent(msgID int64, delta string) error {
	if delta == "" {
		return nil
	}
	_, err := db.Exec(
		`UPDATE messages SET content = content || ? WHERE id = ? AND status = ?`,
		delta, msgID, MessageStatusPending,
	)
	return err
}

// UpdateMessageResultText 寫入 CLI 最終 result 欄位（僅在仍為 pending 時寫入）。
func (db *DB) UpdateMessageResultText(msgID int64, resultText string) error {
	if resultText == "" {
		return nil
	}
	_, err := db.Exec(
		`UPDATE messages SET result_text = ? WHERE id = ? AND status = ?`,
		resultText, msgID, MessageStatusPending,
	)
	return err
}

// FinalizeMessage marks a message as done.
func (db *DB) FinalizeMessage(msgID int64) error {
	_, err := db.Exec(`UPDATE messages SET status = ? WHERE id = ?`, MessageStatusDone, msgID)
	return err
}

// DeleteEmptyMessage 刪除未產生內容的訊息（如 Telegram 直接轉發無回覆之情況）。
func (db *DB) DeleteEmptyMessage(msgID int64) error {
	_, err := db.Exec(
		`DELETE FROM messages WHERE id = ? AND TRIM(COALESCE(content, '')) = ''`,
		msgID,
	)
	return err
}

// FillMessageContentIfEmpty 僅在 content 仍為空且仍 pending 時寫入全文（result 後備，避免空白氣泡）。
func (db *DB) FillMessageContentIfEmpty(msgID int64, content string) error {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	_, err := db.Exec(
		`UPDATE messages SET content = ? WHERE id = ? AND status = ? AND TRIM(COALESCE(content, '')) = ''`,
		content, msgID, MessageStatusPending,
	)
	return err
}

// ResetPendingMessages retains partial output and pauses the affected queues.
func (db *DB) ResetPendingMessages() error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE sessions SET queue_paused = 1,
		last_error_text = CASE WHEN last_error_text = '' THEN '程式重啟，上一個任務已中斷；請確認結果後再重試' ELSE last_error_text END,
		last_error_at = CASE WHEN last_error_text = '' THEN datetime('now') ELSE last_error_at END
		WHERE id IN (SELECT session_id FROM messages WHERE status = ?)`, MessageStatusPending); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE messages SET status = ? WHERE status = ?`, MessageStatusInterrupted, MessageStatusPending); err != nil {
		return err
	}
	return tx.Commit()
}

// FinalizePendingMessagesForSession 將該 session 所有 pending 標為 done（新回合前或中斷時）。
// 若 shell 訊息仍為空內容，寫入「（已中止）」以利 UI 辨識。
func (db *DB) FinalizePendingMessagesForSession(sessionID string) error {
	_, err := db.Exec(
		`UPDATE messages SET status = ?, content = CASE
			WHEN role = ? AND TRIM(COALESCE(content, '')) = '' THEN '（已中止）'
			ELSE content END
		 WHERE session_id = ? AND status = ?`,
		MessageStatusDone, RoleShell, sessionID, MessageStatusPending,
	)
	return err
}

func (db *DB) ClearMessages(sessionID string) error {
	_, err := db.Exec(`DELETE FROM messages WHERE session_id = ?`, sessionID)
	return err
}

func (db *DB) ListMessages(sessionID string) ([]*Message, error) {
	return db.ListMessagesQuery(MessageQuery{SessionID: sessionID, IncludeResult: true})
}

// MessageQuery 為 MCP／內部查詢用的訊息篩選。時間字串須已轉成 UTC SQLite 格式（2006-01-02 15:04:05）。
type MessageQuery struct {
	SessionID     string
	Since         string
	Until         string
	AfterID       int64
	Limit         int
	IncludeResult bool
	// MaxID 非 nil 時只回 id <= *MaxID 的訊息（快照分享用；0 代表一則都不給）。
	MaxID *int64
}

func (db *DB) ListMessagesQuery(q MessageQuery) ([]*Message, error) {
	resultCol := `''`
	if q.IncludeResult {
		resultCol = `COALESCE(result_text, '')`
	}
	inner := `SELECT id, session_id, role, content, status, created_at, ` + resultCol + ` AS result_text, attachment_ids, author FROM messages WHERE session_id = ?`
	args := []any{q.SessionID}
	if q.MaxID != nil {
		inner += ` AND id <= ?`
		args = append(args, *q.MaxID)
	}
	if q.Since != "" {
		inner += ` AND created_at >= ?`
		args = append(args, q.Since)
	}
	if q.Until != "" {
		inner += ` AND created_at <= ?`
		args = append(args, q.Until)
	}
	if q.AfterID > 0 {
		inner += ` AND id > ?`
		args = append(args, q.AfterID)
	}

	// 只給 limit、沒有時間／after_id：取最新 N 則，再依時間正序回。
	newestFirst := q.Limit > 0 && q.Since == "" && q.Until == "" && q.AfterID == 0
	var query string
	if newestFirst {
		query = `SELECT * FROM (` + inner + ` ORDER BY id DESC LIMIT ?) ORDER BY id ASC`
		args = append(args, q.Limit)
	} else {
		query = inner + ` ORDER BY id ASC`
		if q.Limit > 0 {
			query += ` LIMIT ?`
			args = append(args, q.Limit)
		}
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []*Message
	for rows.Next() {
		var m Message
		var ids string
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &m.Status, &m.CreatedAt, &m.ResultText, &ids, &m.Author); err != nil {
			return nil, err
		}
		m.AttachmentIDs, err = decodeAttachmentIDs(ids)
		if err != nil {
			return nil, err
		}
		if m.Status == "" {
			m.Status = MessageStatusDone
		}
		msgs = append(msgs, &m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	all, err := db.AttachmentMap(q.SessionID)
	if err != nil {
		return nil, err
	}
	for _, m := range msgs {
		m.Attachments = hydrateAttachments(m.AttachmentIDs, all)
	}
	return msgs, nil
}

// ActivityRow 是跨 session 活動查詢的一則訊息，帶 session 元資料。
type ActivityRow struct {
	Message
	SessionName string
	WorkDir     string
	AgentType   string
}

// ActivityQuery 時間字串須已轉成 UTC SQLite 格式；ExcludeWorkDir／WorkDir 須已正規化（小寫、/、無尾斜線）。
type ActivityQuery struct {
	Since          string
	Until          string
	Roles          []string
	WorkDir        string
	ExcludeWorkDir []string
	Limit          int
}

const sqlNormWorkDir = `rtrim(lower(replace(s.work_dir, char(92), '/')), '/')`

func (db *DB) ListActivity(q ActivityQuery) ([]*ActivityRow, error) {
	sql := `SELECT m.id, m.session_id, m.role, m.content, m.status, m.created_at,
		s.name, s.work_dir, s.agent_type
		FROM messages m JOIN sessions s ON s.id = m.session_id WHERE 1=1`
	var args []any
	if q.Since != "" {
		sql += ` AND m.created_at >= ?`
		args = append(args, q.Since)
	}
	if q.Until != "" {
		sql += ` AND m.created_at <= ?`
		args = append(args, q.Until)
	}
	if len(q.Roles) > 0 {
		sql += ` AND m.role IN (` + placeholders(len(q.Roles)) + `)`
		for _, r := range q.Roles {
			args = append(args, r)
		}
	}
	if q.WorkDir != "" {
		sql += ` AND ` + sqlNormWorkDir + ` = ?`
		args = append(args, q.WorkDir)
	}
	if len(q.ExcludeWorkDir) > 0 {
		sql += ` AND ` + sqlNormWorkDir + ` NOT IN (` + placeholders(len(q.ExcludeWorkDir)) + `)`
		for _, p := range q.ExcludeWorkDir {
			args = append(args, p)
		}
	}
	sql += ` ORDER BY m.created_at ASC, m.id ASC`
	if q.Limit > 0 {
		sql += ` LIMIT ?`
		args = append(args, q.Limit)
	}

	rows, err := db.Query(sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*ActivityRow
	for rows.Next() {
		var r ActivityRow
		if err := rows.Scan(&r.ID, &r.SessionID, &r.Role, &r.Content, &r.Status, &r.CreatedAt,
			&r.SessionName, &r.WorkDir, &r.AgentType); err != nil {
			return nil, err
		}
		if r.Status == "" {
			r.Status = MessageStatusDone
		}
		out = append(out, &r)
	}
	return out, rows.Err()
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	s := "?"
	for i := 1; i < n; i++ {
		s += ",?"
	}
	return s
}
