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
	SessionStatusAwaitingInput        = "awaiting_input"
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
	// ActiveModel 為此 session 最後已知的 model 顯示名稱。
	ActiveModel string `json:"active_model,omitempty"`
	// ActiveModelSource 對應 internal/model.Source 字串。
	ActiveModelSource string `json:"active_model_source,omitempty"`
	ActiveModelAt     string `json:"active_model_at,omitempty"`
	// Model/Effort 為使用者要求值（空字串＝不指定，交給 CLI 用預設）。
	Model  string `json:"model"`
	Effort string `json:"effort"`
	// DisguiseAgent 為 Telegram 會話的偽裝外觀（例如 claude、cursor、antigravity；空字串＝無偽裝）。
	DisguiseAgent string `json:"disguise_agent,omitempty"`
	// LastReadAt 為使用者最後一次「正在看」此 session 的時間；配合 LastActive 判斷未讀（見前端 isUnread）。
	LastReadAt string `json:"last_read_at"`
	// UnreadCount 為在 last_read_at 之後接收到的非 user 訊息數量。
	UnreadCount int `json:"unread_count"`
}

func (db *DB) CreateSession(name, description, workDir, permissionMode, agentType string, cliExtraArgs []string, inputMode string, disguiseAgent ...string) (*Session, error) {
	id := uuid.New().String()
	workDir = strings.TrimSpace(workDir)
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
	if err := db.AddWorkDir(workDir); err != nil {
		return nil, err
	}
	extraJSON := "[]"
	if len(cliExtraArgs) > 0 {
		b, err := json.Marshal(cliExtraArgs)
		if err != nil {
			return nil, err
		}
		extraJSON = string(b)
	}
	disguise := ""
	if len(disguiseAgent) > 0 {
		disguise = strings.TrimSpace(disguiseAgent[0])
	}
	_, err := db.Exec(
		`INSERT INTO sessions (id, name, description, work_dir, permission_mode, agent_type, cli_extra_args, input_mode, disguise_agent, last_read_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		id, name, description, workDir, permissionMode, agentType, extraJSON, inputMode, disguise,
	)
	if err != nil {
		return nil, err
	}
	return db.GetSession(id)
}

func (db *DB) GetSession(id string) (*Session, error) {
	row := db.QueryRow(
		`SELECT s.id, s.agent_type, s.agent_session_id, s.name, s.description, s.work_dir, s.permission_mode, s.allowed_tools, s.pending_denials, s.last_active, s.status, s.cli_extra_args, s.input_mode, s.shell_pending, s.active_model, s.active_model_source, s.active_model_at, s.model, s.effort, s.last_read_at, s.disguise_agent,
		       (SELECT COUNT(*) FROM messages m WHERE m.session_id = s.id AND m.created_at > s.last_read_at AND m.role != 'user' AND s.agent_type != 'telegram') AS unread_count
		 FROM sessions s WHERE s.id = ?`, id,
	)
	return scanSession(row)
}

func (db *DB) ListSessions() ([]*Session, error) {
	rows, err := db.Query(
		`SELECT s.id, s.agent_type, s.agent_session_id, s.name, s.description, s.work_dir, s.permission_mode, s.allowed_tools, s.pending_denials, s.last_active, s.status, s.cli_extra_args, s.input_mode, s.shell_pending, s.active_model, s.active_model_source, s.active_model_at, s.model, s.effort, s.last_read_at, s.disguise_agent,
		       (SELECT COUNT(*) FROM messages m WHERE m.session_id = s.id AND m.created_at > s.last_read_at AND m.role != 'user' AND s.agent_type != 'telegram') AS unread_count
		 FROM sessions s ORDER BY s.last_active DESC`,
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
	if err := db.ClearQueuedMessages(id); err != nil {
		return err
	}
	if _, err := db.Exec(`DELETE FROM attachments WHERE session_id = ?`, id); err != nil {
		return err
	}
	// 分享綁定的聊天室已不存在，一併結束（記錄保留供列表顯示「已結束」）。
	if _, err := db.RevokeSharesBySession(id); err != nil {
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

// MarkSessionRead 記錄使用者最後一次「正在看」此 session 的時間。
// 只動 last_read_at，不可連動 last_active——否則會把自己的已讀動作誤植為新活動，
// 讓其他分頁／裝置的未讀判斷（last_active > last_read_at）失真。
func (db *DB) MarkSessionRead(id string) error {
	_, err := db.Exec(
		`UPDATE sessions SET last_read_at = datetime('now') WHERE id = ?`,
		id,
	)
	return err
}

// MarkAllSessionsRead 一次把所有 session 標記已讀（Read All 按鈕）。
// 同樣只動 last_read_at，不動 last_active，理由同 MarkSessionRead。
func (db *DB) MarkAllSessionsRead() error {
	_, err := db.Exec(`UPDATE sessions SET last_read_at = datetime('now')`)
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

// ClaimPendingDenials 原子地「認領」待授權請求：pending_denials 非空才清空並回傳 true。
// 多人（擁有者與訪客）同時回應授權時，只有第一個會拿到 true，其餘視為已被處理。
func (db *DB) ClaimPendingDenials(id string) (bool, error) {
	res, err := db.Exec(`UPDATE sessions SET pending_denials = '' WHERE id = ? AND pending_denials != ''`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
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
// awaiting_confirm without pending_denials (kiroacp mid-turn approval) is reset too:
// its approval channel lived in memory and is gone after a restart.
func (db *DB) ResetRunningSessions() error {
	_, err := db.Exec(
		`UPDATE sessions SET status = ? WHERE status IN (?, ?) OR (status = ? AND pending_denials = '')`,
		SessionStatusIdle, SessionStatusRunning, SessionStatusAwaitingInput, SessionStatusAwaitingConfirm,
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
	if err == nil && db.OnSessionChange != nil {
		db.OnSessionChange()
	}
	return err
}

// UpdateSessionActiveModel 寫入 session 最後已知的 model。
func (db *DB) UpdateSessionActiveModel(id, modelName, source string) error {
	_, err := db.Exec(
		`UPDATE sessions SET active_model = ?, active_model_source = ?, active_model_at = datetime('now'), last_active = datetime('now') WHERE id = ?`,
		modelName, source, id,
	)
	return err
}

// UpdateModel 更新 session 要求的 model（空字串＝不指定）。
func (db *DB) UpdateModel(id, model string) error {
	_, err := db.Exec(
		`UPDATE sessions SET model = ?, last_active = datetime('now') WHERE id = ?`,
		model, id,
	)
	return err
}

// UpdateEffort 更新 session 要求的 effort（空字串＝不指定）。
func (db *DB) UpdateEffort(id, effort string) error {
	_, err := db.Exec(
		`UPDATE sessions SET effort = ?, last_active = datetime('now') WHERE id = ?`,
		effort, id,
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
		&sess.ActiveModel, &sess.ActiveModelSource, &sess.ActiveModelAt, &sess.Model, &sess.Effort, &sess.LastReadAt, &sess.DisguiseAgent,
		&sess.UnreadCount,
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

// TouchSessionLastActive 更新 session 的 last_active 時間為當前時間。
func (db *DB) TouchSessionLastActive(id string) error {
	_, err := db.Exec(`UPDATE sessions SET last_active = datetime('now') WHERE id = ?`, id)
	return err
}
