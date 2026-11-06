package cursor

import (
	"bufio"
	"bytes"
	"context"
	"fmt"

	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/media"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/model"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/proc"
	"log/slog"
)

func init() {
	agent.Register(agent.TypeCursor, func() agent.Runner {
		return &Runner{}
	})
}

// resultExitGrace：收到 result 後等進程／stdout 收尾的上限，超過就強制結束（var：測試縮短用）。
var resultExitGrace = 5 * time.Second

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
	start := time.Now()
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
		slog.Info(fmt.Sprintf("[cursor] Windows 偵測到多行 prompt，已把 LF 轉為字面 \\n（避開 cmd.exe 截斷）"))
	}
	args = append(args, prompt)
	slog.Info(fmt.Sprintf("[cursor] 執行指令: cursor-agent %s (prompt len=%d)", strings.Join(args, " "), len(prompt)))
	if opts.WorkDir != "" {
		slog.Info(fmt.Sprintf("[cursor] 工作目錄: %s", opts.WorkDir))
	}

	cmd := exec.CommandContext(ctx, "cursor-agent", args...)
	if opts.WorkDir != "" {
		cmd.Dir = opts.WorkDir
	}
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
		slog.Error(fmt.Sprintf("[cursor] 取得 stdout pipe 失敗: %v", err))
		return err
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		slog.Error(fmt.Sprintf("[cursor] 子進程啟動失敗: %v", err))
		return err
	}
	slog.Info(fmt.Sprintf("[cursor] 子進程已啟動，PID=%d", cmd.Process.Pid))
	if opts.OnStart != nil {
		opts.OnStart(cmd.Process.Pid)
	}

	scanner := bufio.NewScanner(stdout)
	// MCP 截圖 base64 單行可達數 MB；1MB 會讓 Scan 失敗並整行丟棄，圖片永遠進不來。
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)

	var sawResult, abandoned atomic.Bool
	var lineCount atomic.Int64
	sessionID := opts.SessionID
	var st dispatchState
	resultSeen := make(chan struct{})
	var resultOnce sync.Once
	scanDone := make(chan struct{})

	// 掃描放 goroutine：不能依賴 stdout EOF 判斷結束。cursor-agent 自我更新會留下繼承 stdout 的
	// 孤兒 cmd.exe，agent 本體退出後 pipe 仍被占著，Scan 永遠不返回，任務就卡在「執行中」。
	go func() {
		defer close(scanDone)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}
			lineCount.Add(1)
			slog.Debug(fmt.Sprintf("[cursor] 收到第 %d 行 (len=%d): %s", lineCount.Load(), len(line), truncate(string(line), 200)))

			e, parseErr := ParseEvent(line)
			if parseErr != nil {
				slog.Warn(fmt.Sprintf("[cursor] 解析失敗: %v | 原始內容: %s", parseErr, truncate(string(line), 200)))
				continue
			}
			slog.Debug(fmt.Sprintf("[cursor] 事件 type=%s subtype=%s", e.Type, e.Subtype))
			if e.SessionID != "" {
				sessionID = e.SessionID
				slog.Debug(fmt.Sprintf("[cursor]   └─ session_id=%s", e.SessionID))
			}
			if abandoned.Load() {
				return
			}

			if e.Type == "result" {
				sawResult.Store(true)
				resultOnce.Do(func() { close(resultSeen) })
			}

			r.dispatch(e, cb, &st)
		}
		if err := scanner.Err(); err != nil {
			slog.Error(fmt.Sprintf("[cursor] scanner 錯誤: %v", err))
		}
	}()

	select {
	case <-scanDone:
	case <-resultSeen:
		select {
		case <-scanDone:
		case <-time.After(resultExitGrace):
			abandoned.Store(true)
			slog.Warn(fmt.Sprintf("[cursor] result 後 %s 內 stdout 仍未關閉，強制結束進程樹並放棄等待 (PID=%d)", resultExitGrace, cmd.Process.Pid))
			_ = proc.KillTree(cmd.Process.Pid)
		}
	}

	waitErr := cmd.Wait()
	stderr := stderrBuf.String()
	if stderr != "" {
		slog.Debug(fmt.Sprintf("[cursor] stderr 輸出:\n%s", stderr))
	}

	// Cursor 失敗時可能沒有 terminal result event，exit code 才是真實判準。
	// 若被 context 取消，直接回報 waitErr 由上層處理（視為 aborted）。
	duration := time.Since(start)
	if abandoned.Load() {
		// 已收到 result 才強殺，exit code 不代表任務失敗。
		waitErr = nil
	}
	if waitErr != nil {
		if ctx.Err() == nil && !sawResult.Load() {
			cb(agent.Event{Type: agent.EventError, Err: classifyRunnerError(stderr, waitErr)})
		}
		slog.Error("[cursor] run 結束", "session_id", sessionID, "duration", duration, "lines", lineCount.Load(), "ok", false, "err", waitErr)
	} else {
		slog.Info("[cursor] run 結束", "session_id", sessionID, "duration", duration, "lines", lineCount.Load(), "ok", true)
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
		slog.Info(fmt.Sprintf("[cursor] tool_call subtype=%s call_id=%s", e.Subtype, e.CallID))
		label := e.ToolLabel()
		if label == "" {
			label = "工具"
		}
		if e.Subtype == "completed" {
			label = "工具已結束：" + label
		}
		cb(agent.Event{Type: agent.EventActivity, Text: label})
		if e.Subtype != "completed" {
			return
		}
		// tool_call 完成時若帶圖片（如 MCP 截圖工具），落地存檔後以 markdown image 語法併入
		// 既有 delta 管線，前端 marked.js 會直接渲染成 <img>，作法與 internal/claude 一致。
		for _, img := range e.Images() {
			url, err := media.SaveBase64Image(img.MediaType, img.Data)
			if err != nil {
				slog.Info(fmt.Sprintf("[cursor] 圖片存檔失敗: %v", err))
				continue
			}
			if !st.streamStartSent {
				st.streamStartSent = true
				cb(agent.Event{Type: agent.EventStreamStart})
			}
			cb(agent.Event{Type: agent.EventDelta, Text: fmt.Sprintf("\n![screenshot](%s)\n", url)})
		}

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
		slog.Debug(fmt.Sprintf("[cursor]   └─ 略過 assistant 事件 (ts=%v mcid=%v gotDelta=%v)",
			e.TimestampMS != nil, e.ModelCallID != nil, st.gotStreamDelta))
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
