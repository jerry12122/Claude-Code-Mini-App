//go:build windows

package desktopupdate

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDesktopUpdateHelperStartup 以真正的桌面建置執行 helper 模式。
// 工作目錄內放了可讀取的設定與 DB 路徑，且原服務正佔用 port；helper 必須先回報 ready，
// 不讀設定、不建立 DB／log 目錄、不綁 port，且原設定與原執行檔不被改動。
func TestDesktopUpdateHelperStartup(t *testing.T) {
	exe := buildDesktop(t)
	work := t.TempDir()
	writeFile(t, filepath.Join(filepath.Dir(exe), "installed.flag"), "")
	localData := filepath.Join(work, "installed-user-data")

	// 原服務正佔用此 port；helper 不得嘗試綁定。
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	port := occupied.Addr().(*net.TCPAddr).Port

	configPath := filepath.Join(work, "config.yaml")
	dbPath := filepath.Join(work, "app.db")
	writeFile(t, configPath, fmt.Sprintf("no_auth: true\nserver:\n  port: %d\ndb:\n  path: \"%s\"\n", port, filepath.ToSlash(dbPath)))
	configBefore := readFile(t, configPath)

	target := filepath.Join(work, "claude-miniapp-desktop.exe")
	writeFile(t, target, "ORIGINAL-EXE")
	staged := filepath.Join(work, "wails-update-fixture", "artifact.exe")
	writeFile(t, staged, "NEW-EXE")
	readyPath := filepath.Join(work, "ready.txt")

	cmd := exec.Command(exe)
	cmd.Dir = work
	cmd.Env = append(os.Environ(),
		"LOCALAPPDATA="+localData,
		"WAILS_UPDATER_HELPER=1",
		"WAILS_UPDATER_HELPER_TARGET="+target,
		"WAILS_UPDATER_HELPER_NEW="+staged,
		"WAILS_UPDATER_HELPER_PID=0", // 0：不等待父行程，直接進入替換流程
		"WAILS_UPDATER_HELPER_LOG="+filepath.Join(work, "helper.log"),
		"WAILS_UPDATER_HELPER_READY="+readyPath,
	)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill() // 只停止本測試啟動的行程
		<-done
		t.Fatal("helper 逾時未結束（修正前會以一般啟動執行服務）")
	}

	ready, err := os.ReadFile(readyPath)
	if err != nil {
		t.Fatalf("helper 未回報 ready：%v", err)
	}
	if string(ready) != "ready" {
		t.Fatalf("ready 內容 = %q，want ready", ready)
	}
	if got := readFile(t, configPath); got != configBefore {
		t.Error("helper 改動了設定檔")
	}
	if _, err := os.Stat(dbPath); err == nil {
		t.Error("helper 不應初始化 DB")
	}
	if _, err := os.Stat(filepath.Join(work, "logs")); err == nil {
		t.Error("helper 不應初始化應用程式 log 目錄")
	}
	if _, err := os.Stat(localData); !os.IsNotExist(err) {
		t.Error("helper 不應建立安裝版資料或 WebView2 profile")
	}
	if got := readFile(t, target); !strings.Contains(got, "ORIGINAL-EXE") {
		t.Errorf("替換失敗後應保留原執行檔，得到 %q", got)
	}
}

func buildDesktop(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "desktop-under-test.exe")
	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Dir = filepath.Join("..", "..", "cmd", "desktop")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("建置桌面版失敗：%v\n%s", err, b)
	}
	return out
}
