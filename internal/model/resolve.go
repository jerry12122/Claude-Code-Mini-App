package model

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Source 表示 model 字串的取得方式。
type Source string

const (
	SourceInitEvent    Source = "init_event"
	SourceResultEvent  Source = "result_event"
	SourceCliFlag      Source = "cli_flag"
	SourceGlobalConfig Source = "global_config"
	SourceListModels   Source = "list_models"
	SourceUnknown      Source = "unknown"
)

// Info 是正規化後的 session model 資訊。
type Info struct {
	Provider    string `json:"provider"`
	Model       string `json:"model"`
	DisplayText string `json:"display_text"`
	Source      Source `json:"source"`
	RawDetail   string `json:"raw_detail,omitempty"`
	Ok          bool   `json:"ok"`
	Error       string `json:"error,omitempty"`
}

// FormatDisplay 組成前端 badge 顯示字串（與 quota display_text 同層級：後端組好、前端直接顯示）。
func FormatDisplay(model string) string {
	m := strings.TrimSpace(model)
	if m == "" {
		return "—"
	}
	return m
}

// ExtractFromClaudeLines 從 claude stream-json NDJSON 行擷取 model。
// 優先序：system/init.model → result.modelUsage 第一個 key → cliFlag → ~/.claude/settings.json model。
func ExtractFromClaudeLines(lines []string, cliFlag string) Info {
	out := Info{Provider: "claude"}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var obj struct {
			Type        string `json:"type"`
			Subtype     string `json:"subtype"`
			Model       string `json:"model"`
			ModelUsage  map[string]json.RawMessage `json:"modelUsage"`
		}
		if json.Unmarshal([]byte(line), &obj) != nil {
			continue
		}
		if obj.Type == "system" && obj.Subtype == "init" && strings.TrimSpace(obj.Model) != "" {
			out.Model = strings.TrimSpace(obj.Model)
			out.Source = SourceInitEvent
			out.RawDetail = "system/subtype=init.model"
			out.Ok = true
			out.DisplayText = FormatDisplay(out.Model)
			return out
		}
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var obj struct {
			Type       string                     `json:"type"`
			ModelUsage map[string]json.RawMessage `json:"modelUsage"`
		}
		if json.Unmarshal([]byte(line), &obj) != nil || obj.Type != "result" {
			continue
		}
		for k := range obj.ModelUsage {
			k = strings.TrimSpace(k)
			if k != "" {
				out.Model = k
				out.Source = SourceResultEvent
				out.RawDetail = "result.modelUsage key"
				out.Ok = true
				out.DisplayText = FormatDisplay(out.Model)
				return out
			}
		}
	}

	if m := strings.TrimSpace(cliFlag); m != "" {
		out.Model = m
		out.Source = SourceCliFlag
		out.RawDetail = "--model flag"
		out.Ok = true
		out.DisplayText = FormatDisplay(out.Model)
		return out
	}

	if m := readClaudeSettingsModel(); m != "" {
		out.Model = m
		out.Source = SourceGlobalConfig
		out.RawDetail = "~/.claude/settings.json model"
		out.Ok = true
		out.DisplayText = FormatDisplay(out.Model)
		return out
	}

	out.Error = "stream 無 model，且無 CLI / 全域設定 fallback"
	out.Source = SourceUnknown
	out.DisplayText = "—"
	return out
}

// ExtractFromCursorLines 從 cursor-agent stream-json 擷取 model。
// 優先序：system/init.model → --model flag → 預設 "auto"。
func ExtractFromCursorLines(lines []string, cliFlag string) Info {
	out := Info{Provider: "cursor"}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var obj struct {
			Type    string `json:"type"`
			Subtype string `json:"subtype"`
			Model   string `json:"model"`
		}
		if json.Unmarshal([]byte(line), &obj) != nil {
			continue
		}
		if obj.Type == "system" && obj.Subtype == "init" && strings.TrimSpace(obj.Model) != "" {
			out.Model = strings.TrimSpace(obj.Model)
			out.Source = SourceInitEvent
			out.RawDetail = "system/subtype=init.model"
			out.Ok = true
			out.DisplayText = FormatDisplay(out.Model)
			return out
		}
	}

	if m := strings.TrimSpace(cliFlag); m != "" {
		out.Model = m
		out.Source = SourceCliFlag
		out.RawDetail = "--model flag"
		out.Ok = true
		out.DisplayText = FormatDisplay(out.Model)
		return out
	}

	out.Model = "auto"
	out.Source = SourceGlobalConfig
	out.RawDetail = "cursor-agent 帳戶預設（agent models 標示 auto 為 default）"
	out.Ok = true
	out.DisplayText = FormatDisplay(out.Model)
	return out
}

