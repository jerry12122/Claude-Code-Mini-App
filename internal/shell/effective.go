package shell

import (
	"fmt"
	"strings"
)

// EffectiveAllowlist 合併設定檔全域清單與 DB 依 work_dir 累積的指令（去重、正規化）。
func EffectiveAllowlist(global []string, workdirCmds []string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, s := range append(append([]string{}, global...), workdirCmds...) {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		n := normalizeCommandToken(s)
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	return out
}

// FirstCommandName 回傳正規化後的第一個指令名（basename、小寫等）。
func FirstCommandName(line string) string {
	parts := strings.Fields(strings.TrimSpace(line))
	if len(parts) == 0 {
		return ""
	}
	return normalizeCommandToken(parts[0])
}

// ClassifyShellLine：先檢查串接字元；effective 為「允許集合」聯集（設定檔 ∪ DB）。
// 空集合表示尚無任何允許的指令名稱 → 第一個 token 不可能屬於該集合，一律待確認（見 docs/shell-allowlist-schema.md）。
// 非空時：第一 token 在清單內則直接執行，否則待確認。
func ClassifyShellLine(line string, effective []string) (runDirect bool, needConfirm bool, err error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return false, false, fmt.Errorf("指令為空")
	}
	if containsShellChainingOrSubstitution(line) {
		return false, false, fmt.Errorf("不允許指令串接、管線或換行（&&、||、|、; 等）")
	}
	if len(effective) == 0 {
		return false, true, nil
	}
	if tokenMatchesAllowlist(line, effective) {
		return true, false, nil
	}
	return false, true, nil
}

// LineContainsShellChaining 若字串含串接或替換字元則為 true（供執行前再次檢查）。
func LineContainsShellChaining(line string) bool {
	return containsShellChainingOrSubstitution(strings.TrimSpace(line))
}
