package kiro

import (
	"bytes"
	"context"
	"log"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// kiroSession 是 --list-sessions 解析出的單一 session 條目。
type kiroSession struct {
	ID    string
	Title string // list 中顯示的 prompt 摘要（第一則 user 訊息）
}

var (
	sessionIDLineRe = regexp.MustCompile(`(?i)^Chat SessionId:\s+([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\s*$`)
	sessionMetaRe   = regexp.MustCompile(`^\s*.+\|\s*(.+?)\s*\|\s*\d+\s+msgs`)
)

// parseListSessions 從 --list-sessions 的 stderr（已剝 ANSI）解析所有 session。
func parseListSessions(stderr string) []kiroSession {
	lines := strings.Split(stderr, "\n")
	out := make([]kiroSession, 0, 4)

	var pending *kiroSession
	for _, line := range lines {
		if m := sessionIDLineRe.FindStringSubmatch(strings.TrimSpace(line)); len(m) >= 2 {
			if pending != nil {
				out = append(out, *pending)
			}
			pending = &kiroSession{ID: strings.TrimSpace(m[1])}
			continue
		}
		if pending == nil {
			continue
		}
		if m := sessionMetaRe.FindStringSubmatch(line); len(m) >= 2 {
			pending.Title = strings.TrimSpace(m[1])
			out = append(out, *pending)
			pending = nil
		}
	}
	if pending != nil {
		out = append(out, *pending)
	}
	return out
}

// listSessions 執行 kiro-cli chat --list-sessions 並解析結果。
func listSessions(workDir string) ([]kiroSession, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "kiro-cli", "chat", "--list-sessions")
	if workDir != "" {
		cmd.Dir = workDir
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	return parseListSessions(stripAnsi(stderrBuf.String())), nil
}

// sessionIDSet 將 session 列表轉為 id → 存在 的 set。
func sessionIDSet(sessions []kiroSession) map[string]struct{} {
	set := make(map[string]struct{}, len(sessions))
	for _, s := range sessions {
		if s.ID != "" {
			set[s.ID] = struct{}{}
		}
	}
	return set
}

// normalizePromptKey 正規化 prompt 供比對（小寫、合併空白）。
func normalizePromptKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	return strings.Join(strings.Fields(s), " ")
}

// promptMatchesTitle 判斷 list 中的 title 是否對應本次 prompt。
// Kiro 可能截斷 title，因此採雙向 prefix 比對。
func promptMatchesTitle(prompt, title string) bool {
	p := normalizePromptKey(prompt)
	t := normalizePromptKey(title)
	if p == "" || t == "" {
		return false
	}
	if strings.HasPrefix(p, t) || strings.HasPrefix(t, p) {
		return true
	}
	// 取較短者做 prefix，容許 title 被截斷
	minLen := len(p)
	if len(t) < minLen {
		minLen = len(t)
	}
	if minLen < 8 {
		return p == t
	}
	return p[:minLen] == t[:minLen]
}

// resolveNewSessionID 以 before/after 快照 diff 找出本次新建的 session id。
//
// 優先順序：
//  1. after 中 id 不在 before 的唯一新 session
//  2. 多個新 session 時，以 prompt 比對 title
//  3. 無新 session 時，在 after 中找 title 符合 prompt 的（可能是 rare edge case）
//  4. 仍無法判定時返回空字串（降級單回合）
func resolveNewSessionID(before, after []kiroSession, prompt string) string {
	beforeSet := sessionIDSet(before)

	var newOnes []kiroSession
	for _, s := range after {
		if s.ID == "" {
			continue
		}
		if _, ok := beforeSet[s.ID]; !ok {
			newOnes = append(newOnes, s)
		}
	}

	switch len(newOnes) {
	case 1:
		log.Printf("[kiro] session diff: 1 new session id=%s title=%q", newOnes[0].ID, newOnes[0].Title)
		return newOnes[0].ID
	case 0:
		log.Printf("[kiro] session diff: no new session, try prompt match among %d existing", len(after))
	default:
		log.Printf("[kiro] session diff: %d new sessions, disambiguate by prompt", len(newOnes))
		if id := matchByPrompt(newOnes, prompt); id != "" {
			return id
		}
		// 多個新 session 但 prompt 對不上：取 list 順序第一個（最新）
		log.Printf("[kiro] session diff: prompt match failed, fallback to first new session id=%s", newOnes[0].ID)
		return newOnes[0].ID
	}

	if id := matchByPrompt(after, prompt); id != "" {
		log.Printf("[kiro] session diff: matched existing session by prompt id=%s", id)
		return id
	}

	log.Printf("[kiro] session diff: could not resolve session id")
	return ""
}

func matchByPrompt(sessions []kiroSession, prompt string) string {
	for _, s := range sessions {
		if promptMatchesTitle(prompt, s.Title) {
			return s.ID
		}
	}
	return ""
}

// fetchSessionIDAfterRun 在 chat 完成後，以 before 快照 + prompt 比對取得 session id。
func fetchSessionIDAfterRun(workDir, prompt string, before []kiroSession) string {
	after, err := listSessions(workDir)
	if err != nil {
		log.Printf("[kiro] --list-sessions (after) 失敗: %v", err)
		return ""
	}
	log.Printf("[kiro] --list-sessions: before=%d after=%d", len(before), len(after))
	return resolveNewSessionID(before, after, prompt)
}
