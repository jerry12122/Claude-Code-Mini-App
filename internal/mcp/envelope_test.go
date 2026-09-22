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
	got := wrapConsultEnvelope(from, to, "  這個 API 怎麼用？  ")

	mustContain := []string{
		"想詢問／討論",
		"「規劃」（claude · E:\\workspace\\app）",
		"from_session_id: 11111111-1111-1111-1111-111111111111",
		"這個 API 怎麼用？",
		"你是 session_id: 22222222-2222-2222-2222-222222222222（實作）",
		`send_message(session_id="11111111-1111-1111-1111-111111111111", from_session_id="22222222-2222-2222-2222-222222222222", text=...)`,
	}
	for _, s := range mustContain {
		if !strings.Contains(got, s) {
			t.Errorf("envelope missing %q\n%s", s, got)
		}
	}
}

func TestWrapConsultEnvelopeEmptyNameAndDir(t *testing.T) {
	from := &db.Session{ID: "from-id", AgentType: ""}
	to := &db.Session{ID: "to-id", Name: "  "}
	got := wrapConsultEnvelope(from, to, "hi")

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
