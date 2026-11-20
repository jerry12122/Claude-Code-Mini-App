// wsConn 讀取客戶端訊息並分派。

package ws

import (
	"context"
	"encoding/json"
	"fmt"

	"strings"
	"unicode/utf8"

	"log/slog"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/antigravity"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/claude"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/codex"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/cursor"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/kiro"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/kiroacp"
)

// readLoop 讀取客戶端訊息直到連線結束；每則訊息交給 handleMsg。
func (h *wsConn) readLoop() {
	for {
		_, raw, err := h.c.ReadMessage()
		if err != nil {
			break
		}

		var msg clientMsg
		if err := json.Unmarshal(raw, &msg); err != nil {
			h.rejectMalformedQueueEdit(raw)
			h.rejectMalformedQueueBatch(raw)
			continue
		}
		msg.invalidUTF8 = (msg.Type == "queue_edit" || msg.Type == "queue_batch" || msg.Type == "queue_batch_cancel") && !utf8.Valid(raw)

		if msg.Type == "ping" {
			h.send(serverMsg{Type: "pong"})
			continue
		}
		if reason := agent.DisabledReason(h.agentType); reason != "" {
			kind := "error"
			if msg.Type == "input" && msg.RequestID != "" {
				kind = "input_rejected"
			}
			h.send(serverMsg{Type: kind, RequestID: msg.RequestID, Code: "agent_disabled", Content: reason})
			continue
		}
		// 唯讀訪客（viewer）：除 ping 外一律丟棄並回錯誤事件，不進入任何處理。角色由後端強制。
		if h.readOnly {
			slog.Info(fmt.Sprintf("[ws] 丟棄唯讀訪客 %q 的 %q 指令 (share=%d)", h.guestNick, msg.Type, h.shareID))
			h.send(serverMsg{Type: "error", Content: "唯讀分享，無法執行此操作"})
			continue
		}

		h.handleMsg(msg)
	}
}

// handleMsg 依訊息型別分派。各 onXxx 做完即返回，等同處理完這則訊息、回到讀取迴圈。
func (h *wsConn) handleMsg(msg clientMsg) {
	switch msg.Type {
	case "room_drain":
		if h.roomWake {
			h.dispatcher.Drain()
			h.send(serverMsg{Type: "room_drained"})
		}
	case "quota_resume":
		h.onQuotaResume(msg)
	case "quota_resume_cancel":
		h.onQuotaResumeCancel(msg)
	case "input":
		h.onInput(msg)
	case "queue_remove":
		if err := RemoveQueuedMessage(h.database, h.sessionID, msg.ID); err != nil {
			slog.Info(fmt.Sprintf("[ws] DeleteQueuedMessage: %v", err))
			h.send(serverMsg{Type: "error", Content: err.Error()})
		}
	case "queue_resume":
		h.resumeQueue()
	case "queue_edit":
		h.onQueueEdit(msg)
	case "queue_batch", "queue_batch_cancel":
		h.onQueueBatch(msg)
	case "queue_run_now":
		h.dispatcher.RunNow(msg.ID)
	case "set_input_mode":
		h.onSetInputMode(msg)
	case "shell_run":
		h.onShellRun(msg)
	case "shell_approve":
		h.onShellApprove(msg)
	case "shell_cancel":
		if takeShellPending(h.sessionID) != nil {
			h.broadcast(serverMsg{Type: "shell_approval_cancelled"})
			h.broadcast(serverMsg{Type: "status", Value: idleUIStatus(h.database, h.sessionID)})
		}
	case "question_answer":
		if err := questionResolve(h.sessionID, msg.RequestID, msg.Answers); err != nil {
			h.send(serverMsg{Type: "question_error", RequestID: msg.RequestID, Content: err.Error()})
		}
	case "allow_once":
		h.onAllowOnce(msg)
	case "deny_once":
		h.onDenyOnce(msg)
	case "set_mode":
		h.onSetMode(msg)
	case "set_model":
		h.onSetModel(msg)
	case "set_effort":
		h.onSetEffort(msg)
	case "reset_context":
		h.onResetContext(msg)
	case "interrupt":
		h.onInterrupt(msg)
	case "shell_allow_once":
		h.handleShellAllowExecute(false)
	case "shell_allow_remember_workdir":
		h.handleShellAllowExecute(true)
	case "shell_deny":
		clearShellPending(h.database, h.sessionID)
		clearInMemoryShellApproval(h.sessionID)
		if err := h.database.UpdateSessionStatus(h.sessionID, db.SessionStatusIdle); err != nil {
			slog.Info(fmt.Sprintf("[ws] UpdateSessionStatus idle (shell_deny): %v", err))
		}
		h.broadcast(serverMsg{Type: "status", Value: idleUIStatus(h.database, h.sessionID)})
	case "shell_exec":
		h.onShellExec(msg)
	case "refresh_quota":
		h.onRefreshQuota(msg)
	}
}

