package claude

import (
	"encoding/json"
	"strings"
)

// 頂層事件（每行一個 JSON）
type StreamEvent struct {
	Type    string            `json:"type"`
	Subtype string            `json:"subtype,omitempty"`
	Event   *APIEvent         `json:"event,omitempty"`   // type=stream_event
	Message *AssistantMessage `json:"message,omitempty"` // type=assistant
	// type=result
	IsError           bool               `json:"is_error,omitempty"`
	Result            string             `json:"result,omitempty"`
	SessionID         string             `json:"session_id,omitempty"`
	Model             string             `json:"model,omitempty"`
	StopReason        string             `json:"stop_reason,omitempty"`
	PermissionDenials []PermissionDenial `json:"permission_denials,omitempty"`
	// type=user：被拒／未執行的 tool_result 旁會帶 tool_result_meta（claude 2.1.x 觀察到的欄位，官方文件未保證；缺漏時視為沒有訊號）。
	ToolResultMeta []ToolResultMeta `json:"tool_result_meta,omitempty"`
}

// NonExecPermissionRule：該工具呼叫被 settings 的 permissions.deny 規則擋下。
const NonExecPermissionRule = "permission-rule"

// ToolResultMeta 說明某個 tool_use 為何沒有執行（non_execution_kind）。
type ToolResultMeta struct {
	ID               string `json:"id"`
	NonExecutionKind string `json:"non_execution_kind,omitempty"`
}

// AssistantMessage 是 type=assistant 事件的 message 欄位
type AssistantMessage struct {
	Content []MessageContent `json:"content"`
}

type MessageContent struct {
	Type      string          `json:"type"` // "text" | "tool_use" | "tool_result" | "image" | ...
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
	// Content 是 tool_result block 的巢狀內容（type=="tool_result" 時），
	// MCP 工具若回傳圖片（如截圖）會以 type=="image" 出現在這裡。
	Content NestedContent `json:"content,omitempty"`
	// Source 是 image block 的資料來源（type=="image" 時）。
	Source *ImageSource `json:"source,omitempty"`
}

// NestedContent 容許 tool_result.content 為純字串（多數 MCP 工具）或 block 陣列；
// 字串形式沒有圖片可收，直接視為空。
type NestedContent []MessageContent

func (n *NestedContent) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		*n = nil
		return nil
	}
	return json.Unmarshal(b, (*[]MessageContent)(n))
}

// ImageSource 對應 Anthropic API image content block 的 base64 來源。
type ImageSource struct {
	Type      string `json:"type"` // "base64"
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
}

// Images 掃描 message content（含 tool_result 巢狀內容），取出所有 image block。
func (e *StreamEvent) Images() []ImageSource {
	if e.Message == nil {
		return nil
	}
	var out []ImageSource
	for _, c := range e.Message.Content {
		out = append(out, collectImages(c)...)
	}
	return out
}

func collectImages(c MessageContent) []ImageSource {
	var out []ImageSource
	if c.Type == "image" && c.Source != nil && c.Source.Data != "" {
		out = append(out, *c.Source)
	}
	for _, nested := range c.Content {
		out = append(out, collectImages(nested)...)
	}
	return out
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
	Type        string `json:"type"` // "text_delta" | "input_json_delta" | "thinking_delta"
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
