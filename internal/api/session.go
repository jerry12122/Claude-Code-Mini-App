package api

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/gitinfo"

	"github.com/gofiber/fiber/v2"
)

const (
	maxCliExtraArgs   = 64
	maxCliExtraArgLen = 4096
	// gitBranchWorkers：List 時 unique work_dir 平行查分支的併發上限
	gitBranchWorkers = 8
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
	// OnSharesRevoked 在刪除 session 而撤銷其分享後呼叫（用來踢掉訪客連線）；可為 nil。
	OnSharesRevoked func(shareIDs []int64)
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

	dirs := make([]string, 0, len(sessions))
	seen := make(map[string]struct{}, len(sessions))
	for _, s := range sessions {
		wd := strings.TrimSpace(s.WorkDir)
		if wd == "" {
			continue
		}
		if _, ok := seen[wd]; ok {
			continue
		}
		seen[wd] = struct{}{}
		dirs = append(dirs, wd)
	}

	branchByDir := make(map[string]string, len(dirs))
	if len(dirs) > 0 {
		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, gitBranchWorkers)
		for _, wd := range dirs {
			wg.Add(1)
			go func(wd string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				b, ok := gitinfo.Branch(wd)
				if !ok {
					b = ""
				}
				mu.Lock()
				branchByDir[wd] = b
				mu.Unlock()
			}(wd)
		}
		wg.Wait()
	}

	for _, s := range sessions {
		wd := strings.TrimSpace(s.WorkDir)
		if wd == "" {
			continue
		}
		s.GitBranch = branchByDir[wd]
	}
	return c.JSON(sessions)
}

// ModelOptions GET /model-options/:agentType — 回傳該 agent 目前啟用的 model 選項。
func (h *SessionHandler) ModelOptions(c *fiber.Ctx) error {
	agentType := strings.TrimSpace(c.Params("agentType"))
	opts, err := h.db.ListModelOptions(agentType)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(opts)
}

// ListWorkDirs GET /work-dirs — 獨立於 session 的工作目錄清單（下拉用）。
func (h *SessionHandler) ListWorkDirs(c *fiber.Ctx) error {
	dirs, err := h.db.ListWorkDirs()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(dirs)
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
		// Effort：選填，空字串＝不指定交給 CLI 用預設。沿用既有 set_effort 的自由字串，不另驗證合法值。
		Effort string `json:"effort"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	if body.AgentType == "" {
		body.AgentType = "claude"
	}
	if !agent.CanCreate(body.AgentType) {
		reason := agent.CreateDisabledReason(body.AgentType)
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
	if strings.TrimSpace(body.Effort) != "" {
		if err := h.db.UpdateEffort(s.ID, body.Effort); err != nil {
			// 比照 ws/handoff.go 的半成品清除：effort 寫入失敗就刪掉剛建立的 session 再回錯，
			// 不留下沒有 effort 的半成品，否則使用者重按建立會重複產生 session。
			if delErr := h.db.DeleteSession(s.ID); delErr != nil {
				slog.Info(fmt.Sprintf("[api] 建立 session 後 UpdateEffort 失敗，清除半成品 %s 也失敗: %v", s.ID, delErr))
			}
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		s.Effort = body.Effort
	}
	enrichGitBranch(s)
	return c.Status(201).JSON(s)
}

// Patch 更新 Session 名稱與／或自訂 CLI 引數。
func (h *SessionHandler) Patch(c *fiber.Ctx) error {
	id := c.Params("id")
	var body struct {
		Name         *string   `json:"name"`
		CliExtraArgs *[]string `json:"cli_extra_args"`
		MarkRead     *bool     `json:"mark_read"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	if body.Name == nil && body.CliExtraArgs == nil && body.MarkRead == nil {
		return c.Status(400).JSON(fiber.Map{"error": "name、cli_extra_args 或 mark_read 至少指定一項"})
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
	if body.MarkRead != nil && *body.MarkRead {
		if err := h.db.MarkSessionRead(id); err != nil {
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
	if shareIDs, err := h.db.RevokeSharesBySession(id); err == nil && len(shareIDs) > 0 && h.OnSharesRevoked != nil {
		h.OnSharesRevoked(shareIDs)
	}
	if err := h.db.DeleteSession(id); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.SendStatus(204)
}

// ReadAll 一次把所有 session 標記已讀（列表「Read All」按鈕）。
func (h *SessionHandler) ReadAll(c *fiber.Ctx) error {
	if err := h.db.MarkAllSessionsRead(); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.SendStatus(204)
}

func (h *SessionHandler) Messages(c *fiber.Ctx) error {
	id := c.Params("id")
	q := db.MessageQuery{SessionID: id, IncludeResult: true}
	// 快照分享的訪客（authMiddleware 寫入）只能讀到 snapshot_msg_id 為止。
	if maxID, ok := c.Locals("share_snapshot_msg_id").(int64); ok {
		q.MaxID = &maxID
	}
	msgs, err := h.db.ListMessagesQuery(q)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	if msgs == nil {
		msgs = []*db.Message{}
	}
	return c.JSON(msgs)
}