func (h *wsConn) onQuotaResume(msg clientMsg) {
	if !h.roomWake {
		return
	}
	started := false
	if quotaResumeGenerationHeld(h.database, h.sessionID, msg.Generation) {
		started = h.resumeForQuota()
	}
	h.send(serverMsg{Type: "quota_resumed", Started: &started})
}

func (h *wsConn) onQuotaResumeCancel(msg clientMsg) {
	if h.isGuest {
		h.send(serverMsg{Type: "error", Content: "只有擁有者可以取消自動繼續"})
		return
	}
	if err := h.database.CancelQuotaResume(h.sessionID); err != nil {
		h.send(serverMsg{Type: "error", Content: err.Error()})
		return
	}
	update, err := queueUpdate(h.database, h.sessionID)
	if err != nil {
		h.send(serverMsg{Type: "error", Content: err.Error()})
		return
	}
	h.broadcast(update)
}

func (h *wsConn) onInput(msg clientMsg) {
	slog.Info(fmt.Sprintf("[ws] input len=%d", len(msg.Data)))
	if !h.mcpAgent || h.isGuest || msg.SourceSessionID == h.sessionID {
		msg.SourceSessionID = ""
	} else if msg.SourceSessionID != "" {
		if _, err := h.database.GetSession(msg.SourceSessionID); err != nil {
			msg.SourceSessionID = ""
		}
	}
	h.dispatcher.Dispatch(msg)
}

func (h *wsConn) onSetInputMode(msg clientMsg) {
	sx, err := h.database.GetSession(h.sessionID)
	if err == nil && sx.Status == db.SessionStatusAwaitingConfirm {
		h.send(serverMsg{Type: "error", Content: "awaiting confirmation; cannot switch input mode"})
		return
	}
	if err == nil && strings.TrimSpace(sx.ShellPending) != "" {
		h.send(serverMsg{Type: "error", Content: "awaiting shell confirmation; cannot switch input mode"})
		return
	}
	if taskIsActive(h.sessionID) || shellTaskActive(h.sessionID) || peekShellPending(h.sessionID) != nil {
		h.send(serverMsg{Type: "error", Content: "busy; cannot switch input mode"})
		return
	}
	mode := strings.TrimSpace(msg.Mode)
	if mode != "agent" && mode != "shell" {
		return
	}
	if err := h.database.UpdateSessionInputMode(h.sessionID, mode); err != nil {
		slog.Info(fmt.Sprintf("[ws] UpdateSessionInputMode: %v", err))
		return
	}
	h.broadcast(serverMsg{Type: "input_mode_changed", Value: mode, ShellType: shellTypeString()})
}

func (h *wsConn) onShellRun(msg clientMsg) {
	cmd := strings.TrimSpace(msg.Data)
	if cmd == "" {
		return
	}
	if taskIsActive(h.sessionID) {
		h.broadcast(serverMsg{Type: "error", Content: "AI is running"})
		return
	}
	if shellTaskActive(h.sessionID) || peekShellPending(h.sessionID) != nil {
		h.broadcast(serverMsg{Type: "error", Content: "shell busy"})
		return
	}
	sx, err := h.database.GetSession(h.sessionID)
	if err != nil {
		h.broadcast(serverMsg{Type: "error", Content: err.Error()})
		return
	}
	if sx.PermissionMode != "bypassPermissions" {
		// 設為待確認（記憶體）也要過閘門：維護中不產生新的待確認狀態。
		withWorkAdmission(h.send, "", func() {
			setShellPending(h.sessionID, &shellPendingInfo{
				Command:   cmd,
				WorkDir:   sx.WorkDir,
				ShellType: shellTypeString(),
			})
			h.broadcast(serverMsg{
				Type:      "shell_approval_request",
				Content:   cmd,
				WorkDir:   sx.WorkDir,
				ShellType: shellTypeString(),
			})
			h.broadcast(serverMsg{Type: "status", Value: StateShellAwaitingApproval})
		})
		return
	}
	h.beginShellRun(cmd, sx.WorkDir)
}

