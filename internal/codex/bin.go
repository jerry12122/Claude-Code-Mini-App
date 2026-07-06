package codex

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ResolveBin 解析 codex 可執行檔路徑。
// 服務進程的 PATH 常不含使用者 shell 的路徑（例如 Windows 桌面版 Codex 安裝目錄）。
func ResolveBin() (string, error) {
	if p := strings.TrimSpace(os.Getenv("CODEX_BIN")); p != "" {
		if _, err := os.Stat(p); err != nil {
			return "", fmt.Errorf("CODEX_BIN 指向的路徑不存在: %s", p)
		}
		return p, nil
	}
	if p, err := exec.LookPath("codex"); err == nil {
		return p, nil
	}
	if runtime.GOOS == "windows" {
		if p := windowsDefaultBin(); p != "" {
			return p, nil
		}
	}
	return "", fmt.Errorf("找不到 codex CLI：請確認已安裝，或設定 CODEX_BIN 指向 codex 可執行檔")
}

func windowsDefaultBin() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	p := filepath.Join(home, "AppData", "Local", "Programs", "OpenAI", "Codex", "bin", "codex.exe")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

// HasAuthConfig 檢查是否有可用的 Codex 認證（環境變數或本機 auth.json）。
func HasAuthConfig() bool {
	if strings.TrimSpace(os.Getenv("CODEX_API_KEY")) != "" {
		return true
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(home, ".codex", "auth.json"))
	return err == nil
}
