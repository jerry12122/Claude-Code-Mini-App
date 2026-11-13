package api

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/auth"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/proc"
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

func startDetached(cmd *exec.Cmd) error {
	// `code` 在 Windows 是 code.cmd（經 cmd.exe），桌面版無主控台時會彈黑窗。
	cmd.SysProcAttr = proc.SysProcAttr()
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func folderOpenCommand(workDir string) *exec.Cmd {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("explorer", workDir)
	case "darwin":
		return exec.Command("open", workDir)
	default:
		return exec.Command("xdg-open", workDir)
	}
}

// vscodeURI 把 workDir 轉成 vscode://file/<path> URI；Windows 路徑分隔符須換成 /。
// workDir 在呼叫前已經過 resolveWorkDir 驗證為存在的真實目錄（filepath.Abs + os.Stat），
// 不是使用者可任意輸入的字串，故不需額外跳脫特殊字元。
func vscodeURI(workDir string) string {
	p := filepath.ToSlash(workDir)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return "vscode://file" + p
}

// vscodeNoAdminCommand 透過 explorer.exe 分派 vscode:// URI 啟動 VSCode。
// explorer.exe 是使用者桌面殼層，永遠以一般使用者（非提升）權限執行；由它去解析已註冊的
// vscode:// URI handler，啟動出來的 VSCode 進程完整性等級即為 Medium（非提升），
// 藉此避免以系統管理員身分執行本程式時，VSCode 因與使用者現有的非提升執行個體權限
// 不一致而拒絕開啟（"Another instance of Code is already running as administrator"）。
// 僅在 runtime.GOOS == "windows" 時被呼叫；runas /trustlevel 在部分 Windows 11
// 版本上有已知 bug 而不可用，改用此法。
func vscodeNoAdminCommand(workDir string) *exec.Cmd {
	return exec.Command("explorer.exe", vscodeURI(workDir))
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
	cmd := exec.Command("code", workDir)
	if runtime.GOOS == "windows" {
		if gs, err := h.db.GetGeneralSettings(); err == nil && gs.VscodeNoAdmin {
			cmd = vscodeNoAdminCommand(workDir)
		}
	}
	if err := startDetached(cmd); err != nil {
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
	if err := startDetached(folderOpenCommand(workDir)); err != nil {
		return jsonErr(c, 500, "開啟檔案總管失敗: "+err.Error())
	}
	return c.JSON(fiber.Map{"ok": true})
}