func (h *wsConn) onShellApprove(msg clientMsg) {
	// 取走待確認指令之前先過閘門：維護中拒絕時待確認仍保留在記憶體中。
	withWorkAdmission(h.send, "", func() {
		if taskIsActive(h.sessionID) || shellTaskActive(h.sessionID) {
			h.broadcast(serverMsg{Type: "error", Content: "another task is running"})
			return
		}
		p := takeShellPending(h.sessionID)
		if p == nil {
			return
		}
		h.beginShellRun(p.Command, p.WorkDir)
	})
}

func (h *wsConn) onAllowOnce(msg clientMsg) {
	// kiroacp 互動式授權：解析 pending 而非重跑。
	if permResolve(h.sessionID, true) {
		return
	}
	if !h.isClaude {
		slog.Info(fmt.Sprintf("[ws] agent=%s: allow_once ignored (no pending request)", h.agentType))
		if clearStaleAwaitingConfirm(h.database, h.sessionID) {
			h.broadcast(serverMsg{Type: "status", Value: idleUIStatus(h.database, h.sessionID)})
		}
		return
	}
	sAllow, err := h.database.GetSession(h.sessionID)
	if err != nil {
		return
	}
	if strings.TrimSpace(sAllow.ShellPending) != "" {
		h.broadcast(serverMsg{Type: "error", Content: "請先處理待確認的 Shell 指令"})
		return
	}
	once := make([]string, 0, len(msg.Tools))
	for _, t := range msg.Tools {
		t = strings.TrimSpace(t)
		if t != "" {
			once = append(once, t)
		}
	}
	if len(once) == 0 {
		slog.Info(fmt.Sprintf("[ws] allow_once: empty tools"))
		return
	}
	// 多人同時回應同一個授權時先到先贏：認領 pending_denials 成功者才繼續，其餘視為已處理。
	// 認領（寫入）之前就要過閘門；維護中拒絕時 pending_denials 保持原樣。
	withWorkAdmission(h.send, "", func() {
		if claimed, err := h.database.ClaimPendingDenials(h.sessionID); err != nil || !claimed {
			slog.Info(fmt.Sprintf("[ws] allow_once 略過：授權已被處理或不存在 (err=%v)", err))
			return
		}
		if err := h.database.UpdateAllowedTools(h.sessionID, nil); err != nil {
			slog.Info(fmt.Sprintf("[ws] UpdateAllowedTools: %v", err))
		}
		h.runAgent("please retry the previous operation", once, 0, nil)
	})
}

func (h *wsConn) onDenyOnce(msg clientMsg) {
	// kiroacp 互動式授權：解析 pending 而非重跑。
	if permResolve(h.sessionID, false) {
		return
	}
	if !h.isClaude {
		slog.Info(fmt.Sprintf("[ws] agent=%s: deny_once ignored (no pending request)", h.agentType))
		if clearStaleAwaitingConfirm(h.database, h.sessionID) {
			h.broadcast(serverMsg{Type: "status", Value: idleUIStatus(h.database, h.sessionID)})
		}
		return
	}
	// 先到先贏：同 allow_once，已被另一端處理就不再重跑。
	// 認領與 pauseQueue（寫入）之前就要過閘門；維護中拒絕時既不認領也不暫停佇列。
	withWorkAdmission(h.send, "", func() {
		if claimed, err := h.database.ClaimPendingDenials(h.sessionID); err != nil || !claimed {
			slog.Info(fmt.Sprintf("[ws] deny_once 略過：授權已被處理或不存在 (err=%v)", err))
			return
		}
		// 使用者拒絕代表計畫被打斷，後面排隊的不自動接著跑。
		h.pauseQueue()
		h.runAgent("[Permission denied by user. Please acknowledge that you cannot perform the requested operation and stop.]", nil, 0, nil)
	})
}

