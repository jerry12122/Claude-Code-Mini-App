package antigravity

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/proc"
)

func init() {
	agent.Register(agent.TypeAntigravity, func() agent.Runner {
		return &Runner{}
	})
}

// Runner 是 Antigravity CLI（agy）的 agent.Runner 實作。
//
// Headless 整合注意：agy --print 在非 TTY stdout（Go pipe）下可能無輸出（GitHub #76）。
// 本 runner 先嘗試 stream-json（若 CLI 支援），否則走 print 文字模式並在空 stdout 時回報明確錯誤。
// 部署前請執行 poc/antigravity-cli/run_all_poc.ps1 驗證環境。
type Runner struct{}

// Name 實作 agent.Runner。
func (r *Runner) Name() string { return agent.TypeAntigravity }

// Run 實作 agent.Runner。
func (r *Runner) Run(ctx context.Context, opts agent.RunOptions, cb agent.EventCallback) error {
	useStreamJSON := os.Getenv("CC_AGY_STREAM_JSON") == "1"
	if useStreamJSON {
		return r.runStreamJSON(ctx, opts, cb)
	}
	return r.runPrintText(ctx, opts, cb)
}

func (r *Runner) runStreamJSON(ctx context.Context, opts agent.RunOptions, cb agent.EventCallback) error {
	args := []string{
		"--output-format", "stream-json",
	}
	r.appendCommonArgs(&args, opts)

	log.Printf("[antigravity] stream-json: agy %s (prompt len=%d via stdin)", strings.Join(args, " "), len(opts.Prompt))
	return r.execAndParseNDJSON(ctx, opts, args, cb)
}

func (r *Runner) runPrintText(ctx context.Context, opts agent.RunOptions, cb agent.EventCallback) error {
	args := []string{"--print"}
	r.appendCommonArgs(&args, opts)

	log.Printf("[antigravity] print: agy %s (prompt len=%d via stdin)", strings.Join(args, " "), len(opts.Prompt))
	if opts.WorkDir != "" {
		log.Printf("[antigravity] 工作目錄: %s", opts.WorkDir)
	}

	cmd := exec.CommandContext(ctx, agyBinary(), args...)
	cmd.Stdin = strings.NewReader(opts.Prompt)
	if opts.WorkDir != "" {
		cmd.Dir = opts.WorkDir
	}
	configureCmd(cmd)

	var stderrBuf bytes.Buffer
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		log.Printf("[antigravity] 子進程啟動失敗: %v", err)
		return err
	}
	log.Printf("[antigravity] 子進程已啟動，PID=%d", cmd.Process.Pid)

	body, readErr := io.ReadAll(stdout)
	if readErr != nil {
		log.Printf("[antigravity] 讀 stdout 失敗: %v", readErr)
	}

	waitErr := cmd.Wait()
	stderr := stderrBuf.String()
	if stderr != "" {
		log.Printf("[antigravity] stderr:\n%s", stderr)
	}

	text := strings.TrimSpace(string(body))
	if text == "" && waitErr == nil && ctx.Err() == nil {
		cb(agent.Event{
			Type: agent.EventError,
			Err: &headlessError{
				stderr: stderr,
				hint:   "agy --print 在非 TTY（子進程 pipe）可能無 stdout；請執行 poc/antigravity-cli/probe_headless.ps1，或追蹤 google-antigravity/antigravity-cli#76",
			},
			SessionID: opts.SessionID,
		})
		return nil
	}

	if text != "" {
		cb(agent.Event{Type: agent.EventStreamStart})
		cb(agent.Event{Type: agent.EventDelta, Text: text})
		cb(agent.Event{Type: agent.EventDone, SessionID: opts.SessionID, ResultText: text})
	}

	if waitErr != nil && ctx.Err() == nil {
		cb(agent.Event{
			Type:      agent.EventError,
			Err:       &runnerError{stderr: stderr, waitErr: waitErr},
			SessionID: opts.SessionID,
		})
	}
	return waitErr
}

