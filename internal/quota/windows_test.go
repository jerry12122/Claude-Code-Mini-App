package quota

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/usage"
)

type wantWindow struct {
	key     string
	percent float64
}

func checkWindows(t *testing.T, got []Window, want []wantWindow) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("windows len = %d (%+v); want %d", len(got), got, len(want))
	}
	for i, w := range want {
		if got[i].Key != w.key {
			t.Fatalf("windows[%d].Key = %q; want %q", i, got[i].Key, w.key)
		}
		if got[i].Percent == nil || *got[i].Percent != w.percent {
			t.Fatalf("windows[%d] (%s) percent = %v; want %v", i, w.key, got[i].Percent, w.percent)
		}
	}
}

// 五個 runner 各一份 QuotaInfo：key 依 info.Windows 原順序、percent 與 display_text 不變。
func TestSnapshotFromInfoWindows(t *testing.T) {
	cursorInfo, err := usage.FromCursorPeriodUsageJSON([]byte(`{"planUsage":{"autoPercentUsed":8,"apiPercentUsed":100,"totalPercentUsed":30}}`))
	if err != nil {
		t.Fatal(err)
	}
	codexInfo, err := usage.FromCodexRateLimitsJSON([]byte(sampleCodexRateLimitsJSON))
	if err != nil {
		t.Fatal(err)
	}
	agyInfo, err := usage.FromAgyUsageJSON([]byte(`{"status":"SUCCESS","command":{"name":"usage","data":{"groups":[{"buckets":[{"id":"gemini-weekly","remaining_fraction":0.84},{"id":"gemini-5h","remaining_fraction":0.98},{"id":"3p-weekly","remaining_fraction":0},{"id":"3p-5h","remaining_fraction":0.5}]}]}}}`))
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		provider string
		info     *usage.QuotaInfo
		want     []wantWindow
		plan     string
	}{
		{"claude", usage.FromClaudeUsageText("Current session: 16% used · resets Jun 30, 3:10pm (Asia/Taipei)\nCurrent week (all models): 9% used · resets Jul 3, 9:59pm (Asia/Taipei)"),
			[]wantWindow{{"session", 16}, {"weekly", 9}}, ""},
		{"codex", codexInfo,
			[]wantWindow{{"session", 2}, {"weekly", 13}}, "plus"},
		{"cursor", cursorInfo,
			[]wantWindow{{"billing_auto", 8}, {"billing_api", 100}, {"billing_total", 30}}, ""},
		{"kiro", usage.FromKiroUsageText("KIRO FREE\nCredits (4.52 of 50)\n9%\n"),
			[]wantWindow{{"credits", 9}}, "FREE"},
		{"antigravity", agyInfo,
			[]wantWindow{{"weekly", 16}, {"session", 2}, {"weekly_claude", 100}, {"session_claude", 50}}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			snap := snapshotFromInfo(tc.provider, tc.info)
			checkWindows(t, snap.Windows, tc.want)
			if snap.Plan != tc.plan {
				t.Fatalf("plan = %q; want %q", snap.Plan, tc.plan)
			}
			if snap.DisplayText != FormatDisplay(tc.provider, tc.info) {
				t.Fatalf("display_text changed: %q", snap.DisplayText)
			}
			if p := snap.ToPayload(); len(p.Windows) != len(tc.want) || p.Plan != tc.plan {
				t.Fatalf("payload windows/plan not carried: %+v", p)
			}
		})
	}
}

func TestSnapshotFromInfoKiroUsedLimit(t *testing.T) {
	snap := snapshotFromInfo("kiro", usage.FromKiroUsageText("KIRO FREE\nCredits (4.52 of 50)\n9%\n"))
	w := snap.Windows[0]
	if w.Used == nil || *w.Used != 4.52 || w.Limit == nil || *w.Limit != 50 {
		t.Fatalf("used/limit = %v/%v", w.Used, w.Limit)
	}
}

func TestWindowsFromInfoSkipsEmptyAndHandlesNil(t *testing.T) {
	if got := windowsFromInfo(nil); len(got) != 0 {
		t.Fatalf("nil info -> %+v", got)
	}
	pct, used, limit := 10.0, 1.0, 5.0
	info := &usage.QuotaInfo{Windows: []usage.QuotaWindow{
		{Kind: "empty", Label: "no data"},
		{Kind: "pct", Percent: &pct},
		{Kind: "usedlimit", Used: &used, Limit: &limit},
	}}
	got := windowsFromInfo(info)
	if len(got) != 2 || got[0].Key != "pct" || got[1].Key != "usedlimit" {
		t.Fatalf("got %+v", got)
	}
}

// resets_at 能解析就正規化成 RFC3339（前端才算得出倒數）；不能解析就原樣保留。
func TestWindowsFromInfoNormalizesResetsAt(t *testing.T) {
	pct := 1.0
	info := &usage.QuotaInfo{Windows: []usage.QuotaWindow{
		{Kind: "human", Percent: &pct, ResetsAt: "Dec 31, 11:59pm (Asia/Taipei)"},
		{Kind: "iso", Percent: &pct, ResetsAt: "2026-10-12T06:52:28Z"},
		{Kind: "junk", Percent: &pct, ResetsAt: "someday"},
	}}
	got := windowsFromInfo(info)
	if _, err := time.Parse(time.RFC3339, got[0].ResetsAt); err != nil {
		t.Fatalf("human resets_at not normalized: %q", got[0].ResetsAt)
	}
	if got[1].ResetsAt != "2026-10-12T06:52:28Z" || got[2].ResetsAt != "someday" {
		t.Fatalf("iso/junk changed: %q %q", got[1].ResetsAt, got[2].ResetsAt)
	}
}

func TestPayloadOmitsEmptyWindows(t *testing.T) {
	raw, err := json.Marshal(Snapshot{Provider: "claude", DisplayText: "—"}.ToPayload())
	if err != nil {
		t.Fatal(err)
	}
	if s := string(raw); strings.Contains(s, "windows") || strings.Contains(s, "plan") {
		t.Fatalf("empty windows/plan should be omitted: %s", s)
	}
}