// ExtractFromAntigravityLines 從 agy/gemini stream-json 擷取 model。
// 優先序：init.model → result.stats.models key → --model flag。
func ExtractFromAntigravityLines(lines []string, cliFlag string) Info {
	out := Info{Provider: "antigravity"}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var obj struct {
			Type  string `json:"type"`
			Model string `json:"model"`
		}
		if json.Unmarshal([]byte(line), &obj) != nil {
			continue
		}
		if obj.Type == "init" && strings.TrimSpace(obj.Model) != "" {
			out.Model = strings.TrimSpace(obj.Model)
			out.Source = SourceInitEvent
			out.RawDetail = "init.model"
			out.Ok = true
			out.DisplayText = FormatDisplay(out.Model)
			return out
		}
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var obj struct {
			Type  string `json:"type"`
			Stats struct {
				Models map[string]json.RawMessage `json:"models"`
			} `json:"stats"`
		}
		if json.Unmarshal([]byte(line), &obj) != nil || obj.Type != "result" {
			continue
		}
		for k := range obj.Stats.Models {
			k = strings.TrimSpace(k)
			if k != "" {
				out.Model = k
				out.Source = SourceResultEvent
				out.RawDetail = "result.stats.models key"
				out.Ok = true
				out.DisplayText = FormatDisplay(out.Model)
				return out
			}
		}
	}

	if m := strings.TrimSpace(cliFlag); m != "" {
		out.Model = m
		out.Source = SourceCliFlag
		out.RawDetail = "--model flag"
		out.Ok = true
		out.DisplayText = FormatDisplay(out.Model)
		return out
	}

	out.Error = "stream 無 model，且無 --model fallback"
	out.Source = SourceUnknown
	out.DisplayText = "—"
	return out
}

// ExtractFromCodexLines 從 codex exec --json JSONL 擷取 model。
// 官方 stream 目前不含 model（openai/codex#14736）；僅能依 -m flag 或全域設定。
func ExtractFromCodexLines(lines []string, cliFlag string) Info {
	out := Info{Provider: "codex"}

	// 前向相容：若未來 thread.started 帶 model，直接採用。
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var obj struct {
			Type  string `json:"type"`
			Model string `json:"model"`
		}
		if json.Unmarshal([]byte(line), &obj) != nil {
			continue
		}
		if obj.Type == "thread.started" && strings.TrimSpace(obj.Model) != "" {
			out.Model = strings.TrimSpace(obj.Model)
			out.Source = SourceInitEvent
			out.RawDetail = "thread.started.model"
			out.Ok = true
			out.DisplayText = FormatDisplay(out.Model)
			return out
		}
	}

	if m := strings.TrimSpace(cliFlag); m != "" {
		out.Model = m
		out.Source = SourceCliFlag
		out.RawDetail = "-m/--model flag"
		out.Ok = true
		out.DisplayText = FormatDisplay(out.Model)
		return out
	}

	if m := readCodexConfigModel(); m != "" {
		out.Model = m
		out.Source = SourceGlobalConfig
		out.RawDetail = "~/.codex/config.toml model"
		out.Ok = true
		out.DisplayText = FormatDisplay(out.Model)
		return out
	}

	out.Error = "codex JSONL 無 model 欄位；需 -m 或 config.toml fallback"
	out.Source = SourceUnknown
	out.DisplayText = "—"
	return out
}