func (r *Runner) execAndParseNDJSON(ctx context.Context, opts agent.RunOptions, args []string, cb agent.EventCallback) error {
	cmd := exec.CommandContext(ctx, agyBinary(), args...)
	cmd.Stdin = strings.NewReader(opts.Prompt)
	if opts.WorkDir != "" {
		cmd.Dir = opts.WorkDir
	}
	configureCmd(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return err
	}

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
		e, parseErr := ParseEvent(line)
		if parseErr != nil {
			log.Printf("[antigravity] 解析失敗: %v", parseErr)
			continue
		}
		if e.Type == "init" && e.SessionID != "" {
			sessionID = e.SessionID
		}
		if e.Type == "result" {
			sawResult = true
		}
		r.dispatch(e, cb, &streamStartSent, sessionID)
	}

	waitErr := cmd.Wait()
	stderr := stderrBuf.String()
	if waitErr != nil && ctx.Err() == nil && !sawResult {
		cb(agent.Event{Type: agent.EventError, Err: &runnerError{stderr: stderr, waitErr: waitErr}, SessionID: sessionID})
	}
	return waitErr
}

func (r *Runner) appendCommonArgs(args *[]string, opts agent.RunOptions) {
	if opts.SessionID != "" {
		*args = append(*args, "--conversation", opts.SessionID)
	}
	if opts.ExtraArgs != nil {
		if m := strings.TrimSpace(opts.ExtraArgs[agent.ArgModel]); m != "" {
			*args = append(*args, "--model", m)
		}
	}
	if mapSkipPermissions(opts.ExtraArgs) {
		*args = append(*args, "--dangerously-skip-permissions")
	}
}

func configureCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = proc.SysProcAttr()
	cmd.WaitDelay = 5 * time.Second
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return proc.GracefulStop(cmd.Process.Pid, 3*time.Second)
		}
		return nil
	}
}

func agyBinary() string {
	if b := strings.TrimSpace(os.Getenv("CC_AGY_BIN")); b != "" {
		return b
	}
	return "agy"
}

func (r *Runner) dispatch(e *StreamEvent, cb agent.EventCallback, streamStartSent *bool, sessionID string) {
	switch e.Type {
	case "init":
		if e.SessionID != "" {
			cb(agent.Event{Type: agent.EventSessionInit, SessionID: e.SessionID})
		}
	case "message":
		if !e.IsAssistantDelta() || e.Content == "" {
			return
		}
		if e.Thought != nil && *e.Thought {
			cb(agent.Event{Type: agent.EventThinking, Text: e.Content})
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
			Tool: &agent.ToolCall{CallID: e.ToolID, Name: e.ToolName, Arguments: e.Parameters},
		})
	case "tool_result":
		tc := &agent.ToolCall{CallID: e.ToolID, OK: e.Status == "success", Output: e.OutputText()}
		if e.Error != nil {
			tc.ErrMessage = e.Error.Message
		}
		cb(agent.Event{Type: agent.EventToolCompleted, Tool: tc})
	case "error":
		log.Printf("[antigravity] error event severity=%s message=%s", e.Severity, e.Message)
	case "result":
		cb(agent.Event{Type: agent.EventDone, SessionID: sessionID})
	}
}

// mapSkipPermissions 對應 agy --dangerously-skip-permissions（無互動式 allow_once）。
func mapSkipPermissions(extra map[string]string) bool {
	if extra == nil {
		return false
	}
	switch strings.TrimSpace(extra[agent.ArgPermissionMode]) {
	case "yolo", "bypassPermissions":
		return true
	default:
		return false
	}
}

type runnerError struct {
	stderr  string
	waitErr error
}

func (e *runnerError) Error() string {
	if e.stderr != "" {
		return "antigravity failed: " + strings.TrimSpace(e.stderr)
	}
	return "antigravity failed: " + e.waitErr.Error()
}

type headlessError struct {
	stderr string
	hint   string
}

func (e *headlessError) Error() string {
	msg := e.hint
	if e.stderr != "" {
		msg += " | stderr: " + strings.TrimSpace(e.stderr)
	}
	return msg
}
