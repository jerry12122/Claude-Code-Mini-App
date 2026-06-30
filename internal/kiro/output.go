package kiro

import (
	"strings"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
)

// kiroStreamState 追蹤單次 Run 的 stdout 分類進度。
//
// Kiro --no-interactive 的 stdout 混有兩種語意：
//   - 首個 "> " 行之前：工具執行／思考鏈（EventThinking）
//   - 首個 "> " 行及之後：最終回覆（EventDelta，含無 "> " 的多行延續）
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
			if text != "" {
				cb(agent.Event{Type: agent.EventDelta, Text: text + "\n"})
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
