package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/media"
)

// ChangeQueueBatch 僅安排／解組，不改 paused/quota，不取消或啟動任何回合。
// Room HTTP 與私人 WS 共用同一把 dispatchLock，DB 仍在交易內重驗 PM/CAS。
func ChangeQueueBatch(ctx context.Context, database *db.DB, roomID, sessionID, requestID string, items []db.QueueBatchItem, cancelID string) (string, error) {
	release, err := admitWork()
	if err != nil {
		return "", err
	}
	defer release()
	dl := dispatchLock(sessionID)
	dl.Lock()
	defer dl.Unlock()
	s, err := database.GetSession(sessionID)
	if err != nil {
		return "", db.ErrQueueBatchConflict
	}
	if !queueBatchSupported(s) {
		return "", db.ErrQueueBatchForbidden
	}
	if cancelID != "" {
		return cancelID, database.CancelQueueBatch(ctx, roomID, sessionID, cancelID)
	}
	id, err := database.ScheduleQueueBatch(ctx, roomID, sessionID, requestID, items)
	return id, err
}

func QueueBatchError(err error) (string, string) {
	switch {
	case errors.Is(err, db.ErrQueueBatchInvalid), errors.Is(err, db.ErrInvalidAttachment):
		return "invalid", "批次資料或附件無效，請重新載入"
	case errors.Is(err, db.ErrQueueBatchForbidden):
		return "forbidden", "此來源不可一起送"
	case errors.Is(err, db.ErrQueueBatchConflict), errors.Is(err, db.ErrRoomPM), errors.Is(err, db.ErrRoomPMChanged):
		return "conflict", "佇列或 PM 已變更，請重新載入；已安排批次請先取消再修改"
	case errors.Is(err, errUpdateInProgress):
		return codeUpdateInProgress, "系統維護中，請稍後再試"
	default:
		return "failed", "一起送失敗，請稍後再試"
	}
}

func (h *wsConn) onQueueBatch(msg clientMsg) {
	reply := func(id string, err error) {
		out := serverMsg{Type: "queue_batch_success", RequestID: msg.RequestID, BatchID: id}
		if err != nil {
			out.Type = "queue_batch_error"
			out.Code, out.Content = QueueBatchError(err)
		}
		h.send(out)
	}
	if h.isGuest || h.roomWake || h.mcpAgent {
		reply("", db.ErrQueueBatchForbidden)
		return
	}
	if msg.invalidUTF8 || msg.RequestID == "" || len(msg.RequestID) > 128 || (msg.Type == "queue_batch_cancel" && (msg.BatchID == "" || len(msg.BatchID) > 128)) {
		reply("", db.ErrQueueBatchInvalid)
		return
	}
	cancelID := ""
	if msg.Type == "queue_batch_cancel" {
		cancelID = msg.BatchID
	}
	id, err := ChangeQueueBatch(context.Background(), h.database, "", h.sessionID, msg.RequestID, msg.BatchItems, cancelID)
	reply(id, err)
	if err != nil {
		return
	}
	h.broadcastQueue()
	// 完成／取消的操作 receipt 重送只確認舊結果，不用它喚醒後來的新輸入。
	pending, checkErr := h.database.QueueBatchPending(h.sessionID, id)
	if cancelID == "" && checkErr == nil && pending {
		h.dispatcher.drain(true)
	}
}

