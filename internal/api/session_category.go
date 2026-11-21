package api

import (
	"bytes"
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

// errBadCategoryBody 是空 body、JSON null、型別不對或無法解析。不進資料庫。
var errBadCategoryBody = errors.New("無效的分類資料")

// SessionCategoryHandler 只轉呼既有 DB 分類方法。歸屬指派不在這裡。
type SessionCategoryHandler struct {
	db *db.DB
}

func NewSessionCategoryHandler(database *db.DB) *SessionCategoryHandler {
	return &SessionCategoryHandler{db: database}
}

func (h *SessionCategoryHandler) notify() {
	if h.db != nil && h.db.OnSessionChange != nil {
		h.db.OnSessionChange()
	}
}

func writeSessionCategoryErr(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, errBadCategoryBody), errors.Is(err, db.ErrSessionCategoryInvalid):
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	case errors.Is(err, db.ErrSessionCategoryNotFound):
		return c.Status(404).JSON(fiber.Map{"error": err.Error()})
	case errors.Is(err, db.ErrSessionCategoryDuplicate):
		return c.Status(409).JSON(fiber.Map{"error": err.Error()})
	default:
		return c.Status(500).JSON(fiber.Map{"error": "分類資料庫錯誤"})
	}
}

func parseCategoryJSON(c *fiber.Ctx, dest any) error {
	raw := bytes.TrimSpace(c.Body())
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return errBadCategoryBody
	}
	if err := c.BodyParser(dest); err != nil {
		return errBadCategoryBody
	}
	return nil
}

// List GET /session-categories?work_dir=
// 只回該既有目錄的分類；沒有 work_dir 不回跨目錄清單。
func (h *SessionCategoryHandler) List(c *fiber.Ctx) error {
	items, err := h.db.ListSessionCategories(c.Query("work_dir"))
	if err != nil {
		return writeSessionCategoryErr(c, err)
	}
	if items == nil {
		items = []db.SessionCategory{}
	}
	return c.JSON(items)
}

// Create POST /session-categories  body {"name","work_dir"}
func (h *SessionCategoryHandler) Create(c *fiber.Ctx) error {
	var body struct {
		Name    string `json:"name"`
		WorkDir string `json:"work_dir"`
	}
	if err := parseCategoryJSON(c, &body); err != nil {
		return writeSessionCategoryErr(c, err)
	}
	cat, err := h.db.CreateSessionCategory(body.WorkDir, body.Name)
	if err != nil {
		return writeSessionCategoryErr(c, err)
	}
	h.notify()
	return c.Status(201).JSON(cat)
}

// Rename PATCH /session-categories/:id  只改名稱，忽略 body 裡的 work_dir 與 id。
// 只要資料庫改名成功就通知一次，包含名稱其實沒變的請求。不在交易外先讀舊名。
func (h *SessionCategoryHandler) Rename(c *fiber.Ctx) error {
	var body struct {
		Name string `json:"name"`
	}
	if err := parseCategoryJSON(c, &body); err != nil {
		return writeSessionCategoryErr(c, err)
	}
	cat, err := h.db.RenameSessionCategory(c.Params("id"), body.Name)
	if err != nil {
		return writeSessionCategoryErr(c, err)
	}
	h.notify()
	return c.JSON(cat)
}

// Delete DELETE /session-categories/:id
// 走 DeleteSessionCategory，同一交易解綁，不刪 session。
func (h *SessionCategoryHandler) Delete(c *fiber.Ctx) error {
	if err := h.db.DeleteSessionCategory(c.Params("id")); err != nil {
		return writeSessionCategoryErr(c, err)
	}
	h.notify()
	return c.SendStatus(204)
}
