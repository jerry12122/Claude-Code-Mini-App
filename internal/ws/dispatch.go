package ws

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

const runNowNotice = "上一輪回覆已被使用者中途打斷，以下為新指示。"

func supportsRunNow(agentType string) bool {
	switch agentType {
	case "claude", "codex", "cursor":
		return true
	default:
		return false
	}
}

// entry 已移除仍須保留舊回合的通知，避免等待逾時後普通送出或重試重疊 resume。
var runNowFinishing sync.Map // sessionID → <-chan struct{}

func runNowPending(sessionID string) <-chan struct{} {
	v, ok := runNowFinishing.Load(sessionID)
	if !ok {
		return nil
	}
	done := v.(<-chan struct{})
	select {
	case <-done:
		runNowFinishing.CompareAndDelete(sessionID, done)
		return nil
	default:
		return done
	}
}

// sessionDispatcher owns admission/queue ordering. Transport only supplies replies
// and the existing runner callback; reconnecting does not create another policy.
type sessionDispatcher struct {
	database                 *db.DB
	sessionID, actor, author string
	start                    func(clientMsg, *db.QueuedMessage) error
	reply                    func(serverMsg) bool
	broadcastQueue           func()
	broadcast                func(serverMsg)
	// quotaGuard 非 nil 時，伺服器自動啟動佇列下一則前先呼叫；回傳非空字串＝額度不足的原因，需暫停佇列。
	quotaGuard func() string
	runNowWait time.Duration
}

func (d *sessionDispatcher) busy(s *db.Session) bool {
	return queueBatchBlocked(s, d.sessionID) || runNowPending(d.sessionID) != nil || taskIsActive(d.sessionID) || shellTaskActive(d.sessionID) || strings.TrimSpace(s.PendingDenials) != ""
}

func (d *sessionDispatcher) runNowItem(id int64) (*db.QueuedMessage, error) {
	s, err := d.database.GetSession(d.sessionID)
	if err != nil {
		return nil, err
	}
	if !supportsRunNow(s.AgentType) {
		return nil, fmt.Errorf("此 agent 不支援立刻執行")
	}
	_, permission := permPending(d.sessionID)
	if permission || questionPending(d.sessionID) != nil || strings.TrimSpace(s.PendingDenials) != "" {
		return nil, fmt.Errorf("請先處理待確認的授權或問題")
	}
	if shellTaskActive(d.sessionID) || peekShellPending(d.sessionID) != nil || strings.TrimSpace(s.ShellPending) != "" {
		return nil, fmt.Errorf("請先處理 Shell 任務或待確認指令")
	}
	items, err := d.database.ListQueuedMessages(d.sessionID)
	if err != nil {
		return nil, err
	}
	for _, q := range items {
		if q.ID == id {
			if q.BatchID != "" {
				return nil, db.ErrQueueBatchConflict
			}
			if _, err := attachmentPrompt(d.database, d.sessionID, q.Content, q.AttachmentIDs); err != nil {
				return nil, err
			}
			return &q, nil
		}
	}
	return nil, fmt.Errorf("排隊訊息不存在")
}

func (d *sessionDispatcher) RunNow(id int64) {
	release, err := admitWork()
	if err != nil {
		d.reply(updateInProgressMsg(""))
		return
	}
	defer release()
	dl := dispatchLock(d.sessionID)
	dl.Lock()
	defer dl.Unlock()
	q, err := d.runNowItem(id)
	if err != nil {
		d.reply(serverMsg{Type: "error", Content: err.Error()})
		return
	}
	done := runNowPending(d.sessionID)
	if done == nil {
		if oldDone, ok := taskInterruptForRunNow(d.sessionID); ok {
			done = oldDone
			runNowFinishing.Store(d.sessionID, done)
			go func(done <-chan struct{}) {
				<-done
				runNowFinishing.CompareAndDelete(d.sessionID, done)
			}(done)
		}
	}
	if done != nil {
		wait := d.runNowWait
		if wait <= 0 {
			wait = 15 * time.Second
		}
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
			select {
			case <-done:
			default:
				d.reply(serverMsg{Type: "error", Content: "等待逾時，舊回合仍在收尾；訊息保留在佇列，請稍後重試"})
				return
			}
		}
	}
	if _, err := d.runNowItem(id); err != nil {
		d.reply(serverMsg{Type: "error", Content: err.Error()})
		return
	}
	if taskIsActive(d.sessionID) {
		d.reply(serverMsg{Type: "error", Content: "已有另一個回合啟動，請稍後重試"})
		return
	}
	if err := d.start(clientMsg{Data: q.Content, AttachmentIDs: q.AttachmentIDs, interrupted: done != nil}, q); err != nil {
		d.reply(serverMsg{Type: "error", Content: err.Error()})
	}
	d.broadcastQueue()
}

// drainLocked requires the session dispatch lock. Failed preparation leaves the
// queued item untouched and paused, preserving the existing attachment guarantee.
// guarded 只在伺服器自動啟動（任務收尾、重連、授權解除）時為 true；使用者直接送出與按「繼續」不受額度守門影響。
func (d *sessionDispatcher) drainLocked(guarded bool) {
	if paused, err := d.database.QueuePaused(d.sessionID); err != nil || paused {
		return
	}
	items, err := d.database.ListQueuedMessages(d.sessionID)
	if err != nil {
		slog.Info("[ws] ListQueuedMessages", "err", err)
		return
	}
	if len(items) == 0 {
		return
	}
	if guarded && d.quotaGuard != nil {
		if reason := d.quotaGuard(); reason != "" {
			_ = d.database.SetQueuePaused(d.sessionID, true)
			_ = d.database.SetSessionLastError(d.sessionID, reason)
			d.broadcast(serverMsg{Type: "error", Content: reason})
			return
		}
	}
	q := items[0]
	if err := d.start(clientMsg{Data: q.Content, AttachmentIDs: q.AttachmentIDs}, &q); err != nil {
		_ = d.database.SetQueuePaused(d.sessionID, true)
		d.broadcast(serverMsg{Type: "error", Content: err.Error()})
	}
	d.broadcastQueue()
}