func (h *wsConn) startQueuedBatch(q *db.QueuedMessage) error {
	if _, _, err := h.database.QueueBatchInput(h.sessionID, *q); err != nil {
		return err
	}
	rows, err := h.database.ListQueuedMessages(h.sessionID)
	if err != nil {
		return err
	}
	var batch []db.QueuedMessage
	for _, row := range rows {
		if row.BatchID == q.BatchID {
			batch = append(batch, row)
		}
	}
	prompt, err := batchDispatchPrompt(h.database, h.sessionID, batch)
	if err != nil {
		return err
	}
	if q.Author != "" {
		prompt = "[" + db.RoomMessageAuthor(q.Author) + "] " + prompt
	}
	msgs, err := h.database.PromoteQueuedBatch(context.Background(), h.sessionID, *q)
	if err != nil {
		return err
	}
	for _, m := range msgs {
		h.broadcast(serverMsg{Type: "user_message", ID: m.ID, Content: m.Content, CreatedAt: m.CreatedAt, Attachments: m.Attachments, Author: m.Author})
	}
	// 所有 user 原列均先提交，最後一筆是本輪回覆的原來源；不把合送文字寫進 history。
	h.runAgent(prompt, nil, msgs[len(msgs)-1].ID, nil)
	return nil
}

// batchDispatchPrompt 依佇列順序標出每則訊息與它自己的照片／檔案。路徑沿用既有上傳檔，不另複製。
func batchDispatchPrompt(database *db.DB, sessionID string, rows []db.QueuedMessage) (string, error) {
	if len(rows) < 2 {
		return "", db.ErrQueueBatchConflict
	}
	var b strings.Builder
	for i, row := range rows {
		fmt.Fprintf(&b, "訊息%d\n%s\n", i+1, row.Content)
		atts, err := database.ResolveAttachments(sessionID, row.AttachmentIDs)
		if err != nil {
			return "", err
		}
		photos, files := 0, 0
		for _, att := range atts {
			path, err := media.ExistingUploadPath(att.SessionID, att.StorageName)
			if err != nil {
				return "", err
			}
			if strings.HasPrefix(strings.ToLower(att.MimeType), "image/") {
				photos++
				fmt.Fprintf(&b, "照片%d-%d id=%s %s\n路徑：%s\n", i+1, photos, att.ID, att.Name, path)
			} else {
				files++
				fmt.Fprintf(&b, "附件%d-%d id=%s %s\n路徑：%s\n", i+1, files, att.ID, att.Name, path)
			}
		}
	}
	return b.String(), nil
}

// queueBatchSupported 是手動一起送與 idle 自動取出共用的來源規則。shell、Telegram、已停用的 agent 維持逐筆。
func queueBatchSupported(s *db.Session) bool {
	if s == nil {
		return false
	}
	return s.InputMode != "shell" && s.AgentType != agent.TypeTelegram && agent.DisabledReason(s.AgentType) == ""
}

func queueBatchBlocked(s *db.Session, sessionID string) bool {
	_, permission := permPending(sessionID)
	return permission || questionPending(sessionID) != nil || peekShellPending(sessionID) != nil || strings.TrimSpace(s.ShellPending) != ""
}

func (h *wsConn) rejectMalformedQueueBatch(raw []byte) {
	var fields map[string]json.RawMessage
	var kind, requestID string
	if json.Unmarshal(raw, &fields) != nil || json.Unmarshal(fields["type"], &kind) != nil || (kind != "queue_batch" && kind != "queue_batch_cancel") {
		return
	}
	if json.Unmarshal(fields["request_id"], &requestID) != nil || len(requestID) > 128 {
		requestID = ""
	}
	h.send(serverMsg{Type: "queue_batch_error", RequestID: requestID, Code: "invalid", Content: "請提供有效的批次資料"})
}

// UpdateRoomMembers 讓 HTTP 交接與批次 promote→runAgent 的整段啟動共用 PM 鎖，
// 避免歷史剛提交、running 尚未寫回時被交接；DB 的 PM CAS 仍是最後一道保障。
func UpdateRoomMembers(ctx context.Context, database *db.DB, roomID string, name *string, members *[]db.RoomMember, expected ...string) error {
	pm, err := database.RoomPM(roomID)
	if err != nil {
		return database.UpdateRoom(roomID, name, members, expected...)
	}
	dl := dispatchLock(pm)
	dl.Lock()
	defer dl.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	current, err := database.RoomPM(roomID)
	if err != nil || current != pm {
		return db.ErrRoomPMChanged
	}
	return database.UpdateRoom(roomID, name, members, expected...)
}
