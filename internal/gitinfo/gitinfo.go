// Package gitinfo 依伺服器本機路徑偵測 Git 工作樹並解析目前分支（需 PATH 中有 git）。
package gitinfo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	runTimeout = 2 * time.Second
	cacheTTL   = 15 * time.Second
)

type cacheEntry struct {
	branch string
	ok     bool
	at     time.Time
}

var (
	cacheMu sync.Mutex
	cache   = map[string]cacheEntry{}
)

// Branch 若 workDir 為 Git 工作樹則回傳目前分支名稱（detached 時可能為短 hash）；否則 ok 為 false。
// 結果依絕對路徑快取 cacheTTL，避免 /sessions 列表對同一目錄反覆 spawn git。
func Branch(workDir string) (branch string, ok bool) {
	if strings.TrimSpace(workDir) == "" {
		return "", false
	}
	abs, err := filepath.Abs(workDir)
	if err != nil {
		return "", false
	}

	cacheMu.Lock()
	if e, hit := cache[abs]; hit && time.Since(e.at) < cacheTTL {
		cacheMu.Unlock()
		return e.branch, e.ok
	}
	cacheMu.Unlock()

	branch, ok = lookupBranch(abs)

	cacheMu.Lock()
	cache[abs] = cacheEntry{branch: branch, ok: ok, at: time.Now()}
	cacheMu.Unlock()
	return branch, ok
}

func lookupBranch(abs string) (string, bool) {
	fi, err := os.Stat(abs)
	if err != nil || !fi.IsDir() {
		return "", false
	}
	// 優先讀 .git/HEAD（無行程開銷）；失敗再 fallback 到 git CLI。
	if b, ok := branchFromGitDir(abs); ok {
		return b, true
	}

	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", abs, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return "", false
	}
	branch := strings.TrimSpace(string(out))
	if branch == "" {
		return "", false
	}
	return branch, true
}

func branchFromGitDir(workTree string) (string, bool) {
	gitPath := filepath.Join(workTree, ".git")
	fi, err := os.Stat(gitPath)
	if err != nil {
		return "", false
	}

	var headPath string
	if fi.IsDir() {
		headPath = filepath.Join(gitPath, "HEAD")
	} else {
		raw, err := os.ReadFile(gitPath)
		if err != nil {
			return "", false
		}
		line := strings.TrimSpace(string(raw))
		const prefix = "gitdir:"
		if !strings.HasPrefix(strings.ToLower(line), prefix) {
			return "", false
		}
		gitdir := strings.TrimSpace(line[len(prefix):])
		if gitdir == "" {
			return "", false
		}
		if !filepath.IsAbs(gitdir) {
			gitdir = filepath.Join(workTree, gitdir)
		}
		headPath = filepath.Join(gitdir, "HEAD")
	}

	raw, err := os.ReadFile(headPath)
	if err != nil {
		return "", false
	}
	s := strings.TrimSpace(string(raw))
	const heads = "ref: refs/heads/"
	if strings.HasPrefix(s, heads) {
		b := strings.TrimPrefix(s, heads)
		if b != "" {
			return b, true
		}
		return "", false
	}
	if strings.HasPrefix(s, "ref: ") {
		// 非 heads（如 tags/）交給 git CLI 解析
		return "", false
	}
	// detached HEAD：回傳短 hash
	if len(s) >= 7 && isHex(s) {
		return s[:7], true
	}
	return "", false
}

func isHex(s string) bool {
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}
