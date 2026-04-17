package api

import (
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"

	"github.com/gofiber/fiber/v2"
)

type SessionHandler struct {
	db *db.DB
}

func NewSessionHandler(database *db.DB) *SessionHandler {
	return &SessionHandler{db: database}
}

func (h *SessionHandler) List(c *fiber.Ctx) error {
	sessions, err := h.db.ListSessions()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	if sessions == nil {
		sessions = []*db.Session{}
	}
	return c.JSON(sessions)
}

func (h *SessionHandler) Create(c *fiber.Ctx) error {
	var body struct {
		Name           string `json:"name"`
		Description    string `json:"description"`
		WorkDir        string `json:"work_dir"`
		PermissionMode string `json:"permission_mode"`
		AgentType      string `json:"agent_type"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	if body.AgentType == "" {
		body.AgentType = "claude"
	}
	s, err := h.db.CreateSession(body.Name, body.Description, body.WorkDir, body.PermissionMode, body.AgentType)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.Status(201).JSON(s)
}

func (h *SessionHandler) Rename(c *fiber.Ctx) error {
	id := c.Params("id")
	var body struct {
		Name string `json:"name"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	if body.Name == "" {
		return c.Status(400).JSON(fiber.Map{"error": "name 不可為空"})
	}
	if err := h.db.UpdateSessionName(id, body.Name); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	s, err := h.db.GetSession(id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(s)
}

func (h *SessionHandler) Delete(c *fiber.Ctx) error {
	id := c.Params("id")
	if err := h.db.DeleteSession(id); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.SendStatus(204)
}

func (h *SessionHandler) Messages(c *fiber.Ctx) error {
	id := c.Params("id")
	msgs, err := h.db.ListMessages(id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	if msgs == nil {
		msgs = []*db.Message{}
	}
	return c.JSON(msgs)
}
