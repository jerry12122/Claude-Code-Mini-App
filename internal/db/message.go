package db

const (
	MessageStatusPending = "pending"
	MessageStatusDone    = "done"
	// RoleShell 為直連 shell 輸出訊息（非 agent）。
	RoleShell = "shell"
)

type Message struct {
	ID        int64  `json:"id"`
	SessionID string `json:"session_id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

func (db *DB) AddMessage(sessionID, role, content string) error {
	_, err := db.Exec(
		`INSERT INTO messages (session_id, role, content, status) VALUES (?, ?, ?, ?)`,
		sessionID, role, content, MessageStatusDone,
	)
	return err
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

// FinalizeMessage marks a message as done.
func (db *DB) FinalizeMessage(msgID int64) error {
	_, err := db.Exec(`UPDATE messages SET status = ? WHERE id = ?`, MessageStatusDone, msgID)
	return err
}

// ResetPendingMessages marks all pending rows done (server startup cleanup).
func (db *DB) ResetPendingMessages() error {
	_, err := db.Exec(
		`UPDATE messages SET status = ? WHERE status = ?`,
		MessageStatusDone, MessageStatusPending,
	)
	return err
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
	rows, err := db.Query(
		`SELECT id, session_id, role, content, status, created_at FROM messages WHERE session_id = ? ORDER BY id ASC`,
		sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []*Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &m.Status, &m.CreatedAt); err != nil {
			return nil, err
		}
		if m.Status == "" {
			m.Status = MessageStatusDone
		}
		msgs = append(msgs, &m)
	}
	return msgs, rows.Err()
}
