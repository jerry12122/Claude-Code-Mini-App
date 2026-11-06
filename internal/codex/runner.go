package codex

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"

	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/proc"
	"log/slog"
)

func init() {
	agent.Register(agent.TypeCodex, func() agent.Runner {
		return &Runner{}
	})
}

// Runner 是 codex exec --json 的 agent.Runner 實作。
// 參考：docs/spec/codex-cli.md
type Runner struct{}

func (r *Runner) Name() string { return agent.TypeCodex }

func (r *Runner) Run(ctx context.Context, opts agent.RunOptions, cb agent.EventCallback) error {
	start := time.Now()
	codexBin, err := ResolveBin()
	if err != nil {
		cb(agent.Event{Type: agent.EventError, Err: err})
		return err
	}
	if !HasAuthConfig() {
		slog.Info(fmt.Sprintf("[codex] 警告：未找到 CODEX_API_KEY 或 ~/.codex/auth.json，仍嘗試執行 exec"))
	}

	args := buildArgs(opts)
	slog.Info(fmt.Sprintf("[codex] 執行指令: codex %s (prompt len=%d)", strings.Join(redactArgs(args), " "), len(opts.Prompt)))
	if opts.WorkDir != "" {
		slog.Info(fmt.Sprintf("[codex] 工作目錄: %s", opts.WorkDir))
	}

	cmd := exec.CommandContext(ctx, codexBin, args...)
	if opts.SessionID == "" && opts.WorkDir != "" {
		cmd.Dir = opts.WorkDir
	} else if opts.WorkDir != "" {
		cmd.Dir = opts.WorkDir
	}
	cmd.Env = codexEnv()
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
		return err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		slog.Error(fmt.Sprintf("[codex] 子進程啟動失敗: %v", err))
		return err
	}
	_ = stdin.Close()
	slog.Info(fmt.Sprintf("[codex] 子進程已啟動，PID=%d", cmd.Process.Pid))
	if opts.OnStart != nil {
		opts.OnStart(cmd.Process.Pid)
	}

	var st dispatchState
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	lineCount := 0

	for scanner.Scan() {
		raw := scanner.Bytes()
		if len(raw) == 0 {
			continue
		}
		lineCount++
		slog.Debug(fmt.Sprintf("[codex] 收到第 %d 行: %s", lineCount, truncate(string(raw), 200)))
		ev, parseErr := ParseEvent(raw)
		if parseErr != nil {
			slog.Warn(fmt.Sprintf("[codex] 解析失敗: %v", parseErr))
			continue
		}
		r.dispatch(ev, cb, &st)
	}
	if err := scanner.Err(); err != nil {
		slog.Error(fmt.Sprintf("[codex] scanner 錯誤: %v", err))
	}

	waitErr := cmd.Wait()
	if stderr := stderrBuf.String(); stderr != "" {
		slog.Debug(fmt.Sprintf("[codex] stderr:\n%s", truncate(stderr, 500)))
	}

	sessionID := opts.SessionID
	if st.sessionID != "" {
		sessionID = st.sessionID
	}
	duration := time.Since(start)

	if ctx.Err() == nil {
		st.finish(cb, waitErr, stderrBuf.String())
	}
	if waitErr != nil && ctx.Err() == nil {
		slog.Error("[codex] run 結束", "session_id", sessionID, "duration", duration, "lines", lineCount, "ok", false, "err", waitErr)
		return waitErr
	}
	slog.Info("[codex] run 結束", "session_id", sessionID, "duration", duration, "lines", lineCount, "ok", true)
	cb(agent.Event{Type: agent.EventDone, SessionID: sessionID})
	return waitErr
}

type dispatchState struct {
	sawTurnCompleted bool
	sessionID        string
	agentMsgs        int
	lastErr          string // 最近一次 error 事件；可能是可恢復的，未必代表回合失敗
	errSent          bool
}

// emitError 保證同一回合只回報一次錯誤（codex 失敗時會連發 error + turn.failed）。
func (st *dispatchState) emitError(cb agent.EventCallback, err error) {
	if st.errSent {
		return
	}
	st.errSent = true
	cb(agent.Event{Type: agent.EventError, Err: err})
}

// finish 在串流結束後決定是否補報錯誤：未完成回合時，優先用 stream 內的錯誤訊息，其次 stderr。
func (st *dispatchState) finish(cb agent.EventCallback, waitErr error, stderr string) {
	if st.sawTurnCompleted {
		return
	}
	switch {
	case st.lastErr != "":
		st.emitError(cb, errors.New(st.lastErr))
	case waitErr != nil:
		st.emitError(cb, classifyRunnerError(stderr, waitErr))
	default:
		st.emitError(cb, errors.New("codex 串流中斷：未收到 turn.completed"))
	}
}

