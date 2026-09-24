package mcp

import (
	"fmt"
	"strings"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

// wrapConsultEnvelope 是 MCP send_message 送到對方 session 的諮詢本文。
// 結構：送出者自介 header、原文、給接收方回覆用的署名 footer。from/to 不可為 nil。
func wrapConsultEnvelope(from, to *db.Session, body string) string {
	text := strings.TrimSpace(body)
	var b strings.Builder
	fmt.Fprintf(&b, "來自 Mini-App session%s 想詢問／討論：\n", formatSessionIntro(from))
	fmt.Fprintf(&b, "from_session_id: %s\n\n", from.ID)
	b.WriteString(text)
	b.WriteString("\n\n--\n")
	fmt.Fprintf(&b, "你是 session_id: %s%s\n", to.ID, formatSessionNameParen(to))
	fmt.Fprintf(&b, "回覆請用 miniapp MCP send_message(session_id=%q, from_session_id=%q, text=...)\n", from.ID, to.ID)
	return b.String()
}

func formatSessionIntro(s *db.Session) string {
	name := sessionDisplayName(s)
	agent := strings.TrimSpace(s.AgentType)
	if agent == "" {
		agent = "claude"
	}
	dir := strings.TrimSpace(s.WorkDir)
	if dir == "" {
		return fmt.Sprintf("「%s」（%s）", name, agent)
	}
	return fmt.Sprintf("「%s」（%s · %s）", name, agent, dir)
}

func formatSessionNameParen(s *db.Session) string {
	return fmt.Sprintf("（%s）", sessionDisplayName(s))
}

func sessionDisplayName(s *db.Session) string {
	name := strings.TrimSpace(s.Name)
	if name == "" {
		return "未命名"
	}
	return name
}
