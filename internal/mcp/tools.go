package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/gitinfo"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/quota"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/tg"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/ws"
)

type deps struct {
	db       *db.DB
	quota    *quota.Service
	reg      *Registry
	maxHops  int
	botToken string
	notifier func(botToken string, chatID int64, text string) error
}

const (
	defaultActivityLimit    = 200
	maxActivityLimit        = 500
	defaultActivityMaxChars = 500
	maxMessagesLimit        = 500
)

// --- list_sessions ---

type listSessionsIn struct {
	Since string `json:"since,omitempty" jsonschema:"只列出 last_active >= 此時間的 session；ISO8601（建議帶時區，如 2026-09-09T00:00:00+08:00）"`
}

type listSessionsOut struct {
	Sessions []*db.Session `json:"sessions"`
}

func (d *deps) listSessions(_ context.Context, _ *gomcp.CallToolRequest, in listSessionsIn) (*gomcp.CallToolResult, listSessionsOut, error) {
	since, err := parseQueryTime(in.Since)
	if err != nil {
		return nil, listSessionsOut{}, err
	}
	sessions, err := d.db.ListSessions()
	if err != nil {
		return nil, listSessionsOut{}, err
	}
	out := make([]*db.Session, 0, len(sessions))
	for _, s := range sessions {
		if since != "" && s.LastActive < since {
			continue
		}
		if b, ok := gitinfo.Branch(s.WorkDir); ok {
			s.GitBranch = b
		}
		out = append(out, s)
	}
	return nil, listSessionsOut{Sessions: out}, nil
}

// --- create_session ---

type createSessionIn struct {
	WorkDir        string `json:"work_dir" jsonschema:"session 執行的工作目錄（絕對路徑）"`
	Name           string `json:"name,omitempty" jsonschema:"顯示名稱，留空則自動產生"`
	AgentType      string `json:"agent_type,omitempty" jsonschema:"claude/cursor/kiroacp/codex，預設 claude"`
	PermissionMode string `json:"permission_mode,omitempty" jsonschema:"default/acceptEdits/bypassPermissions，預設 default"`
}

func (d *deps) createSession(_ context.Context, _ *gomcp.CallToolRequest, in createSessionIn) (*gomcp.CallToolResult, *db.Session, error) {
	agentType := in.AgentType
	if agentType == "" {
		agentType = "claude"
	}
	if !agent.CanCreate(agentType) {
		reason := agent.CreateDisabledReason(agentType)
		if reason == "" {
			reason = "不支援的 agent_type"
		}
		return nil, nil, errors.New(reason)
	}
	s, err := d.db.CreateSession(in.Name, "", in.WorkDir, in.PermissionMode, agentType, nil, "")
	if err != nil {
		return nil, nil, err
	}
	return nil, s, nil
}

// --- delete_session ---

type sessionIDIn struct {
	SessionID string `json:"session_id"`
}

type okOut struct {
	OK bool `json:"ok"`
}

func (d *deps) deleteSession(_ context.Context, _ *gomcp.CallToolRequest, in sessionIDIn) (*gomcp.CallToolResult, okOut, error) {
	if err := d.db.DeleteSession(in.SessionID); err != nil {
		return nil, okOut{}, err
	}
	return nil, okOut{OK: true}, nil
}

// --- get_messages ---

type getMessagesIn struct {
	SessionID         string `json:"session_id"`
	Since             string `json:"since,omitempty" jsonschema:"ISO8601（建議帶時區，如 2026-09-09T00:00:00+08:00）或 UTC SQLite 時間；只回此時間之後"`
	Until             string `json:"until,omitempty" jsonschema:"只回此時間之前（含）"`
	Limit             int    `json:"limit,omitempty" jsonschema:"最多則數，上限 500。未指定則整包（可能很大）；未帶 since 時取最新 N 則"`
	AfterID           int64  `json:"after_id,omitempty" jsonschema:"只回 id 大於此值的訊息（分頁）"`
	MaxChars          int    `json:"max_chars,omitempty" jsonschema:"單則 content 截斷字元數；0＝不截斷"`
	IncludeResultText bool   `json:"include_result_text,omitempty" jsonschema:"是否回傳 result_text（常與 content 重複，預設否）"`
}

