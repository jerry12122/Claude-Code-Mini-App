package api

import (
	"encoding/json"

	"github.com/gofiber/fiber/v2"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

type SettingsHandler struct {
	db *db.DB
}

func NewSettingsHandler(database *db.DB) *SettingsHandler {
	return &SettingsHandler{db: database}
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

// GetGeneral GET /settings/general
func (h *SettingsHandler) GetGeneral(c *fiber.Ctx) error {
	res, err := h.db.GetGeneralSettings()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(res)
}

// PutGeneral PUT /settings/general — 整包覆寫。
func (h *SettingsHandler) PutGeneral(c *fiber.Ctx) error {
	res, err := h.db.PutGeneralSettings(c.Body())
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
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
