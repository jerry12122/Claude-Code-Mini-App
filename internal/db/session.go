package db

import (
	"encoding/json"
	"strings"

	"github.com/google/uuid"
)

// Session-level status for sidebar badges and background tasks.
const (
	SessionStatusIdle                 = "idle"
	SessionStatusRunning              = "running"
	SessionStatusAwaitingConfirm      = "awaiting_confirm"
	SessionStatusAwaitingShellConfirm = "awaiting_shell_confirm"
)

type Session struct {
	ID             string `json:"id"`
	AgentType      string `json:"agent_type"`
	AgentSessionID string `json:"agent_session_id"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	WorkDir        string `json:"work_dir"`
	// GitBranch 非 DB 欄位，僅於 API 序列化時由後端填入（目前分支名稱）。
	GitBranch      string   `json:"git_branch,omitempty"`
	PermissionMode string   `json:"permission_mode"`
	AllowedTools   []string `json:"allowed_tools"`
	PendingDenials string   `json:"pending_denials"`
	LastActive     string   `json:"last_active"`
	Status         string   `json:"status"`
	// CliExtraArgs: optional argv list, stored as JSON in cli_extra_args.
	CliExtraArgs []string `json:"cli_extra_args"`
	// InputMode：agent（AI 對話）或 shell（本機指令）。
	InputMode string `json:"input_mode"`
	// ShellPending 為待確認的 shell 指令 JSON（見 docs/shell-allowlist-schema.md）；空字串表示無。
	ShellPending string `json:"shell_pending,omitempty"`
}

func (db *DB) CreateSession(name, description, workDir, permissionMode, agentType string, cliExtraArgs []string, inputMode string) (*Session, error) {
	id := uuid.New().String()
	if permissionMode == "" {
		permissionMode = "default"
	}
	if agentType == "" {
		agentType = "claude"
	}
	if inputMode == "" {
		inputMode = "agent"
	}
	if inputMode != "agent" && inputMode != "shell" {
		inputMode = "agent"
	}
	extraJSON := "[]"
	if len(cliExtraArgs) > 0 {
		b, err := json.Marshal(cliExtraArgs)
		if err != nil {
			return nil, err
		}
		extraJSON = string(b)
	}
	_, err := db.Exec(
		`INSERT INTO sessions (id, name, description, work_dir, permission_mode, agent_type, cli_extra_args, input_mode) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, name, description, workDir, permissionMode, agentType, extraJSON, inputMode,
	)
	if err != nil {
		return nil, err
	}
	return db.GetSession(id)
}

func (db *DB) GetSession(id string) (*Session, error) {
	row := db.QueryRow(
		`SELECT id, agent_type, agent_session_id, name, description, work_dir, permission_mode, allowed_tools, pending_denials, last_active, status, cli_extra_args, input_mode, shell_pending FROM sessions WHERE id = ?`, id,
	)
	return scanSession(row)
}

func (db *DB) ListSessions() ([]*Session, error) {
	rows, err := db.Query(
		`SELECT id, agent_type, agent_session_id, name, description, work_dir, permission_mode, allowed_tools, pending_denials, last_active, status, cli_extra_args, input_mode, shell_pending FROM sessions ORDER BY last_active DESC`,
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

// UpdateSessionCliExtraArgs replaces the whole custom CLI argv JSON array.
func (db *DB) UpdateSessionCliExtraArgs(id string, cliExtraArgs []string) error {
	b, err := json.Marshal(cliExtraArgs)
	if err != nil {
		return err
	}
	_, err = db.Exec(
		`UPDATE sessions SET cli_extra_args = ?, last_active = datetime('now') WHERE id = ?`,
		string(b), id,
	)
	return err
}

// UpdateAgentSessionID stores the native tool session id (e.g. Claude session_id).
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

// ResetRunningSessions sets status=running sessions back to idle on server start.
func (db *DB) ResetRunningSessions() error {
	_, err := db.Exec(
		`UPDATE sessions SET status = ? WHERE status = ?`,
		SessionStatusIdle, SessionStatusRunning,
	)
	return err
}

// UpdateShellPending 更新待確認的 shell 指令 JSON；空字串表示清除。
func (db *DB) UpdateShellPending(id, pendingJSON string) error {
	_, err := db.Exec(
		`UPDATE sessions SET shell_pending = ? WHERE id = ?`,
		pendingJSON, id,
	)
	return err
}

// UpdateSessionStatus 更新背景任務／授權狀態（idle | running | awaiting_confirm | awaiting_shell_confirm）。
func (db *DB) UpdateSessionStatus(id, status string) error {
	_, err := db.Exec(
		`UPDATE sessions SET status = ?, last_active = datetime('now') WHERE id = ?`,
		status, id,
	)
	return err
}

// UpdateSessionInputMode sets input_mode to agent or shell.
func (db *DB) UpdateSessionInputMode(id, mode string) error {
	if mode != "agent" && mode != "shell" {
		mode = "agent"
	}
	_, err := db.Exec(
		`UPDATE sessions SET input_mode = ?, last_active = datetime('now') WHERE id = ?`,
		mode, id,
	)
	return err
}

type scanner interface {
	Scan(...any) error
}

func scanSession(s scanner) (*Session, error) {
	var sess Session
	var allowedTools string
	var extraJSON string
	err := s.Scan(
		&sess.ID, &sess.AgentType, &sess.AgentSessionID, &sess.Name, &sess.Description,
		&sess.WorkDir, &sess.PermissionMode, &allowedTools, &sess.PendingDenials, &sess.LastActive, &sess.Status, &extraJSON, &sess.InputMode, &sess.ShellPending,
	)
	if err != nil {
		return nil, err
	}
	if allowedTools != "" {
		sess.AllowedTools = strings.Split(allowedTools, ",")
	} else {
		sess.AllowedTools = []string{}
	}
	if extraJSON != "" && extraJSON != "[]" {
		_ = json.Unmarshal([]byte(extraJSON), &sess.CliExtraArgs)
	}
	if sess.CliExtraArgs == nil {
		sess.CliExtraArgs = []string{}
	}
	if sess.AgentType == "" {
		sess.AgentType = "claude"
	}
	if sess.Status == "" {
		sess.Status = SessionStatusIdle
	}
	if sess.InputMode == "" {
		sess.InputMode = "agent"
	}
	return &sess, nil
}
