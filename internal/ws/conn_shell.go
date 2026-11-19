// wsConn 的 Shell 執行與授權。

package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"strings"
	"sync"
	"sync/atomic"
	"time"

	"log/slog"

	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/antigravity"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/claude"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/codex"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/cursor"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/kiro"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/kiroacp"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/shell"
)

func (h *wsConn) beginShellRun(command string, workDir string) {
	release, err := admitWork()
	if err != nil {
		h.send(updateInProgressMsg(""))
		return
	}
	defer release()
	if taskIsActive(h.sessionID) {
		h.broadcast(serverMsg{Type: "error", Content: "AI is running; cannot run shell"})
		return
	}
	if shellTaskActive(h.sessionID) {
		h.broadcast(serverMsg{Type: "error", Content: "shell already running"})
		return
	}
	clearInMemoryShellApproval(h.sessionID)

	if err := h.database.AddMessage(h.sessionID, "user", command, h.author); err != nil {
		slog.Info(fmt.Sprintf("[ws] shell AddMessage: %v", err))
	}
	h.broadcast(serverMsg{Type: "user_message", Content: command, Author: h.author})

	msgID, err := h.database.CreatePendingMessageWithRole(h.sessionID, db.RoleShell)
	if err != nil {
		h.broadcast(serverMsg{Type: "error", Content: err.Error()})
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	shellTaskStart(h.sessionID, cancel, msgID)

	if err := h.database.UpdateSessionStatus(h.sessionID, db.SessionStatusRunning); err != nil {
		slog.Info(fmt.Sprintf("[ws] shell running status: %v", err))
	}
	h.broadcast(serverMsg{Type: "status", Value: StateShellRunning})

	goRelease := holdWork()
	go func(command string, msgID int64, wdir string) {
		defer goRelease()
		defer func() {
			if r := recover(); r != nil {
				slog.Info(fmt.Sprintf("[ws] shell goroutine panic: %v", r))
			}
			shellTaskEnd(h.sessionID, msgID)
		}()
		content := &messageBuffer{write: func(text string) error { return h.database.AppendMessageContent(msgID, text) }, send: h.broadcast}
		defer content.Flush()
		var cbMu sync.Mutex
		var done atomic.Bool
		var shellErrText string
		var exitCode int
		err := runShellCommand(ctx, shell.RunOptions{Command: command, WorkDir: wdir, Timeout: h.shellTimeoutSec}, func(e shell.Event) {
			cbMu.Lock()
			defer cbMu.Unlock()
			if e.Type != shell.EventDeltaStdout && e.Type != shell.EventDeltaStderr {
				if flushErr := content.Flush(); flushErr != nil {
					shellErrText = flushErr.Error()
					cancel()
					return
				}
			}
			if ctx.Err() != nil {
				return
			}
			switch e.Type {
			case shell.EventDeltaStdout:
				chunk := appendShellDBChunk("stdout", e.Text)
				if err := content.Append(chunk, serverMsg{Type: "shell_delta", Stream: "stdout", Content: e.Text}); err != nil {
					slog.Info(fmt.Sprintf("[ws] shell append: %v", err))
					cancel()
				}
			case shell.EventDeltaStderr:
				chunk := appendShellDBChunk("stderr", e.Text)
				if err := content.Append(chunk, serverMsg{Type: "shell_delta", Stream: "stderr", Content: e.Text}); err != nil {
					slog.Info(fmt.Sprintf("[ws] shell append: %v", err))
					cancel()
				}
			case shell.EventError:
				shellErrText = e.Text
				if e.Text != "" {
					_ = h.database.AppendMessageContent(msgID, "\n[error] "+e.Text+"\n")
				}
				h.broadcast(serverMsg{Type: "shell_error", Content: e.Text})
			case shell.EventDone:
				done.Store(true)
				exitCode = e.ExitCode
				finalizeShellMessage(h.database, msgID, e.ExitCode)
				h.broadcast(serverMsg{Type: "shell_done", ExitCode: e.ExitCode})
			}
		})
		if flushErr := content.Flush(); flushErr != nil {
			err = errors.Join(err, flushErr)
		}
		interrupted := !done.Load()
		if interrupted {
			appendText := "\n[interrupted]\n"
			if err != nil {
				appendText = "\n[error] " + err.Error() + "\n"
			}
			_ = h.database.AppendMessageContent(msgID, appendText)
			finalizeShellMessage(h.database, msgID, -1)
			exitCode = -1
			h.broadcast(serverMsg{Type: "shell_done", ExitCode: -1})
		}
		if err := h.database.UpdateSessionStatus(h.sessionID, db.SessionStatusIdle); err != nil {
			slog.Info(fmt.Sprintf("[ws] shell idle status: %v", err))
		}
		h.broadcast(serverMsg{Type: "status", Value: idleUIStatus(h.database, h.sessionID)})
		finishShellNotify(h.botToken, h.tgUserID, h.notifyCfg, h.sess.Name, command, shellErrText, exitCode, interrupted && err == nil, err)
	}(command, msgID, workDir)
}

func (h *wsConn) startShellGoroutine(line string, absDir string) {
	release, err := admitWork()
	if err != nil {
		h.send(updateInProgressMsg(""))
		return
	}
	defer release()
	msgID, err := h.database.CreatePendingMessageWithRole(h.sessionID, db.RoleShell)
	if err != nil {
		slog.Info(fmt.Sprintf("[ws] CreatePendingMessageWithRole shell: %v", err))
		h.broadcast(serverMsg{Type: "error", Content: err.Error()})
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	shellTaskStart(h.sessionID, cancel, msgID)

	if err := h.database.UpdateSessionStatus(h.sessionID, db.SessionStatusRunning); err != nil {
		slog.Info(fmt.Sprintf("[ws] UpdateSessionStatus running (shell): %v", err))
	}
	h.broadcast(serverMsg{Type: "status", Value: StateShellExec})

	goRelease := holdWork()
	go func(line string, msgID int64, workDir string) {
		defer goRelease()
		defer func() {
			if r := recover(); r != nil {
				slog.Info(fmt.Sprintf("[ws] shell_exec goroutine panic: %v", r))
			}
			shellTaskEnd(h.sessionID, msgID)
		}()
		content := &messageBuffer{write: func(text string) error { return h.database.AppendMessageContent(msgID, text) }, send: h.broadcast}
		defer content.Flush()
		var cbMu sync.Mutex
		var done atomic.Bool
		var shellErrText string
		var exitCode int
		err := runShellCommand(ctx, shell.RunOptions{Command: line, WorkDir: workDir, Timeout: h.shellTimeoutSec}, func(e shell.Event) {
			cbMu.Lock()
			defer cbMu.Unlock()
			if e.Type != shell.EventDeltaStdout && e.Type != shell.EventDeltaStderr {
				if flushErr := content.Flush(); flushErr != nil {
					shellErrText = flushErr.Error()
					cancel()
					return
				}
			}
			if ctx.Err() != nil {
				return
			}
			switch e.Type {
			case shell.EventDeltaStdout:
				chunk := appendShellDBChunk("stdout", e.Text)
				if err := content.Append(chunk, serverMsg{Type: "shell_delta", Stream: "stdout", Content: e.Text}); err != nil {
					slog.Info(fmt.Sprintf("[ws] shell append: %v", err))
					cancel()
				}
			case shell.EventDeltaStderr:
				chunk := appendShellDBChunk("stderr", e.Text)
				if err := content.Append(chunk, serverMsg{Type: "shell_delta", Stream: "stderr", Content: e.Text}); err != nil {
					slog.Info(fmt.Sprintf("[ws] shell append: %v", err))
					cancel()
				}
			case shell.EventError:
				shellErrText = e.Text
				if e.Text != "" {
					_ = h.database.AppendMessageContent(msgID, "\n[error] "+e.Text+"\n")
				}
				h.broadcast(serverMsg{Type: "shell_error", Content: e.Text})
			case shell.EventDone:
				done.Store(true)
				exitCode = e.ExitCode
				finalizeShellMessage(h.database, msgID, e.ExitCode)
				h.broadcast(serverMsg{Type: "shell_done", ExitCode: e.ExitCode, ID: msgID})
			}
		})
		if flushErr := content.Flush(); flushErr != nil {
			err = errors.Join(err, flushErr)
		}
		interrupted := !done.Load()
		if interrupted {
			appendText := "\n[interrupted]\n"
			if err != nil {
				appendText = "\n[error] " + err.Error() + "\n"
			}
			_ = h.database.AppendMessageContent(msgID, appendText)
			finalizeShellMessage(h.database, msgID, -1)
			exitCode = -1
			h.broadcast(serverMsg{Type: "shell_done", ExitCode: -1, ID: msgID})
		}
		if err := h.database.UpdateSessionStatus(h.sessionID, db.SessionStatusIdle); err != nil {
			slog.Info(fmt.Sprintf("[ws] UpdateSessionStatus idle (shell): %v", err))
		}
		h.broadcast(serverMsg{Type: "status", Value: idleUIStatus(h.database, h.sessionID)})
		finishShellNotify(h.botToken, h.tgUserID, h.notifyCfg, h.sess.Name, line, shellErrText, exitCode, interrupted && err == nil, err)
	}(line, msgID, absDir)
}

func (h *wsConn) runShell(line string) {
	// 第一個寫入（FinalizePending、shell_pending、user 訊息）之前先過閘門。
	release, err := admitWork()
	if err != nil {
		h.send(updateInProgressMsg(""))
		return
	}
	defer release()
	taskCancel(h.sessionID)
	if err := h.database.FinalizePendingMessagesForSession(h.sessionID); err != nil {
		slog.Info(fmt.Sprintf("[ws] FinalizePendingMessagesForSession (shell): %v", err))
	}

	s, err := h.database.GetSession(h.sessionID)
	if err != nil {
		slog.Info(fmt.Sprintf("[ws] GetSession (shell): %v", err))
		h.broadcast(serverMsg{Type: "error", Content: err.Error()})
		return
	}
	if strings.TrimSpace(s.ShellPending) != "" {
		h.broadcast(serverMsg{Type: "error", Content: "請先處理待確認的 Shell 指令"})
		return
	}
	if strings.TrimSpace(s.PendingDenials) != "" {
		h.broadcast(serverMsg{Type: "error", Content: "請先處理工具授權請求"})
		return
	}

	absDir, err := resolveShellWorkDir(s)
	if err != nil {
		h.broadcast(serverMsg{Type: "error", Content: err.Error()})
		return
	}

	dbCmds, err := h.database.ListShellCommandsForWorkDirKey(absDir)
	if err != nil {
		slog.Info(fmt.Sprintf("[ws] ListShellCommandsForWorkDirKey: %v", err))
		h.broadcast(serverMsg{Type: "error", Content: err.Error()})
		return
	}
	effective := shell.EffectiveAllowlist(h.shellCfg.AllowedCommands, dbCmds)

	runDirect, needConfirm, err := shell.ClassifyShellLine(line, effective)
	if err != nil {
		slog.Info(fmt.Sprintf("[ws] shell 分類拒絕: %v", err))
		h.broadcast(serverMsg{Type: "error", Content: err.Error()})
		return
	}

	line = strings.TrimSpace(line)

	if needConfirm {
		cmd := shell.FirstCommandName(line)
		payload := shellPendingPayload{
			Line:        line,
			Cmd:         cmd,
			RequestedAt: time.Now().UTC().Format(time.RFC3339),
		}
		b, err := json.Marshal(payload)
		if err != nil {
			h.broadcast(serverMsg{Type: "error", Content: err.Error()})
			return
		}
		if err := h.database.AddMessage(h.sessionID, "user", line, h.author); err != nil {
			slog.Info(fmt.Sprintf("[ws] 儲存 user 訊息 (shell confirm) 失敗: %v", err))
		}
		h.broadcast(serverMsg{Type: "user_message", Content: line, Author: h.author})
		if err := h.database.UpdateShellPending(h.sessionID, string(b)); err != nil {
			slog.Info(fmt.Sprintf("[ws] UpdateShellPending: %v", err))
			h.broadcast(serverMsg{Type: "error", Content: err.Error()})
			return
		}
		if err := h.database.UpdateSessionStatus(h.sessionID, db.SessionStatusAwaitingShellConfirm); err != nil {
			slog.Info(fmt.Sprintf("[ws] UpdateSessionStatus awaiting_shell_confirm: %v", err))
		}
		h.broadcast(serverMsg{Type: "shell_command_request", Command: payload.Cmd, Line: payload.Line, WorkDirKey: absDir})
		h.broadcast(serverMsg{Type: "status", Value: StateAwaitingShellConfirm})
		return
	}
	if !runDirect {
		return
	}

	if err := h.database.AddMessage(h.sessionID, "user", line, h.author); err != nil {
		slog.Info(fmt.Sprintf("[ws] 儲存 user 訊息 (shell) 失敗: %v", err))
	}
	h.broadcast(serverMsg{Type: "user_message", Content: line, Author: h.author})

	h.startShellGoroutine(line, absDir)
}

func (h *wsConn) handleShellAllowExecute(remember bool) {
	release, err := admitWork()
	if err != nil {
		h.send(updateInProgressMsg(""))
		return
	}
	defer release()
	taskCancel(h.sessionID)
	if err := h.database.FinalizePendingMessagesForSession(h.sessionID); err != nil {
		slog.Info(fmt.Sprintf("[ws] FinalizePendingMessagesForSession (shell allow): %v", err))
	}

	sess, err := h.database.GetSession(h.sessionID)
	if err != nil {
		slog.Info(fmt.Sprintf("[ws] GetSession (shell allow): %v", err))
		h.broadcast(serverMsg{Type: "error", Content: err.Error()})
		return
	}
	if strings.TrimSpace(sess.ShellPending) == "" {
		return
	}
	var p shellPendingPayload
	if err := json.Unmarshal([]byte(sess.ShellPending), &p); err != nil {
		slog.Info(fmt.Sprintf("[ws] shell_pending JSON: %v", err))
		clearShellPending(h.database, h.sessionID)
		_ = h.database.UpdateSessionStatus(h.sessionID, db.SessionStatusIdle)
		h.broadcast(serverMsg{Type: "error", Content: "Shell 待確認資料損壞"})
		h.broadcast(serverMsg{Type: "status", Value: idleUIStatus(h.database, h.sessionID)})
		return
	}
	line := strings.TrimSpace(p.Line)
	if line == "" {
		clearShellPending(h.database, h.sessionID)
		_ = h.database.UpdateSessionStatus(h.sessionID, db.SessionStatusIdle)
		h.broadcast(serverMsg{Type: "status", Value: idleUIStatus(h.database, h.sessionID)})
		return
	}
	if shell.LineContainsShellChaining(line) {
		h.broadcast(serverMsg{Type: "error", Content: "不允許指令串接、管線或換行（&&、||、|、; 等）"})
		clearShellPending(h.database, h.sessionID)
		_ = h.database.UpdateSessionStatus(h.sessionID, db.SessionStatusIdle)
		h.broadcast(serverMsg{Type: "status", Value: idleUIStatus(h.database, h.sessionID)})
		return
	}
	absDir, err := resolveShellWorkDir(sess)
	if err != nil {
		h.broadcast(serverMsg{Type: "error", Content: err.Error()})
		return
	}
	if remember {
		if err := h.database.AddShellWorkdirCommand(absDir, p.Cmd); err != nil {
			slog.Info(fmt.Sprintf("[ws] AddShellWorkdirCommand: %v", err))
		}
	}
	clearShellPending(h.database, h.sessionID)
	h.startShellGoroutine(line, absDir)
}
