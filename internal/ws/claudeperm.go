package ws

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
)

// Claude 權限詢問（--permission-prompt-tool）。
//
// 各模式共用 MCP：工具授權走 permission_request，AskUserQuestion 走獨立的 question_request。

var claudePermCfg struct {
	mu         sync.Mutex
	url, token string
}

// SetClaudePermMCP 由 server 在啟用 /mcp/perm 時設定（url 為 loopback 位址，token 為每次啟動產生的內部密鑰）。
// 未設定時 Claude 不帶 --permission-prompt-tool，行為與過去完全相同。
func SetClaudePermMCP(url, token string) {
	claudePermCfg.mu.Lock()
	claudePermCfg.url, claudePermCfg.token = url, token
	claudePermCfg.mu.Unlock()
}

func claudePermMCP() (url, token string, ok bool) {
	claudePermCfg.mu.Lock()
	defer claudePermCfg.mu.Unlock()
	return claudePermCfg.url, claudePermCfg.token, claudePermCfg.url != "" && claudePermCfg.token != ""
}

func (o handlerOptions) writeClaudeMCPConfig(sessionID string, args map[string]string) (string, func(), error) {
	var roomURL, roomToken string
	if o.roomBridge != nil {
		roomURL, roomToken = o.roomBridge(sessionID)
	}
	path, cleanup, err := writePermMCPConfig(sessionID, roomURL, roomToken)
	if err == nil && roomURL != "" {
		const roomTools = "mcp__miniapp_room__get_board,mcp__miniapp_room__get_history,mcp__miniapp_room__get_quota," +
			"mcp__miniapp_room__save_memo,mcp__miniapp_room__create_worker,mcp__miniapp_room__dispatch,mcp__miniapp_room__rename_session"
		args[agent.ArgAllowedTools] = strings.Trim(args[agent.ArgAllowedTools]+","+roomTools, ",")
	}
	return path, cleanup, err
}

// writePermMCPConfig 把 --mcp-config 內容寫成暫存檔（0600）再傳路徑：token 不進 argv，也就不進 log 與行程列表。
func writePermMCPConfig(sessionID string, roomServer ...string) (path string, cleanup func(), err error) {
	url, token, ok := claudePermMCP()
	if !ok {
		return "", nil, fmt.Errorf("perm mcp 未啟用")
	}
	servers := map[string]any{
		agent.PermPromptServer: map[string]any{
			"type": "http",
			"url":  url,
			"headers": map[string]string{
				"Authorization":     "Bearer " + token,
				"X-Miniapp-Session": sessionID,
			},
		},
	}
	if len(roomServer) == 2 && roomServer[0] != "" {
		servers["miniapp_room"] = map[string]any{
			"type": "http", "url": roomServer[0],
			"headers": map[string]string{"Authorization": "Bearer " + roomServer[1], "X-Miniapp-Session": sessionID},
		}
	}
	b, err := json.Marshal(map[string]any{"mcpServers": servers})
	if err != nil {
		return "", nil, err
	}
	f, err := os.CreateTemp("", "miniapp-perm-*.json") // CreateTemp 預設 0600
	if err != nil {
		return "", nil, err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", nil, err
	}
	f.Close()
	name := f.Name()
	return name, func() { os.Remove(name) }, nil
}

// claudeAsker 是單一 Claude run 的授權通道。
type claudeAsker struct {
	runCtx      context.Context
	askQuestion func(context.Context, json.RawMessage) (json.RawMessage, error)
	askUser     func(ctx context.Context, tools any) bool
	autoAllow   func() bool
	audit       func(toolName, summary string)
	sem         chan struct{} // 容量 1：同 session 一次只問一題（pendingPerms 每個 session 只有一格）

	mu       sync.Mutex
	answered map[string]bool // 已由使用者回答過的 tool_use_id
}

var claudeAskers sync.Map // sessionID → *claudeAsker

// registerClaudeAsker 登記本 run 的授權通道；runCtx 取消（使用者中斷）時正在等待的詢問一併解除。
// autoAllow 在每次詢問時呼叫，為真則不取得信號量、不呼叫 askUser，直接回允許。
// audit 在自動允許時呼叫；可為 nil（仍允許）。askUser／askQuestion 的 nil 與過去相同。
func registerClaudeAsker(sessionID string, runCtx context.Context, askUser func(context.Context, any) bool, askQuestion func(context.Context, json.RawMessage) (json.RawMessage, error), autoAllow func() bool, audit func(toolName, summary string)) (a *claudeAsker, unregister func()) {
	a = &claudeAsker{
		runCtx:      runCtx,
		askQuestion: askQuestion,
		autoAllow:   autoAllow,
		audit:       audit,
		askUser: func(ctx context.Context, tools any) bool {
			c, cancel := context.WithCancel(ctx)
			defer cancel()
			stop := context.AfterFunc(runCtx, cancel)
			defer stop()
			return askUser(c, tools)
		},
		sem:      make(chan struct{}, 1),
		answered: map[string]bool{},
	}
	claudeAskers.Store(sessionID, a)
	return a, func() { claudeAskers.CompareAndDelete(sessionID, a) }
}

