package agent

import "encoding/json"

// ToolActivity 只摘取工具的操作對象，不把寫入內容或整包參數塞進活動列。
func ToolActivity(name string, args json.RawMessage) string {
	if name == "" {
		name = "工具"
	}
	// Cursor function.arguments 可能是 JSON 字串，其餘 runner 使用 JSON 物件。
	var encoded string
	if json.Unmarshal(args, &encoded) == nil {
		args = []byte(encoded)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(args, &fields) != nil {
		return name
	}
	for _, key := range []string{"command", "CommandLine", "commandLine", "file_path", "path", "AbsolutePath", "TargetFile", "pattern", "query", "SearchPath"} {
		var value string
		if json.Unmarshal(fields[key], &value) == nil && value != "" {
			return name + "：" + value
		}
	}
	return name
}
