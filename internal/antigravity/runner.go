package antigravity

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
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
type Runner struct{}

// Name 實作 agent.Runner。
func (r *Runner) Name() string { return agent.TypeAntigravity }

// Run 實作 agent.Runner。預設使用 stream-json 模式。
func (r *Runner) Run(ctx context.Context, opts agent.RunOptions, cb agent.EventCallback) error {
	if os.Getenv("CC_AGY_FORCE_PRINT_TEXT") == "1" {
		return r.runPrintText(ctx, opts, cb)
	}
	return r.runStreamJSON(ctx, opts, cb)
}

func (r *Runner) runStreamJSON(ctx context.Context, opts agent.RunOptions, cb agent.EventCallback) error {
	args := []string{
		"--output-format", "stream-json",
	}
	r.appendCommonArgs(&args, opts)
	slog.Info(fmt.Sprintf("[antigravity] stream-json: agy %s (prompt len=%d via stdin)", strings.Join(args, " "), len(opts.Prompt)))
	return r.execAndParseNDJSON(ctx, opts, args, cb)
}

func (r *Runner) runPrintText(ctx context.Context, opts agent.RunOptions, cb agent.EventCallback) error {
	args := []string{"--print"}
	r.appendCommonArgs(&args, opts)
	slog.Info(fmt.Sprintf("[antigravity] print: agy %s (prompt len=%d via stdin)", strings.Join(args, " "), len(opts.Prompt)))
	if opts.WorkDir != "" {
		slog.Info(fmt.Sprintf("[antigravity] 工作目錄: %s", opts.WorkDir))
	}

	cmd := exec.CommandContext(ctx, Binary(), args...)
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
		slog.Info(fmt.Sprintf("[antigravity] 子進程啟動失敗: %v", err))
		return err
	}
	slog.Info(fmt.Sprintf("[antigravity] 子進程已啟動，PID=%d", cmd.Process.Pid))
	if opts.OnStart != nil {
		opts.OnStart(cmd.Process.Pid)
	}

	body, readErr := io.ReadAll(stdout)
	if readErr != nil {
		slog.Info(fmt.Sprintf("[antigravity] 讀 stdout 失敗: %v", readErr))
	}

	waitErr := cmd.Wait()
	stderr := stderrBuf.String()
	if stderr != "" {
		slog.Info(fmt.Sprintf("[antigravity] stderr:\n%s", stderr))
	}

	text := strings.TrimSpace(string(body))
	if text == "" && waitErr == nil && ctx.Err() == nil {
		cb(agent.Event{
			Type:      agent.EventError,
			Err:       errors.New(strings.TrimSpace("antigravity CLI 沒有回傳內容\n" + stderr)),
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

type dispatchState struct {
	streamStartSent bool
	sawResult       bool
	sessionID       string
	accumulatedText string
}

func (r *Runner) execAndParseNDJSON(ctx context.Context, opts agent.RunOptions, args []string, cb agent.EventCallback) error {
	cmd := exec.CommandContext(ctx, Binary(), args...)
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
	if opts.OnStart != nil {
		opts.OnStart(cmd.Process.Pid)
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1024*1024), 2*1024*1024)

	st := dispatchState{
		sessionID: opts.SessionID,
	}

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		e, parseErr := ParseEvent(line)
		if parseErr != nil {
			slog.Debug(fmt.Sprintf("[antigravity] 解析失敗或非 JSON: %v", parseErr))
			continue
		}
		r.dispatch(e, cb, &st)
	}

	waitErr := cmd.Wait()
	stderr := stderrBuf.String()
	if waitErr != nil && ctx.Err() == nil && !st.sawResult {
		cb(agent.Event{
			Type:      agent.EventError,
			Err:       &runnerError{stderr: stderr, waitErr: waitErr},
			SessionID: st.sessionID,
		})
	}
	return waitErr
}

func (r *Runner) appendCommonArgs(args *[]string, opts agent.RunOptions) {
	if opts.SessionID != "" {
		*args = append(*args, "--conversation", opts.SessionID)
	}
	if opts.WorkDir != "" {
		*args = append(*args, "--add-dir", opts.WorkDir)
	}
	if opts.ExtraArgs != nil {
		if m := strings.TrimSpace(opts.ExtraArgs[agent.ArgModel]); m != "" {
			*args = append(*args, "--model", m)
		}
		if ef := strings.TrimSpace(opts.ExtraArgs[agent.ArgEffort]); ef != "" {
			*args = append(*args, "--effort", ef)
		}
	}
	mapPermissionArgs(args, opts.ExtraArgs)
}

func (r *Runner) dispatch(e *StreamTopEvent, cb agent.EventCallback, st *dispatchState) {
	if e == nil {
		return
	}

	switch e.Event {
	case "init":
		if e.ConversationID != "" {
			st.sessionID = e.ConversationID
			cb(agent.Event{Type: agent.EventSessionInit, SessionID: e.ConversationID})
		}
	case "step_update":
		if e.StepUpdate == nil {
			return
		}
		su := e.StepUpdate
		if su.ConversationID != "" && st.sessionID == "" {
			st.sessionID = su.ConversationID
		}

		switch su.StepType {
		case "agent_response":
			if su.TextDelta != "" {
				if !st.streamStartSent {
					st.streamStartSent = true
					cb(agent.Event{Type: agent.EventStreamStart})
				}
				st.accumulatedText += su.TextDelta
				cb(agent.Event{Type: agent.EventDelta, Text: su.TextDelta})
			}
		case "tool":
			toolName := su.ToolName
			if toolName == "" && su.ToolInfo != nil {
				toolName = su.ToolInfo.Name
			}

			if su.State == "ACTIVE" {
				var params json.RawMessage
				if su.ToolInfo != nil {
					params = su.ToolInfo.Parameters
				}
				cb(agent.Event{
					Type: agent.EventActivity,
					Text: "執行工具：" + agent.ToolActivity(toolName, params),
				})
				cb(agent.Event{
					Type: agent.EventToolStarted,
					Tool: &agent.ToolCall{
						CallID:    fmt.Sprintf("step-%d", su.StepIndex),
						Name:      toolName,
						Arguments: params,
					},
				})
			} else if su.State == "DONE" || su.State == "ERROR" {
				var output string
				var errMsg string
				if su.ToolInfo != nil {
					output = su.ToolInfo.OutputText()
					if su.ToolInfo.Error != nil {
						errMsg = su.ToolInfo.Error.Message
					}
				}
				label := "工具已完成：" + agent.ToolActivity(toolName, nil)
				if su.State == "ERROR" {
					label = "工具失敗：" + agent.ToolActivity(toolName, nil)
					if errMsg != "" {
						label += "：" + errMsg
					}
				}
				cb(agent.Event{Type: agent.EventActivity, Text: label})
				cb(agent.Event{
					Type: agent.EventToolCompleted,
					Tool: &agent.ToolCall{
						CallID:     fmt.Sprintf("step-%d", su.StepIndex),
						Name:       toolName,
						Output:     output,
						OK:         su.State == "DONE",
						ErrMessage: errMsg,
					},
				})
			}
		}
	case "result":
		st.sawResult = true
		res := e.Result
		if res == nil {
			cb(agent.Event{Type: agent.EventDone, SessionID: st.sessionID})
			return
		}

		if res.ConversationID != "" {
			st.sessionID = res.ConversationID
		}

		if res.Status == "ERROR" {
			errMsg := res.Error
			if errMsg == "" {
				errMsg = "antigravity execution failed"
			}
			cb(agent.Event{
				Type:      agent.EventError,
				SessionID: st.sessionID,
				Err:       errors.New(errMsg),
			})
			return
		}

		if len(res.DeniedActions) > 0 {
			var actions []string
			for _, a := range res.DeniedActions {
				actions = append(actions, a.DisplayName)
			}
			slog.Info(fmt.Sprintf("[antigravity] 部分操作受權限限制已略過: %s", strings.Join(actions, ", ")))
		}

		resultText := res.Response
		if resultText == "" {
			resultText = st.accumulatedText
		}
		cb(agent.Event{
			Type:       agent.EventDone,
			SessionID:  st.sessionID,
			ResultText: resultText,
		})
	}
}

// mapPermissionArgs 完整映射 4 種 permission_mode 至 agy 旗標。
func mapPermissionArgs(args *[]string, extra map[string]string) {
	if extra == nil {
		return
	}
	pm := strings.TrimSpace(extra[agent.ArgPermissionMode])
	switch pm {
	case "yolo", "bypassPermissions":
		*args = append(*args, "--dangerously-skip-permissions")
	case "acceptEdits":
		*args = append(*args, "--mode", "accept-edits")
	case "plan":
		*args = append(*args, "--mode", "plan")
	}
}

func configureCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = proc.SysProcAttr()
	cmd.WaitDelay = 5 * time.Second
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return proc.KillTree(cmd.Process.Pid)
		}
		return nil
	}
}

// Binary 回傳要執行的 agy。CC_AGY_BIN 優先（路徑或名稱皆可），否則用 PATH 上的名稱 agy。
func Binary() string {
	if b := strings.TrimSpace(os.Getenv("CC_AGY_BIN")); b != "" {
		return b
	}
	return "agy"
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
