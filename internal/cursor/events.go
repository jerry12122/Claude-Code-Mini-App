package cursor

import (
	"encoding/json"
	"strings"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
)

// 參考 docs/cursor-agent-cli.md 與官方 output-format 文件。
// stream-json 採 NDJSON，每行一個 JSON 物件。

// StreamEvent 為 cursor-agent stream-json 的頂層事件。
// 欄位採鬆綁定義，未知欄位會被忽略以保持前向相容。
type StreamEvent struct {
	Type    string `json:"type"`              // system | user | assistant | tool_call | result
	Subtype string `json:"subtype,omitempty"` // init | started | completed | success | error

	SessionID string `json:"session_id,omitempty"`
	CWD       string `json:"cwd,omitempty"`
	Model     string `json:"model,omitempty"`

	// assistant / user
	Message *Message `json:"message,omitempty"`

	// assistant streaming delta 專屬（--stream-partial-output）
	// 用來區分三種 assistant 事件：
	//   streaming delta: 有 timestamp_ms、無 model_call_id → 附加
	//   buffered flush : 兩者皆有 → 略過
	//   final flush    : 兩者皆無 → 略過
	TimestampMS *int64  `json:"timestamp_ms,omitempty"`
	ModelCallID *string `json:"model_call_id,omitempty"`

	// tool_call
	CallID   string          `json:"call_id,omitempty"`
	ToolCall json.RawMessage `json:"tool_call,omitempty"`

	// result
	IsError       bool   `json:"is_error,omitempty"`
	Result        string `json:"result,omitempty"`
	DurationMS    int64  `json:"duration_ms,omitempty"`
	DurationAPIMS int64  `json:"duration_api_ms,omitempty"`
	RequestID     string `json:"request_id,omitempty"`
}

// Message 對應 assistant / user 事件中的 message 欄位。
type Message struct {
	Role    string           `json:"role"`
	Content []MessageContent `json:"content"`
}

// MessageContent 是 message.content 的項目。
type MessageContent struct {
	Type string `json:"type,omitempty"`
	Text string `json:"text,omitempty"`
}

// Text 串接所有 text content 後回傳。
func (m *Message) Text() string {
	if m == nil {
		return ""
	}
	var out string
	for _, c := range m.Content {
		if c.Type == "" || c.Type == "text" {
			out += c.Text
		}
	}
	return out
}

// ParseEvent 解析單行 NDJSON 為 StreamEvent。
func ParseEvent(line []byte) (*StreamEvent, error) {
	var e StreamEvent
	if err := json.Unmarshal(line, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

// mcpContentBlock 對應標準 MCP tool result 的 content block（text/image/...）。
type mcpContentBlock struct {
	Type     string `json:"type"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

// cursorMcpContentBlock 對應 cursor-agent 實際輸出的 MCP content 包裝：
//
//	[{"text":{"text":"..."}},{"image":{"data":"...","mimeType":"image/png"}}]
//
// 與標準 MCP 的 {"type":"image","data":"...","mimeType":"..."} 不同。
type cursorMcpContentBlock struct {
	Text *struct {
		Text string `json:"text"`
	} `json:"text,omitempty"`
	Image *struct {
		Data     string `json:"data"`
		MimeType string `json:"mimeType"`
	} `json:"image,omitempty"`
}

// ImageBlock 是從 tool_call 結果取出的圖片資料，交給 media.SaveBase64Image 落地存檔。
type ImageBlock struct {
	MediaType string
	Data      string
}

// Images 掃描 mcpToolCall 的成功結果，取出所有 image content block。
// 同時相容兩種 schema：
//  1. cursor-agent 實際格式：{"image":{"data","mimeType"}}
//  2. 標準 MCP：{"type":"image","data","mimeType"}
//
// 非 mcpToolCall 或 content 非陣列時回傳空。
func (e *StreamEvent) Images() []ImageBlock {
	content := e.mcpSuccessContent()
	if len(content) == 0 {
		return nil
	}

	// 先試 cursor-agent 包裝格式（實測 Screenshot 工具走這條）。
	var cursorBlocks []cursorMcpContentBlock
	if err := json.Unmarshal(content, &cursorBlocks); err == nil {
		var out []ImageBlock
		for _, b := range cursorBlocks {
			if b.Image != nil && b.Image.Data != "" {
				out = append(out, ImageBlock{MediaType: b.Image.MimeType, Data: b.Image.Data})
			}
		}
		if len(out) > 0 {
			return out
		}
	}

	// fallback：標準 MCP content block。
	var blocks []mcpContentBlock
	if err := json.Unmarshal(content, &blocks); err != nil {
		return nil
	}
	var out []ImageBlock
	for _, b := range blocks {
		if b.Type == "image" && b.Data != "" {
			out = append(out, ImageBlock{MediaType: b.MimeType, Data: b.Data})
		}
	}
	return out
}

func (e *StreamEvent) mcpSuccessContent() json.RawMessage {
	if len(e.ToolCall) == 0 {
		return nil
	}
	var wrapper struct {
		McpToolCall *struct {
			Result *struct {
				Success *struct {
					Content json.RawMessage `json:"content"`
				} `json:"success"`
			} `json:"result"`
		} `json:"mcpToolCall"`
	}
	if err := json.Unmarshal(e.ToolCall, &wrapper); err != nil {
		return nil
	}
	if wrapper.McpToolCall == nil || wrapper.McpToolCall.Result == nil || wrapper.McpToolCall.Result.Success == nil {
		return nil
	}
	return wrapper.McpToolCall.Result.Success.Content
}

// ToolLabel 從多型 tool_call payload 取出前端活動提示文字。
// 已知 schema：readToolCall / writeToolCall；fallback：function.name。
// ponytail: tool_call 物件預期只有單一工具鍵，故 map 首個相符鍵即可（順序不定但只有一個）。
func (e *StreamEvent) ToolLabel() string {
	if len(e.ToolCall) == 0 {
		return ""
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(e.ToolCall, &m) != nil {
		return ""
	}
	if raw, ok := m["function"]; ok {
		var fn struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if json.Unmarshal(raw, &fn) == nil && strings.TrimSpace(fn.Name) != "" {
			return agent.ToolActivity(fn.Name, fn.Arguments)
		}
	}
	for k, raw := range m {
		var call struct {
			Args json.RawMessage `json:"args"`
		}
		_ = json.Unmarshal(raw, &call)
		switch k {
		case "readToolCall":
			return agent.ToolActivity("讀取檔案", call.Args)
		case "writeToolCall":
			return agent.ToolActivity("修改檔案", call.Args)
		default:
			if name := strings.TrimSuffix(k, "ToolCall"); name != k && name != "" {
				return agent.ToolActivity(name, call.Args)
			}
		}
	}
	return "使用工具中…"
}
