package shell

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ValidateAllowlist 在 allowlist 非空時檢查指令；通過回傳 nil。
// allowlist 為空：一律通過（不啟用白名單限制）。
func ValidateAllowlist(line string, allowlist []string) error {
	if len(allowlist) == 0 {
		return nil
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return fmt.Errorf("指令為空")
	}
	if containsShellChainingOrSubstitution(line) {
		return fmt.Errorf("不允許指令串接、管線或換行（&&、||、|、; 等）")
	}
	if tokenMatchesAllowlist(line, allowlist) {
		return nil
	}
	return fmt.Errorf("指令不在允許清單內（請見設定檔 shell.allowed_commands）")
}

// CommandAllowed 為 ValidateAllowlist 的布林包裝，供測試與簡單判斷。
func CommandAllowed(line string, allowlist []string) bool {
	return ValidateAllowlist(line, allowlist) == nil
}

func tokenMatchesAllowlist(line string, allowlist []string) bool {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return false
	}
	cmd := normalizeCommandToken(parts[0])
	if cmd == "" {
		return false
	}
	for _, a := range allowlist {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if cmd == normalizeCommandToken(a) {
			return true
		}
	}
	return false
}

func normalizeCommandToken(t string) string {
	t = strings.TrimSpace(t)
	if t == "" {
		return ""
	}
	t = filepath.Base(t)
	t = strings.ToLower(t)
	t = strings.TrimSuffix(t, ".exe")
	t = strings.TrimSuffix(t, ".cmd")
	t = strings.TrimSuffix(t, ".bat")
	t = strings.TrimSuffix(t, ".com")
	return t
}

func containsShellChainingOrSubstitution(line string) bool {
	if strings.Contains(line, "&&") || strings.Contains(line, "||") {
		return true
	}
	if strings.Contains(line, "\n") || strings.Contains(line, "\r") {
		return true
	}
	if strings.Contains(line, "|") {
		return true
	}
	if strings.Contains(line, ";") {
		return true
	}
	if strings.Contains(line, "`") {
		return true
	}
	if strings.Contains(line, "$(") {
		return true
	}
	return false
}
