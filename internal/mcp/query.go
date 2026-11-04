package mcp

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const sqliteUTC = "2006-01-02 15:04:05"

// parseQueryTime 把 ISO8601／SQLite 時間轉成 UTC SQLite 字串，以便跟 messages.created_at 比對。
// 帶時區（例如 2026-09-09T00:00:00+08:00）會先轉 UTC；無時區視為 UTC。
func parseQueryTime(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	layouts := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	var t time.Time
	var err error
	for _, layout := range layouts {
		t, err = time.Parse(layout, s)
		if err == nil {
			return t.UTC().Format(sqliteUTC), nil
		}
	}
	return "", fmt.Errorf("無法解析時間 %q，請用 ISO8601（例如 2026-09-09T00:00:00+08:00）", s)
}

// sqliteToISO 把 UTC SQLite 時間字串轉成 ISO8601（RFC3339，Z 結尾）；空字串或無法解析時原樣回傳。
func sqliteToISO(s string) string {
	t, err := time.Parse(sqliteUTC, s)
	if err != nil {
		return s
	}
	return t.UTC().Format(time.RFC3339)
}

func truncateRunes(s string, n int) (string, bool) {
	if n <= 0 || utf8.RuneCountInString(s) <= n {
		return s, false
	}
	return string([]rune(s)[:n]) + "…", true
}

func normPath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimRight(p, "/")
	return strings.ToLower(p)
}

func clampLimit(n, def, max int) int {
	if n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}
