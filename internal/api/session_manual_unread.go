package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"

	"github.com/gofiber/fiber/v2"
)

func readManualUnreadIntent(raw, revision json.RawMessage) (mark, clear bool, rev int64, status int, msg string) {
	if raw == nil {
		return false, false, 0, 0, ""
	}
	token := bytes.TrimSpace(raw)
	switch {
	case bytes.Equal(token, []byte("true")):
		return true, false, 0, 0, ""
	case bytes.Equal(token, []byte("false")):
		rev, ok := parseNonNegativeInt(revision)
		if !ok {
			return false, false, 0, 400, "manual_unread_revision 必須是非負整數"
		}
		return false, true, rev, 0, ""
	default:
		return false, false, 0, 400, "manual_unread 必須是 true 或 false"
	}
}

func validateReadMessageIDs(ids *[]int64, markRead *bool, clearManual bool) (int, string) {
	if ids == nil {
		return 0, ""
	}
	if clearManual {
		if len(*ids) > 1000 {
			return 400, "read_message_ids 最多 1000 個"
		}
		for _, id := range *ids {
			if id <= 0 {
				return 400, "訊息 ID 必須為正整數"
			}
		}
		return 0, ""
	}
	if markRead == nil || !*markRead || len(*ids) == 0 || len(*ids) > 1000 {
		return 400, "read_message_ids 需搭配 mark_read 並包含 1 到 1000 個訊息 ID"
	}
	for _, id := range *ids {
		if id <= 0 {
			return 400, "訊息 ID 必須為正整數"
		}
	}
	return 0, ""
}

func (h *SessionHandler) writeManualUnread(c *fiber.Ctx, id string, mark, clear bool, revision int64, readIDs *[]int64) (bool, error) {
	if mark {
		if err := h.db.MarkSessionManualUnread(id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return true, c.Status(404).JSON(fiber.Map{"error": "session 不存在"})
			}
			return true, c.Status(500).JSON(fiber.Map{"error": "手動標記資料庫錯誤"})
		}
		if h.db.OnSessionChange != nil {
			h.db.OnSessionChange()
		}
	}
	if clear {
		var ids []int64
		if readIDs != nil {
			ids = *readIDs
		}
		res, err := h.db.ApplyObservedReads([]db.ObservedRead{{
			ID: id, Revision: revision, MessageIDs: ids,
		}})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return true, c.Status(404).JSON(fiber.Map{"error": "session 不存在"})
			}
			return true, c.Status(500).JSON(fiber.Map{"error": "手動標記資料庫錯誤"})
		}
		if res.Changed && h.db.OnSessionChange != nil {
			h.db.OnSessionChange()
		}
		if res.Conflict {
			state := res.States[0]
			return true, c.Status(409).JSON(fiber.Map{
				"error": "manual_unread_revision 已變更",
				"id":    state.ID, "manual_unread": state.ManualUnread, "manual_unread_revision": state.Revision,
			})
		}
	}
	return false, nil
}

func parseNonNegativeInt(raw json.RawMessage) (int64, bool) {
	raw = bytes.TrimSpace(raw)
	// Go 1.26 的 json.Number 會接受數字形狀的字串。revision 只收 JSON 數字字面量。
	if len(raw) == 0 || raw[0] < '0' || raw[0] > '9' {
		return 0, false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var n json.Number
	if err := dec.Decode(&n); err != nil || dec.More() {
		return 0, false
	}
	i, err := n.Int64()
	if err != nil || i < 0 {
		return 0, false
	}
	return i, true
}

func parseObservedReadAll(raw []byte) ([]db.ObservedRead, string) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, "sessions 必須是觀察清單"
	}
	sessions, ok := top["sessions"]
	if !ok || bytes.Equal(bytes.TrimSpace(sessions), []byte("null")) {
		return nil, "sessions 必須是觀察清單"
	}
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(sessions, &rows); err != nil || rows == nil {
		return nil, "sessions 必須是觀察清單"
	}
	if rows == nil {
		return nil, "sessions 必須是觀察清單"
	}
	seen := map[string]struct{}{}
	out := make([]db.ObservedRead, 0, len(rows))
	for _, row := range rows {
		var id string
		if json.Unmarshal(row["id"], &id) != nil || strings.TrimSpace(id) == "" {
			return nil, "sessions.id 必須是非空字串"
		}
		if _, dup := seen[id]; dup {
			return nil, "sessions.id 不可重複"
		}
		seen[id] = struct{}{}
		rev, ok := parseNonNegativeInt(row["manual_unread_revision"])
		if !ok {
			return nil, "manual_unread_revision 必須是非負整數"
		}
		idsRaw, ok := row["read_message_ids"]
		if !ok || bytes.Equal(bytes.TrimSpace(idsRaw), []byte("null")) {
			return nil, "read_message_ids 必須是訊息 ID 陣列"
		}
		var ids []int64
		if json.Unmarshal(idsRaw, &ids) != nil || len(ids) > 1000 {
			return nil, "read_message_ids 必須是最多 1000 個正整數"
		}
		for _, msgID := range ids {
			if msgID <= 0 {
				return nil, "訊息 ID 必須為正整數"
			}
		}
		out = append(out, db.ObservedRead{ID: id, Revision: rev, MessageIDs: ids})
	}
	return out, ""
}