func (r *Runner) dispatch(ev *StreamEvent, cb agent.EventCallback, st *dispatchState) {
	switch ev.Type {
	case "thread.started":
		if ev.ThreadID != "" {
			st.sessionID = ev.ThreadID
			cb(agent.Event{Type: agent.EventSessionInit, SessionID: ev.ThreadID})
		}
	case "item.started", "item.updated", "item.completed":
		if ev.Item != nil {
			if ev.Item.Type == "reasoning" {
				text := ev.Item.Text
				if text == "" {
					text = "思考中…"
				}
				cb(agent.Event{Type: agent.EventThinking, Text: text})
			}
			if label := ev.Item.ActivityText(); label != "" {
				if ev.Type == "item.completed" && ev.Item.Type != "todo_list" {
					prefix := "已完成："
					if ev.Item.Status == "failed" || (ev.Item.ExitCode != nil && *ev.Item.ExitCode != 0) {
						prefix = "執行失敗："
					}
					label = prefix + label
				}
				cb(agent.Event{Type: agent.EventActivity, Text: label})
			}
		}
		if ev.Type != "item.completed" {
			break
		}
		if ev.Item != nil && ev.Item.Type == "error" {
			// 非致命警告（例如 model metadata 找不到），不中斷回合，只留 log 供排查。
			slog.Warn("[codex] warning item", "message", ev.Item.Message)
		}
		if text := AgentMessageText(ev.Item); text != "" {
			// 工具呼叫前後會各有一段 agent_message，不分段會黏成一句。
			if st.agentMsgs > 0 {
				text = "\n\n" + text
			}
			st.agentMsgs++
			cb(agent.Event{Type: agent.EventDelta, Text: text})
		}
	case "turn.completed":
		st.sawTurnCompleted = true
	case "error":
		st.lastErr = ErrorMessage(ev)
	case "turn.failed":
		st.emitError(cb, errors.New(ErrorMessage(ev)))
	}
}

func buildArgs(opts agent.RunOptions) []string {
	if opts.SessionID != "" {
		args := []string{
			"exec", "resume", opts.SessionID,
			"--json", "--skip-git-repo-check",
			"--dangerously-bypass-approvals-and-sandbox",
		}
		if opts.ExtraArgs != nil {
			if m := strings.TrimSpace(opts.ExtraArgs[agent.ArgModel]); m != "" {
				args = append(args, "-m", m)
			}
			if effort := strings.TrimSpace(opts.ExtraArgs[agent.ArgEffort]); effort != "" {
				args = append(args, "-c", "model_reasoning_effort="+effort)
			}
		}
		args = append(args, opts.Prompt)
		return args
	}

	args := []string{
		"exec", "--json", "--skip-git-repo-check",
		"--dangerously-bypass-approvals-and-sandbox",
		"-C", opts.WorkDir,
	}
	if opts.ExtraArgs != nil {
		if m := strings.TrimSpace(opts.ExtraArgs[agent.ArgModel]); m != "" {
			args = append(args, "-m", m)
		}
		if effort := strings.TrimSpace(opts.ExtraArgs[agent.ArgEffort]); effort != "" {
			args = append(args, "-c", "model_reasoning_effort="+effort)
		}
	}
	args = append(args, opts.Prompt)
	return args
}

func codexEnv() []string {
	out := make([]string, 0, len(os.Environ()))
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "TERM=") {
			continue
		}
		out = append(out, e)
	}
	return out
}

// stdinNotice：即使 stdin 已關閉，0.159.x 仍固定印在 stderr，不是錯誤內容。
const stdinNotice = "Reading additional input from stdin..."

func classifyRunnerError(stderr string, waitErr error) error {
	s := strings.TrimSpace(strings.ReplaceAll(stderr, stdinNotice, ""))
	if strings.Contains(strings.ToLower(s), "unauthorized") ||
		strings.Contains(strings.ToLower(s), "not logged in") ||
		strings.Contains(strings.ToLower(s), "authentication") {
		return fmt.Errorf("codex 認證失敗：請在本機執行 codex login，或設定 CODEX_API_KEY / CODEX_BIN。原始訊息：%s", s)
	}
	if s != "" {
		return fmt.Errorf("codex failed: %s", s)
	}
	return waitErr
}

func redactArgs(args []string) []string {
	out := make([]string, len(args))
	copy(out, args)
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