func (d *sessionDispatcher) Dispatch(input clientMsg) {
	reject := func(err error) {
		kind := "error"
		if input.RequestID != "" {
			kind = "input_rejected"
		}
		d.reply(serverMsg{Type: kind, RequestID: input.RequestID, Content: err.Error()})
	}
	uncertain := func(err error) {
		if errors.Is(err, db.ErrInputConflict) {
			reject(err)
			return
		}
		slog.Error("[ws] input acceptance unavailable", "session", d.sessionID, "err", err)
		d.reply(serverMsg{Type: "input_rejected", RequestID: input.RequestID, Code: "retry_input", Content: "暫時無法確認訊息，請稍後使用原訊息重試"})
	}
	if len(input.RequestID) > 128 {
		reject(fmt.Errorf("訊息 ID 不可超過 128 bytes"))
		return
	}
	if strings.TrimSpace(input.Data) == "" && len(input.AttachmentIDs) == 0 {
		reject(fmt.Errorf("請輸入訊息或加入附件"))
		return
	}
	release, err := admitWork()
	if err != nil {
		d.reply(updateInProgressMsg(input.RequestID))
		return
	}
	defer release()
	dl := dispatchLock(d.sessionID)
	dl.Lock()
	defer dl.Unlock()
	if input.RequestID != "" {
		accepted, err := d.database.InputAccepted(d.sessionID, d.actor, input.RequestID, input.Data, d.author, input.AttachmentIDs)
		if err != nil {
			uncertain(err)
			return
		}
		if accepted {
			d.reply(serverMsg{Type: "input_accepted", RequestID: input.RequestID})
			if s, err := d.database.GetSession(d.sessionID); err == nil && !d.busy(s) && strings.TrimSpace(s.ShellPending) == "" {
				d.drainLocked(false)
			}
			return
		}
	}
	if _, err := attachmentPrompt(d.database, d.sessionID, input.Data, input.AttachmentIDs); err != nil {
		reject(err)
		return
	}
	s, err := d.database.GetSession(d.sessionID)
	if err != nil {
		reject(err)
		return
	}
	if strings.TrimSpace(s.ShellPending) != "" {
		reject(fmt.Errorf("請先處理待確認的 Shell 指令"))
		return
	}
	if input.RequestID != "" {
		// 新接受與取消舊排程同一筆交易。失敗時不留 receipt／佇列，也不啟動。
		// 同一個 request_id 重送不會再次取消。
		if _, err := d.database.AcceptInputCancelQuota(d.sessionID, d.actor, input.RequestID, input.Data, d.author, input.AttachmentIDs); err != nil {
			uncertain(err)
			return
		}
		d.reply(serverMsg{Type: "input_accepted", RequestID: input.RequestID})
		d.broadcastQueue()
		if !d.busy(s) {
			d.drainLocked(false)
		}
		return
	}
	// 舊客戶端沒有 request_id：先把新訊息與取消一起提交，再啟動。
	// 不能先 start 再取消，否則新 run 可能已經武裝，晚到的取消會清掉新排程。
	if _, err := d.database.EnqueueAgentMessageCancelQuota(d.sessionID, input.Data, d.author, input.SourceSessionID, input.agentMessage, input.AttachmentIDs...); err != nil {
		reject(err)
		return
	}
	d.broadcastQueue()
	if !d.busy(s) {
		d.drainLocked(false)
	}
}

// Drain 是伺服器自動啟動佇列的入口，會過額度守門。
func (d *sessionDispatcher) Drain() { d.drain(true) }

func (d *sessionDispatcher) drain(guarded bool) {
	release, err := admitWork()
	if err != nil {
		return
	}
	defer release()
	dl := dispatchLock(d.sessionID)
	dl.Lock()
	defer dl.Unlock()
	if s, err := d.database.GetSession(d.sessionID); err == nil && !d.busy(s) && strings.TrimSpace(s.ShellPending) == "" {
		d.drainLocked(guarded)
	}
	d.broadcastQueue()
}

// Resume 是使用者按「繼續」。先清掉自動排程，再沿原本的手動恢復啟動一次，不過額度守門。
func (d *sessionDispatcher) Resume() {
	d.resume(true)
}

// ResumeAutomatic 是內部 quota_resume 搶到這一個世代之後的啟動。不清排程，也不把次數歸零。
func (d *sessionDispatcher) ResumeAutomatic() {
	d.resume(false)
}

func (d *sessionDispatcher) resume(cancelSchedule bool) {
	release, err := admitWork()
	if err != nil {
		d.reply(updateInProgressMsg(""))
		return
	}
	defer release()
	if cancelSchedule {
		if err := d.database.CancelQuotaResume(d.sessionID); err != nil {
			d.reply(serverMsg{Type: "error", Content: "無法取消自動繼續，請稍後再試"})
			return
		}
	}
	if err := d.database.SetQueuePaused(d.sessionID, false); err != nil {
		d.reply(serverMsg{Type: "error", Content: "無法恢復佇列，請稍後再試"})
		return
	}
	d.drain(false) // 使用者明確按「繼續」，不再被額度守門擋回
}
