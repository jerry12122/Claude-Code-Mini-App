package gemini

import "encoding/json"

// 參考 docs/gemini-cli.md 與 gemini-cli 原始碼 packages/cli/src/nonInteractiveCli.ts。
// stream-json 採 NDJSON，每行一個 JSON 物件。欄位定義鬆綁，未知欄位忽略以保持前向相容。
//
// 已知事件 type（對應 Gemini CLI JsonStreamEventType）：
//   init         { type, timestamp, session_id, model }
//   message      { type, timestamp, role, content, delta?(bool) }
//   tool_use     { type, timestamp, tool_name, tool_id, parameters }
//   tool_result  { type, timestamp, tool_id, status, output?, error?{type,message} }
//   error        { type, timestamp, severity, message }
//   result       { type, timestamp, status, stats }
//
// 注意：`result` 事件**不**帶 session_id，session_id 僅出現在第一個 `init` 事件。

// StreamEvent 為 gemini stream-json 的頂層事件。
type StreamEvent struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp,omitempty"`

	// init
	SessionID string `json:"session_id,omitempty"`
	Model     string `json:"model,omitempty"`

	// message
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
	// Gemini 用 pointer 區分「assistant delta chunk」與「非 delta 聚合」。
	// 目前 user message 無此欄位；assistant streaming 時 delta=true。
	Delta *bool `json:"delta,omitempty"`
	// Thought=true 表示此 chunk 為模型內部思考鏈，不應直接呈現在對話內容中。
	Thought *bool `json:"thought,omitempty"`

	// tool_use
	ToolName   string          `json:"tool_name,omitempty"`
	ToolID     string          `json:"tool_id,omitempty"`
	Parameters json.RawMessage `json:"parameters,omitempty"`

	// tool_result
	Status string          `json:"status,omitempty"` // success | error
	Output json.RawMessage `json:"output,omitempty"` // 可能為 string 也可能為 object
	Error  *ToolError      `json:"error,omitempty"`

	// error（非致命 warning/system error）
	Severity string `json:"severity,omitempty"` // warning | error
	Message  string `json:"message,omitempty"`

	// result
	Stats json.RawMessage `json:"stats,omitempty"`
}

// ToolError 對應 tool_result.error。
type ToolError struct {
	Type    string `json:"type,omitempty"`
	Message string `json:"message,omitempty"`
}

// IsAssistantDelta 判斷 message 事件是否為 assistant 的 streaming delta chunk。
func (e *StreamEvent) IsAssistantDelta() bool {
	if e.Type != "message" || e.Role != "assistant" {
		return false
	}
	return e.Delta != nil && *e.Delta
}

// OutputText 嘗試以字串形式取出 tool_result.output；若非字串則回傳原始 JSON。
func (e *StreamEvent) OutputText() string {
	if len(e.Output) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(e.Output, &s); err == nil {
		return s
	}
	return string(e.Output)
}

// ParseEvent 解析單行 NDJSON 為 StreamEvent。
func ParseEvent(line []byte) (*StreamEvent, error) {
	var e StreamEvent
	if err := json.Unmarshal(line, &e); err != nil {
		return nil, err
	}
	return &e, nil
}
