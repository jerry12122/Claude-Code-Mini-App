package claude

import (
	"bufio"
	"bytes"
	"context"
	"fmt"

	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/media"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/model"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/proc"
	"log/slog"
)

func init() {
	agent.Register(agent.TypeClaude, func() agent.Runner {
		return &Runner{}
	})
}

// Runner 是 Claude Code CLI 的 agent.Runner 實作。
type Runner struct{}

// Name 實作 agent.Runner。
func (r *Runner) Name() string { return agent.TypeClaude }

// buildClaudeArgs 組出傳給 `claude` 可執行檔的 argv（不含程式名本身）。供 Run 與單元測試共用。
func buildClaudeArgs(opts agent.RunOptions) []string {
	args := make([]string, 0, len(opts.CliExtraArgs)+16)
	args = append(args, opts.CliExtraArgs...)
	args = append(args,
		"-p",
		"--output-format", "stream-json",
		"--verbose",
		"--include-partial-messages",
	)

	if opts.SessionID != "" {
		args = append(args, "--resume", opts.SessionID)
	}

	mode := ""
	if opts.ExtraArgs != nil {
		mode = opts.ExtraArgs[agent.ArgPermissionMode]
	}
	if mode == "" {
		mode = "default"
	}
	args = append(args, "--permission-mode", mode)

	if opts.ExtraArgs != nil {
		if raw := opts.ExtraArgs[agent.ArgAllowedTools]; raw != "" {
			for _, tool := range strings.Split(raw, ",") {
				tool = strings.TrimSpace(tool)
				if tool == "" {
					continue
				}
				args = append(args, "--allowedTools", tool)
			}
		}
		if m := strings.TrimSpace(opts.ExtraArgs[agent.ArgModel]); m != "" {
			args = append(args, "--model", m)
		}
		if effort := strings.TrimSpace(opts.ExtraArgs[agent.ArgEffort]); effort != "" {
			args = append(args, "--effort", effort)
		}
		// 權限詢問 MCP：設定檔路徑（內含 token 的 header 不出現在 argv／log）。
		if p := strings.TrimSpace(opts.ExtraArgs[agent.ArgPermPromptConfig]); p != "" {
			args = append(args, "--mcp-config", p, "--permission-prompt-tool", agent.PermPromptFlag)
		}
	}
	return args
}

// permPromptToolTimeoutMs：權限詢問等使用者回答的上限（30 分鐘）。
// 實測 claude 的 MCP 工具呼叫（HTTP）預設約 60 秒就放棄，使用者還沒看到通知詢問就失敗；
// 設 MCP_TOOL_TIMEOUT 後 90 秒才回答仍可執行。
const permPromptToolTimeoutMs = "1800000"

// claudeEnv 組出 claude 子進程的環境變數。
func claudeEnv(base []string, permPrompt bool) []string {
	// -p 結束後背景 Bash 約 5 秒會被殺；禁止背景任務，改前景跑完再回 result。
	env := append(base[:len(base):len(base)], "CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1")
	if permPrompt {
		for _, kv := range base {
			if strings.HasPrefix(kv, "MCP_TOOL_TIMEOUT=") {
				return env // 使用者自己設定過就尊重
			}
		}
		env = append(env, "MCP_TOOL_TIMEOUT="+permPromptToolTimeoutMs)
	}
	return env
}

