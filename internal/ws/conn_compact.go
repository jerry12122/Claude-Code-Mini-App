package ws

import (
	"context"
	"strings"
	"sync/atomic"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

// compactTaskSeq 給每一回合獨立的負數身份。常數 -7 會讓取消後晚返回的收尾刪掉後繼回合。
var compactTaskSeq atomic.Int64

func nextCompactTaskID() int64 {
	return -compactTaskSeq.Add(1)
}

func (h *wsConn) onCompact() {
	if reason := h.compactRejectReason(); reason != "" {
		h.send(serverMsg{Type: "compact", Value: "failed", Content: reason})
		return
	}
	withWorkAdmission(h.send, "", func() {
		dl := dispatchLock(h.sessionID)
		dl.Lock()
		defer dl.Unlock()
		if reason := h.compactRejectReason(); reason != "" {
			h.send(serverMsg{Type: "compact", Value: "failed", Content: reason})
			return
		}
		s, err := h.database.GetSession(h.sessionID)
		if err != nil {
			h.send(serverMsg{Type: "compact", Value: "failed", Content: "找不到會話"})
			return
		}
		providerID := strings.TrimSpace(s.AgentSessionID)
		ctx, cancel := context.WithCancel(context.Background())
		taskID := nextCompactTaskID()
		done := taskStartWithDone(h.sessionID, cancel, taskID)
		if err := h.database.UpdateSessionStatus(h.sessionID, db.SessionStatusRunning); err != nil {
			taskFinish(h.sessionID, taskID)
			close(done)
			cancel()
			h.send(serverMsg{Type: "compact", Value: "failed", Content: "壓縮行程失敗"})
			return
		}
		h.broadcast(serverMsg{Type: "compact", Value: "running", Content: "正在壓縮上下文"})
		h.broadcast(serverMsg{Type: "status", Value: StateCompacting})
		// 背景回合接手 admission token，持到收尾與佇列決策之後。
		goRelease := holdWork()
		go h.runCompact(ctx, cancel, done, goRelease, taskID, s, providerID)
	})
}

func (h *wsConn) compactRejectReason() string {
	if h.isGuest {
		return "分享連線不能壓縮"
	}
	if h.roomWake {
		return "聊天室不能壓縮"
	}
	s, err := h.database.GetSession(h.sessionID)
	if err != nil {
		return "找不到會話"
	}
	rooms, err := h.database.SessionRoomIDs(h.sessionID)
	if err != nil || len(rooms) > 0 {
		return "聊天室不能壓縮"
	}
	if s.AgentType != agent.TypeClaude {
		return "此 runner 尚未驗證，不能壓縮"
	}
	if s.InputMode == "shell" || strings.TrimSpace(s.ShellPending) != "" || shellTaskActive(h.sessionID) || peekShellPending(h.sessionID) != nil {
		return "Shell 模式不能壓縮"
	}
	if strings.TrimSpace(s.AgentSessionID) == "" {
		return "沒有可續接的會話"
	}
	if compactBusy(h.sessionID, s) {
		return "請等目前工作結束再壓縮"
	}
	return ""
}

func compactBusy(sessionID string, s *db.Session) bool {
	if taskIsActive(sessionID) || shellTaskActive(sessionID) {
		return true
	}
	if _, pending := permPending(sessionID); pending || questionPending(sessionID) != nil {
		return true
	}
	if strings.TrimSpace(s.PendingDenials) != "" || strings.TrimSpace(s.ShellPending) != "" {
		return true
	}
	switch s.Status {
	case db.SessionStatusRunning, db.SessionStatusAwaitingConfirm, db.SessionStatusAwaitingInput, db.SessionStatusAwaitingShellConfirm:
		return true
	default:
		return false
	}
}

func (h *wsConn) runCompact(ctx context.Context, cancel context.CancelFunc, done chan struct{}, release func(), taskID int64, s *db.Session, providerID string) {
	defer release()
	defer cancel()
	success := false
	terminal := ""
	defer func() {
		h.finishCompactLocked(done, taskID, success, terminal)
	}()
	fail := func(content string) { terminal = content }
	runner, err := agent.NewRunner(agent.TypeClaude)
	if err != nil {
		fail("壓縮行程失敗")
		return
	}
	extra := map[string]string{}
	if s.PermissionMode != "" {
		extra[agent.ArgPermissionMode] = s.PermissionMode
	}
	if s.Model != "" {
		extra[agent.ArgModel] = s.Model
	}
	var cliExtra []string
	if len(s.CliExtraArgs) > 0 {
		cliExtra = append([]string(nil), s.CliExtraArgs...)
	}
	var boundary string
	var native, resultFailed, wrong bool
	err = runner.Run(ctx, agent.RunOptions{
		Prompt:       "/compact",
		SessionID:    providerID,
		WorkDir:      s.WorkDir,
		ExtraArgs:    extra,
		CliExtraArgs: cliExtra,
		OnStart:      func(pid int) { taskSetPid(h.sessionID, taskID, pid) },
	}, func(e agent.Event) {
		switch e.Type {
		case agent.EventCompactBoundary:
			if boundary == "" {
				boundary = e.SessionID
			}
			if e.SessionID != providerID {
				wrong = true
			}
		case agent.EventSessionInit:
			if e.SessionID != "" && e.SessionID != providerID {
				wrong = true
			}
		case agent.EventError:
			resultFailed = true
		case agent.EventDone:
			if e.SessionID != "" && e.SessionID != providerID {
				wrong = true
			}
			if e.CompactNative && boundary == providerID && e.SessionID == providerID {
				native = true
			}
		}
	})
	if wrong {
		fail("壓縮後的會話不一致")
		return
	}
	if err != nil {
		fail("壓縮行程失敗")
		return
	}
	if resultFailed {
		fail("壓縮結果失敗")
		return
	}
	if !native || boundary != providerID {
		fail("沒有收到壓縮確認")
		return
	}
	success = true
	terminal = "已壓縮上下文"
}

// finishCompactTurn 讓既有呼叫點走同一條收尾。終端訊息由帶文字的入口送出。
func (h *wsConn) finishCompactTurn(done chan struct{}, taskID int64, success bool) {
	h.finishCompactLocked(done, taskID, success, "")
}

// finishCompactLocked 先關自己的 done，再拿 dispatchLock。
// RunNow 持著這把鎖等 done，關 done 之前去等鎖會死鎖。
// 身份、狀態、終端訊息與佇列只在這把鎖裡決定一次，不在解鎖後再讀任務補窗。
func (h *wsConn) finishCompactLocked(done chan struct{}, taskID int64, success bool, terminal string) {
	close(done)
	dl := dispatchLock(h.sessionID)
	dl.Lock()
	defer dl.Unlock()
	taskManager.mu.Lock()
	current := taskManager.tasks[h.sessionID]
	settle := current == nil || current.msgID == taskID
	if current != nil && current.msgID == taskID {
		delete(taskManager.tasks, h.sessionID)
	}
	_, _ = interjectedTasks.LoadAndDelete(taskID)
	taskManager.mu.Unlock()
	if !settle {
		return
	}
	if terminal != "" {
		value := "failed"
		if success {
			value = "done"
		}
		h.broadcast(serverMsg{Type: "compact", Value: value, Content: terminal})
	}
	_ = h.database.UpdateSessionStatus(h.sessionID, db.SessionStatusIdle)
	h.broadcast(serverMsg{Type: "status", Value: idleUIStatus(h.database, h.sessionID)})
	if success {
		if h.dispatcher != nil {
			if s, err := h.database.GetSession(h.sessionID); err == nil && !h.dispatcher.busy(s) && strings.TrimSpace(s.ShellPending) == "" {
				h.dispatcher.drainLocked(true)
			}
		}
		return
	}
	h.pauseQueue()
}
