package gemini

import (
	"bufio"
	"bytes"
	"context"
	"log"
	"os/exec"
	"strings"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
)

func init() {
	agent.Register(agent.TypeGemini, func() agent.Runner {
		return &Runner{}
	})
}

// Runner 是 gemini CLI 的 agent.Runner 實作。
//
// 採用官方 stream-json 格式。
// 參考文件：docs/gemini-cli.md
type Runner struct{}

// Name 實作 agent.Runner。
func (r *Runner) Name() string { return agent.TypeGemini }

// Run 實作 agent.Runner：啟動 gemini 子進程並串流事件。
//
// 指令格式：
//
//	gemini -p <prompt> --output-format stream-json \
//	  [--resume <session_id>] [-m <model>] [--approval-mode <mode>]
//
// prompt 透過 `-p/--prompt` 旗標傳入（不是 positional），因為 positional 在 TTY 下
// 會走互動模式。
func (r *Runner) Run(ctx context.Context, opts agent.RunOptions, cb agent.EventCallback) error {
	args := []string{
		"-p", opts.Prompt,
		"--output-format", "stream-json",
	}

	if opts.SessionID != "" {
		args = append(args, "--resume", opts.SessionID)
	}

	if opts.ExtraArgs != nil {
		if m := strings.TrimSpace(opts.ExtraArgs[agent.ArgModel]); m != "" {
			args = append(args, "-m", m)
		}
	}

	// Gemini 接受：default | auto_edit | yolo | plan
	// 同時接受 Claude 風格值並做 mapping，方便與前端現有 permission_mode 共用同一個 DB 欄位。
	approval := "default"
	if opts.ExtraArgs != nil {
		approval = mapApprovalMode(opts.ExtraArgs[agent.ArgPermissionMode])
	}
	args = append(args, "--approval-mode", approval)

	log.Printf("[gemini] 執行指令: gemini %s", strings.Join(args, " "))
	if opts.WorkDir != "" {
		log.Printf("[gemini] 工作目錄: %s", opts.WorkDir)
	}

	cmd := exec.CommandContext(ctx, "gemini", args...)
	if opts.WorkDir != "" {
		cmd.Dir = opts.WorkDir
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("[gemini] 取得 stdout pipe 失敗: %v", err)
		return err
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		log.Printf("[gemini] 子進程啟動失敗: %v", err)
		return err
	}
	log.Printf("[gemini] 子進程已啟動，PID=%d", cmd.Process.Pid)

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	streamStartSent := false
	sawResult := false
	lineCount := 0
	sessionID := opts.SessionID

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		lineCount++
		log.Printf("[gemini] 收到第 %d 行 (len=%d): %s", lineCount, len(line), truncate(string(line), 200))

		e, parseErr := ParseEvent(line)
		if parseErr != nil {
			log.Printf("[gemini] 解析失敗: %v | 原始內容: %s", parseErr, truncate(string(line), 200))
			continue
		}
		log.Printf("[gemini] 事件 type=%s role=%s tool=%s", e.Type, e.Role, e.ToolName)

		if e.Type == "init" && e.SessionID != "" {
			sessionID = e.SessionID
			log.Printf("[gemini]   └─ session_id=%s model=%s", e.SessionID, e.Model)
		}
		if e.Type == "result" {
			sawResult = true
		}

		r.dispatch(e, cb, &streamStartSent, sessionID)
	}

	if err := scanner.Err(); err != nil {
		log.Printf("[gemini] scanner 錯誤: %v", err)
	}

	waitErr := cmd.Wait()
	stderr := stderrBuf.String()
	if stderr != "" {
		log.Printf("[gemini] stderr 輸出:\n%s", stderr)
	}

	// Gemini headless exit code：0 成功、1 一般錯誤、42 輸入錯誤、53 超過 turn 上限。
	// 失敗時可能沒有 terminal result event；被 context 取消的 run 交由上層處理為 aborted。
	if waitErr != nil {
		if ctx.Err() == nil && !sawResult {
			cb(agent.Event{Type: agent.EventError, Err: &runnerError{stderr: stderr, waitErr: waitErr}, SessionID: sessionID})
		}
		log.Printf("[gemini] 子進程結束，exit error: %v", waitErr)
	} else {
		log.Printf("[gemini] 子進程正常結束，共處理 %d 行", lineCount)
	}

	// stream-json 的 `result` 事件沒有 session_id，為了讓上層拿到 session_id
	// 能持久化，在子進程結束後補發一個 EventDone（帶 init 事件記下的 session_id）。
	// 若已有 result 事件送出 EventDone，這裡不會重複（dispatch 對 result 會送）。
	// 反之若中途異常沒收到 result，也不補 Done，交給 EventError 處理。
	return waitErr
}