type messageOut struct {
	ID         int64  `json:"id"`
	SessionID  string `json:"session_id"`
	Role       string `json:"role"`
	Content    string `json:"content"`
	Status     string `json:"status"`
	CreatedAt  string `json:"created_at"`
	ResultText string `json:"result_text,omitempty"`
	Truncated  bool   `json:"truncated,omitempty"`
}

type getMessagesOut struct {
	Messages []messageOut `json:"messages"`
}

func (d *deps) getMessages(_ context.Context, _ *gomcp.CallToolRequest, in getMessagesIn) (*gomcp.CallToolResult, getMessagesOut, error) {
	if strings.TrimSpace(in.SessionID) == "" {
		return nil, getMessagesOut{}, fmt.Errorf("session_id 不可為空")
	}
	since, err := parseQueryTime(in.Since)
	if err != nil {
		return nil, getMessagesOut{}, err
	}
	until, err := parseQueryTime(in.Until)
	if err != nil {
		return nil, getMessagesOut{}, err
	}
	limit := in.Limit
	if limit > maxMessagesLimit {
		limit = maxMessagesLimit
	}
	msgs, err := d.db.ListMessagesQuery(db.MessageQuery{
		SessionID:     in.SessionID,
		Since:         since,
		Until:         until,
		AfterID:       in.AfterID,
		Limit:         limit,
		IncludeResult: in.IncludeResultText,
	})
	if err != nil {
		return nil, getMessagesOut{}, err
	}
	out := make([]messageOut, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, toMessageOut(m, in.MaxChars, in.IncludeResultText))
	}
	return nil, getMessagesOut{Messages: out}, nil
}

func toMessageOut(m *db.Message, maxChars int, includeResult bool) messageOut {
	content, trunc := truncateRunes(m.Content, maxChars)
	o := messageOut{
		ID: m.ID, SessionID: m.SessionID, Role: m.Role, Content: content,
		Status: m.Status, CreatedAt: m.CreatedAt, Truncated: trunc,
	}
	if includeResult {
		rt, rtTrunc := truncateRunes(m.ResultText, maxChars)
		o.ResultText = rt
		if rtTrunc {
			o.Truncated = true
		}
	}
	return o
}

// --- get_queue / delete_queue_message ---

type getQueueOut struct {
	Queue       []db.QueuedMessage `json:"queue"`
	QueuePaused bool               `json:"queue_paused"`
}

func (d *deps) getQueue(_ context.Context, _ *gomcp.CallToolRequest, in sessionIDIn) (*gomcp.CallToolResult, getQueueOut, error) {
	sessionID := strings.TrimSpace(in.SessionID)
	if sessionID == "" {
		return nil, getQueueOut{}, fmt.Errorf("session_id 不可為空")
	}
	paused, err := d.db.QueuePaused(sessionID)
	if err != nil {
		return nil, getQueueOut{}, err
	}
	queue, err := d.db.ListQueuedMessages(sessionID)
	if err != nil {
		return nil, getQueueOut{}, err
	}
	return nil, getQueueOut{Queue: queue, QueuePaused: paused}, nil
}

type deleteQueueMessageIn struct {
	SessionID string `json:"session_id"`
	MessageID int64  `json:"message_id" jsonschema:"get_queue 回傳的 queue 項目 id，必須大於 0"`
}

func (d *deps) deleteQueueMessage(_ context.Context, _ *gomcp.CallToolRequest, in deleteQueueMessageIn) (*gomcp.CallToolResult, okOut, error) {
	sessionID := strings.TrimSpace(in.SessionID)
	if sessionID == "" {
		return nil, okOut{}, fmt.Errorf("session_id 不可為空")
	}
	if in.MessageID <= 0 {
		return nil, okOut{}, fmt.Errorf("message_id 必須大於 0")
	}
	if _, err := d.db.GetSession(sessionID); err != nil {
		return nil, okOut{}, err
	}
	if err := ws.RemoveQueuedMessage(d.db, sessionID, in.MessageID); err != nil {
		return nil, okOut{}, err
	}
	return nil, okOut{OK: true}, nil
}

// --- list_activity ---

