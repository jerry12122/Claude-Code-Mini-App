package usage

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// QuotaWindow 表示單一用量窗口（5 小時、週、帳單週期等）。
type QuotaWindow struct {
	Kind     string   `json:"kind"` // session | weekly | billing | credits
	Label    string   `json:"label,omitempty"`
	Percent  *float64 `json:"percent,omitempty"`
	Used     *float64 `json:"used,omitempty"`
	Limit    *float64 `json:"limit,omitempty"`
	ResetsAt string   `json:"resets_at,omitempty"`
}

// QuotaInfo 是跨 provider 正規化後的帳戶用量百分比。
type QuotaInfo struct {
	Provider string        `json:"provider"`
	Plan     string        `json:"plan,omitempty"`
	Windows  []QuotaWindow `json:"windows"`
	Source   string        `json:"source"` // 資料來源描述
}

var (
	claudeSessionRe    = regexp.MustCompile(`(?i)Current session:\s*(\d+(?:\.\d+)?)%\s*used\s+[^\r\n]*?resets\s*(.+)`)
	claudeWeeklyRe     = regexp.MustCompile(`(?i)Current week \(all models\):\s*(\d+(?:\.\d+)?)%\s*used\s+[^\r\n]*?resets\s*(.+)`)
	kiroQuotaCreditsRe = regexp.MustCompile(`Credits\s*\(([\d.]+)\s+of\s+([\d.]+)`)
	kiroQuotaPercentRe = regexp.MustCompile(`(\d+(?:\.\d+)?)%`)
)

// FromClaudeUsageText 解析 `claude -p "/usage"` 的純文字輸出。
func FromClaudeUsageText(text string) *QuotaInfo {
	out := &QuotaInfo{Provider: "claude", Source: "claude -p /usage text"}
	if m := claudeSessionRe.FindStringSubmatch(text); len(m) == 3 {
		out.Windows = append(out.Windows, QuotaWindow{
			Kind:     "session",
			Label:    "Current session",
			Percent:  floatPtr(parseFloat(m[1])),
			ResetsAt: strings.TrimSpace(m[2]),
		})
	}
	if m := claudeWeeklyRe.FindStringSubmatch(text); len(m) == 3 {
		out.Windows = append(out.Windows, QuotaWindow{
			Kind:     "weekly",
			Label:    "Current week (all models)",
			Percent:  floatPtr(parseFloat(m[1])),
			ResetsAt: strings.TrimSpace(m[2]),
		})
	}
	return out
}