// dispatch 將 gemini 專屬事件轉換為 agent.Event。
// sessionID 由呼叫端提供（優先使用 init 事件收到的值）。
func (r *Runner) dispatch(e *StreamEvent, cb agent.EventCallback, streamStartSent *bool, sessionID string) {
	switch e.Type {
	case "init":
		if e.SessionID != "" {
			cb(agent.Event{Type: agent.EventSessionInit, SessionID: e.SessionID})
		}

	case "message":
		// user 訊息與非 delta 聚合 assistant 都跳過：我們只要 streaming chunks，
		// 累積後的 assistant 文字在上層 WS handler 儲存。
		if !e.IsAssistantDelta() {
			log.Printf("[gemini]   └─ 略過非 streaming delta 的 message (role=%s delta=%v)", e.Role, e.Delta)
			return
		}
		if e.Content == "" {
			return
		}
		if !*streamStartSent {
			*streamStartSent = true
			cb(agent.Event{Type: agent.EventStreamStart})
		}
		cb(agent.Event{Type: agent.EventDelta, Text: e.Content})

	case "tool_use":
		cb(agent.Event{
			Type: agent.EventToolStarted,
			Tool: &agent.ToolCall{
				CallID:    e.ToolID,
				Name:      e.ToolName,
				Arguments: e.Parameters,
			},
		})

	case "tool_result":
		tc := &agent.ToolCall{
			CallID: e.ToolID,
			OK:     e.Status == "success",
			Output: e.OutputText(),
		}
		if e.Error != nil {
			tc.ErrMessage = e.Error.Message
		}
		cb(agent.Event{Type: agent.EventToolCompleted, Tool: tc})

	case "error":
		// Gemini 的 error 事件是非致命 warning / system error；依 severity 決定是否上報。
		// 預設僅 log，避免前端被非致命訊息汙染。嚴重錯誤仍會以 exit code != 0 呈現。
		log.Printf("[gemini] error event severity=%s message=%s", e.Severity, e.Message)

	case "result":
		// result 不帶 session_id，改由 dispatcher 傳入的 sessionID 補上。
		cb(agent.Event{Type: agent.EventDone, SessionID: sessionID})
	}
}

// mapApprovalMode 將前端傳來的 permission_mode 值，正規化為 Gemini 接受的值。
//   - "", "default" → default
//   - "auto_edit", "acceptEdits" → auto_edit
//   - "yolo", "bypassPermissions" → yolo
//   - "plan" → plan
//   - 其他未知值 → default（Gemini 會拒絕不合法值，避免直接讓 CLI fail）
func mapApprovalMode(v string) string {
	switch strings.TrimSpace(v) {
	case "", "default":
		return "default"
	case "auto_edit", "acceptEdits":
		return "auto_edit"
	case "yolo", "bypassPermissions":
		return "yolo"
	case "plan":
		return "plan"
	default:
		log.Printf("[gemini] 未知的 permission_mode=%q，退回 default", v)
		return "default"
	}
}

// runnerError 表示子進程異常結束而沒有 terminal result。
type runnerError struct {
	stderr  string
	waitErr error
}

func (e *runnerError) Error() string {
	if e.stderr != "" {
		return "gemini failed: " + strings.TrimSpace(e.stderr)
	}
	return "gemini failed: " + e.waitErr.Error()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…(truncated)"
}
