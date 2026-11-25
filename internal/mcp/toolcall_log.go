package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// toolCallLogMaxArgs 是 log 中 arguments 的最大字元數，超過截斷。
const toolCallLogMaxArgs = 500

// toolCallLogValueTools 是參數值可原樣寫進 log 的工具：參數只有 id、時間、模式等短值，不含訊息內容或指令。
// 不在清單內的工具（含日後新增的）只記參數鍵名與長度。
var toolCallLogValueTools = map[string]bool{
	"list_sessions": true, "create_session": true, "delete_session": true,
	"get_messages": true, "get_queue": true, "delete_queue_message": true,
	"list_activity": true, "get_status": true, "get_quota": true, "list_rooms": true,
	"set_permission_mode": true, "set_model": true, "set_effort": true,
	"interrupt": true, "resume_queue": true,
	"room_get_board": true, "room_get_history": true, "room_rename_session": true,
	"room_room_delete": true, "room_reset_session": true,
}

// toolCallLogMiddleware 對每次 tools/call 記一行 log：client、MCP session、工具名、參數、耗時、結果大小與錯誤。
func toolCallLogMiddleware(next gomcp.MethodHandler) gomcp.MethodHandler {
	return func(ctx context.Context, method string, req gomcp.Request) (gomcp.Result, error) {
		call, ok := req.(*gomcp.CallToolRequest)
		if !ok || method != "tools/call" {
			return next(ctx, method, req)
		}
		start := time.Now()
		res, err := next(ctx, method, req)

		attrs := []any{
			"tool", call.Params.Name,
			"client", toolCallClient(call),
			"args", toolCallArgs(call.Params.Name, call.Params.Arguments),
			"duration", time.Since(start),
		}
		if call.Session != nil {
			attrs = append(attrs, "mcp_session", call.Session.ID())
		}
		if b, mErr := json.Marshal(res); mErr == nil {
			attrs = append(attrs, "result_bytes", len(b))
		}
		if r, ok := res.(*gomcp.CallToolResult); ok && r != nil && r.IsError {
			attrs = append(attrs, "is_error", true)
		}
		if err != nil {
			attrs = append(attrs, "error", err.Error())
		}
		slog.Info("[mcp] tool call", attrs...)
		return res, err
	}
}

// toolCallClient 回傳 initialize 時 client 自報的 name/version；取不到回空字串。
func toolCallClient(call *gomcp.CallToolRequest) string {
	if call.Session == nil {
		return ""
	}
	p := call.Session.InitializeParams()
	if p == nil || p.ClientInfo == nil {
		return ""
	}
	return p.ClientInfo.Name + "/" + p.ClientInfo.Version
}

// toolCallArgs 回傳要寫進 log 的參數：白名單工具回傳截斷後的原始 JSON，其餘回傳鍵名與各值的位元組數。
func toolCallArgs(tool string, raw json.RawMessage) string {
	if toolCallLogValueTools[tool] {
		s, _ := truncateRunes(string(raw), toolCallLogMaxArgs)
		return s
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Sprintf("<redacted %d bytes>", len(raw))
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s:%dB", k, len(m[k]))
	}
	return "{" + strings.Join(parts, " ") + "}"
}
