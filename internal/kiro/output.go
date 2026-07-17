package kiro

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
)

// kiroStreamState 追蹤單次 Run 的 stdout 分類進度。
//
// Kiro --no-interactive 的 stdout 混有三種語意：
//   - 首個 "> " 行之前：工具執行／思考鏈（EventThinking）
//   - 首個 "> " 行及之後的最終回覆（EventDelta，含無 "> " 的多行延續）
//   - 回覆開始後仍交錯出現的工具敘述行（EventActivity，不寫 DB）
//
// 注意：回覆開始後不可再送 EventThinking——前端收到 thinking 會覆寫訊息並清空 stream buffer。
type kiroStreamState struct {
	responseStarted bool
	streamStartSent bool
	thinkingBuf     strings.Builder
}

// isKiroResponseLine 判斷此行是否為最終回覆的起始行（剝除 ANSI 後以 "> " 開頭）。
func isKiroResponseLine(raw string) bool {
	clean := strings.TrimRight(stripAnsi(raw), "\r")
	return strings.HasPrefix(clean, "> ")
}

// toolResultItemRe 匹配 Kiro code/grep 工具回傳的符號列表行，例如：
//
//	1. Method ComputeFooIntent at service\foopkg\intent.go:57:1
var toolResultItemRe = regexp.MustCompile(`(?i)^\d+\.\s+(Method|Function|Type|Class|Variable|Interface|Struct|Const|Field)\s+\S+\s+at\s+\S+:\d+`)

// isKiroToolNarration 判斷剝除前綴後的一行是否為 Kiro CLI 固定格式的工具敘述／結果摘要。
// 這些是確定性 signature，不是語意猜測。
func isKiroToolNarration(text string) bool {
	s := strings.TrimSpace(text)
	if s == "" {
		return false
	}

	// 常見：同一行串接多段工具敘述，例如 Searching for: ... (using tool: grep)Searching for symbols...
	if strings.Contains(s, "(using tool:") {
		return true
	}

	switch {
	case strings.HasPrefix(s, "Reading file:"):
		return true
	case strings.HasPrefix(s, "Searching for:"):
		return true
	case strings.HasPrefix(s, "Searching for symbols matching:"):
		return true
	case strings.HasPrefix(s, "I will run the following command:"):
		return true
	case strings.HasPrefix(s, "Purpose:"):
		return true
	case strings.HasPrefix(s, "Batch ") && strings.Contains(s, "operation"):
		return true
	case strings.HasPrefix(s, "- Completed in "), strings.HasPrefix(s, "Completed in "):
		return true
	case strings.HasPrefix(s, "- Summary:"):
		return true
	case s == "⋮" || s == "...":
		return true
	}

	// 前綴符號：✓ Successfully...、✗ ...、↱ Operation N:...
	if r, rest := firstRune(s); r != 0 {
		switch r {
		case '✓', '✔', '✗', '✘':
			return true
		case '↱', '↳':
			return strings.HasPrefix(rest, "Operation ") || strings.Contains(s, "Operation ")
		}
	}

	// (3 more items found) / (15 more items found)
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") && strings.Contains(s, "more item") {
		return true
	}

	if toolResultItemRe.MatchString(s) {
		return true
	}

	return false
}

func firstRune(s string) (rune, string) {
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError && size == 0 {
		return 0, ""
	}
	return r, strings.TrimSpace(s[size:])
}

// dispatchLine 將單行 stdout 分類並透過 cb 送出對應 agent.Event。
func (st *kiroStreamState) dispatchLine(raw string, cb agent.EventCallback) {
	clean := strings.TrimRight(stripAnsi(raw), "\r")
	if strings.TrimSpace(clean) == "" {
		return
	}

	text := stripKiroPrefix(raw)

	if !st.responseStarted {
		if isKiroResponseLine(raw) {
			st.responseStarted = true
			st.emitStreamStart(cb)
			// 極罕見："> " 行本身若是工具敘述，仍不當正文。
			if text != "" && !isKiroToolNarration(text) {
				cb(agent.Event{Type: agent.EventDelta, Text: text + "\n"})
			} else if text != "" {
				cb(agent.Event{Type: agent.EventActivity, Text: text})
			}
			return
		}
		// 工具執行／思考鏈：首個 "> " 之前累積為 thinking（覆寫式）。
		if st.thinkingBuf.Len() > 0 {
			st.thinkingBuf.WriteString("\n")
		}
		st.thinkingBuf.WriteString(text)
		cb(agent.Event{Type: agent.EventThinking, Text: st.thinkingBuf.String()})
		return
	}

	// 回覆開始後仍交錯的工具敘述：走 activity（不寫 DB、不清空 stream）。
	if isKiroToolNarration(text) {
		cb(agent.Event{Type: agent.EventActivity, Text: text})
		return
	}

	// 回覆延續行（多行 markdown 僅首行帶 "> " 前綴）。
	if text != "" {
		cb(agent.Event{Type: agent.EventDelta, Text: text + "\n"})
	}
}

// flushFallback 若全程未見 "> " 行，將累積內容降級為回覆（罕見 edge case）。
func (st *kiroStreamState) flushFallback(cb agent.EventCallback) {
	if st.responseStarted || st.thinkingBuf.Len() == 0 {
		return
	}
	st.responseStarted = true
	st.emitStreamStart(cb)
	cb(agent.Event{Type: agent.EventDelta, Text: st.thinkingBuf.String()})
}

func (st *kiroStreamState) emitStreamStart(cb agent.EventCallback) {
	if st.streamStartSent {
		return
	}
	st.streamStartSent = true
	cb(agent.Event{Type: agent.EventStreamStart})
}
