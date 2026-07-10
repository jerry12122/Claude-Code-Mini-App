package claude

import (
	"encoding/json"
	"strings"
)

// 頂層事件（每行一個 JSON）
type StreamEvent struct {
	Type    string           `json:"type"`
	Subtype string           `json:"subtype,omitempty"`
	Event   *APIEvent        `json:"event,omitempty"`   // type=stream_event
	Message *AssistantMessage `json:"message,omitempty"` // type=assistant
	// type=result
	IsError           bool               `json:"is_error,omitempty"`
	Result            string             `json:"result,omitempty"`
	SessionID         string             `json:"session_id,omitempty"`
	Model             string             `json:"model,omitempty"`
	StopReason        string             `json:"stop_reason,omitempty"`
	PermissionDenials []PermissionDenial `json:"permission_denials,omitempty"`
}

// AssistantMessage 是 type=assistant 事件的 message 欄位
type AssistantMessage struct {
	Content []MessageContent `json:"content"`
}

type MessageContent struct {
	Type string `json:"type"` // "text" | "tool_use" | ...
	Text string `json:"text,omitempty"`
}

// TextContent 提取 assistant message 中所有文字區塊，串接後回傳
func (e *StreamEvent) TextContent() string {
	if e.Message == nil {
		return ""
	}
	var sb strings.Builder
	for _, c := range e.Message.Content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
		}
	}
	return sb.String()
}

// Anthropic API 內層事件（stream_event 的 event 欄位）
type APIEvent struct {
	Type         string        `json:"type"`
	Index        int           `json:"index,omitempty"`
	ContentBlock *ContentBlock `json:"content_block,omitempty"`
	Delta        *Delta        `json:"delta,omitempty"`
}

type ContentBlock struct {
	Type string `json:"type"` // "text" | "thinking" | "tool_use"
	Name string `json:"name,omitempty"`
}

type Delta struct {
	Type        string `json:"type"`              // "text_delta" | "input_json_delta" | "thinking_delta"
	Text        string `json:"text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	StopReason  string `json:"stop_reason,omitempty"`
}

type PermissionDenial struct {
	ToolName  string          `json:"tool_name"`
	ToolUseID string          `json:"tool_use_id"`
	ToolInput json.RawMessage `json:"tool_input"`
}

func ParseEvent(line []byte) (*StreamEvent, error) {
	var e StreamEvent
	if err := json.Unmarshal(line, &e); err != nil {
		return nil, err
	}
	return &e, nil
}
