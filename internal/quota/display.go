package quota

import (
	"fmt"
	"math"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/usage"
)

// FormatDisplay 依 provider 將 QuotaInfo 組成 UI 顯示字串。
func FormatDisplay(provider string, info *usage.QuotaInfo) string {
	if info == nil {
		return "—"
	}
	switch provider {
	case "claude":
		return formatClaudeDisplay(info)
	case "cursor":
		return formatCursorDisplay(info)
	case "kiro":
		return formatKiroDisplay(info)
	default:
		return "—"
	}
}

func formatClaudeDisplay(info *usage.QuotaInfo) string {
	var sessPct, weekPct *float64
	for _, w := range info.Windows {
		if w.Percent == nil {
			continue
		}
		switch w.Kind {
		case "session":
			sessPct = w.Percent
		case "weekly":
			weekPct = w.Percent
		}
	}
	if sessPct != nil && weekPct != nil {
		return fmt.Sprintf("5h %.0f%% · Week %.0f%%", roundPct(*sessPct), roundPct(*weekPct))
	}
	if sessPct != nil {
		return fmt.Sprintf("5h %.0f%%", roundPct(*sessPct))
	}
	if weekPct != nil {
		return fmt.Sprintf("Week %.0f%%", roundPct(*weekPct))
	}
	return "—"
}

func formatCursorDisplay(info *usage.QuotaInfo) string {
	var total, auto, api *float64
	for _, w := range info.Windows {
		if w.Percent == nil {
			continue
		}
		switch w.Kind {
		case "billing_total":
			total = w.Percent
		case "billing_auto":
			auto = w.Percent
		case "billing_api":
			api = w.Percent
		}
	}
	if total == nil && auto == nil && api == nil {
		return "—"
	}
	parts := make([]string, 0, 3)
	if total != nil {
		parts = append(parts, fmt.Sprintf("Total %.0f%%", roundPct(*total)))
	}
	if auto != nil {
		parts = append(parts, fmt.Sprintf("Auto %.0f%%", roundPct(*auto)))
	}
	if api != nil {
		parts = append(parts, fmt.Sprintf("API %.0f%%", roundPct(*api)))
	}
	out := parts[0]
	for i := 1; i < len(parts); i++ {
		out += " · " + parts[i]
	}
	return out
}

func formatKiroDisplay(info *usage.QuotaInfo) string {
	var pct, used, limit *float64
	for _, w := range info.Windows {
		if w.Kind != "credits" {
			continue
		}
		pct, used, limit = w.Percent, w.Used, w.Limit
		break
	}
	plan := info.Plan
	if plan != "" && pct != nil && used != nil && limit != nil {
		return fmt.Sprintf("%s · Credits %.0f%% (%.2f/%.0f)", plan, roundPct(*pct), *used, *limit)
	}
	if pct != nil && used != nil && limit != nil {
		return fmt.Sprintf("Credits %.0f%% (%.2f/%.0f)", roundPct(*pct), *used, *limit)
	}
	if plan != "" && pct != nil {
		return fmt.Sprintf("%s · Credits %.0f%%", plan, roundPct(*pct))
	}
	if pct != nil {
		return fmt.Sprintf("Credits %.0f%%", roundPct(*pct))
	}
	return "—"
}

func roundPct(v float64) float64 {
	if math.Mod(v, 1) == 0 {
		return v
	}
	return math.Round(v*10) / 10
}
