package model

import (
	"strings"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
)

// ResolveForSession 在 WS 連線或 run 前解析 session model（無 stream 時使用）。
// storedModel 為 DB 最後已知值；非空時優先採用。
func ResolveForSession(provider string, cliArgs []string, storedModel, storedSource string) Info {
	if m := strings.TrimSpace(storedModel); m != "" && m != "—" {
		return Info{
			Provider:    provider,
			Model:       m,
			DisplayText: m,
			Source:      Source(storedSource),
			Ok:          true,
		}
	}

	flag := ParseModelFromCliArgs(cliArgs)
	switch provider {
	case agent.TypeClaude:
		return resolveClaudeOffline(flag)
	case agent.TypeCursor:
		return resolveCursorOffline(flag)
	case agent.TypeCodex:
		return ExtractFromCodexLines(nil, flag)
	case agent.TypeKiro, agent.TypeKiroACP:
		return ResolveKiro(flag)
	default:
		return Info{Provider: provider, DisplayText: "—", Source: SourceUnknown}
	}
}

func resolveClaudeOffline(cliFlag string) Info {
	if m := strings.TrimSpace(cliFlag); m != "" {
		return Info{
			Provider:    agent.TypeClaude,
			Model:       m,
			DisplayText: FormatDisplay(m),
			Source:      SourceCliFlag,
			RawDetail:   "--model flag",
			Ok:          true,
		}
	}
	if m := readClaudeSettingsModel(); m != "" {
		return Info{
			Provider:    agent.TypeClaude,
			Model:       m,
			DisplayText: FormatDisplay(m),
			Source:      SourceGlobalConfig,
			RawDetail:   "~/.claude/settings.json model",
			Ok:          true,
		}
	}
	return Info{Provider: agent.TypeClaude, DisplayText: "—", Source: SourceUnknown}
}

func resolveCursorOffline(cliFlag string) Info {
	if m := strings.TrimSpace(cliFlag); m != "" {
		return Info{
			Provider:    agent.TypeCursor,
			Model:       m,
			DisplayText: FormatDisplay(m),
			Source:      SourceCliFlag,
			Ok:          true,
		}
	}
	return Info{
		Provider:    agent.TypeCursor,
		Model:       "auto",
		DisplayText: "auto",
		Source:      SourceGlobalConfig,
		RawDetail:   "cursor-agent 帳戶預設",
		Ok:          true,
	}
}

// ResolveKiro：--model → settings.defaultModel → list-models default。
func ResolveKiro(cliFlag string) Info {
	if m := strings.TrimSpace(cliFlag); m != "" {
		return Info{
			Provider:    agent.TypeKiro,
			Model:       m,
			DisplayText: FormatDisplay(m),
			Source:      SourceCliFlag,
			RawDetail:   "--model flag",
			Ok:          true,
		}
	}
	if m := readKiroSettingsModel(); m != "" {
		return Info{
			Provider:    agent.TypeKiro,
			Model:       m,
			DisplayText: FormatDisplay(m),
			Source:      SourceGlobalConfig,
			RawDetail:   "~/.kiro/settings/cli.json chat.defaultModel",
			Ok:          true,
		}
	}
	if m := readKiroListModelsDefault(); m != "" {
		return Info{
			Provider:    agent.TypeKiro,
			Model:       m,
			DisplayText: FormatDisplay(m),
			Source:      SourceListModels,
			RawDetail:   "kiro-cli --list-models default_model",
			Ok:          true,
		}
	}
	return Info{
		Provider:    agent.TypeKiro,
		DisplayText: "—",
		Source:      SourceUnknown,
		Error:       "無法解析 Kiro model",
	}
}

// AgentSnapshot 將 Info 轉為 agent.Event 用的 ModelSnapshot。
func AgentSnapshot(i Info) *agent.ModelSnapshot {
	if !i.Ok || strings.TrimSpace(i.Model) == "" {
		if strings.TrimSpace(i.DisplayText) == "" || i.DisplayText == "—" {
			return nil
		}
	}
	return &agent.ModelSnapshot{
		Model:       i.Model,
		DisplayText: i.DisplayText,
		Source:      string(i.Source),
	}
}

// InfoFromStream 由 stream init 建立 Info。
func InfoFromStream(provider, modelName string) Info {
	m := strings.TrimSpace(modelName)
	if m == "" {
		return Info{Provider: provider, DisplayText: "—", Source: SourceUnknown}
	}
	return Info{
		Provider:    provider,
		Model:       m,
		DisplayText: FormatDisplay(m),
		Source:      SourceInitEvent,
		RawDetail:   "stream init.model",
		Ok:          true,
	}
}
