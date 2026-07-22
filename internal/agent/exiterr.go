package agent

import (
	"fmt"
	"regexp"
	"strings"
)

var exitStatusOnlyRe = regexp.MustCompile(`^exit status \d+$`)

// ExitError 表示 CLI 子進程異常結束；Error() 優先回傳 stderr／stdout 診斷內容。
type ExitError struct {
	Tool   string
	Detail string
	Wait   error
}

func (e *ExitError) Error() string {
	tool := strings.TrimSpace(e.Tool)
	if tool == "" {
		tool = "agent"
	}
	if detail := compactDetail(e.Detail); detail != "" {
		return fmt.Sprintf("%s: %s", tool, detail)
	}
	if e.Wait != nil {
		return fmt.Sprintf("%s: %s", tool, e.Wait.Error())
	}
	return tool + " failed"
}

func compactDetail(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return truncateBytes(s, 500)
	}
	joined := strings.Join(lines, " | ")
	return truncateBytes(joined, 500)
}

func truncateBytes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// NewExitError 合併 stderr／stdout 診斷與 wait 錯誤為可讀 error。
func NewExitError(tool, detail string, wait error) error {
	detail = strings.TrimSpace(detail)
	if detail == "" && wait == nil {
		return fmt.Errorf("%s failed", tool)
	}
	return &ExitError{Tool: tool, Detail: detail, Wait: wait}
}

// PreferErrorText 選較有資訊的錯誤文字（避開僅 exit status N）。
func PreferErrorText(current, next string) string {
	current = strings.TrimSpace(current)
	next = strings.TrimSpace(next)
	if current == "" {
		return next
	}
	if next == "" {
		return current
	}
	curGeneric := isGenericExitStatus(current)
	nextGeneric := isGenericExitStatus(next)
	if curGeneric && !nextGeneric {
		return next
	}
	if !curGeneric && nextGeneric {
		return current
	}
	if len(next) > len(current) {
		return next
	}
	return current
}

func isGenericExitStatus(s string) bool {
	return exitStatusOnlyRe.MatchString(strings.TrimSpace(s))
}
