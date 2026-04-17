package claude

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
	agent.Register(agent.TypeClaude, func() agent.Runner {
		return &Runner{}
	})
}

// Runner 是 Claude Code CLI 的 agent.Runner 實作。
type Runner struct{}

// Name 實作 agent.Runner。
func (r *Runner) Name() string { return agent.TypeClaude }

// Run 實作 agent.Runner：啟動 claude -p 子進程，逐行解析 stream-json 並透過 cb 回傳事件。
func (r *Runner) Run(ctx context.Context, opts agent.RunOptions, cb agent.EventCallback) error {
	args := []string{
		"-p",
		"--output-format", "stream-json",
		"--verbose",
	}

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
	}

	log.Printf("[claude] 執行指令: claude %s (prompt len=%d)", strings.Join(args, " "), len(opts.Prompt))
	if opts.WorkDir != "" {
		log.Printf("[claude] 工作目錄: %s", opts.WorkDir)
	}

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Stdin = strings.NewReader(opts.Prompt)
	if opts.WorkDir != "" {
		cmd.Dir = opts.WorkDir
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("[claude] 取得 stdout pipe 失敗: %v", err)
		return err
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		log.Printf("[claude] 子進程啟動失敗: %v", err)
		return err
	}
	log.Printf("[claude] 子進程已啟動，PID=%d", cmd.Process.Pid)

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer

	streamStartSent := false
	lineCount := 0
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		lineCount++
		log.Printf("[claude] 收到第 %d 行 (len=%d): %s", lineCount, len(line), truncate(string(line), 200))

		e, err := ParseEvent(line)
		if err != nil {
			log.Printf("[claude] 解析失敗: %v | 原始內容: %s", err, truncate(string(line), 200))
			continue
		}
		log.Printf("[claude] 事件 type=%s subtype=%s", e.Type, e.Subtype)
		if e.Event != nil {
			log.Printf("[claude]   └─ API event type=%s", e.Event.Type)
		}
		if e.SessionID != "" {
			log.Printf("[claude]   └─ session_id=%s", e.SessionID)
		}
		if e.IsError {
			log.Printf("[claude]   └─ IS_ERROR result=%s", e.Result)
		}
		if len(e.PermissionDenials) > 0 {
			log.Printf("[claude]   └─ permission_denials=%d 項", len(e.PermissionDenials))
		}

		r.dispatch(e, cb, &streamStartSent)
	}

	if err := scanner.Err(); err != nil {
		log.Printf("[claude] scanner 錯誤: %v", err)
	}

	waitErr := cmd.Wait()
	stderr := stderrBuf.String()
	if stderr != "" {
		log.Printf("[claude] stderr 輸出:\n%s", stderr)
	}
	if waitErr != nil {
		log.Printf("[claude] 子進程結束，exit error: %v", waitErr)
	} else {
		log.Printf("[claude] 子進程正常結束，共處理 %d 行", lineCount)
	}
	return waitErr
}

// dispatch 將 Claude 專屬 StreamEvent 轉換為 agent.Event 送給 cb。
func (r *Runner) dispatch(e *StreamEvent, cb agent.EventCallback, streamStartSent *bool) {
	switch e.Type {
	case "system":
		if e.Subtype == "init" && e.SessionID != "" {
			cb(agent.Event{Type: agent.EventSessionInit, SessionID: e.SessionID})
		}

	case "stream_event":
		if e.Event == nil {
			return
		}
		switch e.Event.Type {
		case "content_block_start":
			if e.Event.ContentBlock != nil && e.Event.ContentBlock.Type == "text" && !*streamStartSent {
				*streamStartSent = true
				cb(agent.Event{Type: agent.EventStreamStart})
			}
		case "content_block_delta":
			if e.Event.Delta != nil && e.Event.Delta.Type == "text_delta" && e.Event.Delta.Text != "" {
				if !*streamStartSent {
					*streamStartSent = true
					cb(agent.Event{Type: agent.EventStreamStart})
				}
				cb(agent.Event{Type: agent.EventDelta, Text: e.Event.Delta.Text})
			}
		}

	case "assistant":
		text := e.TextContent()
		if text != "" {
			if !*streamStartSent {
				*streamStartSent = true
				cb(agent.Event{Type: agent.EventStreamStart})
			}
			cb(agent.Event{Type: agent.EventDelta, Text: text})
		}

	case "result":
		if len(e.PermissionDenials) > 0 {
			denials := make([]agent.PermissionDenial, 0, len(e.PermissionDenials))
			for _, d := range e.PermissionDenials {
				denials = append(denials, agent.PermissionDenial{
					ToolName:  d.ToolName,
					ToolUseID: d.ToolUseID,
					ToolInput: d.ToolInput,
				})
			}
			cb(agent.Event{Type: agent.EventPermDenied, Denials: denials, SessionID: e.SessionID})
		}
		cb(agent.Event{Type: agent.EventDone, SessionID: e.SessionID})
	}
}

// truncate 截斷過長字串，避免 log 爆炸
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…(truncated)"
}
