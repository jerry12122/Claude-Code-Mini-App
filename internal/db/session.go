package db

import (
	"strings"

	"github.com/google/uuid"
)

// Session 層級狀態（供列表角標與背景任務）
const (
	SessionStatusIdle            = "idle"
	SessionStatusRunning         = "running"
	SessionStatusAwaitingConfirm = "awaiting_confirm"
)

type Session struct {
	ID             string   `json:"id"`
	AgentType      string   `json:"agent_type"`
	AgentSessionID string   `json:"agent_session_id"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	WorkDir        string   `json:"work_dir"`
	PermissionMode string   `json:"permission_mode"`
	AllowedTools   []string `json:"allowed_tools"`
	PendingDenials string   `json:"pending_denials"`
	LastActive     string   `json:"last_active"`
	Status         string   `json:"status"`
}

func (db *DB) CreateSession(name, description, workDir, permissionMode, agentType string) (*Session, error) {
	id := uuid.New().String()
	if permissionMode == "" {
		permissionMode = "default"
	}
	if agentType == "" {
		agentType = "claude"
	}
	_, err := db.Exec(
		`INSERT INTO sessions (id, name, description, work_dir, permission_mode, agent_type) VALUES (?, ?, ?, ?, ?, ?)`,
		id, name, description, workDir, permissionMode, agentType,
	)
	if err != nil {
		return nil, err
	}
	return db.GetSession(id)
}

func (db *DB) GetSession(id string) (*Session, error) {
	row := db.QueryRow(
		`SELECT id, agent_type, agent_session_id, name, description, work_dir, permission_mode, allowed_tools, pending_denials, last_active, status FROM sessions WHERE id = ?`, id,
	)
	return scanSession(row)
}

func (db *DB) ListSessions() ([]*Session, error) {
	rows, err := db.Query(
		`SELECT id, agent_type, agent_session_id, name, description, work_dir, permission_mode, allowed_tools, pending_denials, last_active, status FROM sessions ORDER BY last_active DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

func (db *DB) DeleteSession(id string) error {
	if _, err := db.Exec(`DELETE FROM messages WHERE session_id = ?`, id); err != nil {
		return err
	}
	_, err := db.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	return err
}

func (db *DB) UpdateSessionName(id, name string) error {
	_, err := db.Exec(
		`UPDATE sessions SET name = ?, last_active = datetime('now') WHERE id = ?`,
		name, id,
	)
	return err
}

// UpdateAgentSessionID 更新 session 中由 AI 工具回傳的原生 session id（例如 Claude 的 session_id）。
func (db *DB) UpdateAgentSessionID(id, agentSessionID string) error {
	_, err := db.Exec(
		`UPDATE sessions SET agent_session_id = ?, last_active = datetime('now') WHERE id = ?`,
		agentSessionID, id,
	)
	return err
}

func (db *DB) UpdatePermissionMode(id, mode string) error {
	_, err := db.Exec(
		`UPDATE sessions SET permission_mode = ?, last_active = datetime('now') WHERE id = ?`,
		mode, id,
	)
	return err
}

func (db *DB) UpdatePendingDenials(id, denials string) error {
	_, err := db.Exec(
		`UPDATE sessions SET pending_denials = ? WHERE id = ?`,
		denials, id,
	)
	return err
}

func (db *DB) UpdateAllowedTools(id string, tools []string) error {
	_, err := db.Exec(
		`UPDATE sessions SET allowed_tools = ?, last_active = datetime('now') WHERE id = ?`,
		strings.Join(tools, ","), id,
	)
	return err
}

func (db *DB) TouchSession(id string) error {
	_, err := db.Exec(`UPDATE sessions SET last_active = datetime('now') WHERE id = ?`, id)
	return err
}

// UpdateSessionStatus 更新背景任務／授權狀態（idle | running | awaiting_confirm）
func (db *DB) UpdateSessionStatus(id, status string) error {
	_, err := db.Exec(
		`UPDATE sessions SET status = ?, last_active = datetime('now') WHERE id = ?`,
		status, id,
	)
	return err
}

type scanner interface {
	Scan(...any) error
}

func scanSession(s scanner) (*Session, error) {
	var sess Session
	var allowedTools string
	err := s.Scan(
		&sess.ID, &sess.AgentType, &sess.AgentSessionID, &sess.Name, &sess.Description,
		&sess.WorkDir, &sess.PermissionMode, &allowedTools, &sess.PendingDenials, &sess.LastActive, &sess.Status,
	)
	if err != nil {
		return nil, err
	}
	if allowedTools != "" {
		sess.AllowedTools = strings.Split(allowedTools, ",")
	} else {
		sess.AllowedTools = []string{}
	}
	if sess.AgentType == "" {
		sess.AgentType = "claude"
	}
	if sess.Status == "" {
		sess.Status = SessionStatusIdle
	}
	return &sess, nil
}
