package usage

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

var cursorUsageRe = []*regexp.Regexp{
	regexp.MustCompile(`"duration_ms"\s*:\s*(\d+)`),
	regexp.MustCompile(`"inputTokens"\s*:\s*(\d+)`),
	regexp.MustCompile(`"outputTokens"\s*:\s*(\d+)`),
	regexp.MustCompile(`"cacheReadTokens"\s*:\s*(\d+)`),
	regexp.MustCompile(`"cacheWriteTokens"\s*:\s*(\d+)`),
}

// Info 是跨 provider 正規化後的單回合用量（POC / 後續 WS 可共用）。
type Info struct {
	Provider         string  `json:"provider"`
	CostUSD          *float64 `json:"cost_usd,omitempty"`
	Credits          *float64 `json:"credits,omitempty"`
	InputTokens      *int64  `json:"input_tokens,omitempty"`
	OutputTokens     *int64  `json:"output_tokens,omitempty"`
	CacheReadTokens  *int64  `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens *int64  `json:"cache_write_tokens,omitempty"`
	DurationMS       *int64  `json:"duration_ms,omitempty"`
	DurationText     string  `json:"duration_text,omitempty"` // Kiro: "2s"
}

type claudeResult struct {
	Type          string   `json:"type"`
	TotalCostUSD  *float64 `json:"total_cost_usd"`
	DurationMS    *int64   `json:"duration_ms"`
	APIErrorStatus *string `json:"api_error_status"`
	Usage         *struct {
		InputTokens              *int64 `json:"input_tokens"`
		OutputTokens             *int64 `json:"output_tokens"`
		CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`
		CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

type cursorResult struct {
	Type       string `json:"type"`
	DurationMS *int64 `json:"duration_ms"`
	Usage      *struct {
		InputTokens      *int64 `json:"inputTokens"`
		OutputTokens     *int64 `json:"outputTokens"`
		CacheReadTokens  *int64 `json:"cacheReadTokens"`
		CacheWriteTokens *int64 `json:"cacheWriteTokens"`
	} `json:"usage"`
}

var (
	ansiRe     = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]|\x1b[A-Za-z]`)
	kiroCreditsRe = regexp.MustCompile(`Credits:\s*([\d.]+).*?Time:\s*(\S+)`)
)

// FromClaudeResult 解析 claude stream-json 的 result 行。
func FromClaudeResult(line []byte) (*Info, error) {
	var r claudeResult
	if err := json.Unmarshal(line, &r); err != nil {
		return nil, err
	}
	out := &Info{Provider: "claude", CostUSD: r.TotalCostUSD, DurationMS: r.DurationMS}
	if r.Usage != nil {
		out.InputTokens = r.Usage.InputTokens
		out.OutputTokens = r.Usage.OutputTokens
		out.CacheReadTokens = r.Usage.CacheReadInputTokens
		out.CacheWriteTokens = r.Usage.CacheCreationInputTokens
	}
	return out, nil
}

// FromCursorResult 解析 cursor-agent stream-json 的 result 行。
// result 欄位若含損壞 UTF-8 會使整行 JSON 無效，此時改以 regex 擷取 usage。
func FromCursorResult(line []byte) (*Info, error) {
	var r cursorResult
	if err := json.Unmarshal(line, &r); err != nil {
		if info := fromCursorRegex(string(line)); info != nil {
			return info, nil
		}
		return nil, err
	}
	out := &Info{Provider: "cursor", DurationMS: r.DurationMS}
	if r.Usage != nil {
		out.InputTokens = r.Usage.InputTokens
		out.OutputTokens = r.Usage.OutputTokens
		out.CacheReadTokens = r.Usage.CacheReadTokens
		out.CacheWriteTokens = r.Usage.CacheWriteTokens
	}
	return out, nil
}

func fromCursorRegex(s string) *Info {
	m := cursorUsageRe
	out := &Info{Provider: "cursor"}
	if x := matchInt64(m[0], s); x != nil {
		out.DurationMS = x
	}
	if x := matchInt64(m[1], s); x != nil {
		out.InputTokens = x
	}
	if x := matchInt64(m[2], s); x != nil {
		out.OutputTokens = x
	}
	if x := matchInt64(m[3], s); x != nil {
		out.CacheReadTokens = x
	}
	if x := matchInt64(m[4], s); x != nil {
		out.CacheWriteTokens = x
	}
	if out.InputTokens == nil && out.DurationMS == nil {
		return nil
	}
	return out
}

func matchInt64(re *regexp.Regexp, s string) *int64 {
	m := re.FindStringSubmatch(s)
	if len(m) < 2 {
		return nil
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return nil
	}
	return &n
}

// FromKiroStderr 解析 kiro-cli chat --no-interactive 的 stderr 尾端 Credits 行。
func FromKiroStderr(stderr string) *Info {
	clean := ansiRe.ReplaceAllString(stderr, "")
	m := kiroCreditsRe.FindStringSubmatch(clean)
	if m == nil {
		return &Info{Provider: "kiro"}
	}
	credits := parseFloat(m[1])
	out := &Info{
		Provider:     "kiro",
		Credits:      &credits,
		DurationText: strings.TrimSpace(m[2]),
	}
	return out
}

func parseFloat(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}