func (h *wsConn) onSetMode(msg clientMsg) {
	if h.agentType != agent.TypeClaude && h.agentType != agent.TypeCursor && h.agentType != agent.TypeAntigravity && h.agentType != agent.TypeGemini && h.agentType != agent.TypeKiro && h.agentType != agent.TypeKiroACP {
		slog.Info(fmt.Sprintf("[ws] agent=%s: set_mode ignored", h.agentType))
		return
	}
	sMode, err := h.database.GetSession(h.sessionID)
	if err != nil {
		return
	}
	if strings.TrimSpace(sMode.ShellPending) != "" {
		h.broadcast(serverMsg{Type: "error", Content: "請先處理待確認的 Shell 指令"})
		return
	}
	clearPendingDenials(h.database, h.sessionID)
	if err := h.database.UpdatePermissionMode(h.sessionID, msg.Mode); err != nil {
		slog.Info(fmt.Sprintf("[ws] UpdatePermissionMode: %v", err))
	}
	// 「允許並記住」會走到這裡：授權已解除但 status 原本不會被改回，重連時 sync 仍顯示等授權。
	if sMode.Status == db.SessionStatusAwaitingConfirm && !taskIsActive(h.sessionID) {
		if err := h.database.UpdateSessionStatus(h.sessionID, db.SessionStatusIdle); err != nil {
			slog.Info(fmt.Sprintf("[ws] UpdateSessionStatus idle (set_mode): %v", err))
		}
	}
	h.broadcast(serverMsg{Type: "status", Value: idleUIStatus(h.database, h.sessionID)})
	slog.Info(fmt.Sprintf("[ws] permission mode: %v", msg.Mode))
	// 等授權期間排隊的訊息：授權解除後接著跑（非失敗，不暫停）。
	h.drainIfIdle()
}

func (h *wsConn) onSetModel(msg clientMsg) {
	if err := h.database.UpdateModel(h.sessionID, strings.TrimSpace(msg.Model)); err != nil {
		slog.Info(fmt.Sprintf("[ws] UpdateModel: %v", err))
	} else if isStreamless(h.agentType) {
		// stream 不含 model，不會有人回填 badge；切換當下就要更新。
		if cur, gerr := h.database.GetSession(h.sessionID); gerr == nil {
			if p := persistInfoUpdate(h.database, h.sessionID, resolveStreamless(cur, h.agentType)); p != nil {
				h.broadcast(serverMsg{Type: "model_update", Model: p})
			}
		}
	}
	h.broadcast(serverMsg{Type: "status", Value: idleUIStatus(h.database, h.sessionID)})
	slog.Info(fmt.Sprintf("[ws] model: %v", msg.Model))
}

func (h *wsConn) onSetEffort(msg clientMsg) {
	if h.agentType == agent.TypeCursor {
		slog.Info(fmt.Sprintf("[ws] agent=%s: set_effort ignored", h.agentType))
		return
	}
	if err := h.database.UpdateEffort(h.sessionID, strings.TrimSpace(msg.Effort)); err != nil {
		slog.Info(fmt.Sprintf("[ws] UpdateEffort: %v", err))
	}
	h.broadcast(serverMsg{Type: "status", Value: idleUIStatus(h.database, h.sessionID)})
	slog.Info(fmt.Sprintf("[ws] effort: %v", msg.Effort))
}

