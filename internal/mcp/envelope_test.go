package mcp

import (
	"strings"
	"testing"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

func TestWrapConsultEnvelopeContainsIdentitiesAndBody(t *testing.T) {
	from := &db.Session{
		ID:        "11111111-1111-1111-1111-111111111111",
		Name:      "規劃",
		AgentType: "claude",
		WorkDir:   `E:\workspace\app`,
	}
	to := &db.Session{
		ID:        "22222222-2222-2222-2222-222222222222",
		Name:      "實作",
		AgentType: "cursor",
		WorkDir:   `E:\workspace\app`,
	}
	got := wrapConsultEnvelope(from, to, "  這個 API 怎麼用？  ", 2, consultAsync)

	mustContain := []string{
		"想詢問／討論",
		"「規劃」（claude · E:\\workspace\\app）",
		"from_session_id: 11111111-1111-1111-1111-111111111111",
		"\nhop: 2\n",
		"這個 API 怎麼用？",
		"你是 session_id: 22222222-2222-2222-2222-222222222222（實作）",
		"若需要回覆",
		"純通知不必回覆",
		`send_message(session_id="11111111-1111-1111-1111-111111111111", from_session_id="22222222-2222-2222-2222-222222222222", text=...)`,
	}
	for _, s := range mustContain {
		if !strings.Contains(got, s) {
			t.Errorf("envelope missing %q\n%s", s, got)
		}
	}

	ask := wrapConsultEnvelope(from, to, "q", 1, consultAsk)
	if strings.Contains(ask, "send_message(") || !strings.Contains(ask, "直接回答") {
		t.Errorf("ask 模式 footer 應要求直接回答、不教 send_message:\n%s", ask)
	}
}

func TestWrapConsultEnvelopeEmptyNameAndDir(t *testing.T) {
	from := &db.Session{ID: "from-id", AgentType: ""}
	to := &db.Session{ID: "to-id", Name: "  "}
	got := wrapConsultEnvelope(from, to, "hi", 1, consultAsync)

	if !strings.Contains(got, "「未命名」（claude）") {
		t.Errorf("from intro: %s", got)
	}
	if !strings.Contains(got, "你是 session_id: to-id（未命名）") {
		t.Errorf("to footer: %s", got)
	}
	if strings.Contains(got, " · ）") {
		t.Errorf("empty work_dir should omit dir: %s", got)
	}
}

func TestNextHop(t *testing.T) {
	cases := []struct {
		msg  string
		want int
	}{
		{"", 1},
		{"使用者直接輸入的訊息", 1},
		{"from_session_id: x\nhop: 3\n\nbody", 4},
		{"from_session_id: x\nhop: 2\n\n引用：\nhop: 4\n", 5}, // 取最大，不被低估
		{"文中提到 hop: 9 但不在行首", 1},
	}
	for _, c := range cases {
		if got := nextHop(c.msg); got != c.want {
			t.Errorf("nextHop(%q)=%d want %d", c.msg, got, c.want)
		}
	}
	if err := checkHop(5, 5); err != nil {
		t.Errorf("第 5 跳應允許: %v", err)
	}
	if err := checkHop(6, 5); err == nil {
		t.Error("第 6 跳應拒絕")
	}
	if err := checkHop(6, 0); err == nil {
		t.Error("maxHops<=0 應套用預設 5")
	}
}

// A↔B 互相回嘴：每一跳把對方收到的 envelope 寫進對方的 user 訊息，第 max+1 跳被擋。
func TestPrepareConsultHopChain(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/hop.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	a, _ := database.CreateSession("A", "", "/tmp/a", "default", "claude", nil, "agent")
	b, _ := database.CreateSession("B", "", "/tmp/b", "default", "codex", nil, "agent")
	d := &deps{db: database, maxHops: 3}

	if err := database.AddMessage(a.ID, "user", "人類：去問 B", ""); err != nil {
		t.Fatal(err)
	}
	from, to := a, b
	for hop := 1; hop <= 3; hop++ {
		text, err := d.prepareConsult(to.ID, from.ID, "msg", consultAsync)
		if err != nil {
			t.Fatalf("hop %d 應允許: %v", hop, err)
		}
		if !strings.Contains(text, "hop: "+string(rune('0'+hop))) {
			t.Fatalf("hop %d envelope:\n%s", hop, text)
		}
		_ = database.AddMessage(to.ID, "user", text, "") // 模擬對方收到
		from, to = to, from
	}
	if _, err := d.prepareConsult(to.ID, from.ID, "msg", consultAsync); err == nil {
		t.Fatal("第 4 跳應被拒絕")
	}

	// 人插話後歸零
	_ = database.AddMessage(from.ID, "user", "人類：好，再問一次", "")
	if _, err := d.prepareConsult(to.ID, from.ID, "msg", consultAsync); err != nil {
		t.Fatalf("人插話後應重新計數: %v", err)
	}

	if _, err := d.prepareConsult(a.ID, a.ID, "msg", consultAsync); err == nil {
		t.Fatal("問自己應被拒絕")
	}
}
