package usage

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	resetsAtTZRe   = regexp.MustCompile(`^(.+?)\s*\(([^)]+)\)\s*$`)
	resetsAtHumanRe = regexp.MustCompile(`(?i)^([A-Za-z]{3})\s+(\d{1,2}),\s+(\d{1,2}):(\d{2})\s*(am|pm)$`)
)

var monthAbbrev = map[string]time.Month{
	"jan": time.January, "feb": time.February, "mar": time.March, "apr": time.April,
	"may": time.May, "jun": time.June, "jul": time.July, "aug": time.August,
	"sep": time.September, "oct": time.October, "nov": time.November, "dec": time.December,
}

// ParseResetsAt 解析 Claude quota 的 resets_at（OAuth ISO8601 或 /usage 人類可讀字串）。
func ParseResetsAt(s string, ref time.Time) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}

	datePart := s
	loc := ref.Location()
	if m := resetsAtTZRe.FindStringSubmatch(s); len(m) == 3 {
		datePart = strings.TrimSpace(m[1])
		if name := strings.TrimSpace(m[2]); name != "" {
			if l, err := time.LoadLocation(name); err == nil {
				loc = l
			}
		}
	}

	if t, ok := parseClaudeHumanResetsAt(datePart, loc, ref); ok {
		return t, true
	}
	return time.Time{}, false
}

func parseClaudeHumanResetsAt(datePart string, loc *time.Location, ref time.Time) (time.Time, bool) {
	m := resetsAtHumanRe.FindStringSubmatch(strings.TrimSpace(datePart))
	if len(m) != 6 {
		return time.Time{}, false
	}
	mon, ok := monthAbbrev[strings.ToLower(m[1])]
	if !ok {
		return time.Time{}, false
	}
	day, err1 := strconv.Atoi(m[2])
	hour, err2 := strconv.Atoi(m[3])
	min, err3 := strconv.Atoi(m[4])
	if err1 != nil || err2 != nil || err3 != nil {
		return time.Time{}, false
	}
	if strings.EqualFold(m[5], "pm") && hour < 12 {
		hour += 12
	}
	if strings.EqualFold(m[5], "am") && hour == 12 {
		hour = 0
	}
	t := time.Date(ref.Year(), mon, day, hour, min, 0, 0, loc)
	if t.Before(ref) {
		t = t.Add(24 * time.Hour)
	}
	return t, true
}

// FormatDurationUntil 將距離 reset 的剩餘時間格式化（如 Resets in 4 hr 54 min）。
func FormatDurationUntil(until, now time.Time) string {
	d := until.Sub(now)
	if d <= 0 {
		return ""
	}
	mins := int(d.Round(time.Minute).Minutes())
	if mins < 1 {
		return "Resets in <1 min"
	}
	if mins < 60 {
		return fmt.Sprintf("Resets in %d min", mins)
	}
	h := mins / 60
	m := mins % 60
	if m == 0 {
		return fmt.Sprintf("Resets in %d hr", h)
	}
	return fmt.Sprintf("Resets in %d hr %d min", h, m)
}
