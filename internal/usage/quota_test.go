package usage

import (
	"math"
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

func TestFromAgyUsageJSON(t *testing.T) {
	raw := `{"status":"SUCCESS","command":{"name":"usage","data":{"groups":[{"name":"renamed group","buckets":[{"id":"gemini-weekly","remaining_fraction":0.27464595437049866,"reset_time":"2026-11-14T00:35:19Z"},{"id":"gemini-5h","remaining_fraction":0.9193424582481384},{"id":"3p-weekly","remaining_fraction":1},{"id":"3p-5h","remaining_fraction":0},{"id":"new-unknown-bucket"}]}]}}}`
	q, err := FromAgyUsageJSON([]byte(raw))
	if err != nil || len(q.Windows) != 4 {
		t.Fatalf("q=%+v err=%v", q, err)
	}
	if q.Windows[0].Kind != "weekly" || math.Abs(*q.Windows[0].Percent-72.53540456295013) > 1e-9 || q.Windows[0].ResetsAt != "2026-11-14T00:35:19Z" {
		t.Fatalf("weekly: %+v", q.Windows[0])
	}
	if q.Windows[1].Kind != "session" || q.Windows[2].Kind != "weekly_claude" || *q.Windows[2].Percent != 0 || q.Windows[3].Kind != "session_claude" || *q.Windows[3].Percent != 100 {
		t.Fatalf("windows: %+v", q.Windows)
	}
	for _, raw := range []string{
		`not json`, `null`, `{"status":"ERROR"}`,
		`{"status":"SUCCESS","usage":{"input_tokens":12}}`,
		`{"status":"SUCCESS","command":{"name":"usage","data":{"groups":[]}}}`,
		`{"status":"SUCCESS","command":{"name":"usage","data":{"groups":[{"buckets":[{"id":"gemini-5h"}]}]}}}`,
		`{"status":"SUCCESS","command":{"name":"usage","data":{"groups":[{"buckets":[{"id":"gemini-5h","remaining_fraction":-0.1}]}]}}}`,
		`{"status":"SUCCESS","command":{"name":"usage","data":{"groups":[{"buckets":[{"id":"gemini-5h","remaining_fraction":1.1}]}]}}}`,
	} {
		if _, err := FromAgyUsageJSON([]byte(raw)); err == nil {
			t.Fatalf("accepted invalid quota: %s", raw)
		}
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

func TestFromCodexRateLimitsJSON(t *testing.T) {
	raw := `{"rateLimits":{"limitId":"other","primary":{"usedPercent":99,"windowDurationMins":300}},"rateLimitsByLimitId":{"codex":{"limitId":"codex","planType":"plus","primary":{"usedPercent":9,"windowDurationMins":10080,"resetsAt":9000},"secondary":{"usedPercent":0,"windowDurationMins":300,"resetsAt":2000}}}}`
	q, err := FromCodexRateLimitsJSON([]byte(raw))
	if err != nil || q.Plan != "plus" || len(q.Windows) != 2 {
		t.Fatalf("q=%+v err=%v", q, err)
	}
	// Classify by duration, preserve explicit zero, never infer zero from old reset dates.
	if q.Windows[0].Kind != "weekly" || *q.Windows[0].Percent != 9 || q.Windows[1].Kind != "session" || *q.Windows[1].Percent != 0 || q.Windows[1].ResetsAt != "1970-01-01T00:33:20Z" {
		t.Fatalf("windows: %+v", q.Windows)
	}
	q, err = FromCodexRateLimitsJSON([]byte(`{"rateLimits":{"primary":{"usedPercent":42,"windowDurationMins":300,"resetsAt":1}}}`))
	if err != nil || *q.Windows[0].Percent != 42 {
		t.Fatalf("legacy single bucket: q=%+v err=%v", q, err)
	}
	for _, raw := range []string{
		`not json`, `null`, `{}`, `{"rateLimits":{}}`,
		`{"rateLimits":{"primary":{"windowDurationMins":300}}}`,
		`{"rateLimits":{"primary":{"usedPercent":2}}}`,
		`{"rateLimits":{"primary":{"usedPercent":-2,"windowDurationMins":300}}}`,
		`{"rateLimits":{"limitId":"codex_other","primary":{"usedPercent":2,"windowDurationMins":300}}}`,
		`{"rateLimitsByLimitId":{"codex":null}}`,
	} {
		if _, err := FromCodexRateLimitsJSON([]byte(raw)); err == nil {
			t.Fatalf("accepted invalid quota: %s", raw)
		}
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
