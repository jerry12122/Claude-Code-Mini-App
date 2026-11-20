package api

import (
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gofiber/fiber/v2"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/ws"
)

func (h *RoomsHandler) ReadQueue(c *fiber.Ctx) error {
	pm := c.Query("expected_pm_id")
	if strings.TrimSpace(pm) == "" || len(pm) > 128 || !utf8.ValidString(pm) || strings.ContainsRune(pm, 0) {
		return jsonErr(c, 400, "請提供 expected_pm_id")
	}
	limit := 100
	after := int64(0)
	var err error
	if raw := c.Query("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 100 {
			return jsonErr(c, 400, "limit 必須介於 1–100")
		}
	}
	if raw := c.Query("after_id"); raw != "" {
		after, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || after < 0 {
			return jsonErr(c, 400, "after_id 必須是非負整數")
		}
	}
	if _, err := h.db.GetRoom(c.Params("id")); err != nil {
		return h.writeErr(c, "read_queue_room", err)
	}
	page, err := h.db.RoomQueue(c.UserContext(), c.Params("id"), pm, limit, after)
	if err != nil {
		return h.writeErr(c, "read_queue", err)
	}
	return c.JSON(page)
}

func (h *RoomsHandler) QueueBatch(c *fiber.Ctx) error {
	var body struct {
		PMID      string              `json:"expected_pm_id"`
		RequestID string              `json:"request_id"`
		Items     []db.QueueBatchItem `json:"items"`
	}
	if !utf8.Valid(c.Body()) || json.Unmarshal(c.Body(), &body) != nil || strings.TrimSpace(body.PMID) == "" || strings.ContainsRune(body.PMID, 0) {
		return jsonErr(c, 400, "請提供 expected_pm_id 與有效的批次資料")
	}
	if h.BatchQueued == nil {
		return jsonErr(c, 503, "一起送暫時無法使用")
	}
	if _, err := h.db.GetRoom(c.Params("id")); err != nil {
		return h.writeErr(c, "batch_room", err)
	}
	id, err := h.BatchQueued(c.UserContext(), c.Params("id"), body.PMID, body.RequestID, body.Items, c.Params("batch_id"))
	if err != nil {
		code, text := ws.QueueBatchError(err)
		status := 500
		switch code {
		case "invalid":
			status = 400
		case "conflict":
			status = 409
		case "forbidden":
			status = 403
		case "update_in_progress":
			status = 503
		}
		return c.Status(status).JSON(fiber.Map{"code": code, "error": text})
	}
	if c.Method() == "DELETE" {
		return c.SendStatus(204)
	}
	return c.Status(202).JSON(fiber.Map{"batch_id": id})
}
