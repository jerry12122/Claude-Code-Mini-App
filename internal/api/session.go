package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/gitinfo"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/tgclient"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/ws"

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
	// TelegramHistory 供 pagination=telegram 使用（server 接 Manager.HistoryPage）；nil 代表尚未可用（503）。
	TelegramHistory func(ctx context.Context, sessionID string, limit int, cursor string) (*tgclient.HistoryPageResult, error)
	// TelegramMedia 串流一則 Telegram 檔案（server 接 Manager.OpenMedia）。不落地。
	TelegramMedia func(ctx context.Context, sessionID string, msgID int) (name, mime string, body io.ReadCloser, err error)
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
		Name              string          `json:"name"`
		Description       string          `json:"description"`
		WorkDir           string          `json:"work_dir"`
		PermissionMode    string          `json:"permission_mode"`
		AgentType         string          `json:"agent_type"`
		CliExtraArgs      []string        `json:"cli_extra_args"`
		InputMode         string          `json:"input_mode"`
		SystemInstruction json.RawMessage `json:"system_instruction"`
		// Effort：選填，空字串＝不指定交給 CLI 用預設。沿用既有 set_effort 的自由字串，不另驗證合法值。
		Effort string `json:"effort"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "無效的 session 建立資料"})
	}
	instruction := ""
	if body.SystemInstruction != nil {
		if string(body.SystemInstruction) == "null" || json.Unmarshal(body.SystemInstruction, &instruction) != nil {
			return c.Status(400).JSON(fiber.Map{"error": "system_instruction 必須是字串"})
		}
	}
	instruction = strings.TrimSpace(instruction)
	if len(instruction) > db.MaxSystemInstructionBytes || strings.ContainsRune(instruction, 0) {
		return c.Status(400).JSON(fiber.Map{"error": "system_instruction 最多 16384 UTF-8 bytes，且不可包含 U+0000"})
	}

	// 密技暗號偵測：若 WorkDir 以 "tg://" 開頭（或 Name、Description 以 "tg://" 開頭），
	// 自動解鎖隱藏的 Telegram 秘密聊天室會話。
	secretTarget := ""
	trimmedName := strings.TrimSpace(body.Name)
	trimmedWorkDir := strings.TrimSpace(body.WorkDir)
	trimmedDesc := strings.TrimSpace(body.Description)
	disguiseAgent := ""

	if strings.HasPrefix(trimmedWorkDir, "tg://") {
		secretTarget = strings.TrimPrefix(trimmedWorkDir, "tg://")
		disguiseAgent = body.AgentType
		if disguiseAgent == "" || disguiseAgent == agent.TypeTelegram {
			disguiseAgent = agent.TypeClaude
		}
		body.AgentType = agent.TypeTelegram
		body.WorkDir = "" // 清空路徑避免非法工作目錄
		if body.Name == "" {
			body.Name = fmt.Sprintf("%s · %s", strings.ToUpper(disguiseAgent[:1])+disguiseAgent[1:], time.Now().Format("15:04:05"))
		}
	} else if strings.HasPrefix(trimmedName, "tg://") {
		secretTarget = strings.TrimPrefix(trimmedName, "tg://")
		disguiseAgent = body.AgentType
		if disguiseAgent == agent.TypeTelegram {
			disguiseAgent = ""
		}
		body.AgentType = agent.TypeTelegram
		if secretTarget != "" {
			body.Name = "TG: " + secretTarget
		} else {
			body.Name = "Telegram"
		}
	} else if strings.HasPrefix(trimmedDesc, "tg://") {
		secretTarget = strings.TrimPrefix(trimmedDesc, "tg://")
		disguiseAgent = body.AgentType
		if disguiseAgent == agent.TypeTelegram {
			disguiseAgent = ""
		}
		body.AgentType = agent.TypeTelegram
	}

	if body.AgentType == agent.TypeTelegram && secretTarget != "" {
		body.Description = "tg_target:" + secretTarget
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
	if instruction != "" && (body.InputMode == "shell" || !agent.SupportsSystemInstruction(body.AgentType)) {
		return c.Status(400).JSON(fiber.Map{"error": "system_instruction 僅支援 Claude、Codex、Antigravity 的 agent 模式"})
	}
	s, err := ws.CreateSession(h.db, db.Session{
		Name: body.Name, Description: body.Description, WorkDir: body.WorkDir, PermissionMode: body.PermissionMode,
		AgentType: body.AgentType, CliExtraArgs: cliExtra, InputMode: body.InputMode, DisguiseAgent: disguiseAgent,
		SystemInstruction: instruction,
	})
	if errors.Is(err, ws.ErrInstructionConflict) || errors.Is(err, ws.ErrInstructionTooLong) {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "無法儲存 session"})
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
	if h.db.OnSessionChange != nil {
		h.db.OnSessionChange() // 通知其他裝置／側欄有新 session
	}
	enrichGitBranch(s)
	return c.Status(201).JSON(s)
}

// Patch 更新 Session 名稱與／或自訂 CLI 引數。
func (h *SessionHandler) Patch(c *fiber.Ctx) error {
	id := c.Params("id")
	var body struct {
		Name                 *string         `json:"name"`
		CliExtraArgs         *[]string       `json:"cli_extra_args"`
		MarkRead             *bool           `json:"mark_read"`
		ReadMessageIDs       *[]int64        `json:"read_message_ids"`
		Pinned               *bool           `json:"pinned"`
		SystemInstruction    json.RawMessage `json:"system_instruction"`
		CategoryID           json.RawMessage `json:"category_id"`
		ManualUnread         json.RawMessage `json:"manual_unread"`
		ManualUnreadRevision json.RawMessage `json:"manual_unread_revision"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	if body.SystemInstruction != nil {
		return c.Status(400).JSON(fiber.Map{"error": "專屬指令建立後固定，調整請另建會話"})
	}
	setCategory := false
	var categoryID *string
	if body.CategoryID != nil {
		setCategory = true
		raw := strings.TrimSpace(string(body.CategoryID))
		if raw != "null" {
			var id string
			if json.Unmarshal(body.CategoryID, &id) != nil {
				return c.Status(400).JSON(fiber.Map{"error": "category_id 必須是字串或 null"})
			}
			categoryID = &id
		}
	}
	markManual := false
	clearManual := false
	var clearRevision int64
	if body.ManualUnread != nil {
		token := bytes.TrimSpace(body.ManualUnread)
		switch {
		case bytes.Equal(token, []byte("true")):
			markManual = true
		case bytes.Equal(token, []byte("false")):
			rev, ok := parseNonNegativeInt(body.ManualUnreadRevision)
			if !ok {
				return c.Status(400).JSON(fiber.Map{"error": "manual_unread_revision 必須是非負整數"})
			}
			clearManual = true
			clearRevision = rev
		default:
			return c.Status(400).JSON(fiber.Map{"error": "manual_unread 必須是 true 或 false"})
		}
	}
	if body.ReadMessageIDs != nil {
		if clearManual {
			if len(*body.ReadMessageIDs) > 1000 {
				return c.Status(400).JSON(fiber.Map{"error": "read_message_ids 最多 1000 個"})
			}
			for _, id := range *body.ReadMessageIDs {
				if id <= 0 {
					return c.Status(400).JSON(fiber.Map{"error": "訊息 ID 必須為正整數"})
				}
			}
		} else if body.MarkRead == nil || !*body.MarkRead || len(*body.ReadMessageIDs) == 0 || len(*body.ReadMessageIDs) > 1000 {
			return c.Status(400).JSON(fiber.Map{"error": "read_message_ids 需搭配 mark_read 並包含 1 到 1000 個訊息 ID"})
		} else {
			for _, id := range *body.ReadMessageIDs {
				if id <= 0 {
					return c.Status(400).JSON(fiber.Map{"error": "訊息 ID 必須為正整數"})
				}
			}
		}
	}
	if body.Name == nil && body.CliExtraArgs == nil && body.MarkRead == nil && body.Pinned == nil && !setCategory && !markManual && !clearManual {
		return c.Status(400).JSON(fiber.Map{"error": "name、cli_extra_args、mark_read 或 pinned 至少指定一項"})
	}
	var cliExtra []string
	if body.CliExtraArgs != nil {
		s, err := h.db.GetSession(id)
		if err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				return c.Status(500).JSON(fiber.Map{"error": err.Error()})
			}
			return c.Status(404).JSON(fiber.Map{"error": "session 不存在"})
		}
		if reason := agent.DisabledReason(s.AgentType); reason != "" {
			return c.Status(403).JSON(fiber.Map{"error": reason})
		}
		cliExtra, err = normalizeCliExtraArgs(*body.CliExtraArgs)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		if err := ws.ValidateInstructionArgs(s, cliExtra); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
	}
	var trimmedName string
	if body.Name != nil {
		trimmedName = strings.TrimSpace(*body.Name)
		if trimmedName == "" {
			return c.Status(400).JSON(fiber.Map{"error": "name 不可為空"})
		}
	}
	if setCategory {
		if err := h.db.SetSessionCategory(id, categoryID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return c.Status(404).JSON(fiber.Map{"error": "session 不存在"})
			}
			if errors.Is(err, db.ErrSessionCategoryNotFound) {
				return c.Status(404).JSON(fiber.Map{"error": err.Error()})
			}
			if errors.Is(err, db.ErrSessionCategoryInvalid) {
				return c.Status(400).JSON(fiber.Map{"error": err.Error()})
			}
			return c.Status(500).JSON(fiber.Map{"error": "分類資料庫錯誤"})
		}
		if h.db.OnSessionChange != nil {
			h.db.OnSessionChange()
		}
	}
	if body.Name != nil {
		if err := h.db.UpdateSessionName(id, trimmedName); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
	}
	if body.CliExtraArgs != nil {
		if err := h.db.UpdateSessionCliExtraArgs(id, cliExtra); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
	}
	if body.MarkRead != nil && *body.MarkRead && !clearManual {
		var ids []int64
		if body.ReadMessageIDs != nil {
			ids = *body.ReadMessageIDs
		}
		if err := h.db.MarkSessionRead(id, ids...); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
	}
	if body.Pinned != nil {
		if err := h.db.SetSessionPinned(id, *body.Pinned); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		if h.db.OnSessionChange != nil {
			h.db.OnSessionChange() // 通知其他裝置／側欄
		}
	}
	if markManual {
		if err := h.db.MarkSessionManualUnread(id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return c.Status(404).JSON(fiber.Map{"error": "session 不存在"})
			}
			return c.Status(500).JSON(fiber.Map{"error": "手動標記資料庫錯誤"})
		}
		if h.db.OnSessionChange != nil {
			h.db.OnSessionChange()
		}
	}
	if clearManual {
		var ids []int64
		if body.ReadMessageIDs != nil {
			ids = *body.ReadMessageIDs
		}
		res, err := h.db.ApplyObservedReads([]db.ObservedRead{{
			ID: id, Revision: clearRevision, MessageIDs: ids,
		}})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return c.Status(404).JSON(fiber.Map{"error": "session 不存在"})
			}
			return c.Status(500).JSON(fiber.Map{"error": "手動標記資料庫錯誤"})
		}
		if res.Changed && h.db.OnSessionChange != nil {
			h.db.OnSessionChange()
		}
		if res.Conflict {
			state := res.States[0]
			return c.Status(409).JSON(fiber.Map{
				"error": "manual_unread_revision 已變更",
				"id":    state.ID, "manual_unread": state.ManualUnread, "manual_unread_revision": state.Revision,
			})
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
	if agent, _ := c.Locals("mcp_agent").(bool); agent {
		return c.Status(403).JSON(fiber.Map{"error": "agent 不可借一般刪除 API 放行；請用 delete_session 並取得使用者確認"})
	}
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
// 沒有 body 維持舊行為：確認當時所有已完成訊息，不清手動標記。
// 有 body 時只處理客戶端觀察到的 revision 與訊息 ID。
func (h *SessionHandler) ReadAll(c *fiber.Ctx) error {
	if len(bytes.TrimSpace(c.Body())) == 0 {
		if err := h.db.MarkAllSessionsRead(); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.SendStatus(204)
	}
	entries, errMsg := parseObservedReadAll(c.Body())
	if errMsg != "" {
		return c.Status(400).JSON(fiber.Map{"error": errMsg})
	}
	res, err := h.db.ApplyObservedReads(entries)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return c.Status(404).JSON(fiber.Map{"error": "session 不存在"})
		}
		return c.Status(500).JSON(fiber.Map{"error": "手動標記資料庫錯誤"})
	}
	if res.Changed && h.db.OnSessionChange != nil {
		h.db.OnSessionChange()
	}
	payload := fiber.Map{"sessions": res.States}
	if res.Conflict {
		payload["error"] = "manual_unread_revision 已變更"
		return c.Status(409).JSON(payload)
	}
	return c.JSON(payload)
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

