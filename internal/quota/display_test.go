package quota

import (
	"testing"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/usage"
)

func TestFormatClaudeDisplay(t *testing.T) {
	info := usage.FromClaudeUsageText(`Current session: 16% used · resets Jun 30
Current week (all models): 9% used · resets Jul 3`)
	got := FormatDisplay("claude", info)
	if got != "5h 16% · Week 9%" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatCursorDisplay(t *testing.T) {
	raw := `{"planUsage":{"autoPercentUsed":8.05,"apiPercentUsed":100,"totalPercentUsed":30.28}}`
	info, err := usage.FromCursorPeriodUsageJSON([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	got := FormatDisplay("cursor", info)
	if got != "Total 30% · Auto 8% · API 100%" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatKiroDisplay(t *testing.T) {
	info := usage.FromKiroUsageText("KIRO FREE\nCredits (4.52 of 50)\n9%\n")
	got := FormatDisplay("kiro", info)
	if got != "FREE · Credits 9% (4.52/50)" {
		t.Fatalf("got %q", got)
	}
}

func TestServiceGetAntigravity(t *testing.T) {
	s := NewService()
	snap := s.Get("antigravity")
	if snap.DisplayText != "未實作" {
		t.Fatalf("got %q", snap.DisplayText)
	}
	// 舊 agent_type 別名
	snap = s.Get("gemini")
	if snap.DisplayText != "未實作" {
		t.Fatalf("gemini alias got %q", snap.DisplayText)
	}
}
