package quota

import (
	"testing"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/usage"
)

func TestFormatClaudeDisplay(t *testing.T) {
	now := time.Date(2026, 6, 30, 11, 45, 0, 0, time.FixedZone("CST", 8*3600))
	info := usage.FromClaudeUsageText(`Current session: 16% used · resets Jun 30, 3:10pm (Asia/Taipei)
Current week (all models): 9% used · resets Jul 3, 9:59pm (Asia/Taipei)`)
	got := FormatDisplayAt("claude", info, now)
	if got != "5h 16% · Resets in 3 hr 25 min · Week 9%" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatClaudeDisplayOAuth(t *testing.T) {
	now := time.Date(2026, 6, 30, 11, 45, 0, 0, time.FixedZone("CST", 8*3600))
	info, err := usage.FromClaudeOAuthUsageJSON([]byte(`{"five_hour":{"utilization":16,"resets_at":"2026-06-30T15:09:59+08:00"},"seven_day":{"utilization":9,"resets_at":"2026-07-03T22:00:00+08:00"}}`))
	if err != nil {
		t.Fatal(err)
	}
	got := FormatDisplayAt("claude", info, now)
	if got != "5h 16% · Resets in 3 hr 25 min · Week 9%" {
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

func TestFormatCodexDisplay(t *testing.T) {
	info, err := usage.FromCodexRateLimitsJSON([]byte(sampleCodexRateLimitsJSON))
	if err != nil {
		t.Fatal(err)
	}
	if got := FormatDisplay("codex", info); got != "5h 2% · Week 13%" {
		t.Fatalf("got %q", got)
	}
}

const sampleCodexRateLimitsJSON = `{"rateLimits":{"limitId":"codex","primary":{"usedPercent":2,"windowDurationMins":300,"resetsAt":1790767809},"secondary":{"usedPercent":13,"windowDurationMins":10080,"resetsAt":1791354609},"planType":"plus"}}`

func quotaTestWindow(kind string, percent float64, reset string) usage.QuotaWindow {
	return usage.QuotaWindow{Kind: kind, Percent: &percent, ResetsAt: reset}
}

func TestFormatAntigravityDisplay(t *testing.T) {
	now := time.Date(2026, 10, 12, 3, 30, 0, 0, time.UTC)
	info := &usage.QuotaInfo{Windows: []usage.QuotaWindow{
		quotaTestWindow("weekly", 16, "2026-10-14T08:22:23Z"),
		quotaTestWindow("session", 2, "2026-10-12T06:52:28Z"),
	}}
	if got := FormatDisplayAt("antigravity", info, now); got != "Gemini 5h 2% · Resets in 3 hr 22 min · Gemini Week 16%" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatAntigravityQuotaGroups(t *testing.T) {
	now := time.Date(2026, 10, 12, 3, 30, 0, 0, time.UTC)
	gemini := []usage.QuotaWindow{quotaTestWindow("weekly", 0, ""), quotaTestWindow("session", 0, "")}
	claude := []usage.QuotaWindow{quotaTestWindow("weekly_claude", 100, ""), quotaTestWindow("session_claude", 100, "2026-10-12T06:52:28Z")}
	for _, tc := range []struct {
		name    string
		windows []usage.QuotaWindow
		want    string
	}{
		{"both", append(append([]usage.QuotaWindow{}, gemini...), claude...), "Gemini 5h 0% · Gemini Week 0% · Claude/GPT 5h 100% · Resets in 3 hr 22 min · Claude/GPT Week 100%"},
		{"gemini", gemini, "Gemini 5h 0% · Gemini Week 0%"},
		{"claude", claude, "Claude/GPT 5h 100% · Resets in 3 hr 22 min · Claude/GPT Week 100%"},
		{"weekly-only", []usage.QuotaWindow{quotaTestWindow("weekly_claude", 16, "")}, "Claude/GPT Week 16%"},
		{"empty", nil, "—"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := FormatDisplayAt("antigravity", &usage.QuotaInfo{Windows: tc.windows}, now)
			if got != tc.want {
				t.Fatalf("got %q; want %q", got, tc.want)
			}
		})
	}
}

func TestServiceGetAntigravity(t *testing.T) {
	s := NewService()
	snap := s.Get("antigravity")
	if snap.DisplayText != "—" {
		t.Fatalf("got %q, expected '—'", snap.DisplayText)
	}
	// 舊 agent_type 別名
	snap = s.Get("gemini")
	if snap.DisplayText != "—" {
		t.Fatalf("gemini alias got %q, expected '—'", snap.DisplayText)
	}
}
