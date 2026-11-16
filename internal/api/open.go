package api

import (
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/auth"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/opener"
)

// OpenHandler 提供「在伺服器主機開啟 VSCode／檔案總管」，僅允許本機存取。
// 指令與參數皆固定，不接受使用者輸入拼接；不受 shell.enabled 控制。
type OpenHandler struct {
	db *db.DB
}

func NewOpenHandler(database *db.DB) *OpenHandler {
	return &OpenHandler{db: database}
}

func jsonErr(c *fiber.Ctx, status int, msg string) error {
	return c.Status(status).JSON(fiber.Map{"error": msg})
}

// hostOpenAllowed 同時檢查實際連線、可信代理解析的來源與原始 Host，避免轉發標頭或 DNS rebinding 繞過本機限制。
func hostOpenAllowed(c *fiber.Ctx) bool {
	if !c.Context().RemoteIP().IsLoopback() || !net.ParseIP(auth.RealIP(c)).IsLoopback() {
		return false
	}
	host := strings.ToLower(string(c.Request().Host()))
	if name, _, err := net.SplitHostPort(host); err == nil {
		host = name
	}
	return host == "127.0.0.1" || host == "localhost" || host == "::1" || host == "[::1]"
}

// resolveWorkDir 取得 session 的 work_dir，並確認為本機請求、目錄存在。
// 失敗時回傳 HTTP 狀態與錯誤訊息（status != 0）。
func (h *OpenHandler) resolveWorkDir(c *fiber.Ctx) (string, int, string) {
	if !hostOpenAllowed(c) {
		return "", 403, "僅允許透過本機位址開啟 VSCode 或目錄"
	}
	return sessionWorkDir(h.db, c.Params("id"))
}

// sessionWorkDir 取得 session 的 work_dir 絕對路徑並確認目錄存在；失敗時 status != 0。
func sessionWorkDir(database *db.DB, sessionID string) (string, int, string) {
	sess, err := database.GetSession(sessionID)
	if err != nil {
		return "", 404, "session 不存在"
	}
	raw := strings.TrimSpace(sess.WorkDir)
	if raw == "" {
		return "", 400, "此 session 未設定 work_dir"
	}
	abs, err := filepath.Abs(filepath.Clean(raw))
	if err != nil {
		return "", 400, "work_dir 無效"
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return "", 400, "work_dir 不存在或不是目錄"
	}
	return abs, 0, ""
}

// OpenVSCode POST /sessions/:id/open-vscode — 在伺服器主機以 `code <work_dir>` 開啟 VSCode。
// 若「一般設定」開了 vscodeNoAdmin，且伺服器本身以系統管理員身分執行（Windows），
// 改用 vscode:// URI + explorer.exe 以一般使用者權限啟動，避免 VSCode 因權限不一致
// 跳出「Another instance of Code is already running as administrator」錯誤。
func (h *OpenHandler) OpenVSCode(c *fiber.Ctx) error {
	workDir, status, msg := h.resolveWorkDir(c)
	if status != 0 {
		return jsonErr(c, status, msg)
	}
	noAdmin := false
	if gs, err := h.db.GetGeneralSettings(); err == nil {
		noAdmin = gs.VscodeNoAdmin
	}
	if err := opener.VSCode(workDir, noAdmin); err != nil {
		return jsonErr(c, 500, "啟動 code 失敗（需在伺服器 PATH 內安裝 VSCode CLI）: "+err.Error())
	}
	return c.JSON(fiber.Map{"ok": true})
}

// OpenFolder POST /sessions/:id/open-folder — 在伺服器主機開啟 work_dir 所在的檔案總管。
func (h *OpenHandler) OpenFolder(c *fiber.Ctx) error {
	workDir, status, msg := h.resolveWorkDir(c)
	if status != 0 {
		return jsonErr(c, status, msg)
	}
	if err := opener.Folder(workDir); err != nil {
		return jsonErr(c, 500, "開啟檔案總管失敗: "+err.Error())
	}
	return c.JSON(fiber.Map{"ok": true})
}
