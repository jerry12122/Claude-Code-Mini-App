package cursor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// Windows 的 cursor-agent.cmd 會經過 cmd.exe 與 PowerShell 5.1 才到 node，
// 引號、%VAR%、!x!、LF 都會被改寫或截斷。這裡照官方 cursor-agent.ps1 的規則
// 找到 node.exe 與 index.js，由 Go 直接啟動以避開這兩層。

// 與官方 cursor-agent.ps1 相同：YYYY.M.D[-HH-MM-SS]-<commit>。
var versionDirRe = regexp.MustCompile(`^(\d{4})\.(\d{1,2})\.(\d{1,2})(-\d{2}-\d{2}-\d{2})?-[a-f0-9]+$`)

// findNode 在 shimDir（cursor-agent.cmd 所在目錄）下找 node.exe 與 index.js。
// 先找同層，否則取 versions\ 下日期最新的目錄；找不到回傳 ok=false。
func findNode(shimDir string) (node, index string, ok bool) {
	if hasPair(shimDir) {
		return filepath.Join(shimDir, "node.exe"), filepath.Join(shimDir, "index.js"), true
	}
	entries, err := os.ReadDir(filepath.Join(shimDir, "versions"))
	if err != nil {
		return "", "", false
	}
	type cand struct {
		name string
		date int
	}
	var cands []cand
	for _, e := range entries {
		m := versionDirRe.FindStringSubmatch(e.Name())
		if !e.IsDir() || m == nil {
			continue
		}
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		d, _ := strconv.Atoi(m[3])
		cands = append(cands, cand{e.Name(), y*10000 + mo*100 + d})
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].date != cands[j].date {
			return cands[i].date > cands[j].date
		}
		return cands[i].name > cands[j].name
	})
	if len(cands) == 0 {
		return "", "", false
	}
	dir := filepath.Join(shimDir, "versions", cands[0].name)
	if !hasPair(dir) {
		return "", "", false
	}
	return filepath.Join(dir, "node.exe"), filepath.Join(dir, "index.js"), true
}

func hasPair(dir string) bool {
	for _, f := range []string{"node.exe", "index.js"} {
		if st, err := os.Stat(filepath.Join(dir, f)); err != nil || st.IsDir() {
			return false
		}
	}
	return true
}

// resolveDirect 僅在 Windows 且找得到官方佈局時回傳 node 啟動參數。
// 先執行 versions\promote-staged.cmd（無參數），讓背景下載的更新照官方流程生效。
func resolveDirect(ctx context.Context) (node, index string, ok bool) {
	if runtime.GOOS != "windows" {
		return "", "", false
	}
	shim, err := exec.LookPath("cursor-agent")
	if err != nil {
		return "", "", false
	}
	dir := filepath.Dir(shim)
	if p := filepath.Join(dir, "versions", "promote-staged.cmd"); fileExists(p) {
		if staged, _ := filepath.Glob(filepath.Join(dir, "versions", "*.new")); len(staged) > 0 {
			_ = exec.CommandContext(ctx, p).Run()
		}
	}
	return findNode(dir)
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// shimPrompt 是走 cursor-agent.cmd 時的有損後備：LF 轉字面 \n，" 預先跳脫為 \"。
// %VAR% 與 !x! 仍會被 cmd.exe 改寫，無法補救。
func shimPrompt(p string) string {
	p = strings.ReplaceAll(p, "\n", `\n`)
	return strings.ReplaceAll(p, `"`, `\"`)
}

// directEnv 補上官方 cursor-agent.cmd/.ps1 會設定的環境變數。
func directEnv() []string {
	env := os.Environ()
	if os.Getenv("CURSOR_INVOKED_AS") == "" {
		env = append(env, "CURSOR_INVOKED_AS=cursor-agent.cmd")
	}
	if os.Getenv("NODE_COMPILE_CACHE") == "" {
		env = append(env, "NODE_COMPILE_CACHE="+filepath.Join(os.Getenv("LOCALAPPDATA"), "cursor-compile-cache"))
	}
	return env
}
