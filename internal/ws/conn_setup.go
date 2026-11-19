package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/antigravity"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/claude"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/codex"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/cursor"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/kiro"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/kiroacp"
)

// sendInitialSync 送出連線後的第一則 sync（歷史、佇列、額度、額度自動繼續狀態），登記上線，
// 並在額度快取過期時背景補刷。history_limit 不合法時關閉連線並回傳 false。
func (h *wsConn) sendInitialSync(entry *connEntry) bool {
	historyLimit, err := strconv.Atoi(h.c.Query("history_limit", "0"))
	if h.c.Query("sync_messages") == "0" {
		historyLimit = -1
	} else if err != nil || historyLimit < 0 || historyLimit > 200 {
		h.c.Close()
		return false
	}
	syncData, err := buildSyncPayload(h.database, h.sessionID, historyLimit)
	if err != nil {
		slog.Info(fmt.Sprintf("[ws] buildSyncPayload: %v", err))
		syncData = SyncPayload{UIState: StateIdle, InputMode: "agent", ShellType: shellTypeString()}
	}
	syncMsg := serverMsg{
		Type:           "sync",
		Value:          syncData.UIState,
		Messages:       syncData.Messages,
		HistoryHasMore: syncData.HistoryHasMore,
		InputMode:      syncData.InputMode,
		ShellType:      syncData.ShellType,
		ShellPending:   syncData.ShellPendingCmd,
	}
	if h.quotaSvc != nil && agent.IsEnabled(h.agentType) {
		q := h.quotaSvc.Get(h.agentType)
		p := q.ToPayload()
		syncMsg.Quota = &p
	}
	if h.agentType == agent.TypeTelegram {
		syncMsg.SyncState = telegramSyncStateFn(h.sessionID)
		syncMsg.TelegramScope = syncData.TelegramScope
	}
	syncMsg.MaxMessageBytes, _ = h.c.Locals("max_message_bytes").(int)
	syncMsg.Model = sessionModelPayload(h.sess)
	syncMsg.Queue, syncMsg.QueuePaused = loadQueueState(h.database, h.sessionID)
	if state, at, _, err := h.database.GetQuotaResume(h.sessionID); err != nil {
		slog.Info(fmt.Sprintf("[ws] quota resume sync: %v", err))
	} else {
		syncMsg.QuotaResumeState = stringPtr(state)
		syncMsg.QuotaResumeAt = stringPtr(at)
	}
	syncMsg.Lingering = syncData.UIState == StateIdle && taskIsActive(h.sessionID)
	h.send(syncMsg.withTaskTiming(h.sessionID))
	announcePresence(h.sessionID, "join", entry.displayName(), h.isGuest)

	// 進入會話時若 quota cache 已過期，背景補打一次並推播更新（cache 未過期則 RefreshAfterRun 內部直接跳過）。
	if h.quotaSvc != nil && agent.IsEnabled(h.agentType) {
		go func(at string) {
			snap, err := h.quotaSvc.RefreshAfterRun(context.Background(), at)
			if err != nil {
				slog.Info(fmt.Sprintf("[quota] refresh on connect %s: %v", at, err))
			}
			p := snap.ToPayload()
			h.broadcast(serverMsg{Type: "quota_update", Quota: &p})
		}(h.agentType)
	}
	return true
}

// restoreState 讀取 Shell 逾時設定，並把重連前尚未處理的授權、提問與 Shell 確認重新推給這條連線。
func (h *wsConn) restoreState() {
	h.shellTimeoutSec = 60
	if ts := strings.TrimSpace(h.shellCfg.Timeout); ts != "" {
		if d, err := time.ParseDuration(ts); err == nil {
			if sec := int(d.Seconds()); sec > 0 {
				h.shellTimeoutSec = sec
			}
		}
	}

	if h.isClaude && h.sess.PendingDenials != "" {
		h.send(serverMsg{Type: "permission_request", Tools: json.RawMessage(h.sess.PendingDenials), RetryTools: true})
		slog.Info(fmt.Sprintf("[ws] restored pending_denials session=%s", h.sessionID))
	}
	if tools, ok := permPending(h.sessionID); ok {
		h.send(serverMsg{Type: "permission_request", Tools: tools})
	}
	if q := questionPending(h.sessionID); q != nil {
		h.send(serverMsg{Type: "question_request", Question: q})
	}

	if strings.TrimSpace(h.sess.ShellPending) != "" {
		var p shellPendingPayload
		if err := json.Unmarshal([]byte(h.sess.ShellPending), &p); err == nil {
			if absDir, err := resolveShellWorkDir(h.sess); err == nil {
				h.send(serverMsg{Type: "shell_command_request", Command: p.Cmd, Line: p.Line, WorkDirKey: absDir})
			}
		}
		slog.Info(fmt.Sprintf("[ws] 還原 shell_pending for session %s", h.sessionID))
	}
}

// initDispatcher 建立佇列分派器；drainIfIdle／resumeQueue 在 runAgent 之後才可用，所以放在最後。
func (h *wsConn) initDispatcher() {
	actor := "owner"
	if h.isGuest {
		actor = fmt.Sprintf("share:%d:%s", h.shareID, h.guestNick)
	}
	h.dispatcher = &sessionDispatcher{
		database: h.database, sessionID: h.sessionID, actor: actor, author: h.author,
		start: h.startInput, reply: h.send, broadcastQueue: h.broadcastQueue, broadcast: h.broadcast,
		quotaGuard: quotaGuardFor(h.quotaSvc, h.agentType, h.ho.quotaGuardPercent, h.ho.quotaGuardSettings),
	}
	h.drainIfIdle = h.dispatcher.Drain
	h.resumeQueue = h.dispatcher.Resume
}
