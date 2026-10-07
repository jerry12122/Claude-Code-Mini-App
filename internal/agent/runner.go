package agent

import (
	"context"
	"encoding/json"
	"errors"
)

// ErrHandoff 表示復原策略建議換新會話；不代表已證明 session 永久損壞。
// 以 errors.Is 判斷，由呼叫端告知使用者並處理接手。
var ErrHandoff = errors.New("agent: session 無法繼續，需換新會話接手")

// RunOptions 是啟動 AI 工具子進程的共用參數。
//
// ExtraArgs 用來傳遞工具專屬的參數（例如 Claude 的 permission_mode、allowed_tools），
// 以避免把工具專屬欄位直接放在 RunOptions 上。
type RunOptions struct {
	Prompt    string
	SessionID string // 空字串表示新 session
	WorkDir   string
	ExtraArgs map[string]string
	// CliExtraArgs 為使用者自訂的額外 argv（每個元素對應一個命令列引數），由 Session 持久化；僅 Claude runner 會附加。
	CliExtraArgs []string
	// RequestPermission 為互動式授權回呼（目前僅 kiroacp ACP 使用）。
	// 當 runner 在回合中途收到工具授權請求時同步呼叫，會阻塞直到使用者決定或 ctx 取消。
	// 回傳選定的 optionID；空字串代表拒絕／取消。nil 表示呼叫端不支援互動授權。
	RequestPermission func(ctx context.Context, req PermissionRequest) string
	// OnStart 在子進程 cmd.Start() 成功後立即呼叫，帶入 PID。
	// 供呼叫端記錄 PID 以便日後送出優雅停止信號（第一次按停止時使用）。nil 表示不需要。
	OnStart func(pid int)
}

// PermissionOption 是一個授權選項（對應 ACP session/request_permission 的 options 項目）。
type PermissionOption struct {
	OptionID string `json:"option_id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"` // allow_once | allow_always | reject_once | reject_always
}

// PermissionRequest 是 runner 中途發起的工具授權請求。
type PermissionRequest struct {
	ToolCallID string             `json:"tool_call_id"`
	Title      string             `json:"title"`
	Options    []PermissionOption `json:"options"`
}

// EventType 代表 Runner 送出的事件種類。
type EventType string

const (
	EventDelta         EventType = "delta"
	EventThinking      EventType = "thinking" // 思考鏈 chunk（覆寫式，不累積）
	EventDone          EventType = "done"
	EventError         EventType = "error"
	EventPermDenied    EventType = "permission_denied"
	EventSessionInit   EventType = "session_init"
	EventStreamStart   EventType = "stream_start"
	EventActivity      EventType = "activity" // Codex item.started 活動提示（THINKING 期間）
	EventToolStarted   EventType = "tool_started"
	EventToolCompleted EventType = "tool_completed"
)

// PermissionDenial 是 Claude 特有的授權拒絕資訊，其他工具可忽略。
type PermissionDenial struct {
	ToolName  string          `json:"tool_name"`
	ToolUseID string          `json:"tool_use_id"`
	ToolInput json.RawMessage `json:"tool_input"`
	// BlockedByRule：該次呼叫被 settings 的 permissions.deny 規則擋下（stream 的 non_execution_kind=permission-rule）。
	// 按「允許」重跑也不會通過，所以不該進等授權。只在記憶體流動，不寫入 pending_denials。
	BlockedByRule bool `json:"blocked_by_rule,omitempty"`
}

// ToolCall 是 tool_started / tool_completed 事件的共用承載結構，
// 各 runner 會把自身 CLI 的工具事件正規化成此結構。
type ToolCall struct {
	CallID     string          `json:"call_id"`
	Name       string          `json:"name"`
	Arguments  json.RawMessage `json:"arguments,omitempty"`
	Output     string          `json:"output,omitempty"` // 僅 tool_completed 帶入（文字輸出）
	OK         bool            `json:"ok,omitempty"`     // 僅 tool_completed 帶入
	ErrMessage string          `json:"err_message,omitempty"`
}

// ModelSnapshot 是 session 目前 model 的快照（由 runner 或 ws 層填入）。
type ModelSnapshot struct {
	Model       string `json:"model,omitempty"`
	DisplayText string `json:"display_text,omitempty"`
	Source      string `json:"source,omitempty"`
}

// Event 是 Runner 透過 callback 回傳的統一事件結構。
type Event struct {
	Type       EventType
	Text       string             // delta 文字
	SessionID  string             // session_init / done 時帶入
	ResultText string             // 僅 done：CLI 最終 result 行若帶純文字摘要／輸出（stream-json 之 result 欄位）
	Model      *ModelSnapshot     // session_init 時若 stream 帶 model
	Denials    []PermissionDenial // 僅 Claude 有
	Tool       *ToolCall          // tool_started / tool_completed 時帶入
	Err        error              // error 時帶入
}

// EventCallback 是 Runner 每收到一個事件都會呼叫一次的 callback。
type EventCallback func(e Event)

// Runner 是所有 AI 工具（Claude、Codex、Gemini…）必須實作的介面。
type Runner interface {
	// Run 啟動子進程並串流事件，子進程結束後函式才返回。
	Run(ctx context.Context, opts RunOptions, cb EventCallback) error

	// Name 回傳工具名稱，例如 "claude"、"antigravity"、"kiro"。
	Name() string
}

// ExtraArg 是 ExtraArgs map 的共用 key。
// Claude 權限詢問 MCP 的固定名稱：mcp-config 的 server key 與工具名。--permission-prompt-tool 要寫成 mcp__<server>__<tool>。
const (
	PermPromptServer = "miniapp_perm"
	PermPromptTool   = "claude_permission"
	PermPromptFlag   = "mcp__" + PermPromptServer + "__" + PermPromptTool
)

const (
	// 共用語意：授權/權限模式
	// Claude 值：default / acceptEdits / bypassPermissions / plan
	// Cursor 值：default / bypassPermissions（僅決定是否加 --force）
	// Antigravity（agy）值：default；bypassPermissions → --dangerously-skip-permissions
	// legacy Gemini 值仍經 UI 映射為 antigravity
	ArgPermissionMode = "permission_mode"

	// Claude 專屬
	ArgAllowedTools = "allowed_tools" // 以逗號分隔
	// ArgPermPromptConfig 為 --mcp-config 的 JSON 檔路徑；有值時 Claude runner 同時帶
	// --permission-prompt-tool，讓命中 permissions.ask 的呼叫轉給 miniapp 詢問使用者。
	ArgPermPromptConfig = "perm_prompt_config"

	// Cursor Agent / Gemini 共用
	ArgModel = "model" // --model <m>
	ArgForce = "force" // Cursor: --force，值為 "true"/"1" 表示開啟

	// ArgEffort 為推理強度（low/medium/high/xhigh/max）。
	// Claude/Kiro/KiroACP：--effort <level>
	// Codex：轉成 -c model_reasoning_effort=<level>（無獨立旗標）
	// Cursor：不支援獨立旗標，忽略
	ArgEffort = "effort"
)
