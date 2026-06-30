package antigravity

import "encoding/json"

// stream-json 事件格式與 legacy Gemini CLI 相容（Antigravity 共用 agent harness）。
// 參考 docs/spec/antigravity-cli.md；POC：poc/antigravity-cli/
//
// 已知 type：init | message | tool_use | tool_result | error | result

// StreamEvent 為 stream-json 頂層事件。
type StreamEvent struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp,omitempty"`

	// init
	SessionID string `json:"session_id,omitempty"`
	Model     string `json:"model,omitempty"`

	// message
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
	Delta   *bool  `json:"delta,omitempty"`
	Thought *bool  `json:"thought,omitempty"`

	// tool_use
	ToolName   string          `json:"tool_name,omitempty"`
	ToolID     string          `json:"tool_id,omitempty"`
	Parameters json.RawMessage `json:"parameters,omitempty"`

	// tool_result
	Status string          `json:"status,omitempty"`
	Output json.RawMessage `json:"output,omitempty"`
	Error  *ToolError      `json:"error,omitempty"`

	// error（非致命）
	Severity string `json:"severity,omitempty"`
	Message  string `json:"message,omitempty"`

	// result
	Stats json.RawMessage `json:"stats,omitempty"`
}

// ToolError 對應 tool_result.error。
type ToolError struct {
	Type    string `json:"type,omitempty"`
	Message string `json:"message,omitempty"`
}

// IsAssistantDelta 判斷 message 是否為 assistant streaming chunk。
func (e *StreamEvent) IsAssistantDelta() bool {
	if e.Type != "message" || e.Role != "assistant" {
		return false
	}
	return e.Delta != nil && *e.Delta
}

// OutputText 取出 tool_result.output 文字。
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

// ParseEvent 解析單行 NDJSON。
func ParseEvent(line []byte) (*StreamEvent, error) {
	var e StreamEvent
	if err := json.Unmarshal(line, &e); err != nil {
		return nil, err
	}
	return &e, nil
}