// Run 實作 agent.Runner：啟動 claude -p 子進程，逐行解析 stream-json 並透過 cb 回傳事件。
func (r *Runner) Run(ctx context.Context, opts agent.RunOptions, cb agent.EventCallback) error {
	start := time.Now()
	args := buildClaudeArgs(opts)
	slog.Info(fmt.Sprintf("[claude] 執行指令: claude %s (prompt len=%d)", strings.Join(args, " "), len(opts.Prompt)))
	if opts.WorkDir != "" {
		slog.Info(fmt.Sprintf("[claude] 工作目錄: %s", opts.WorkDir))
	}

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Stdin = strings.NewReader(opts.Prompt)
	if opts.WorkDir != "" {
		cmd.Dir = opts.WorkDir
	}
	// -p 結束後背景 Bash 約 5 秒會被殺；禁止背景任務，改前景跑完再回 result。
	cmd.Env = claudeEnv(os.Environ(), opts.ExtraArgs[agent.ArgPermPromptConfig] != "")
	cmd.SysProcAttr = proc.SysProcAttr()
	cmd.WaitDelay = 5 * time.Second
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return proc.KillTree(cmd.Process.Pid)
		}
		return nil
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		slog.Error(fmt.Sprintf("[claude] 取得 stdout pipe 失敗: %v", err))
		return err
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		slog.Error(fmt.Sprintf("[claude] 子進程啟動失敗: %v", err))
		return err
	}
	slog.Info(fmt.Sprintf("[claude] 子進程已啟動，PID=%d", cmd.Process.Pid))
	if opts.OnStart != nil {
		opts.OnStart(cmd.Process.Pid)
	}

	scanner := bufio.NewScanner(stdout)
	// MCP 截圖 base64 單行可達數 MB；1MB 會讓 Scan 失敗並整行丟棄。
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)

	var st streamState
	lineCount := 0
	sessionID := opts.SessionID
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		lineCount++
		slog.Debug(fmt.Sprintf("[claude] 收到第 %d 行 (len=%d): %s", lineCount, len(line), truncate(string(line), 200)))

		e, err := ParseEvent(line)
		if err != nil {
			slog.Warn(fmt.Sprintf("[claude] 解析失敗: %v | 原始內容: %s", err, truncate(string(line), 200)))
			continue
		}
		slog.Debug(fmt.Sprintf("[claude] 事件 type=%s subtype=%s", e.Type, e.Subtype))
		if e.Event != nil {
			slog.Debug(fmt.Sprintf("[claude]   └─ API event type=%s", e.Event.Type))
		}
		if e.SessionID != "" {
			sessionID = e.SessionID
			slog.Debug(fmt.Sprintf("[claude]   └─ session_id=%s", e.SessionID))
		}
		if e.IsError {
			slog.Error(fmt.Sprintf("[claude]   └─ IS_ERROR result=%s", e.Result))
		}
		if len(e.PermissionDenials) > 0 {
			slog.Info(fmt.Sprintf("[claude]   └─ permission_denials=%d 項", len(e.PermissionDenials)))
		}

		r.dispatch(e, cb, &st)
	}

	if err := scanner.Err(); err != nil {
		slog.Error(fmt.Sprintf("[claude] scanner 錯誤: %v", err))
	}

	waitErr := cmd.Wait()
	stderr := stderrBuf.String()
	if stderr != "" {
		slog.Debug(fmt.Sprintf("[claude] stderr 輸出:\n%s", stderr))
	}
	duration := time.Since(start)
	if waitErr != nil {
		slog.Error("[claude] run 結束", "session_id", sessionID, "duration", duration, "lines", lineCount, "ok", false, "err", waitErr)
		if ctx.Err() == nil {
			return agent.NewExitError("claude", stderr, waitErr)
		}
	} else {
		slog.Info("[claude] run 結束", "session_id", sessionID, "duration", duration, "lines", lineCount, "ok", true)
	}
	return waitErr
}

// streamState 追蹤單次 Run 的串流進度（同一則 assistant 可能先 delta 再送完整 assistant，後者需略過）。
type streamState struct {
	streamStartSent bool
	// gotStreamTextDelta 表示「本輪」已透過 content_block_delta 送出至少一則文字（與下方 assistant 全文重複）
	gotStreamTextDelta bool
	// emittedAnyText 表示整次 Run 是否已送出任何可見回覆文字（供 result 後備）
	emittedAnyText bool
	// thinkingBuf 累積 thinking_delta，每次送出 EventThinking 時語意為「當前完整思考快照」（與 Gemini runner 一致）
	thinkingBuf strings.Builder
	toolNames   map[string]string
	// blockedByRule 記錄被 permissions.deny 規則擋下的 tool_use_id（來自 user 事件的 tool_result_meta），result 時對應回 denial。
	blockedByRule map[string]bool
}

func (st *streamState) emitDelta(cb agent.EventCallback, text string) {
	if text == "" {
		return
	}
	if !st.streamStartSent {
		st.streamStartSent = true
		cb(agent.Event{Type: agent.EventStreamStart})
	}
	st.emittedAnyText = true
	cb(agent.Event{Type: agent.EventDelta, Text: text})
}

