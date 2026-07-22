package kiro

import (
	"bufio"
	"bytes"
	"context"
	"log"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/proc"
)

func init() {
	agent.Register(agent.TypeKiro, func() agent.Runner {
		return &Runner{}
	})
}

// Runner 是 kiro-cli 的 agent.Runner 實作。
//
// 使用 --no-interactive 模式執行，回應為一次性輸出（非逐字 streaming）。
// Session ID 透過 --list-sessions 在首回合完成後取得。
// 參考文件：docs/spec/kiro-cli.md
type Runner struct{}

// Name 實作 agent.Runner。
func (r *Runner) Name() string { return agent.TypeKiro }

// ansiRe 匹配 ANSI escape sequences（含游標控制、色彩碼等）。
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]|\x1b[A-Za-z]`)

// stripAnsi 剝除字串中的 ANSI escape codes。
func stripAnsi(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

// stripKiroPrefix 剝除 Kiro stdout 回應行的 `> ` 前綴（剝除 ANSI 後）。
// 若該行不含前綴則原樣返回（可能為空行或系統訊息）。
func stripKiroPrefix(line string) string {
	clean := strings.TrimRight(stripAnsi(line), "\r")
	if strings.HasPrefix(clean, "> ") {
		return clean[2:]
	}
	return clean
}

// Run 實作 agent.Runner：啟動 kiro-cli chat --no-interactive 子進程並串流事件。
//
// 指令格式：
//
//	# 首回合
//	kiro-cli chat --no-interactive --trust-all-tools "<prompt>"
//
//	# Resume
//	kiro-cli chat --no-interactive --trust-all-tools --resume-id <SESSION_ID> "<prompt>"
//
// Prompt 以 positional argument 傳遞（kiro-cli.exe 為原生 EXE，無 cmd.exe wrapper）。
func (r *Runner) Run(ctx context.Context, opts agent.RunOptions, cb agent.EventCallback) error {
	args := buildArgs(opts)

	log.Printf("[kiro] 執行指令: kiro-cli %s (prompt len=%d)", strings.Join(args, " "), len(opts.Prompt))
	if opts.WorkDir != "" {
		log.Printf("[kiro] 工作目錄: %s", opts.WorkDir)
	}

	// 首回合：chat 前先快照 session 列表，完成後 diff 比對取得新 session id。
	var beforeSessions []kiroSession
	if opts.SessionID == "" {
		var err error
		beforeSessions, err = listSessions(opts.WorkDir)
		if err != nil {
			log.Printf("[kiro] --list-sessions (before) 失敗: %v（仍繼續執行 chat）", err)
			beforeSessions = nil
		} else {
			log.Printf("[kiro] --list-sessions (before): %d sessions", len(beforeSessions))
		}
	}

	cmd := exec.CommandContext(ctx, "kiro-cli", args...)
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
		log.Printf("[kiro] 取得 stdout pipe 失敗: %v", err)
		return err
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		log.Printf("[kiro] 子進程啟動失敗: %v", err)
		return err
	}
	log.Printf("[kiro] 子進程已啟動，PID=%d", cmd.Process.Pid)

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	lineCount := 0
	var st kiroStreamState

	for scanner.Scan() {
		raw := scanner.Bytes()
		if len(raw) == 0 {
			continue
		}
		lineCount++
		log.Printf("[kiro] 收到第 %d 行 (len=%d): %s", lineCount, len(raw), truncate(string(raw), 200))
		st.dispatchLine(string(raw), cb)
	}

	if err := scanner.Err(); err != nil {
		log.Printf("[kiro] scanner 錯誤: %v", err)
	}

	// 若全程未見 "> " 行，將累積內容降級為回覆。
	st.flushFallback(cb)

	waitErr := cmd.Wait()
	stderr := stderrBuf.String()
	if stderr != "" {
		log.Printf("[kiro] stderr 輸出:\n%s", truncate(stderr, 500))
	}

	if waitErr != nil {
		log.Printf("[kiro] 子進程結束，exit error: %v", waitErr)
		if ctx.Err() == nil {
			detail := strings.TrimSpace(stderr)
			if detail == "" {
				detail = strings.TrimSpace(st.thinkingBuf.String())
			}
			runErr := agent.NewExitError("kiro-cli", detail, waitErr)
			cb(agent.Event{Type: agent.EventError, Err: runErr})
			return runErr
		}
		return waitErr
	}

	log.Printf("[kiro] 子進程正常結束，共處理 %d 行", lineCount)

	// 首回合：以 before/after 快照 diff + prompt 比對取得 session id。
	sessionID := opts.SessionID
	if sessionID == "" && ctx.Err() == nil {
		sessionID = fetchSessionIDAfterRun(opts.WorkDir, opts.Prompt, beforeSessions)
		if sessionID != "" {
			log.Printf("[kiro] 取得 session id: %s", sessionID)
			cb(agent.Event{Type: agent.EventSessionInit, SessionID: sessionID})
		} else {
			log.Printf("[kiro] 無法取得 session id，降級為單回合模式")
		}
	}

	cb(agent.Event{Type: agent.EventDone, SessionID: sessionID})
	return nil
}

// buildArgs 組出傳給 kiro-cli 的引數列表。
func buildArgs(opts agent.RunOptions) []string {
	args := []string{"chat", "--no-interactive", "--trust-all-tools"}

	if opts.SessionID != "" {
		args = append(args, "--resume-id", opts.SessionID)
	}

	// effort 旗標（可選）
	if opts.ExtraArgs != nil {
		if effort := strings.TrimSpace(opts.ExtraArgs["effort"]); effort != "" {
			args = append(args, "--effort", effort)
		}
		if agentProfile := strings.TrimSpace(opts.ExtraArgs["agent"]); agentProfile != "" {
			args = append(args, "--agent", agentProfile)
		}
		if m := strings.TrimSpace(opts.ExtraArgs[agent.ArgModel]); m != "" {
			args = append(args, "--model", m)
		}
	}

	// prompt 必須是最後一個 positional argument
	args = append(args, opts.Prompt)
	return args
}

// truncate 截斷過長字串，避免 log 爆炸。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…(truncated)"
}
