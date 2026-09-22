package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/gitinfo"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/quota"
)

type deps struct {
	db    *db.DB
	quota *quota.Service
	reg   *Registry
}

// --- list_sessions ---

type listSessionsOut struct {
	Sessions []*db.Session `json:"sessions"`
}

func (d *deps) listSessions(_ context.Context, _ *gomcp.CallToolRequest, _ struct{}) (*gomcp.CallToolResult, listSessionsOut, error) {
	sessions, err := d.db.ListSessions()
	if err != nil {
		return nil, listSessionsOut{}, err
	}
	for _, s := range sessions {
		if b, ok := gitinfo.Branch(s.WorkDir); ok {
			s.GitBranch = b
		}
	}
	return nil, listSessionsOut{Sessions: sessions}, nil
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

type getMessagesOut struct {
	Messages []*db.Message `json:"messages"`
}

func (d *deps) getMessages(_ context.Context, _ *gomcp.CallToolRequest, in sessionIDIn) (*gomcp.CallToolResult, getMessagesOut, error) {
	msgs, err := d.db.ListMessages(in.SessionID)
	if err != nil {
		return nil, getMessagesOut{}, err
	}
	return nil, getMessagesOut{Messages: msgs}, nil
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
	text := strings.TrimSpace(in.Text)
	if text == "" {
		return nil, startedOut{}, fmt.Errorf("text 不可為空")
	}
	fromID := strings.TrimSpace(in.FromSessionID)
	if fromID != "" {
		from, err := d.db.GetSession(fromID)
		if err != nil {
			return nil, startedOut{}, fmt.Errorf("from_session_id 不存在")
		}
		to, err := d.db.GetSession(in.SessionID)
		if err != nil {
			return nil, startedOut{}, fmt.Errorf("session_id 不存在")
		}
		text = wrapConsultEnvelope(from, to, text)
	}
	if err := d.reg.SendMessage(in.SessionID, text); err != nil {
		return nil, startedOut{}, err
	}
	return nil, startedOut{Status: "started"}, nil
}

// --- get_status ---

type getStatusOut struct {
	State             string          `json:"state"` // idle | running | awaiting_permission | shell_pending
	LatestText        string          `json:"latest_text,omitempty"`
	PendingPermission json.RawMessage `json:"pending_permission,omitempty"`
	PendingShell      *pendingShell   `json:"pending_shell,omitempty"`
	Error             string          `json:"error,omitempty"`
	Connected         bool            `json:"connected"`
}

func (d *deps) getStatus(_ context.Context, _ *gomcp.CallToolRequest, in sessionIDIn) (*gomcp.CallToolResult, getStatusOut, error) {
	state, text, perm, shell, lastErr, connected := d.reg.Status(in.SessionID)
	return nil, getStatusOut{
		State: state, LatestText: text, PendingPermission: perm,
		PendingShell: shell, Error: lastErr, Connected: connected,
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

func registerTools(s *gomcp.Server, d *deps) {
	gomcp.AddTool(s, &gomcp.Tool{Name: "list_sessions", Description: "列出所有 session。互問／討論時用 id 當 send_message 的 session_id；自己的 id 見使用者 prompt 的 [miniapp] self，或上次諮詢信封的「你是 session_id」"}, d.listSessions)
	gomcp.AddTool(s, &gomcp.Tool{Name: "create_session", Description: "建立新 session"}, d.createSession)
	gomcp.AddTool(s, &gomcp.Tool{Name: "delete_session", Description: "刪除 session"}, d.deleteSession)
	gomcp.AddTool(s, &gomcp.Tool{Name: "get_messages", Description: "讀取 session 歷史訊息"}, d.getMessages)
	gomcp.AddTool(s, &gomcp.Tool{Name: "send_message", Description: "非阻塞送出給目標 session。session 互問／討論時必填 from_session_id（你自己的 miniapp session_id），伺服器會蓋上自介與回覆署名；立刻回傳，用 get_status 輪詢結果"}, d.sendMessage)
	gomcp.AddTool(s, &gomcp.Tool{Name: "get_status", Description: "查詢 session 目前狀態與累積回覆內容"}, d.getStatus)
	gomcp.AddTool(s, &gomcp.Tool{Name: "respond_permission", Description: "回覆待授權的工具請求（allow_once/deny_once）"}, d.respondPermission)
	gomcp.AddTool(s, &gomcp.Tool{Name: "set_permission_mode", Description: "切換 session 的權限模式"}, d.setPermissionMode)
	gomcp.AddTool(s, &gomcp.Tool{Name: "set_model", Description: "切換 session 的 model"}, d.setModel)
	gomcp.AddTool(s, &gomcp.Tool{Name: "set_effort", Description: "切換 session 的 effort（推理強度）"}, d.setEffort)
	gomcp.AddTool(s, &gomcp.Tool{Name: "interrupt", Description: "中斷目前執行中的任務"}, d.interrupt)
	gomcp.AddTool(s, &gomcp.Tool{Name: "shell_exec", Description: "非阻塞執行 shell 指令；立刻回傳，用 get_status 輪詢輸出"}, d.shellExec)
	gomcp.AddTool(s, &gomcp.Tool{Name: "get_quota", Description: "查詢 provider 用量配額"}, d.getQuota)
}
