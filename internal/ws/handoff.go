package ws

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

// 自動交接：runner 建議換新會話（agent.ErrHandoff）時，保留原會話，
// 另建一個同設定的新會話，由新會話的 agent 透過 miniapp MCP 的 get_messages 讀舊會話後接手。

var handoffCfg struct {
	mu    sync.Mutex
	start func(sessionID, text string) error
}

// SetHandoffStarter 由 server 在啟用 /mcp 時注入（start = 把 text 送進某 session 並啟動一輪）。
// 未設定時提供手動接手入口，改帶入前端已載入的有限歷史。
func SetHandoffStarter(start func(sessionID, text string) error) {
	handoffCfg.mu.Lock()
	handoffCfg.start = start
	handoffCfg.mu.Unlock()
}

// handoffMarker 同時用來擋連鎖：交接會話自己再失敗時不再交接，避免無限產生新會話。
const handoffMarker = "[miniapp 自動交接]"

// manualHandoffInfo 是「自動交接未成功，提供手動建立接手會話」的結構化資料，
// 透過 WS 的 serverMsg.Handoff 送到前端；前端只讀欄位，不解析任何中文顯示字串或錯誤訊息來判斷故障。
type manualHandoffInfo struct {
	OldSessionID   string   `json:"old_session_id"`
	OldName        string   `json:"old_name"`
	OldWorkDir     string   `json:"old_work_dir"`
	AgentType      string   `json:"agent_type"`
	PermissionMode string   `json:"permission_mode"`
	Model          string   `json:"model"`
	Effort         string   `json:"effort"`
	CliExtraArgs   []string `json:"cli_extra_args"`
	LastPrompt     string   `json:"last_prompt"`
	Reason         string   `json:"reason"`
	// MCPEnabled：MCP 未啟用時前端改帶入既有聊天歷史（有限筆數），而不是提示呼叫 get_messages。
	MCPEnabled bool `json:"mcp_enabled"`
}

// newManualHandoffInfo 用既有欄位直接組出結構化資訊；cause 僅取 Error() 字串當作「原因」顯示文字，
// 不做任何語意解析（前端不得反過來從 Reason 解析狀態）。
func newManualHandoffInfo(old *db.Session, lastPrompt string, cause error) manualHandoffInfo {
	handoffCfg.mu.Lock()
	enabled := handoffCfg.start != nil
	handoffCfg.mu.Unlock()
	return manualHandoffInfo{
		OldSessionID:   old.ID,
		OldName:        old.Name,
		OldWorkDir:     old.WorkDir,
		AgentType:      old.AgentType,
		PermissionMode: old.PermissionMode,
		Model:          old.Model,
		Effort:         old.Effort,
		CliExtraArgs:   old.CliExtraArgs,
		LastPrompt:     lastPrompt,
		Reason:         cause.Error(),
		MCPEnabled:     enabled,
	}
}

// startHandoff 建立接手會話並送出交接訊息，回傳接手會話。失敗時不留下半成品會話。
// lastPrompt 是使用者這一輪送給 agent 的完整內容（含附件路徑）；cause 是觸發交接的錯誤。
func startHandoff(database *db.DB, old *db.Session, lastPrompt string, cause error) (*db.Session, error) {
	handoffCfg.mu.Lock()
	start := handoffCfg.start
	handoffCfg.mu.Unlock()
	if start == nil {
		return nil, errors.New("mcp 未啟用（mcp_token 未設定）")
	}
	if strings.Contains(lastPrompt, handoffMarker) {
		return nil, errors.New("本身已是交接會話，不再連鎖交接")
	}

	name := strings.TrimSpace(old.Name)
	if name == "" {
		name = old.ID[:8]
	}
	ns, err := database.CreateSession(name+"（接手）", "由 "+old.ID+" 自動交接", old.WorkDir, old.PermissionMode, old.AgentType, old.CliExtraArgs, "agent")
	if err != nil {
		return nil, fmt.Errorf("建立接手會話: %w", err)
	}
	cleanup := func() {
		if err := database.DeleteSession(ns.ID); err != nil {
			slog.Info(fmt.Sprintf("[ws] handoff 清除半成品會話 %s: %v", ns.ID, err))
		}
		NotifySessionsChanged()
	}
	if old.Model != "" {
		if err := database.UpdateModel(ns.ID, old.Model); err != nil {
			cleanup()
			return nil, err
		}
	}
	if old.Effort != "" {
		if err := database.UpdateEffort(ns.ID, old.Effort); err != nil {
			cleanup()
			return nil, err
		}
	}
	NotifySessionsChanged()

	if err := start(ns.ID, handoffPrompt(old, lastPrompt, cause)); err != nil {
		cleanup()
		return nil, fmt.Errorf("啟動接手會話: %w", err)
	}
	return ns, nil
}

func handoffPrompt(old *db.Session, lastPrompt string, cause error) string {
	why := cause.Error()
	if r := []rune(why); len(r) > 200 {
		why = string(r[:200]) + "…"
	}
	return fmt.Sprintf(`%s 原會話（id=%s，名稱「%s」）的 %s 執行失敗（%s），所以自動建立你這個新會話來接手。
請先呼叫 miniapp MCP 的 get_messages 讀取該會話（session_id="%s"）最近的對話，弄清楚進度；需要更多內容再自行往前翻。
注意：舊會話最後一輪可能已執行到一半，動手前先檢查檔案／git 現況，不要重複已完成的步驟。
接手後請完成使用者最後這則需求（若 MCP 讀不到舊會話，就只憑下面原文處理）：
---
%s`, handoffMarker, old.ID, old.Name, old.AgentType, why, old.ID, lastPrompt)
}