// AskClaudePermission 供 mcp.NewPermHTTPHandler 呼叫。
func AskClaudePermission(ctx context.Context, sessionID, toolName string, input json.RawMessage, toolUseID string) (bool, string) {
	v, ok := claudeAskers.Load(sessionID)
	if !ok {
		return false, "miniapp：此 session 目前沒有進行中的授權通道"
	}
	return v.(*claudeAsker).ask(ctx, toolName, input, toolUseID)
}

func (a *claudeAsker) ask(ctx context.Context, toolName string, input json.RawMessage, toolUseID string) (bool, string) {
	// 早於信號量與 askUser：不登記待決權限、不改 session 狀態、不廣播、不發通知。
	if a.autoAllow != nil && a.autoAllow() {
		if a.audit != nil {
			a.audit(toolName, claudeAskSummary(toolName, input))
		}
		return true, ""
	}
	select {
	case a.sem <- struct{}{}:
		defer func() { <-a.sem }()
	case <-ctx.Done():
		return false, "已取消"
	}
	// 與 Claude 既有 denial 同形（tool_name／tool_use_id／tool_input），前端授權面板不用分辨來源。
	tools := []agent.PermissionDenial{{ToolName: toolName, ToolUseID: toolUseID, ToolInput: input}}
	allow := a.askUser(ctx, tools)
	if ctx.Err() != nil {
		return false, "已取消"
	}
	a.mu.Lock()
	a.answered[toolUseID] = true
	a.mu.Unlock()
	if !allow {
		return false, "使用者拒絕了此操作"
	}
	return true, ""
}

// dropAnswered 剔除已由使用者回答過的 denial：prompt-tool 回 deny 時，result.permission_denials 仍會帶出該筆，
// 不剔除會讓使用者剛按完拒絕又跳一次「允許」，而這次重跑必被 ask 規則擋下。
func (a *claudeAsker) dropAnswered(ds []agent.PermissionDenial) []agent.PermissionDenial {
	if a == nil || len(ds) == 0 {
		return ds
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	out := ds[:0:0]
	for _, d := range ds {
		if d.ToolUseID != "" && a.answered[d.ToolUseID] {
			continue
		}
		out = append(out, d)
	}
	return out
}

const claudeAskSummaryMax = 200

// claudeAutoAllow 判斷這次詢問要不要直接允許。pm 是 run 建立時的權限模式，不中途重讀。
// 只有 bypassPermissions 會讀設定；讀取失敗視為關並寫 log。
func claudeAutoAllow(pm string, read func() (bool, error)) bool {
	if pm != "bypassPermissions" {
		return false
	}
	if read == nil {
		slog.Warn("[ws] 讀取 claudeAutoAllowAsk 失敗，改走詢問", "err", "未提供讀取函式")
		return false
	}
	on, err := read()
	if err != nil {
		slog.Warn("[ws] 讀取 claudeAutoAllowAsk 失敗，改走詢問", "err", err)
		return false
	}
	return on
}

// claudeAskSummary：Bash 取 command，其他工具取壓縮後的 input JSON，超過 200 字元加省略標記。
func claudeAskSummary(toolName string, input json.RawMessage) string {
	text := compactJSON(input)
	if toolName == "Bash" {
		var body struct {
			Command *string `json:"command"`
		}
		if json.Unmarshal(input, &body) == nil && body.Command != nil {
			text = *body.Command
		}
	}
	return truncateRunes(text, claudeAskSummaryMax)
}

func compactJSON(input json.RawMessage) string {
	if len(bytes.TrimSpace(input)) == 0 {
		return ""
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, input); err != nil {
		return string(input)
	}
	return buf.String()
}

func claudeAskAuditLine(toolName, summary string) string {
	// 與 blockedNotice 相同：前面空一行。行尾再空一行，後續模型文字才會另起一段。
	return "\n\n已自動允許 " + toolName + "：" + summary + "\n\n"
}

// claudeAskAudit 每次自動允許寫 server log，並在回覆附加函式就緒後附上一行說明。
// 附加函式由 run goroutine 晚綁定；尚未綁定或附加失敗時仍允許，log 一定要寫。
type claudeAskAudit struct {
	sessionID string
	mu        sync.Mutex
	notice    func(line string)
}

func newClaudeAskAudit(sessionID string) *claudeAskAudit {
	return &claudeAskAudit{sessionID: sessionID}
}

func (a *claudeAskAudit) bindNotice(notice func(string)) {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.notice = notice
	a.mu.Unlock()
}

func (a *claudeAskAudit) record(toolName, summary string) {
	if a == nil {
		return
	}
	slog.Info("[ws] claude ask 已自動允許", "session", a.sessionID, "tool", toolName, "summary", summary)
	a.mu.Lock()
	notice := a.notice
	a.mu.Unlock()
	if notice == nil {
		return
	}
	notice(claudeAskAuditLine(toolName, summary))
}