// FromAgyUsageJSON parses the native /usage command data, not per-turn token usage.
func FromAgyUsageJSON(raw []byte) (*QuotaInfo, error) {
	var body struct {
		Status  string `json:"status"`
		Command struct {
			Name string `json:"name"`
			Data struct {
				Groups []struct {
					Buckets []struct {
						ID        string   `json:"id"`
						Remaining *float64 `json:"remaining_fraction"`
						ResetTime string   `json:"reset_time"`
					} `json:"buckets"`
				} `json:"groups"`
			} `json:"data"`
		} `json:"command"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("antigravity quota: %w", err)
	}
	if body.Status != "SUCCESS" || body.Command.Name != "usage" {
		return nil, fmt.Errorf("antigravity quota: missing successful usage command")
	}
	out := &QuotaInfo{Provider: "antigravity", Source: "agy -p /usage --output-format json"}
	for _, group := range body.Command.Data.Groups {
		for _, bucket := range group.Buckets {
			var kind, label string
			switch bucket.ID {
			case "gemini-5h":
				kind, label = "session", "Gemini 5h"
			case "gemini-weekly":
				kind, label = "weekly", "Gemini Week"
			case "3p-5h":
				kind, label = "session_claude", "Claude/GPT 5h"
			case "3p-weekly":
				kind, label = "weekly_claude", "Claude/GPT Week"
			default:
				continue
			}
			if bucket.Remaining == nil || *bucket.Remaining < 0 || *bucket.Remaining > 1 {
				return nil, fmt.Errorf("antigravity quota: invalid remaining fraction for %s", bucket.ID)
			}
			out.Windows = append(out.Windows, QuotaWindow{
				Kind: kind, Label: label, Percent: floatPtr(100 - *bucket.Remaining*100),
				ResetsAt: bucket.ResetTime,
			})
		}
	}
	if len(out.Windows) == 0 {
		return nil, fmt.Errorf("antigravity quota: no known quota buckets")
	}
	return out, nil
}

type claudeOAuthUsage struct {
	FiveHour *struct {
		Utilization float64 `json:"utilization"`
		ResetsAt    string  `json:"resets_at"`
	} `json:"five_hour"`
	SevenDay *struct {
		Utilization float64 `json:"utilization"`
		ResetsAt    string  `json:"resets_at"`
	} `json:"seven_day"`
	Limits []struct {
		Kind     string  `json:"kind"`
		Percent  float64 `json:"percent"`
		ResetsAt string  `json:"resets_at"`
	} `json:"limits"`
}

// FromClaudeOAuthUsageJSON 解析 Anthropic OAuth usage API 回應。
func FromClaudeOAuthUsageJSON(raw []byte) (*QuotaInfo, error) {
	var body claudeOAuthUsage
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	out := &QuotaInfo{Provider: "claude", Source: "api.anthropic.com/api/oauth/usage"}
	if body.FiveHour != nil {
		out.Windows = append(out.Windows, QuotaWindow{
			Kind: "session", Label: "five_hour",
			Percent:  floatPtr(body.FiveHour.Utilization),
			ResetsAt: body.FiveHour.ResetsAt,
		})
	}
	if body.SevenDay != nil {
		out.Windows = append(out.Windows, QuotaWindow{
			Kind: "weekly", Label: "seven_day",
			Percent:  floatPtr(body.SevenDay.Utilization),
			ResetsAt: body.SevenDay.ResetsAt,
		})
	}
	for _, lim := range body.Limits {
		out.Windows = append(out.Windows, QuotaWindow{
			Kind:     lim.Kind,
			Percent:  floatPtr(lim.Percent),
			ResetsAt: lim.ResetsAt,
		})
	}
	return out, nil
}

type cursorPeriodUsage struct {
	PlanUsage *struct {
		AutoPercentUsed  float64 `json:"autoPercentUsed"`
		APIPercentUsed   float64 `json:"apiPercentUsed"`
		TotalPercentUsed float64 `json:"totalPercentUsed"`
		TotalSpend       int64   `json:"totalSpend"`
		Limit            int64   `json:"limit"`
		Remaining        int64   `json:"remaining"`
	} `json:"planUsage"`
	DisplayMessage string `json:"displayMessage"`
}

// FromCursorPeriodUsageJSON 解析 Cursor GetCurrentPeriodUsage 回應。
func FromCursorPeriodUsageJSON(raw []byte) (*QuotaInfo, error) {
	var body cursorPeriodUsage
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	out := &QuotaInfo{Provider: "cursor", Source: "api2.cursor.sh GetCurrentPeriodUsage"}
	if body.PlanUsage == nil {
		return out, nil
	}
	pu := body.PlanUsage
	out.Windows = append(out.Windows,
		QuotaWindow{Kind: "billing_auto", Label: "autoPercentUsed", Percent: floatPtr(pu.AutoPercentUsed)},
		QuotaWindow{Kind: "billing_api", Label: "apiPercentUsed", Percent: floatPtr(pu.APIPercentUsed)},
		QuotaWindow{Kind: "billing_total", Label: "totalPercentUsed", Percent: floatPtr(pu.TotalPercentUsed)},
	)
	if pu.Limit > 0 {
		used := float64(pu.TotalSpend) / 100.0
		limit := float64(pu.Limit) / 100.0
		out.Windows = append(out.Windows, QuotaWindow{
			Kind: "billing_spend", Label: "plan spend USD",
			Used: floatPtr(used), Limit: floatPtr(limit),
		})
	}
	return out, nil
}

// FromKiroUsageText 解析 `kiro-cli chat "/usage" --no-interactive` 的 stdout。
func FromKiroUsageText(text string) *QuotaInfo {
	clean := ansiRe.ReplaceAllString(text, "")
	out := &QuotaInfo{Provider: "kiro", Source: "kiro-cli chat /usage --no-interactive"}
	if m := kiroQuotaCreditsRe.FindStringSubmatch(clean); len(m) == 3 {
		used := parseFloat(m[1])
		limit := parseFloat(m[2])
		var pct *float64
		if limit > 0 {
			pct = floatPtr(used / limit * 100)
		}
		if pm := kiroQuotaPercentRe.FindStringSubmatch(clean); len(pm) == 2 {
			pct = floatPtr(parseFloat(pm[1]))
		}
		out.Windows = append(out.Windows, QuotaWindow{
			Kind: "credits", Label: "monthly credits",
			Percent: pct, Used: floatPtr(used), Limit: floatPtr(limit),
		})
	}
	if plan := regexp.MustCompile(`KIRO\s+(\w+)`).FindStringSubmatch(clean); len(plan) == 2 {
		out.Plan = plan[1]
	}
	return out
}

// FromCodexRateLimitsJSON parses account/rateLimits/read's result.
func FromCodexRateLimitsJSON(raw []byte) (*QuotaInfo, error) {
	type rateWindow struct {
		UsedPercent   *float64 `json:"usedPercent"`
		WindowMinutes int      `json:"windowDurationMins"`
		ResetsAt      int64    `json:"resetsAt"`
	}
	type rateLimits struct {
		LimitID   string      `json:"limitId"`
		Primary   *rateWindow `json:"primary"`
		Secondary *rateWindow `json:"secondary"`
		PlanType  string      `json:"planType"`
	}
	var body struct {
		RateLimits *rateLimits            `json:"rateLimits"`
		ByLimitID  map[string]*rateLimits `json:"rateLimitsByLimitId"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("codex quota: %w", err)
	}
	limits := body.RateLimits
	if codex, ok := body.ByLimitID["codex"]; ok {
		limits = codex
	}
	if limits == nil || (limits.LimitID != "" && limits.LimitID != "codex") {
		return nil, fmt.Errorf("codex quota: no codex rate limits")
	}
	out := &QuotaInfo{Provider: "codex", Plan: limits.PlanType, Source: "codex app-server account/rateLimits/read"}
	for _, window := range []*rateWindow{limits.Primary, limits.Secondary} {
		if window == nil {
			continue
		}
		if window.UsedPercent == nil || *window.UsedPercent < 0 || window.WindowMinutes <= 0 || window.ResetsAt < 0 {
			return nil, fmt.Errorf("codex quota: invalid quota window")
		}
		kind := "session"
		if window.WindowMinutes >= 7*24*60 {
			kind = "weekly"
		}
		qw := QuotaWindow{Kind: kind, Label: fmt.Sprintf("%d min", window.WindowMinutes), Percent: window.UsedPercent}
		if window.ResetsAt > 0 {
			qw.ResetsAt = time.Unix(window.ResetsAt, 0).UTC().Format(time.RFC3339)
		}
		out.Windows = append(out.Windows, qw)
	}
	if len(out.Windows) == 0 {
		return nil, fmt.Errorf("codex quota: no quota windows")
	}
	return out, nil
}

func floatPtr(f float64) *float64 { return &f }