type listActivityIn struct {
	Since          string   `json:"since" jsonschema:"必填。ISO8601，建議帶時區，例如 2026-09-09T00:00:00+08:00"`
	Until          string   `json:"until,omitempty" jsonschema:"結束時間（含）；省略＝不限制"`
	Roles          []string `json:"roles,omitempty" jsonschema:"要包含的 role，預設只回 user。要全部則傳 [\"user\",\"claude\",\"shell\"]"`
	WorkDir        string   `json:"work_dir,omitempty" jsonschema:"只看此工作目錄（斜線與大小寫會正規化）"`
	ExcludeWorkDir []string `json:"exclude_work_dir,omitempty" jsonschema:"排除的工作目錄，例如 Eve 自己的路徑"`
	MaxChars       int      `json:"max_chars,omitempty" jsonschema:"單則截斷字元數，預設 500"`
	Limit          int      `json:"limit,omitempty" jsonschema:"最多訊息則數，預設 200、上限 500"`
}

type activityItem struct {
	ID        int64  `json:"id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
	Truncated bool   `json:"truncated,omitempty"`
}

type activityGroup struct {
	SessionID string         `json:"session_id"`
	Name      string         `json:"name"`
	WorkDir   string         `json:"work_dir"`
	AgentType string         `json:"agent_type"`
	Count     int            `json:"count"`
	Messages  []activityItem `json:"messages"`
}

type listActivityOut struct {
	Sessions []activityGroup `json:"sessions"`
}

func (d *deps) listActivity(_ context.Context, _ *gomcp.CallToolRequest, in listActivityIn) (*gomcp.CallToolResult, listActivityOut, error) {
	if strings.TrimSpace(in.Since) == "" {
		return nil, listActivityOut{}, fmt.Errorf("since 必填，例如 2026-09-09T00:00:00+08:00")
	}
	since, err := parseQueryTime(in.Since)
	if err != nil {
		return nil, listActivityOut{}, err
	}
	until, err := parseQueryTime(in.Until)
	if err != nil {
		return nil, listActivityOut{}, err
	}
	roles := in.Roles
	if len(roles) == 0 {
		roles = []string{"user"}
	}
	exclude := make([]string, 0, len(in.ExcludeWorkDir))
	for _, p := range in.ExcludeWorkDir {
		if n := normPath(p); n != "" {
			exclude = append(exclude, n)
		}
	}
	maxChars := in.MaxChars
	if maxChars <= 0 {
		maxChars = defaultActivityMaxChars
	}
	rows, err := d.db.ListActivity(db.ActivityQuery{
		Since:          since,
		Until:          until,
		Roles:          roles,
		WorkDir:        normPath(in.WorkDir),
		ExcludeWorkDir: exclude,
		Limit:          clampLimit(in.Limit, defaultActivityLimit, maxActivityLimit),
	})
	if err != nil {
		return nil, listActivityOut{}, err
	}
	return nil, listActivityOut{Sessions: groupActivity(rows, maxChars)}, nil
}

func groupActivity(rows []*db.ActivityRow, maxChars int) []activityGroup {
	out := make([]activityGroup, 0)
	idx := map[string]int{}
	for _, r := range rows {
		content, trunc := truncateRunes(r.Content, maxChars)
		item := activityItem{ID: r.ID, Role: r.Role, Content: content, CreatedAt: r.CreatedAt, Truncated: trunc}
		i, ok := idx[r.SessionID]
		if !ok {
			idx[r.SessionID] = len(out)
			out = append(out, activityGroup{
				SessionID: r.SessionID, Name: r.SessionName, WorkDir: r.WorkDir, AgentType: r.AgentType,
				Count: 1, Messages: []activityItem{item},
			})
			continue
		}
		out[i].Messages = append(out[i].Messages, item)
		out[i].Count++
	}
	return out
}

// --- send_message（非阻塞：立刻回，結果靠 get_status 輪詢） ---

type sendMessageIn struct {
	SessionID     string `json:"session_id"`
	Text          string `json:"text"`
	FromSessionID string `json:"from_session_id,omitempty" jsonschema:"送出者的 miniapp session_id；session 互問／討論時必填，伺服器會蓋上自介與回覆署名"`
}

type startedOut struct {
	Status string `json:"status"`
}

func (d *deps) sendMessage(_ context.Context, _ *gomcp.CallToolRequest, in sendMessageIn) (*gomcp.CallToolResult, startedOut, error) {
	text, err := d.prepareConsult(in.SessionID, in.FromSessionID, in.Text, consultAsync)
	if err != nil {
		return nil, startedOut{}, err
	}
	if err := d.reg.SendMessage(in.SessionID, text); err != nil {
		return nil, startedOut{}, err
	}
	return nil, startedOut{Status: "started"}, nil
}

// prepareConsult 驗證輸入；有 from_session_id 時蓋上 envelope 並檢查 hop 上限。
func (d *deps) prepareConsult(toID, fromID, rawText string, mode consultMode) (string, error) {
	text := strings.TrimSpace(rawText)
	if text == "" {
		return "", fmt.Errorf("text 不可為空")
	}
	fromID = strings.TrimSpace(fromID)
	if fromID == "" {
		return text, nil
	}
	if fromID == strings.TrimSpace(toID) {
		return "", fmt.Errorf("from_session_id 不可等於 session_id（不能問自己）")
	}
	from, err := d.db.GetSession(fromID)
	if err != nil {
		return "", fmt.Errorf("from_session_id 不存在")
	}
	to, err := d.db.GetSession(toID)
	if err != nil {
		return "", fmt.Errorf("session_id 不存在")
	}
	latest, err := d.db.LatestUserMessage(fromID)
	if err != nil {
		return "", err
	}
	hop := nextHop(latest)
	if err := checkHop(hop, d.maxHops); err != nil {
		return "", err
	}
	return wrapConsultEnvelope(from, to, text, hop, mode), nil
}

// --- ask_session（阻塞：送出後等對方這一輪結束才回） ---

const (
	defaultAskTimeoutSec = 600
	maxAskTimeoutSec     = 1800
)

type askSessionIn struct {
	SessionID     string `json:"session_id"`
	Text          string `json:"text"`
	FromSessionID string `json:"from_session_id,omitempty" jsonschema:"你自己的 miniapp session_id；session 互問時必填，伺服器會蓋上自介並計算 hop"`
	TimeoutSec    int    `json:"timeout_sec,omitempty" jsonschema:"最多等幾秒，預設 600、上限 1800；逾時回傳目前的部分回覆，可再用 get_status 追"`
}

type askSessionOut struct {
	PendingQuestion   any    `json:"pending_question,omitempty"`
	State             string `json:"state" jsonschema:"本輪結果狀態：idle 已回答完；awaiting_input 等待使用者回答；awaiting_permission 或 shell_pending 對方卡在授權；running 逾時仍在跑"`
	Text              string `json:"text,omitempty"`
	PendingPermission any    `json:"pending_permission,omitempty"`
	Error             string `json:"error,omitempty"`
	TimedOut          bool   `json:"timed_out,omitempty"`
}

func (d *deps) askSession(ctx context.Context, _ *gomcp.CallToolRequest, in askSessionIn) (*gomcp.CallToolResult, askSessionOut, error) {
	target, err := d.db.GetSession(in.SessionID)
	if err != nil {
		return nil, askSessionOut{}, fmt.Errorf("session_id 不存在")
	}
	// ponytail: 忙碌就拒絕而不排隊——排隊後等到的「結束」可能是前一輪，要正確就得依 msgID 追蹤。
	if target.Status != db.SessionStatusIdle {
		return nil, askSessionOut{}, fmt.Errorf("目標 session 忙碌中（%s），請稍後再問，或改用 send_message（會排入佇列）", target.Status)
	}
	text, err := d.prepareConsult(in.SessionID, in.FromSessionID, in.Text, consultAsk)
	if err != nil {
		return nil, askSessionOut{}, err
	}
	sec := clampLimit(in.TimeoutSec, defaultAskTimeoutSec, maxAskTimeoutSec)
	res, err := d.reg.Ask(ctx, in.SessionID, text, time.Duration(sec)*time.Second)
	if err != nil {
		return nil, askSessionOut{}, err
	}
	return nil, askSessionOut{
		State: res.State, Text: res.Text, PendingPermission: rawToAny(res.PendingPermission), PendingQuestion: rawToAny(res.PendingQuestion),
		Error: res.Error, TimedOut: res.TimedOut,
	}, nil
}

// --- get_status ---

type getStatusOut struct {
	PendingQuestion   any           `json:"pending_question,omitempty"`
	State             string        `json:"state"` // idle | running | awaiting_permission | shell_pending
	LatestText        string        `json:"latest_text,omitempty"`
	PendingPermission any           `json:"pending_permission,omitempty"`
	PendingShell      *pendingShell `json:"pending_shell,omitempty"`
	Error             string        `json:"error,omitempty"`
	Connected         bool          `json:"connected"`
}

// rawToAny：json.RawMessage 會被 schema 推導成「整數陣列」，實際內容是物件陣列，
// 輸出驗證會失敗；解成 any 讓 schema 不設限。
func rawToAny(raw json.RawMessage) any {
	var v any
	if len(raw) == 0 || json.Unmarshal(raw, &v) != nil {
		return nil
	}
	return v
}

func (d *deps) getStatus(_ context.Context, _ *gomcp.CallToolRequest, in sessionIDIn) (*gomcp.CallToolResult, getStatusOut, error) {
	state, text, perm, shell, lastErr, connected, question := d.reg.Status(in.SessionID)
	return nil, getStatusOut{
		State: state, LatestText: text, PendingPermission: rawToAny(perm),
		PendingQuestion: rawToAny(question), PendingShell: shell, Error: lastErr, Connected: connected,
	}, nil
}

// --- respond_permission（非阻塞：觸發重跑，結果一樣靠 get_status） ---

type respondPermissionIn struct {
	SessionID string   `json:"session_id"`
	Decision  string   `json:"decision" jsonschema:"allow_once 或 deny_once"`
	Tools     []string `json:"tools,omitempty" jsonschema:"decision 為 allow_once 時要放行的 tool 名稱清單"`
}

func (d *deps) respondPermission(_ context.Context, _ *gomcp.CallToolRequest, in respondPermissionIn) (*gomcp.CallToolResult, startedOut, error) {
	if err := d.reg.RespondPermission(in.SessionID, in.Decision, in.Tools); err != nil {
		return nil, startedOut{}, err
	}
	return nil, startedOut{Status: "started"}, nil
}

// --- set_permission_mode ---

type setPermissionModeIn struct {
	SessionID string `json:"session_id"`
	Mode      string `json:"mode" jsonschema:"default/acceptEdits/bypassPermissions"`
}

func (d *deps) setPermissionMode(_ context.Context, _ *gomcp.CallToolRequest, in setPermissionModeIn) (*gomcp.CallToolResult, okOut, error) {
	if err := d.reg.SetPermissionMode(in.SessionID, in.Mode); err != nil {
		return nil, okOut{}, err
	}
	return nil, okOut{OK: true}, nil
}

// --- set_model / set_effort ---

type setModelIn struct {
	SessionID string `json:"session_id"`
	Model     string `json:"model" jsonschema:"要用的 model id/別名；空字串＝不指定，交由 CLI 用預設"`
}

func (d *deps) setModel(_ context.Context, _ *gomcp.CallToolRequest, in setModelIn) (*gomcp.CallToolResult, okOut, error) {
	if err := d.reg.SetModel(in.SessionID, in.Model); err != nil {
		return nil, okOut{}, err
	}
	return nil, okOut{OK: true}, nil
}

type setEffortIn struct {
	SessionID string `json:"session_id"`
	Effort    string `json:"effort" jsonschema:"low/medium/high/xhigh/max；空字串＝不指定，交由 CLI 用預設；cursor 不支援會被忽略"`
}

func (d *deps) setEffort(_ context.Context, _ *gomcp.CallToolRequest, in setEffortIn) (*gomcp.CallToolResult, okOut, error) {
	if err := d.reg.SetEffort(in.SessionID, in.Effort); err != nil {
		return nil, okOut{}, err
	}
	return nil, okOut{OK: true}, nil
}

// --- interrupt ---

func (d *deps) interrupt(_ context.Context, _ *gomcp.CallToolRequest, in sessionIDIn) (*gomcp.CallToolResult, okOut, error) {
	if err := d.reg.Interrupt(in.SessionID); err != nil {
		return nil, okOut{}, err
	}
	return nil, okOut{OK: true}, nil
}

// --- shell_exec（非阻塞：立刻回，輸出靠 get_status 輪詢） ---

type shellExecIn struct {
	SessionID string `json:"session_id"`
	Command   string `json:"command"`
}

func (d *deps) shellExec(_ context.Context, _ *gomcp.CallToolRequest, in shellExecIn) (*gomcp.CallToolResult, startedOut, error) {
	if strings.TrimSpace(in.Command) == "" {
		return nil, startedOut{}, fmt.Errorf("command 不可為空")
	}
	if err := d.reg.ShellExec(in.SessionID, in.Command); err != nil {
		return nil, startedOut{}, err
	}
	return nil, startedOut{Status: "started"}, nil
}

// --- get_quota ---

type getQuotaIn struct {
	Provider string `json:"provider,omitempty" jsonschema:"留空回傳所有 provider"`
}

type getQuotaOut struct {
	Quota map[string]quota.Payload `json:"quota"`
}

func (d *deps) getQuota(_ context.Context, _ *gomcp.CallToolRequest, in getQuotaIn) (*gomcp.CallToolResult, getQuotaOut, error) {
	if in.Provider != "" {
		p := d.quota.Get(in.Provider).ToPayload()
		return nil, getQuotaOut{Quota: map[string]quota.Payload{in.Provider: p}}, nil
	}
	all := d.quota.GetAll()
	out := make(map[string]quota.Payload, len(all))
	for k, v := range all {
		out[k] = v.ToPayload()
	}
	return nil, getQuotaOut{Quota: out}, nil
}

// --- post_message ---

type postMessageIn struct {
	SessionID string `json:"session_id" jsonschema:"目標 miniapp session_id"`
	Text      string `json:"text" jsonschema:"要寫入的 Markdown 訊息內容"`
	Role      string `json:"role,omitempty" jsonschema:"訊息角色：assistant 或 system，預設 assistant"`
	Author    string `json:"author,omitempty" jsonschema:"訊息署名（選填，如 'PR Worker'、'Reviewer'，會顯示於氣泡上方）"`
	Notify    bool   `json:"notify,omitempty" jsonschema:"是否同時透過 Telegram Bot 發送推播通知給使用者"`
}

type postMessageOut struct {
	OK        bool   `json:"ok"`
	MessageID int64  `json:"message_id"`
	CreatedAt string `json:"created_at"`
}

func (d *deps) postMessage(_ context.Context, _ *gomcp.CallToolRequest, in postMessageIn) (*gomcp.CallToolResult, postMessageOut, error) {
	sessionID := strings.TrimSpace(in.SessionID)
	if sessionID == "" {
		return nil, postMessageOut{}, fmt.Errorf("session_id 不可為空")
	}
	text := strings.TrimSpace(in.Text)
	if text == "" {
		return nil, postMessageOut{}, fmt.Errorf("text 不可為空")
	}
	_, err := d.db.GetSession(sessionID)
	if err != nil {
		return nil, postMessageOut{}, fmt.Errorf("session_id 不存在: %w", err)
	}

	role := strings.ToLower(strings.TrimSpace(in.Role))
	if role == "" || role == "assistant" {
		role = "claude"
	}

	author := strings.TrimSpace(in.Author)
	msgID, createdAt, err := d.db.AddMessageWithID(sessionID, role, text, author)
	if err != nil {
		return nil, postMessageOut{}, fmt.Errorf("寫入訊息失敗: %w", err)
	}

	_ = d.db.TouchSessionLastActive(sessionID)

	// 廣播至該 session 的 WebSocket 連線，讓前端即時長出訊息
	ws.BroadcastSessionMessage(sessionID, "message", text, role, author, msgID, createdAt)
	// 通知側欄 session 列表更新
	ws.NotifySessionsChanged()

	if in.Notify && d.botToken != "" {
		notifyFn := d.notifier
		if notifyFn == nil {
			notifyFn = tg.Notify
		}
		users, err := d.db.ListUsers()
		if err == nil {
			for _, uid := range users {
				_ = notifyFn(d.botToken, uid, text)
			}
		}
	}

	return nil, postMessageOut{
		OK:        true,
		MessageID: msgID,
		CreatedAt: createdAt,
	}, nil
}

func registerTools(s *gomcp.Server, d *deps) {
	gomcp.AddTool(s, &gomcp.Tool{Name: "list_sessions", Description: "列出所有 session。互問／討論時用 id 當 send_message 的 session_id；自己的 id 見使用者 prompt 的 [miniapp] self，或上次諮詢信封的「你是 session_id」"}, d.listSessions)
	gomcp.AddTool(s, &gomcp.Tool{Name: "create_session", Description: "建立新 session"}, d.createSession)
	gomcp.AddTool(s, &gomcp.Tool{Name: "delete_session", Description: "刪除 session"}, d.deleteSession)
	gomcp.AddTool(s, &gomcp.Tool{Name: "get_messages", Description: "讀取 session 歷史訊息；建議帶 since/until/limit，避免整包歷史"}, d.getMessages)
	gomcp.AddTool(s, &gomcp.Tool{Name: "get_queue", Description: "讀取指定 session 尚未開始執行的訊息佇列（FIFO，含 id、內容、署名、時間與附件）及 queue_paused；空佇列回傳 []"}, d.getQueue)
	gomcp.AddTool(s, &gomcp.Tool{Name: "delete_queue_message", Description: "刪除指定 session 的單則待執行訊息，message_id 取自 get_queue 的 id，並即時更新畫面。訊息已不在該佇列時仍回傳 ok；不會中斷已開始的任務或解除佇列暫停"}, d.deleteQueueMessage)
	gomcp.AddTool(s, &gomcp.Tool{Name: "list_activity", Description: "依時間列出跨 session 活動（日報／秘書用）。since 建議帶時區。預設只回 user 訊息。操控 session 仍用 create_session / send_message"}, d.listActivity)
	gomcp.AddTool(s, &gomcp.Tool{Name: "send_message", Description: "非阻塞送出給目標 session（對方忙碌時排入佇列）。session 互問／討論時必填 from_session_id（你自己的 miniapp session_id），伺服器會蓋上自介與回覆署名；立刻回傳，用 get_status 輪詢結果。要直接拿回答請用 ask_session"}, d.sendMessage)
	gomcp.AddTool(s, &gomcp.Tool{Name: "post_message", Description: "主動將回覆、通知或執行結果寫入指定 session，立刻呈現在聊天畫面中，不觸發目標 session 的 Agent 執行（不消耗額外 token、不啟動 CLI）。目標 session 正在執行其他任務時亦可寫入"}, d.postMessage)
	gomcp.AddTool(s, &gomcp.Tool{Name: "ask_session", Description: "阻塞式詢問：送出後等目標 session 這一輪回答完才回傳（含回答全文）。目標須閒置；互問時帶 from_session_id。可設 timeout_sec，逾時回部分回覆。等待期間若同一 session 被其他操作（send_message、set_model 等）介入，會回傳 error 說明結果可能不完整"}, d.askSession)
	gomcp.AddTool(s, &gomcp.Tool{Name: "get_status", Description: "查詢 session 目前狀態與累積回覆內容"}, d.getStatus)
	gomcp.AddTool(s, &gomcp.Tool{Name: "respond_permission", Description: "回覆待授權的工具請求（allow_once/deny_once）"}, d.respondPermission)
	gomcp.AddTool(s, &gomcp.Tool{Name: "set_permission_mode", Description: "切換 session 的權限模式"}, d.setPermissionMode)
	gomcp.AddTool(s, &gomcp.Tool{Name: "set_model", Description: "切換 session 的 model"}, d.setModel)
	gomcp.AddTool(s, &gomcp.Tool{Name: "set_effort", Description: "切換 session 的 effort（推理強度）"}, d.setEffort)
	gomcp.AddTool(s, &gomcp.Tool{Name: "interrupt", Description: "中斷目前執行中的任務"}, d.interrupt)
	gomcp.AddTool(s, &gomcp.Tool{Name: "shell_exec", Description: "非阻塞執行 shell 指令；立刻回傳，用 get_status 輪詢輸出"}, d.shellExec)
	gomcp.AddTool(s, &gomcp.Tool{Name: "get_quota", Description: "查詢 provider 用量配額"}, d.getQuota)
}
