package claude

import (
	"bufio"
	"bytes"
	"context"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/proc"
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
	}
	return args
}

// Run 實作 agent.Runner：啟動 claude -p 子進程，逐行解析 stream-json 並透過 cb 回傳事件。
func (r *Runner) Run(ctx context.Context, opts agent.RunOptions, cb agent.EventCallback) error {
	args := buildClaudeArgs(opts)

	log.Printf("[claude] 執行指令: claude %s (prompt len=%d)", strings.Join(args, " "), len(opts.Prompt))
	if opts.WorkDir != "" {
		log.Printf("[claude] 工作目錄: %s", opts.WorkDir)
	}

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Stdin = strings.NewReader(opts.Prompt)
	if opts.WorkDir != "" {
		cmd.Dir = opts.WorkDir
	}
	cmd.SysProcAttr = proc.SysProcAttr()
	cmd.WaitDelay = 5 * time.Second
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return proc.GracefulStop(cmd.Process.Pid, 3*time.Second)
		}
		return nil
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

	var st streamState
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

		r.dispatch(e, cb, &st)
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

// streamState 追蹤單次 Run 的串流進度（同一則 assistant 可能先 delta 再送完整 assistant，後者需略過）。
type streamState struct {
	streamStartSent bool
	// gotStreamTextDelta 表示已透過 content_block_delta 送出至少一則文字（與下方 assistant 全文重複）
	gotStreamTextDelta bool
	// thinkingBuf 累積 thinking_delta，每次送出 EventThinking 時語意為「當前完整思考快照」（與 Gemini runner 一致）
	thinkingBuf strings.Builder
}

// dispatch 將 Claude 專屬 StreamEvent 轉換為 agent.Event 送給 cb。
func (r *Runner) dispatch(e *StreamEvent, cb agent.EventCallback, st *streamState) {
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
			if e.Event.ContentBlock != nil {
				switch e.Event.ContentBlock.Type {
				case "thinking":
					// 每個 thinking block 開始時重置，避免連續兩輪 thinking 跨塊累積。
					st.thinkingBuf.Reset()
				case "text":
					st.thinkingBuf.Reset()
					if !st.streamStartSent {
						st.streamStartSent = true
						cb(agent.Event{Type: agent.EventStreamStart})
					}
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
				if !st.streamStartSent {
					st.streamStartSent = true
					cb(agent.Event{Type: agent.EventStreamStart})
				}
				st.gotStreamTextDelta = true
				cb(agent.Event{Type: agent.EventDelta, Text: e.Event.Delta.Text})
			}
		}

	case "assistant":
		// 已透過 content_block_delta 累積內容時，勿再送完整 assistant（與 delta 全文重複，中間易夾大量換行）。
		// 僅有 content_block_start、尚無任何 text_delta 時仍須採用 assistant 全文。
		if st.gotStreamTextDelta {
			break
		}
		text := e.TextContent()
		if text != "" {
			st.streamStartSent = true
			cb(agent.Event{Type: agent.EventStreamStart})
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
		cb(agent.Event{
			Type:       agent.EventDone,
			SessionID:  e.SessionID,
			ResultText: strings.TrimSpace(e.Result),
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