func (h *wsConn) onResetContext(msg clientMsg) {
	// 會清除訊息與佇列，屬於寫入；維護中整段拒絕，佇列與訊息保留。
	withWorkAdmission(h.send, "", func() {
		dl := dispatchLock(h.sessionID)
		dl.Lock()
		defer dl.Unlock()
		fail := func(err error) {
			slog.Info(fmt.Sprintf("[ws] session %s reset failed: %v", h.sessionID, err))
			h.send(serverMsg{Type: "error", Content: err.Error()})
		}
		if h.roomWake && msg.RequireIdle {
			// 工具重置：不取消任何東西；範圍前提與清除在同一個 DB 交易內。
			if err := resetRoomMember(h.database, h.sessionID, msg.RoomID, msg.PMSessionID); err != nil {
				fail(err)
				return
			}
			clearInMemoryShellApproval(h.sessionID)
		} else {
			taskCancel(h.sessionID)
			shellTaskCancel(h.sessionID)
			clearInMemoryShellApproval(h.sessionID)
			if err := h.clearSessionData(); err != nil {
				fail(err)
				return
			}
		}
		clearLiveAgentSession(h.sessionID)
		h.broadcastQueue()
		h.broadcast(serverMsg{Type: "reset"})
		h.broadcast(serverMsg{Type: "status", Value: idleUIStatus(h.database, h.sessionID)})
		slog.Info(fmt.Sprintf("[ws] session %s reset", h.sessionID))
	})
}

// clearSessionData 是一般（UI）重置的資料清除：逐項寫入，任何一項失敗就回錯。
func (h *wsConn) clearSessionData() error {
	for _, step := range []func() error{
		func() error { return h.database.FinalizePendingMessagesForSession(h.sessionID) },
		func() error { return h.database.UpdateAgentSessionID(h.sessionID, "") },
		func() error { return h.database.ClearMessages(h.sessionID) },
		func() error { return h.database.UpdatePendingDenials(h.sessionID, "") },
		func() error { return h.database.UpdateShellPending(h.sessionID, "") },
		func() error { return h.database.ClearQueuedMessages(h.sessionID) },
		func() error { return h.database.UpdateSessionStatus(h.sessionID, db.SessionStatusIdle) },
	} {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}

func (h *wsConn) onInterrupt(msg clientMsg) {
	if shellTaskActive(h.sessionID) {
		// 只 cancel；DB 寫入與 finalize 由 goroutine 的 !done 分支負責
		shellTaskCancel(h.sessionID)
		return
	}
	// 按一次就 cancel context 強制 KillTree（見 taskInterrupt）；
	// 收尾一律由 Run goroutine 的既有邏輯處理，避免前端提早變 IDLE 但進程還在跑。
	// 使用者按停止＝不要自動接著跑，故暫停佇列。
	// kiroacp 等授權時子進程在等我們回覆，直接強制取消。
	if _, waiting := permPending(h.sessionID); waiting || questionPending(h.sessionID) != nil {
		if !dropStalePrompts(h.sessionID) { // 沒有任務支撐的殘留項目直接清掉；有任務則照舊取消
			taskCancel(h.sessionID)
		}
		h.pauseQueue()
		return
	}
	if taskInterrupt(h.sessionID) {
		h.pauseQueue()
	} else if clearStaleAwaitingConfirm(h.database, h.sessionID) {
		h.broadcast(serverMsg{Type: "status", Value: idleUIStatus(h.database, h.sessionID)})
	}
}

func (h *wsConn) onShellExec(msg clientMsg) {
	if !h.shellCfg.Enabled {
		h.broadcast(serverMsg{Type: "error", Content: "直連 Shell 未啟用（請在 config.yaml 設定 shell.enabled: true）"})
		return
	}
	sSh, err := h.database.GetSession(h.sessionID)
	if err != nil {
		return
	}
	if strings.TrimSpace(sSh.ShellPending) != "" {
		h.broadcast(serverMsg{Type: "error", Content: "請先處理待確認的 Shell 指令"})
		return
	}
	if strings.TrimSpace(sSh.PendingDenials) != "" {
		h.broadcast(serverMsg{Type: "error", Content: "請先處理工具授權請求"})
		return
	}
	line := strings.TrimSpace(msg.Data)
	if line == "" {
		return
	}
	slog.Info(fmt.Sprintf("[ws] 收到 shell_exec，長度=%d", len(line)))
	h.runShell(line)
}

func (h *wsConn) onRefreshQuota(msg clientMsg) {
	if h.quotaSvc == nil {
		return
	}
	go func(at string) {
		snap, _ := h.quotaSvc.RefreshManual(context.Background(), at)
		p := snap.ToPayload()
		h.broadcast(serverMsg{Type: "quota_update", Quota: &p})
	}(h.agentType)
}
