package api

import (
	"encoding/json"
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/ws"
)

type SettingsHandler struct {
	db                      *db.DB
	quotaGuardConfigPercent float64
}

func NewSettingsHandler(database *db.DB, quotaGuardConfigPercent float64) *SettingsHandler {
	return &SettingsHandler{db: database, quotaGuardConfigPercent: quotaGuardConfigPercent}
}

// GetAppearance GET /settings/appearance
func (h *SettingsHandler) GetAppearance(c *fiber.Ctx) error {
	res, err := h.db.GetAppearance()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(res)
}

// PutAppearance PUT /settings/appearance — 整包覆寫。
func (h *SettingsHandler) PutAppearance(c *fiber.Ctx) error {
	res, err := h.db.PutAppearance(c.Body())
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(res)
}

// GetGeneral GET /settings/general — 回傳有效值（含尚未儲存時由 config 百分比推得的守門設定）。
func (h *SettingsHandler) GetGeneral(c *fiber.Ctx) error {
	res, err := h.db.EffectiveGeneralSettings(h.quotaGuardConfigPercent)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(res)
}

// PutGeneral PUT /settings/general — 整包覆寫。
// quotaGuardPercent 必須在 (0, 100]；不合法回 400 且不寫入、也不清除自動復原狀態。
// autoResumeOnQuota 由 true 改為 false 時，由 PutGeneralSettings 在同一筆交易裡清除狀態並寫入設定。
func (h *SettingsHandler) PutGeneral(c *fiber.Ctx) error {
	var incoming db.GeneralSettings
	if err := json.Unmarshal(c.Body(), &incoming); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	if incoming.QuotaGuardPercent <= 0 || incoming.QuotaGuardPercent > 100 {
		return c.Status(400).JSON(fiber.Map{"error": "quotaGuardPercent 必須大於 0 且小於等於 100"})
	}
	prev, err := h.db.GetGeneralSettings()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	res, err := h.db.PutGeneralSettings(c.Body())
	if errors.Is(err, db.ErrInvalidQueueDelivery) {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	if prev.AutoResumeOnQuota && !res.AutoResumeOnQuota {
		if err := ws.BroadcastQuotaResumeClears(h.db); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
	}
	return c.JSON(res)
}

func (h *SettingsHandler) GetGlobalInstruction(c *fiber.Ctx) error {
	instruction, err := h.db.GetGlobalInstruction()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "無法讀取全域指令設定"})
	}
	c.Set("Cache-Control", "no-store")
	return c.JSON(instruction)
}

func (h *SettingsHandler) PutGlobalInstruction(c *fiber.Ctx) error {
	var body struct {
		Text *string `json:"text"`
		Mode *string `json:"mode"`
	}
	if err := json.Unmarshal(c.Body(), &body); err != nil || body.Text == nil || body.Mode == nil {
		return c.Status(400).JSON(fiber.Map{"error": "全域指令文字與注入模式須為字串"})
	}
	instruction := db.GlobalInstruction{Text: *body.Text, Mode: *body.Mode}
	if err := instruction.Normalize(); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	if err := h.db.PutGlobalInstruction(instruction); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "無法儲存全域指令設定"})
	}
	c.Set("Cache-Control", "no-store")
	return c.JSON(instruction)
}
