package db

const (
	MessageStatusPending = "pending"
	MessageStatusDone    = "done"
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

// CreatePendingMessage 建立一筆串流中的 assistant 訊息（content 空、status=pending）
func (db *DB) CreatePendingMessage(sessionID string) (int64, error) {
	res, err := db.Exec(
		`INSERT INTO messages (session_id, role, content, status) VALUES (?, 'claude', '', ?)`,
		sessionID, MessageStatusPending,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// AppendMessageContent 將 delta 累加到 pending 訊息
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

// FinalizeMessage 將訊息標記為完成
func (db *DB) FinalizeMessage(msgID int64) error {
	_, err := db.Exec(`UPDATE messages SET status = ? WHERE id = ?`, MessageStatusDone, msgID)
	return err
}

// FinalizePendingMessagesForSession 將該 session 所有 pending 標為 done（新回合前或中斷時）
func (db *DB) FinalizePendingMessagesForSession(sessionID string) error {
	_, err := db.Exec(
		`UPDATE messages SET status = ? WHERE session_id = ? AND status = ?`,
		MessageStatusDone, sessionID, MessageStatusPending,
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
