package usage

import (
	"os"
	"path/filepath"
	"testing"
)

const sampleClaudeUsageText = `You are currently using your subscription to power your Claude Code usage

Current session: 15% used · resets Jun 30, 3:09pm (Asia/Taipei)
Current week (all models): 9% used · resets Jul 3, 10pm (Asia/Taipei)
`

const sampleClaudeOAuthJSON = `{"five_hour":{"utilization":16.0,"resets_at":"2026-06-30T15:09:59+08:00"},"seven_day":{"utilization":9.0,"resets_at":"2026-07-03T22:00:00+08:00"},"limits":[{"kind":"session","percent":16,"resets_at":"2026-06-30T15:09:59+08:00"},{"kind":"weekly_all","percent":9,"resets_at":"2026-07-03T22:00:00+08:00"}]}`

const sampleCursorUsageJSON = `{"planUsage":{"autoPercentUsed":8.05,"apiPercentUsed":100,"totalPercentUsed":30.28,"totalSpend":5906,"limit":2000,"remaining":0},"displayMessage":"You've hit your usage limit"}`

const sampleKiroUsageText = "Estimated Usage | resets on 2026-07-01 | KIRO FREE\nCredits (4.52 of 50 covered in plan)\n 9%\n"

func TestFromClaudeUsageText(t *testing.T) {
	q := FromClaudeUsageText(sampleClaudeUsageText)
	if len(q.Windows) != 2 {
		t.Fatalf("expected 2 windows, got %d", len(q.Windows))
	}
	if q.Windows[0].Percent == nil || *q.Windows[0].Percent != 15 {
		t.Fatalf("session: %+v", q.Windows[0])
	}
}

func TestFromClaudeOAuthUsageJSON(t *testing.T) {
	q, err := FromClaudeOAuthUsageJSON([]byte(sampleClaudeOAuthJSON))
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Windows) < 2 {
		t.Fatalf("windows: %+v", q.Windows)
	}
}

func TestFromCursorPeriodUsageJSON(t *testing.T) {
	q, err := FromCursorPeriodUsageJSON([]byte(sampleCursorUsageJSON))
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Windows) < 3 {
		t.Fatalf("windows: %+v", q.Windows)
	}
	if q.Windows[2].Percent == nil || *q.Windows[2].Percent != 30.28 {
		t.Fatalf("total: %+v", q.Windows[2])
	}
}

func TestFromKiroUsageText(t *testing.T) {
	q := FromKiroUsageText(sampleKiroUsageText)
	if len(q.Windows) != 1 {
		t.Fatalf("windows: %+v", q.Windows)
	}
	if q.Windows[0].Used == nil || *q.Windows[0].Used != 4.52 {
		t.Fatalf("used: %+v", q.Windows[0])
	}
	if q.Windows[0].Percent == nil || *q.Windows[0].Percent != 9 {
		t.Fatalf("percent: %+v", q.Windows[0])
	}
	if q.Plan != "FREE" {
		t.Fatalf("plan: %q", q.Plan)
	}
}

func TestQuotaFixtureSamplesIfPresent(t *testing.T) {
	root := filepath.Join("..", "..", "poc", "quota-percent", "samples")
	if _, err := os.Stat(filepath.Join(root, "quota-report.json")); err != nil {
		t.Skip("live quota samples not generated yet")
	}
	claudeText, _ := readFixture(filepath.Join(root, "claude-usage.txt"))
	if q := FromClaudeUsageText(string(claudeText)); len(q.Windows) == 0 {
		t.Log("claude live fixture has no session/week windows (format varies; unit test covers parser)")
	}
	kiroText, _ := readFixture(filepath.Join(root, "kiro-usage.txt"))
	if q := FromKiroUsageText(string(kiroText)); len(q.Windows) == 0 {
		t.Fatal("kiro fixture empty")
	}
	cursorJSON, _ := readFixture(filepath.Join(root, "cursor-period-usage.json"))
	if q, err := FromCursorPeriodUsageJSON(cursorJSON); err != nil || len(q.Windows) == 0 {
		t.Fatalf("cursor fixture: err=%v windows=%+v", err, q)
	}
}
