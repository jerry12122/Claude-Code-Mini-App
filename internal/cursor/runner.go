package cursor

import (
	"bufio"
	"bytes"
	"context"
	"log"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/model"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/proc"
)

func init() {
	agent.Register(agent.TypeCursor, func() agent.Runner {
		return &Runner{}
	})
}

// Runner 是 cursor-agent CLI 的 agent.Runner 實作。
//
// 採用官方 stream-json 格式 + --stream-partial-output 取得字元級 delta。
// 參考文件：docs/cursor-agent-cli.md
type Runner struct{}

// Name 實作 agent.Runner。
func (r *Runner) Name() string { return agent.TypeCursor }

// Run 實作 agent.Runner：啟動 cursor-agent 子進程並串流事件。
//
// 指令格式：
//
//	cursor-agent --print --output-format stream-json --stream-partial-output \
//	  [--resume <session_id>] [--model <m>] [--force] <prompt>
//
// prompt 是 positional argument（不是 flag）。
func (r *Runner) Run(ctx context.Context, opts agent.RunOptions, cb agent.EventCallback) error {
	args := []string{
		"--print",
		"--output-format", "stream-json",
		"--stream-partial-output",
		// Server 端非互動執行，需預先信任 work_dir，否則首次進入新目錄會要求互動確認。
		// 信任僅代表允許 cursor-agent 在此目錄執行；是否放寬命令授權由 --force 控制。
		"--trust",
	}

	if opts.SessionID != "" {
		args = append(args, "--resume", opts.SessionID)
	}

	if opts.ExtraArgs != nil {
		if m := strings.TrimSpace(opts.ExtraArgs[agent.ArgModel]); m != "" {
			args = append(args, "--model", m)
		}
		if f := opts.ExtraArgs[agent.ArgForce]; f == "true" || f == "1" {
			args = append(args, "--force")
		}
	}

	// prompt 必須放在最後當 positional argument。
	//
	// Windows workaround：`cursor-agent` 在 Windows 是 `.cmd` → `.ps1` → node.exe 的 wrapper，
	// Go `exec.Command` 啟動 `.cmd` 時會走 `cmd.exe /c`，而 cmd.exe 的 parser 遇到 arg
	// 裡的 LF (0x0A) 會截斷整條命令，造成多行 prompt 只剩第一行。cursor-agent 又沒有
	// stdin 模式可退，只能在傳進去前把 LF 轉成字面 `\n`（兩字元）—— 實測 LLM 會把它當
	// 成換行理解（訓練語料裡 `\n` 本來就代表換行）。代價是極罕見的 edge case：使用者
	// 的 prompt 本身就包含字面 `\n`（例如在問「Python 的 `\n` 怎麼用」）時語意會偏差。
	// 因為是 Windows dev 環境限定，權衡下來可接受。正式部署的 Linux 不會走這條路。
	prompt := opts.Prompt
	if runtime.GOOS == "windows" && strings.ContainsRune(prompt, '\n') {
		prompt = strings.ReplaceAll(prompt, "\n", `\n`)
		log.Printf("[cursor] Windows 偵測到多行 prompt，已把 LF 轉為字面 \\n（避開 cmd.exe 截斷）")
	}
	args = append(args, prompt)

	log.Printf("[cursor] 執行指令: cursor-agent %s (prompt len=%d)", strings.Join(args, " "), len(prompt))
	if opts.WorkDir != "" {
		log.Printf("[cursor] 工作目錄: %s", opts.WorkDir)
	}

	cmd := exec.CommandContext(ctx, "cursor-agent", args...)
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
		log.Printf("[cursor] 取得 stdout pipe 失敗: %v", err)
		return err
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		log.Printf("[cursor] 子進程啟動失敗: %v", err)
		return err
	}
	log.Printf("[cursor] 子進程已啟動，PID=%d", cmd.Process.Pid)

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer

	sawResult := false
	lineCount := 0
	var st dispatchState

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		lineCount++
		log.Printf("[cursor] 收到第 %d 行 (len=%d): %s", lineCount, len(line), truncate(string(line), 200))

		e, parseErr := ParseEvent(line)
		if parseErr != nil {
			log.Printf("[cursor] 解析失敗: %v | 原始內容: %s", parseErr, truncate(string(line), 200))
			continue
		}
		log.Printf("[cursor] 事件 type=%s subtype=%s", e.Type, e.Subtype)
		if e.SessionID != "" {
			log.Printf("[cursor]   └─ session_id=%s", e.SessionID)
		}

		if e.Type == "result" {
			sawResult = true
		}

		r.dispatch(e, cb, &st)
	}

	if err := scanner.Err(); err != nil {
		log.Printf("[cursor] scanner 錯誤: %v", err)
	}

	waitErr := cmd.Wait()
	stderr := stderrBuf.String()
	if stderr != "" {
		log.Printf("[cursor] stderr 輸出:\n%s", stderr)
	}

	// Cursor 失敗時可能沒有 terminal result event，exit code 才是真實判準。
	// 若被 context 取消，直接回報 waitErr 由上層處理（視為 aborted）。
	if waitErr != nil {
		if ctx.Err() == nil && !sawResult && stderr != "" {
			cb(agent.Event{Type: agent.EventError, Err: classifyRunnerError(stderr, waitErr)})
		}
		log.Printf("[cursor] 子進程結束，exit error: %v", waitErr)
	} else {
		log.Printf("[cursor] 子進程正常結束，共處理 %d 行", lineCount)
	}
	return waitErr
}

