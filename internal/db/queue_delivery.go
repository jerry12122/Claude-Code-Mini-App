package db

import (
	"context"
	"strings"

	"github.com/google/uuid"
)

// ApplyIdleDelivery 只在即將取出佇列時呼叫。逐筆回傳第一則且不寫批次；
// 一起送只把當時連續、同會話、同來源且來源合法的前綴做成既有批次。超限不截斷、不消耗。
func (db *DB) ApplyIdleDelivery(ctx context.Context, sessionID string) (QueuedMessage, error) {
	items, err := db.ListQueuedMessages(sessionID)
	if err != nil || len(items) == 0 {
		return QueuedMessage{}, err
	}
	if items[0].BatchID != "" {
		return items[0], nil
	}
	settings, err := db.GetGeneralSettings()
	if err != nil {
		return QueuedMessage{}, err
	}
	if settings.QueueDelivery != QueueDeliveryBatch {
		return items[0], nil
	}
	prefix := idleBatchPrefix(items)
	if len(prefix) < 2 {
		return items[0], nil
	}
	size := 0
	for _, item := range prefix {
		size += len(item.Content)
	}
	if len(prefix) > 100 || size > 16<<20 {
		return QueuedMessage{}, ErrQueueBatchInvalid
	}
	roomID := ""
	if strings.HasPrefix(prefix[0].Author, RoomUserAuthorPrefix) {
		roomID = strings.TrimPrefix(prefix[0].Author, RoomUserAuthorPrefix)
	}
	batch := make([]QueueBatchItem, len(prefix))
	for i := range prefix {
		text := prefix[i].Content
		batch[i] = QueueBatchItem{ID: prefix[i].ID, Previous: &text}
	}
	id, err := db.ScheduleQueueBatch(ctx, roomID, sessionID, "idle-"+uuid.NewString(), batch)
	if err != nil {
		return QueuedMessage{}, err
	}
	again, err := db.ListQueuedMessages(sessionID)
	if err != nil {
		return QueuedMessage{}, err
	}
	if len(again) == 0 || again[0].ID != prefix[0].ID || again[0].BatchID != id {
		return QueuedMessage{}, ErrQueueBatchConflict
	}
	return again[0], nil
}

func idleBatchPrefix(items []QueuedMessage) []QueuedMessage {
	if len(items) == 0 || !idleBatchItem(items[0]) {
		return nil
	}
	author, source := items[0].Author, items[0].SourceSessionID
	var out []QueuedMessage
	for _, item := range items {
		if item.BatchID != "" || item.Author != author || item.SourceSessionID != source || !idleBatchItem(item) {
			break
		}
		out = append(out, item)
	}
	return out
}

func idleBatchItem(item QueuedMessage) bool {
	if item.SourceSessionID != "" || item.BatchID != "" {
		return false
	}
	if item.Author == "" {
		return true
	}
	roomID := strings.TrimPrefix(item.Author, RoomUserAuthorPrefix)
	return strings.HasPrefix(item.Author, RoomUserAuthorPrefix) && roomID != ""
}
