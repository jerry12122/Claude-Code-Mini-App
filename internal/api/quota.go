package api

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/quota"
)

type QuotaHandler struct {
	svc *quota.Service
}

func NewQuotaHandler(svc *quota.Service) *QuotaHandler {
	return &QuotaHandler{svc: svc}
}

// GetAll GET /quota — 回傳各 provider cache（不 trigger fetch）。
func (h *QuotaHandler) GetAll(c *fiber.Ctx) error {
	all := h.svc.GetAll()
	out := make(map[string]quota.Payload, len(all))
	for k, v := range all {
		out[k] = v.ToPayload()
	}
	return c.JSON(out)
}

// Get GET /quota/:provider — 單一 provider cache。
func (h *QuotaHandler) Get(c *fiber.Ctx) error {
	provider := strings.TrimSpace(c.Params("provider"))
	snap := h.svc.Get(provider)
	return c.JSON(snap.ToPayload())
}

// Refresh POST /quota/:provider/refresh — 手動刷新（60s cooldown）。
func (h *QuotaHandler) Refresh(c *fiber.Ctx) error {
	provider := strings.TrimSpace(c.Params("provider"))
	snap, err := h.svc.RefreshManual(c.Context(), provider)
	payload := snap.ToPayload()
	if err != nil && snap.DisplayText == "—" {
		return c.Status(502).JSON(fiber.Map{"error": err.Error(), "quota": payload})
	}
	return c.JSON(payload)
}
