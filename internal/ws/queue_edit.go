package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

// EditRoomQueuedMessage 與 Dispatch／Promote／RunNow 使用同一把鎖。
// DB 在鎖內再次核對現任 PM；交接後不自動改鎖新 PM 或重試。
func EditRoomQueuedMessage(ctx context.Context, database *db.DB, roomID, expectedPM string, id int64, previous, content string) error {
	dl := dispatchLock(expectedPM)
	dl.Lock()
	defer dl.Unlock()
	if err := database.EditRoomQueuedMessage(ctx, roomID, expectedPM, id, previous, content); err != nil {
		return err
	}
	msg, err := queueUpdate(database, expectedPM)
	if err != nil {
		return err
	}
	hub.Broadcast(expectedPM, msg)
	return nil
}

// EditPrivateQueuedMessage 與 Dispatch／Promote／RunNow 使用同一把鎖。只改文字：不 Drain、不送 input、
// 不碰 receipt／附件／FIFO／paused／quota。編輯已提交後，廣播失敗只記 log，不把成功說成失敗。
func EditPrivateQueuedMessage(ctx context.Context, database *db.DB, sessionID, requestID string, id int64, previous, content string) error {
	dl := dispatchLock(sessionID)
	dl.Lock()
	defer dl.Unlock()
	if err := database.EditQueuedMessage(ctx, sessionID, id, previous, content); err != nil {
		return err
	}
	msg, err := queueUpdate(database, sessionID)
	if err != nil {
		slog.Info(fmt.Sprintf("[ws] queue_edit 已儲存但 queue_update 失敗: %v", err))
		return nil
	}
	msg.RequestID = requestID
	hub.Broadcast(sessionID, msg)
	return nil
}

const queueEditMaxRequestID = 128

// onQueueEdit 只服務人類 owner 的連線；guest（含 editor）與內部連線（工作室喚醒、MCP agent）一律拒絕。
// 來源（author／source）只由 DB 的精確比對決定，訊息裡的任何 author／source 欄位都不讀。
func (h *wsConn) onQueueEdit(msg clientMsg) {
	reply := func(typ, code, text string) {
		h.send(serverMsg{Type: typ, RequestID: msg.RequestID, ID: msg.ID, Code: code, Content: text})
	}
	if h.isGuest || h.roomWake || h.mcpAgent {
		reply("queue_edit_error", "forbidden", "只有擁有者可以編輯佇列")
		return
	}
	if msg.invalidUTF8 {
		reply("queue_edit_error", "invalid", "訊息含無效的 UTF-8，未儲存")
		return
	}
	previous, okPrevious := queueEditString(msg.Previous)
	content, okContent := queueEditString(msg.EditContent)
	if msg.ID <= 0 || msg.RequestID == "" || len(msg.RequestID) > queueEditMaxRequestID || !okPrevious || !okContent {
		reply("queue_edit_error", "invalid", "請提供有效的佇列 ID、request_id、previous 與 content")
		return
	}
	err := EditPrivateQueuedMessage(context.Background(), h.database, h.sessionID, msg.RequestID, msg.ID, previous, content)
	switch {
	case err == nil:
		reply("queue_edit_success", "", "")
	case errors.Is(err, db.ErrQueueEditInvalid), errors.Is(err, db.ErrInvalidAttachment):
		reply("queue_edit_error", "invalid", err.Error())
	case errors.Is(err, db.ErrQueueEditConflict):
		reply("queue_edit_error", "conflict", err.Error())
	case errors.Is(err, db.ErrQueueEditForbidden):
		reply("queue_edit_error", "forbidden", err.Error())
	default:
		slog.Info(fmt.Sprintf("[ws] queue_edit: %v", err))
		reply("queue_edit_error", "failed", "編輯失敗，請稍後再試")
	}
}

// queueEditString 要求明確的 JSON 字串：缺欄、null、其他型別都不算。
func queueEditString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	return s, true
}

// rejectMalformedQueueEdit 補一個可讀的拒絕：id／request_id 型別錯時整則 JSON 無法解析，
// readLoop 原本會靜默丟棄，UI 就會永遠等不到結果。只針對 type=queue_edit，不改其他訊息的行為。
func (h *wsConn) rejectMalformedQueueEdit(raw []byte) {
	var fields map[string]json.RawMessage
	var kind string
	if json.Unmarshal(raw, &fields) != nil || json.Unmarshal(fields["type"], &kind) != nil || kind != "queue_edit" {
		return
	}
	out := serverMsg{Type: "queue_edit_error", Code: "invalid", Content: "請提供有效的佇列 ID、request_id、previous 與 content"}
	var requestID string
	if json.Unmarshal(fields["request_id"], &requestID) == nil && len(requestID) <= queueEditMaxRequestID {
		out.RequestID = requestID
	}
	var id int64
	if json.Unmarshal(fields["id"], &id) == nil {
		out.ID = id
	}
	h.send(out)
}