// ExtractFromKiro 從 kiro headless 設定擷取 model（stdout 無 model）。
// 優先序：--model flag → settings.defaultModel → list-models default。
func ExtractFromKiro(cliFlag string, listModelsText string) Info {
	return ResolveKiroWithListModels(cliFlag, listModelsText)
}

func ResolveKiroWithListModels(cliFlag string, listModelsJSON string) Info {
	if m := strings.TrimSpace(cliFlag); m != "" {
		return Info{
			Provider:    "kiro",
			Model:       m,
			DisplayText: FormatDisplay(m),
			Source:      SourceCliFlag,
			RawDetail:   "--model flag",
			Ok:          true,
		}
	}
	if m := readKiroSettingsModel(); m != "" {
		return Info{
			Provider:    "kiro",
			Model:       m,
			DisplayText: FormatDisplay(m),
			Source:      SourceGlobalConfig,
			RawDetail:   "~/.kiro/settings/cli.json chat.defaultModel",
			Ok:          true,
		}
	}
	if m := parseKiroListModelsJSON(listModelsJSON); m != "" {
		return Info{
			Provider:    "kiro",
			Model:       m,
			DisplayText: FormatDisplay(m),
			Source:      SourceListModels,
			RawDetail:   "kiro-cli --list-models default_model",
			Ok:          true,
		}
	}
	if m := parseKiroDefaultModel(listModelsJSON); m != "" {
		return Info{
			Provider:    "kiro",
			Model:       m,
			DisplayText: FormatDisplay(m),
			Source:      SourceListModels,
			RawDetail:   "kiro-cli --list-models 預設（*）",
			Ok:          true,
		}
	}

	return Info{
		Provider: "kiro",
		Error:    "kiro headless 輸出無 model；需 --model 或 settings fallback",
		Source:   SourceUnknown,
		DisplayText: "—",
	}
}

func readKiroSettingsModel() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(home, ".kiro", "settings", "cli.json"))
	if err != nil {
		return ""
	}
	var cfg struct {
		DefaultModel string `json:"chat.defaultModel"`
	}
	if json.Unmarshal(b, &cfg) != nil {
		return ""
	}
	return strings.TrimSpace(cfg.DefaultModel)
}

func readKiroListModelsDefault() string {
	// 無 exec 時無法 live 查；由 ResolveKiro 走 settings 即可。
	return ""
}

func parseKiroListModelsJSON(text string) string {
	text = strings.TrimSpace(text)
	if text == "" || text[0] != '{' {
		return ""
	}
	var obj struct {
		DefaultModel string `json:"default_model"`
	}
	if json.Unmarshal([]byte(text), &obj) != nil {
		return ""
	}
	return strings.TrimSpace(obj.DefaultModel)
}

func readClaudeSettingsModel() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		return ""
	}
	var cfg struct {
		Model string `json:"model"`
	}
	if json.Unmarshal(b, &cfg) != nil {
		return ""
	}
	return strings.TrimSpace(cfg.Model)
}

func readCodexConfigModel() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if err != nil {
		return ""
	}
	// 簡易 TOML：model = "..." 或 model="..."
	re := regexp.MustCompile(`(?m)^\s*model\s*=\s*"(.*?)"\s*$`)
	if m := re.FindStringSubmatch(string(b)); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

var kiroDefaultRe = regexp.MustCompile(`(?m)^\*\s+(\S+)`)

func parseKiroDefaultModel(text string) string {
	if m := kiroDefaultRe.FindStringSubmatch(text); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// ParseModelFromCliArgs 從 cli_extra_args（或 argv 切片）取出 --model / -m 值。
func ParseModelFromCliArgs(args []string) string {
	for i, a := range args {
		a = strings.TrimSpace(a)
		if a == "--model" || a == "-m" {
			if i+1 < len(args) {
				return strings.TrimSpace(args[i+1])
			}
		}
		if strings.HasPrefix(a, "--model=") {
			return strings.TrimSpace(strings.TrimPrefix(a, "--model="))
		}
	}
	return ""
}