// dispatch 將 Claude 專屬 StreamEvent 轉換為 agent.Event 送給 cb。
func (r *Runner) dispatch(e *StreamEvent, cb agent.EventCallback, st *streamState) {
	switch e.Type {
	case "system":
		if e.Subtype == "init" {
			ev := agent.Event{Type: agent.EventSessionInit, SessionID: e.SessionID}
			if m := strings.TrimSpace(e.Model); m != "" {
				ev.Model = model.AgentSnapshot(model.InfoFromStream(agent.TypeClaude, m))
			}
			if ev.SessionID != "" || ev.Model != nil {
				cb(ev)
			}
		}

	case "user":
		// tool_result 等以 user 事件回來；下一則 assistant 是新一輪，不可沿用上一輪的 gotStreamTextDelta。
		st.gotStreamTextDelta = false
		if e.Message != nil {
			for _, block := range e.Message.Content {
				if block.Type != "tool_result" {
					continue
				}
				label := "工具已結束："
				if block.IsError {
					label = "工具失敗："
				}
				cb(agent.Event{Type: agent.EventActivity, Text: label + agent.ToolActivity(st.toolNames[block.ToolUseID], nil)})
				delete(st.toolNames, block.ToolUseID)
			}
		}
		for _, m := range e.ToolResultMeta {
			if m.NonExecutionKind == NonExecPermissionRule && m.ID != "" {
				if st.blockedByRule == nil {
					st.blockedByRule = map[string]bool{}
				}
				st.blockedByRule[m.ID] = true
			}
		}
		// tool_result 裡若帶圖片（如 MCP 截圖工具），落地存檔後以 markdown image 語法併入既有 delta
		// 管線，前端 marked.js 會直接渲染成 <img>，不需另開事件型別或 WS 訊息格式。
		for _, img := range e.Images() {
			url, err := media.SaveBase64Image(img.MediaType, img.Data)
			if err != nil {
				slog.Info(fmt.Sprintf("[claude] 圖片存檔失敗: %v", err))
				continue
			}
			st.emitDelta(cb, fmt.Sprintf("\n![screenshot](%s)\n", url))
		}

	case "stream_event":
		if e.Event == nil {
			return
		}
		switch e.Event.Type {
		case "content_block_start":
			if e.Event.ContentBlock != nil {
				switch e.Event.ContentBlock.Type {
				case "thinking":
					// 每個 thinking block 開始時重置，避免連續兩輪 thinking 跨塊累積。
					st.thinkingBuf.Reset()
					cb(agent.Event{Type: agent.EventThinking, Text: "思考中…"})
				case "text":
					st.thinkingBuf.Reset()
					if !st.streamStartSent {
						st.streamStartSent = true
						cb(agent.Event{Type: agent.EventStreamStart})
					}
				case "tool_use":
					// 從逐行 stream-json 萃取語意事件：現在在呼叫哪個工具。
					// Info 級、預設就看得到，不用開 Debug 也能確認「還在跑、跑到哪」。
					slog.Info(fmt.Sprintf("[claude] 呼叫工具: %s", e.Event.ContentBlock.Name))
					cb(agent.Event{Type: agent.EventActivity, Text: "準備工具：" + agent.ToolActivity(e.Event.ContentBlock.Name, nil)})
				}
			}
		case "content_block_delta":
			if e.Event.Delta == nil {
				break
			}
			switch e.Event.Delta.Type {
			case "thinking_delta":
				if e.Event.Delta.Text == "" {
					break
				}
				// 限制 thinkingBuf 上限（512 KB），防止超長 thinking 造成記憶體壓力。
				if st.thinkingBuf.Len() < 512*1024 {
					st.thinkingBuf.WriteString(e.Event.Delta.Text)
				}
				cb(agent.Event{Type: agent.EventThinking, Text: st.thinkingBuf.String()})
			case "text_delta":
				if e.Event.Delta.Text == "" {
					break
				}
				st.gotStreamTextDelta = true
				st.emitDelta(cb, e.Event.Delta.Text)
			}
		}

	case "assistant":
		// 文字先送、工具活動後送；避免同一訊息的文字把目前工具提示清掉。
		if !st.gotStreamTextDelta {
			st.emitDelta(cb, e.TextContent())
		}
		if e.Message != nil {
			for _, block := range e.Message.Content {
				if block.Type != "tool_use" {
					continue
				}
				if st.toolNames == nil {
					st.toolNames = make(map[string]string)
				}
				st.toolNames[block.ID] = block.Name
				cb(agent.Event{Type: agent.EventActivity, Text: "執行工具：" + agent.ToolActivity(block.Name, block.Input)})
			}
		}

	case "result":
		if len(e.PermissionDenials) > 0 {
			denials := make([]agent.PermissionDenial, 0, len(e.PermissionDenials))
			for _, d := range e.PermissionDenials {
				denials = append(denials, agent.PermissionDenial{
					ToolName:      d.ToolName,
					ToolUseID:     d.ToolUseID,
					ToolInput:     d.ToolInput,
					BlockedByRule: st.blockedByRule[d.ToolUseID],
				})
			}
			cb(agent.Event{Type: agent.EventPermDenied, Denials: denials, SessionID: e.SessionID})
		}
		resultText := strings.TrimSpace(e.Result)
		if e.IsError {
			msg := resultText
			if msg == "" {
				msg = "claude reported error"
			}
			cb(agent.Event{Type: agent.EventError, Err: fmt.Errorf("claude: %s", msg), SessionID: e.SessionID})
		}
		// 多輪略過 assistant、或僅有 result 欄位時，避免前端／DB 空白。
		if resultText != "" && !st.emittedAnyText {
			st.emitDelta(cb, resultText)
		}
		cb(agent.Event{
			Type:       agent.EventDone,
			SessionID:  e.SessionID,
			ResultText: resultText,
		})
	}
}

// truncate 截斷過長字串，避免 log 爆炸
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…(truncated)"
}