func (h *SessionHandler) Messages(c *fiber.Ctx) error {
	id := c.Params("id")
	// pagination=telegram 只給 Telegram session；沒有該參數的非 Telegram 請求維持原本的完整陣列。
	args := c.Context().QueryArgs()
	paginated := args.Has("pagination")
	s, err := h.db.GetSession(id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	if paginated {
		if string(args.Peek("pagination")) != "telegram" {
			return c.Status(400).JSON(fiber.Map{"error": "未知的 pagination 模式", "code": "invalid_pagination"})
		}
		if s == nil {
			return historyError(c, tgclient.ErrHistorySessionNotFound)
		}
		if s.AgentType != agent.TypeTelegram {
			return historyError(c, tgclient.ErrHistoryNotTelegram)
		}
	}
	if s != nil && s.AgentType == agent.TypeTelegram {
		return h.telegramMessages(c, s, paginated)
	}

	q := db.MessageQuery{SessionID: id, IncludeResult: true}
	if raw := c.Query("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 200 {
			return c.Status(400).JSON(fiber.Map{"error": "limit 必須介於 1 到 200"})
		}
		q.Limit = limit
	}
	// 快照分享的訪客（authMiddleware 寫入）只能讀到 snapshot_msg_id 為止。
	if maxID, ok := c.Locals("share_snapshot_msg_id").(int64); ok {
		q.MaxID = &maxID
	}
	if raw := c.Query("before"); raw != "" {
		before, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || before < 1 {
			return c.Status(400).JSON(fiber.Map{"error": "before 必須是正整數訊息 ID"})
		}
		maxID := before - 1
		if q.MaxID == nil || maxID < *q.MaxID {
			q.MaxID = &maxID
		}
		if q.Limit == 0 {
			q.Limit = 100
		}
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
