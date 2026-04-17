package agent

import (
	"context"
	"encoding/json"
)

// RunOptions 是啟動 AI 工具子進程的共用參數。
//
// ExtraArgs 用來傳遞工具專屬的參數（例如 Claude 的 permission_mode、allowed_tools），
// 以避免把工具專屬欄位直接放在 RunOptions 上。
type RunOptions struct {
	Prompt    string
	SessionID string // 空字串表示新 session
	WorkDir   string
	ExtraArgs map[string]string
}

// EventType 代表 Runner 送出的事件種類。
type EventType string

const (
	EventDelta         EventType = "delta"
	EventDone          EventType = "done"
	EventError         EventType = "error"
	EventPermDenied    EventType = "permission_denied"
	EventSessionInit   EventType = "session_init"
	EventStreamStart   EventType = "stream_start"
	EventToolStarted   EventType = "tool_started"
	EventToolCompleted EventType = "tool_completed"
)

// PermissionDenial 是 Claude 特有的授權拒絕資訊，其他工具可忽略。
type PermissionDenial struct {
	ToolName  string          `json:"tool_name"`
	ToolUseID string          `json:"tool_use_id"`
	ToolInput json.RawMessage `json:"tool_input"`
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

// Event 是 Runner 透過 callback 回傳的統一事件結構。
type Event struct {
	Type      EventType
	Text      string             // delta 文字
	SessionID string             // session_init / done 時帶入
	Denials   []PermissionDenial // 僅 Claude 有
	Tool      *ToolCall          // tool_started / tool_completed 時帶入
	Err       error              // error 時帶入
}

// EventCallback 是 Runner 每收到一個事件都會呼叫一次的 callback。
type EventCallback func(e Event)

// Runner 是所有 AI 工具（Claude、Codex、Gemini…）必須實作的介面。
type Runner interface {
	// Run 啟動子進程並串流事件，子進程結束後函式才返回。
	Run(ctx context.Context, opts RunOptions, cb EventCallback) error

	// Name 回傳工具名稱，例如 "claude"、"codex"、"gemini"。
	Name() string
}

// ExtraArg 是 ExtraArgs map 的共用 key。
const (
	// 共用語意：授權/權限模式
	// Claude 值：default / acceptEdits / bypassPermissions / plan
	// Cursor 值：default / bypassPermissions（僅決定是否加 --force）
	// Gemini 值：default / auto_edit / yolo / plan（Gemini runner 另外接受 acceptEdits → auto_edit、bypassPermissions → yolo 的向下相容 mapping）
	ArgPermissionMode = "permission_mode"

	// Claude 專屬
	ArgAllowedTools = "allowed_tools" // 以逗號分隔

	// Cursor Agent / Gemini 共用
	ArgModel = "model" // --model <m>
	ArgForce = "force" // Cursor: --force，值為 "true"/"1" 表示開啟
)
