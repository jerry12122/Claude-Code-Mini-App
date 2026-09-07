package api

import (
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
