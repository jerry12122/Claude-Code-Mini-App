package mcp

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

// DefaultMaxHops 是 session 互問的預設跳數上限（config mcp_max_hops 可覆寫）。
const DefaultMaxHops = 5

// consultMode 決定 envelope footer 教接收方怎麼回覆。
type consultMode int

const (
	// consultAsync：send_message，發送方不等待；接收方自行判斷是否需要回覆。
	consultAsync consultMode = iota
	// consultAsk：ask_session，發問方卡在 tool call 等這一輪結果，接收方直接回答即可。
	consultAsk
)

var hopLineRe = regexp.MustCompile(`(?m)^hop: (\d+)\s*$`)

// nextHop 由送出者「最近一則 user 訊息」推算新訊息的跳數：
// 那則訊息若是別的 session 送來的諮詢（含 hop: N），新訊息就是 N+1；
// 否則是人發起的（使用者直接輸入），從 1 起算——人一插話計數自然歸零。
func nextHop(senderLatestUserMsg string) int {
	m := hopLineRe.FindAllStringSubmatch(senderLatestUserMsg, -1)
	if len(m) == 0 {
		return 1
	}
	// 取最大值：本文若引用了別的 envelope 會出現多個 hop 行，取最大較不會被低估而繞過上限。
	max := 0
	for _, g := range m {
		if n, err := strconv.Atoi(g[1]); err == nil && n > max {
			max = n
		}
	}
	return max + 1
}

// checkHop 超過上限時回錯誤，擋住 A↔B 互相轉問無限燒額度。
func checkHop(hop, maxHops int) error {
	if maxHops <= 0 {
		maxHops = DefaultMaxHops
	}
	if hop > maxHops {
		return fmt.Errorf("session 互問已達上限 %d 跳（本次為第 %d 跳），為避免循環已拒絕；請停止轉問，直接回覆使用者", maxHops, hop)
	}
	return nil
}

// wrapConsultEnvelope 是 MCP send_message / ask_session 送到對方 session 的諮詢本文。
// 結構：送出者自介 header（含 hop）、原文、給接收方的回覆說明 footer。from/to 不可為 nil。
func wrapConsultEnvelope(from, to *db.Session, body string, hop int, mode consultMode) string {
	text := strings.TrimSpace(body)
	var b strings.Builder
	fmt.Fprintf(&b, "來自 Mini-App session%s 想詢問／討論：\n", formatSessionIntro(from))
	fmt.Fprintf(&b, "from_session_id: %s\n", from.ID)
	fmt.Fprintf(&b, "hop: %d\n\n", hop)
	b.WriteString(text)
	b.WriteString("\n\n--\n")
	fmt.Fprintf(&b, "你是 session_id: %s%s\n", to.ID, formatSessionNameParen(to))
	if mode == consultAsk {
		b.WriteString("對方正同步等待你這一輪的回答：直接回答即可，不要再用 send_message 回覆。\n")
	} else {
		fmt.Fprintf(&b, "若需要回覆，使用 miniapp MCP send_message(session_id=%q, from_session_id=%q, text=...)；是否回覆由目前的工作流程決定，純通知不必回覆。\n", from.ID, to.ID)
	}
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
