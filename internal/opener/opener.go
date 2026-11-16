// Package opener 在伺服器主機啟動桌面殼層程式（檔案總管、VSCode）。
//
// 命令名稱與旗標皆為固定字面值，workDir 只作為單一參數傳入，不經 shell 拼接。
// 呼叫端必須先驗證 workDir 是實際存在的目錄，並自行限制誰能觸發（目前僅本機）。
package opener

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/proc"
)

// Folder 開啟 workDir 所在的檔案總管。
func Folder(workDir string) error { return startDetached(folderCommand(workDir)) }

// VSCode 以 `code <workDir>` 開啟 VSCode。
// preferNoAdmin 為真且在 Windows 上時，改走 vscode:// URI 以一般使用者權限啟動。
func VSCode(workDir string, preferNoAdmin bool) error {
	cmd := exec.Command("code", workDir)
	if preferNoAdmin && runtime.GOOS == "windows" {
		cmd = vscodeNoAdminCommand(workDir)
	}
	return startDetached(cmd)
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

func folderCommand(workDir string) *exec.Cmd {
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
// workDir 在呼叫前已由呼叫端驗證為存在的真實目錄（filepath.Abs + os.Stat），
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
