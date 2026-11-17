package api

import (
	"errors"
	"github.com/gofiber/fiber/v2"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/gitinfo"
	"os"
	"strings"
)

type GitHandler struct{ db *db.DB }

func NewGitHandler(database *db.DB) *GitHandler { return &GitHandler{db: database} }

func (h *GitHandler) Status(c *fiber.Ctx) error {
	dir, status, msg := sessionWorkDir(h.db, c.Params("id"))
	if status != 0 {
		return jsonErr(c, status, msg)
	}
	report, err := gitinfo.Status(dir)
	if err != nil {
		return gitError(c, err)
	}
	if !report.IsRepo {
		return c.JSON(fiber.Map{"is_repo": false})
	}
	counts, err := gitinfo.ChangeStats(dir)
	if err != nil {
		return gitError(c, err)
	}
	return c.JSON(struct {
		gitinfo.StatusReport
		gitinfo.LineStats
	}{report, counts})
}

func (h *GitHandler) Branches(c *fiber.Ctx) error {
	dir, status, msg := sessionWorkDir(h.db, c.Params("id"))
	if status != 0 {
		return jsonErr(c, status, msg)
	}
	branches, err := gitinfo.Branches(dir)
	if err != nil {
		return gitError(c, err)
	}
	return c.JSON(branches)
}

func (h *GitHandler) Checkout(c *fiber.Ctx) error {
	var body struct {
		Branch string `json:"branch"`
	}
	if err := c.BodyParser(&body); err != nil || body.Branch == "" || strings.HasPrefix(body.Branch, "-") {
		return jsonErr(c, 400, "請提供有效的本機分支名稱")
	}
	return h.write(c, func(dir string) (string, error) { return gitinfo.Switch(dir, body.Branch) })
}

func (h *GitHandler) Pull(c *fiber.Ctx) error { return h.write(c, gitinfo.Pull) }
func (h *GitHandler) Push(c *fiber.Ctx) error { return h.write(c, gitinfo.Push) }

func (h *GitHandler) write(c *fiber.Ctx, operation func(string) (string, error)) error {
	dir, status, msg := sessionWorkDir(h.db, c.Params("id"))
	if status != 0 {
		return jsonErr(c, status, msg)
	}
	target, err := os.Stat(dir)
	if err != nil {
		return jsonErr(c, 400, "work_dir 不存在或不是目錄")
	}
	sessions, err := h.db.ListSessions()
	if err != nil {
		return jsonErr(c, 500, "無法檢查 session 狀態")
	}
	// ponytail: 檢查與 agent 啟動之間仍有空窗；若需完全排他，改由回合排程器共同持鎖。
	for _, session := range sessions {
		if session.Status == db.SessionStatusIdle || strings.TrimSpace(session.WorkDir) == "" {
			continue
		}
		other, err := os.Stat(session.WorkDir)
		if err == nil && os.SameFile(target, other) {
			return jsonErr(c, 409, "此工作目錄有 session 忙碌中，請在所有 session 閒置後執行")
		}
	}
	output, err := operation(dir)
	if err != nil {
		return gitError(c, err)
	}
	return c.JSON(fiber.Map{"ok": true, "output": output})
}

func gitError(c *fiber.Ctx, err error) error {
	status := 500
	message, output := err.Error(), ""
	var command *gitinfo.CommandError
	if errors.As(err, &command) {
		output = command.Output
		switch command.Kind {
		case "invalid_branch":
			status = 400
		case "busy":
			status = 409
		case "no_upstream", "failed":
			status = 422
		case "timeout":
			status = 504
		}
		if command.Kind == "failed" && output != "" {
			paragraphs := strings.Split(strings.TrimSpace(output), "\n\n")
			message = paragraphs[len(paragraphs)-1]
		}
	}
	return c.Status(status).JSON(fiber.Map{"error": message, "output": output})
}