// dispatch 將 cursor-agent 專屬事件轉換為 agent.Event。
func (r *Runner) dispatch(e *StreamEvent, cb agent.EventCallback, st *dispatchState) {
	switch e.Type {
	case "system":
		if e.Subtype == "init" {
			ev := agent.Event{Type: agent.EventSessionInit, SessionID: e.SessionID}
			if m := strings.TrimSpace(e.Model); m != "" {
				ev.Model = model.AgentSnapshot(model.InfoFromStream(agent.TypeCursor, m))
			}
			if ev.SessionID != "" || ev.Model != nil {
				cb(ev)
			}
		}

	case "assistant":
		r.dispatchAssistant(e, cb, st)

	case "tool_call":
		log.Printf("[cursor] tool_call subtype=%s call_id=%s", e.Subtype, e.CallID)

	case "result":
		r.dispatchResult(e, cb)
	}
}

// classifyRunnerError 將 stderr 轉為更易懂的錯誤訊息。
func classifyRunnerError(stderr string, waitErr error) error {
	s := strings.TrimSpace(stderr)
	if strings.Contains(s, "Authentication required") {
		return &authError{detail: s}
	}
	if s != "" {
		return &runnerError{stderr: s, waitErr: waitErr}
	}
	return waitErr
}

type authError struct{ detail string }

func (e *authError) Error() string {
	return "cursor-agent 認證失敗：headless 模式需設定 CURSOR_API_KEY 或在本機執行 agent login。原始訊息：" + e.detail
}

// dispatchState 追蹤 assistant 串流進度。
type dispatchState struct {
	streamStartSent bool
	gotStreamDelta  bool
}

// shouldEmitAssistantText 依官方 stream-partial-output 規則判斷是否輸出 assistant 文字。
// 參考：https://cursor.com/docs/cli/reference/output-format
func shouldEmitAssistantText(e *StreamEvent, st *dispatchState) (text string, ok bool) {
	text = e.Message.Text()
	if text == "" {
		return "", false
	}

	hasTS := e.TimestampMS != nil
	hasMCID := e.ModelCallID != nil

	// buffered flush（兩者皆有）→ 略過
	if hasTS && hasMCID {
		return "", false
	}
	// streaming delta（有 ts、無 mcid）→ 輸出
	if hasTS && !hasMCID {
		return text, true
	}
	// 無 ts、無 mcid：非 partial 模式的 segment，或 partial 模式的 final flush
	if !hasTS && !hasMCID {
		if st.gotStreamDelta {
			return "", false // final flush duplicate
		}
		return text, true // 非 partial 模式完整 segment
	}
	return "", false
}

// dispatchAssistant 處理 assistant 事件。
func (r *Runner) dispatchAssistant(e *StreamEvent, cb agent.EventCallback, st *dispatchState) {
	text, emit := shouldEmitAssistantText(e, st)
	if !emit {
		log.Printf("[cursor]   └─ 略過 assistant 事件 (ts=%v mcid=%v gotDelta=%v)",
			e.TimestampMS != nil, e.ModelCallID != nil, st.gotStreamDelta)
		return
	}
	if !st.streamStartSent {
		st.streamStartSent = true
		cb(agent.Event{Type: agent.EventStreamStart})
	}
	st.gotStreamDelta = st.gotStreamDelta || e.TimestampMS != nil
	cb(agent.Event{Type: agent.EventDelta, Text: text})
}

// dispatchResult 處理 result 事件。
func (r *Runner) dispatchResult(e *StreamEvent, cb agent.EventCallback) {
	if e.IsError {
		cb(agent.Event{Type: agent.EventError, Err: &providerError{msg: e.Result}, SessionID: e.SessionID})
	}
	cb(agent.Event{
		Type:       agent.EventDone,
		SessionID:  e.SessionID,
		ResultText: strings.TrimSpace(e.Result),
	})
}


// runnerError 表示子進程異常結束而沒有 terminal result。
type runnerError struct {
	stderr  string
	waitErr error
}

func (e *runnerError) Error() string {
	if e.stderr != "" {
		return "cursor-agent failed: " + strings.TrimSpace(e.stderr)
	}
	return "cursor-agent failed: " + e.waitErr.Error()
}

// providerError 表示 result.is_error = true 的 provider 錯誤。
type providerError struct{ msg string }

func (e *providerError) Error() string {
	if e.msg == "" {
		return "cursor-agent reported error"
	}
	return "cursor-agent: " + e.msg
}

// truncate 截斷過長字串，避免 log 爆炸。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…(truncated)"
}
