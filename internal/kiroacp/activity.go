package kiroacp

import (
	"strings"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
)

type sessionActivity struct {
	thoughts strings.Builder
	tools    map[string]sessionUpdateBody
}

func (st *sessionActivity) update(body sessionUpdateBody) agent.Event {
	switch body.SessionUpdate {
	case "agent_thought_chunk":
		text := extractAgentText(body)
		if text == "" {
			return agent.Event{}
		}
		// ponytail: 與 Claude 一樣限制為 512 KiB 快照；更長的思考只保留已接收部分。
		if st.thoughts.Len()+len(text) <= 512*1024 {
			st.thoughts.WriteString(text)
		}
		return agent.Event{Type: agent.EventThinking, Text: st.thoughts.String()}
	case "agent_message_chunk":
		st.thoughts.Reset()
	case "plan":
		st.thoughts.Reset()
		for _, entry := range body.Entries {
			if entry.Status == "in_progress" {
				return agent.Event{Type: agent.EventActivity, Text: "進行中：" + entry.Content}
			}
		}
	case "tool_call", "tool_call_update":
		st.thoughts.Reset()
		// ACP 更新只帶變動欄位，以 call ID 保留各工具的標題／參數，完成即釋放。
		tool := st.tools[body.ToolCallID]
		if body.Title != "" {
			tool.Title = body.Title
		}
		if body.Name != "" {
			tool.Name = body.Name
		}
		if body.Kind != "" {
			tool.Kind = body.Kind
		}
		if body.Status != "" {
			tool.Status = body.Status
		}
		if len(body.RawInput) > 0 && strings.TrimSpace(string(body.RawInput)) != "null" {
			tool.RawInput = body.RawInput
		}
		if body.Locations != nil {
			tool.Locations = body.Locations
		}
		if body.ToolCallID != "" {
			if st.tools == nil {
				st.tools = make(map[string]sessionUpdateBody)
			}
			st.tools[body.ToolCallID] = tool
			if tool.Status == "completed" || tool.Status == "failed" {
				delete(st.tools, body.ToolCallID)
			}
		}
		name := strings.TrimSpace(tool.Title)
		if name == "" {
			name = tool.Name
		}
		if name == "" {
			name = tool.Kind
		}
		label := agent.ToolActivity(name, tool.RawInput)
		if len(tool.Locations) > 0 && label == agent.ToolActivity(name, nil) {
			var paths []string
			for _, location := range tool.Locations {
				if location.Path != "" {
					paths = append(paths, location.Path)
				}
			}
			if len(paths) > 0 {
				label += "：" + strings.Join(paths, ", ")
			}
		}
		prefix := "執行工具："
		switch tool.Status {
		case "pending":
			prefix = "準備工具："
		case "completed":
			prefix = "工具已完成："
		case "failed":
			prefix = "工具失敗："
		}
		return agent.Event{Type: agent.EventActivity, Text: prefix + label}
	}
	return agent.Event{}
}
