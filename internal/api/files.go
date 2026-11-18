package api

import (
	"errors"
	"log/slog"

	"github.com/gofiber/fiber/v2"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/fsbrowse"
)

// FilesHandler 提供 session work_dir 的唯讀瀏覽。路徑驗證與安全邊界都在 fsbrowse；
// 這裡只負責找出 work_dir、把錯誤對應成 HTTP 狀態。
// 分享訪客不在 guestRouteAllowed 的放行清單內，由 authMiddleware 以 403 擋下，這裡不重複判斷。
type FilesHandler struct {
	db *db.DB
}

func NewFilesHandler(database *db.DB) *FilesHandler { return &FilesHandler{db: database} }

// List GET /sessions/:id/files?path=<相對路徑>
func (h *FilesHandler) List(c *fiber.Ctx) error {
	root, status, msg := sessionWorkDir(h.db, c.Params("id"))
	if status != 0 {
		return jsonErr(c, status, msg)
	}
	l, err := fsbrowse.List(root, c.Query("path"))
	if err != nil {
		return filesErr(c, err)
	}
	c.Set("Cache-Control", "no-store")
	return c.JSON(l)
}

// Content GET /sessions/:id/files/content?path=<相對路徑>
func (h *FilesHandler) Content(c *fiber.Ctx) error {
	root, status, msg := sessionWorkDir(h.db, c.Params("id"))
	if status != 0 {
		return jsonErr(c, status, msg)
	}
	f, err := fsbrowse.ReadText(root, c.Query("path"))
	if err != nil {
		return filesErr(c, err)
	}
	c.Set("Cache-Control", "no-store")
	return c.JSON(f)
}

// filesErr 只回固定訊息：底層 os 錯誤帶完整絕對路徑，不能原樣交給使用者。
func filesErr(c *fiber.Ctx, err error) error {
	var tl *fsbrowse.TooLargeError
	switch {
	case errors.As(err, &tl):
		return c.Status(413).JSON(fiber.Map{"error": tl.Error(), "size": tl.Size})
	case errors.Is(err, fsbrowse.ErrProtected):
		return jsonErr(c, 403, err.Error())
	case errors.Is(err, fsbrowse.ErrNotFound):
		return jsonErr(c, 404, err.Error())
	case errors.Is(err, fsbrowse.ErrBinary):
		return jsonErr(c, 415, err.Error())
	case errors.Is(err, fsbrowse.ErrInvalidPath), errors.Is(err, fsbrowse.ErrIsDir), errors.Is(err, fsbrowse.ErrNotDir):
		return jsonErr(c, 400, err.Error())
	}
	slog.Warn("[files] 未預期的錯誤類型", "err", err)
	return jsonErr(c, 500, "讀取失敗")
}
