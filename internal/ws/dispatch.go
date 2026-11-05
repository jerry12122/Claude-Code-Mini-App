package ws

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

// sessionDispatcher owns admission/queue ordering. Transport only supplies replies
// and the existing runner callback; reconnecting does not create another policy.
type sessionDispatcher struct {
	database                 *db.DB
	sessionID, actor, author string
	start                    func(clientMsg, *db.QueuedMessage) error
	reply                    func(serverMsg) bool
	broadcastQueue           func()
	broadcast                func(serverMsg)
}

func (d *sessionDispatcher) busy(s *db.Session) bool {
	return taskIsActive(d.sessionID) || shellTaskActive(d.sessionID) || strings.TrimSpace(s.PendingDenials) != ""
}

// drainLocked requires the session dispatch lock. Failed preparation leaves the
// queued item untouched and paused, preserving the existing attachment guarantee.
func (d *sessionDispatcher) drainLocked() {
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
				d.drainLocked()
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
		if _, err := d.database.AcceptInput(d.sessionID, d.actor, input.RequestID, input.Data, d.author, input.AttachmentIDs); err != nil {
			uncertain(err)
			return
		}
		// Acceptance is durable before replying, even when no runner can start yet.
		d.reply(serverMsg{Type: "input_accepted", RequestID: input.RequestID})
		d.broadcastQueue()
		if !d.busy(s) {
			d.drainLocked()
		}
		return
	}
	// Legacy clients without a request ID keep their original input contract.
	n, err := d.database.CountQueuedMessages(d.sessionID)
	if err != nil {
		reject(err)
		return
	}
	if d.busy(s) || n > 0 {
		if _, err := d.database.EnqueueMessage(d.sessionID, input.Data, d.author, input.AttachmentIDs...); err != nil {
			reject(err)
			return
		}
		d.broadcastQueue()
		if !d.busy(s) {
			d.drainLocked()
		}
		return
	}
	if err := d.start(input, nil); err != nil {
		reject(err)
	}
}

func (d *sessionDispatcher) Drain() {
	release, err := admitWork()
	if err != nil {
		return
	}
	defer release()
	dl := dispatchLock(d.sessionID)
	dl.Lock()
	defer dl.Unlock()
	if s, err := d.database.GetSession(d.sessionID); err == nil && !d.busy(s) && strings.TrimSpace(s.ShellPending) == "" {
		d.drainLocked()
	}
	d.broadcastQueue()
}

func (d *sessionDispatcher) Resume() {
	release, err := admitWork()
	if err != nil {
		d.reply(updateInProgressMsg(""))
		return
	}
	defer release()
	if err := d.database.SetQueuePaused(d.sessionID, false); err != nil {
		d.reply(serverMsg{Type: "error", Content: "無法恢復佇列，請稍後再試"})
		return
	}
	d.Drain()
}
