package api

import (
	"fmt"
	"strings"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/gitinfo"

	"github.com/gofiber/fiber/v2"
)

const (
	maxCliExtraArgs    = 64
	maxCliExtraArgLen  = 4096
)

// normalizeCliExtraArgs 修剪空白、略過空行，並限制數量與單一長度（與前端「每行一個引數」一致，路徑含空格不會被切開）。
func normalizeCliExtraArgs(in []string) ([]string, error) {
	if len(in) == 0 {
		return []string{}, nil
	}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if len(s) > maxCliExtraArgLen {
			return nil, fmt.Errorf("單一自訂參數長度不可超過 4096 字元")
		}
		out = append(out, s)
	}
	if len(out) > maxCliExtraArgs {
		return nil, fmt.Errorf("自訂參數最多 64 個")
	}
	return out, nil
}

type SessionHandler struct {
	db *db.DB
}

func NewSessionHandler(database *db.DB) *SessionHandler {
	return &SessionHandler{db: database}
}

func enrichGitBranch(s *db.Session) {
	if s == nil {
		return
	}
	if b, ok := gitinfo.Branch(s.WorkDir); ok {
		s.GitBranch = b
	}
}

func (h *SessionHandler) List(c *fiber.Ctx) error {
	sessions, err := h.db.ListSessions()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	if sessions == nil {
		sessions = []*db.Session{}
	}
	for _, s := range sessions {
		enrichGitBranch(s)
	}
	return c.JSON(sessions)
}

func (h *SessionHandler) Create(c *fiber.Ctx) error {
	var body struct {
		Name           string   `json:"name"`
		Description    string   `json:"description"`
		WorkDir        string   `json:"work_dir"`
		PermissionMode string   `json:"permission_mode"`
		AgentType      string   `json:"agent_type"`
		CliExtraArgs   []string `json:"cli_extra_args"`
		InputMode      string   `json:"input_mode"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	if body.AgentType == "" {
		body.AgentType = "claude"
	}
	if !agent.IsEnabled(body.AgentType) {
		reason := agent.DisabledReason(body.AgentType)
		if reason == "" {
			reason = "不支援的 agent_type"
		}
		return c.Status(400).JSON(fiber.Map{"error": reason})
	}
	cliExtra, err := normalizeCliExtraArgs(body.CliExtraArgs)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	s, err := h.db.CreateSession(body.Name, body.Description, body.WorkDir, body.PermissionMode, body.AgentType, cliExtra, body.InputMode)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	enrichGitBranch(s)
	return c.Status(201).JSON(s)
}

// Patch 更新 Session 名稱與／或自訂 CLI 引數。
func (h *SessionHandler) Patch(c *fiber.Ctx) error {
	id := c.Params("id")
	var body struct {
		Name         *string  `json:"name"`
		CliExtraArgs *[]string `json:"cli_extra_args"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	if body.Name == nil && body.CliExtraArgs == nil {
		return c.Status(400).JSON(fiber.Map{"error": "name 或 cli_extra_args 至少指定一項"})
	}
	if body.Name != nil {
		trimmed := strings.TrimSpace(*body.Name)
		if trimmed == "" {
			return c.Status(400).JSON(fiber.Map{"error": "name 不可為空"})
		}
		if err := h.db.UpdateSessionName(id, trimmed); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
	}
	if body.CliExtraArgs != nil {
		cliExtra, err := normalizeCliExtraArgs(*body.CliExtraArgs)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		if err := h.db.UpdateSessionCliExtraArgs(id, cliExtra); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
	}
	s, err := h.db.GetSession(id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	enrichGitBranch(s)
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
