package ws

import (
	"context"
	"strconv"
	"sync"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/shell"
)

const (
	StateShellIdle             = "SHELL_IDLE"
	StateShellAwaitingApproval = "SHELL_AWAITING_APPROVAL"
	StateShellRunning          = "SHELL_RUNNING"
)

type shellPendingInfo struct {
	Command   string
	WorkDir   string
	ShellType string
}

var (
	shellPendingMu sync.Mutex
	shellPending   = map[string]*shellPendingInfo{}
)

func setShellPending(sessionID string, p *shellPendingInfo) {
	shellPendingMu.Lock()
	shellPending[sessionID] = p
	shellPendingMu.Unlock()
}

func takeShellPending(sessionID string) *shellPendingInfo {
	shellPendingMu.Lock()
	defer shellPendingMu.Unlock()
	p := shellPending[sessionID]
	delete(shellPending, sessionID)
	return p
}

func peekShellPending(sessionID string) *shellPendingInfo {
	shellPendingMu.Lock()
	defer shellPendingMu.Unlock()
	return shellPending[sessionID]
}

func clearInMemoryShellApproval(sessionID string) {
	shellPendingMu.Lock()
	delete(shellPending, sessionID)
	shellPendingMu.Unlock()
}

type shellTaskEntry struct {
	cancel context.CancelFunc
	msgID  int64
	id     int64 // task id（用 msgID 作為唯一識別）
}

var shellTaskManager = struct {
	mu    sync.Mutex
	tasks map[string]*shellTaskEntry
}{tasks: make(map[string]*shellTaskEntry)}

func shellTaskStart(sessionID string, cancel context.CancelFunc, msgID int64) {
	shellTaskManager.mu.Lock()
	if old, ok := shellTaskManager.tasks[sessionID]; ok && old.cancel != nil {
		old.cancel()
	}
	shellTaskManager.tasks[sessionID] = &shellTaskEntry{cancel: cancel, msgID: msgID, id: msgID}
	shellTaskManager.mu.Unlock()
}

func shellTaskEnd(sessionID string, taskID int64) {
	shellTaskManager.mu.Lock()
	if e, ok := shellTaskManager.tasks[sessionID]; ok && e.id == taskID {
		delete(shellTaskManager.tasks, sessionID)
	}
	shellTaskManager.mu.Unlock()
}

func shellTaskCancel(sessionID string) {
	shellTaskManager.mu.Lock()
	e, ok := shellTaskManager.tasks[sessionID]
	if !ok {
		shellTaskManager.mu.Unlock()
		return
	}
	delete(shellTaskManager.tasks, sessionID)
	shellTaskManager.mu.Unlock()
	if e != nil && e.cancel != nil {
		e.cancel()
	}
}

func shellTaskActive(sessionID string) bool {
	shellTaskManager.mu.Lock()
	defer shellTaskManager.mu.Unlock()
	_, ok := shellTaskManager.tasks[sessionID]
	return ok
}

func shellTaskPeekMsgID(sessionID string) int64 {
	shellTaskManager.mu.Lock()
	defer shellTaskManager.mu.Unlock()
	if e := shellTaskManager.tasks[sessionID]; e != nil {
		return e.msgID
	}
	return 0
}

func shellTypeString() string {
	return string(shell.DetectType())
}

func appendShellDBChunk(stream string, line string) string {
	if stream == "stderr" {
		return "[stderr] " + line
	}
	return line
}

func finalizeShellMessage(database *db.DB, msgID int64, exitCode int) {
	tail := "\n[exit " + strconv.Itoa(exitCode) + "]\n"
	_ = database.AppendMessageContent(msgID, tail)
	_ = database.FinalizeMessage(msgID)
}
