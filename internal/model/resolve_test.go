package model

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractFromClaudeLines_InitPreferred(t *testing.T) {
	lines := []string{
		`{"type":"system","subtype":"init","session_id":"s1","model":"claude-sonnet-5"}`,
		`{"type":"result","modelUsage":{"claude-opus-4-6":{}}}`,
	}
	info := ExtractFromClaudeLines(lines, "")
	if !info.Ok || info.Model != "claude-sonnet-5" || info.Source != SourceInitEvent {
		t.Fatalf("got %+v", info)
	}
	if info.DisplayText != "claude-sonnet-5" {
		t.Fatalf("display=%q", info.DisplayText)
	}
}

func TestExtractFromClaudeLines_ModelUsageFallback(t *testing.T) {
	lines := []string{`{"type":"result","modelUsage":{"claude-sonnet-4-6":{}}}`}
	info := ExtractFromClaudeLines(lines, "")
	if !info.Ok || info.Model != "claude-sonnet-4-6" || info.Source != SourceResultEvent {
		t.Fatalf("got %+v", info)
	}
}

func TestExtractFromClaudeLines_CliFlagFallback(t *testing.T) {
	info := ExtractFromClaudeLines(nil, "opus")
	if !info.Ok || info.Model != "opus" || info.Source != SourceCliFlag {
		t.Fatalf("got %+v", info)
	}
}

func TestExtractFromCursorLines_Init(t *testing.T) {
	lines := []string{
		`{"type":"system","subtype":"init","session_id":"s1","model":"Composer 2.5 Fast"}`,
	}
	info := ExtractFromCursorLines(lines, "")
	if !info.Ok || info.Model != "Composer 2.5 Fast" {
		t.Fatalf("got %+v", info)
	}
	if info.DisplayText != "Composer 2.5 Fast" {
		t.Fatalf("display=%q", info.DisplayText)
	}
}

func TestExtractFromCursorLines_DefaultAuto(t *testing.T) {
	info := ExtractFromCursorLines(nil, "")
	if !info.Ok || info.Model != "auto" || info.Source != SourceGlobalConfig {
		t.Fatalf("got %+v", info)
	}
}

func TestExtractFromAntigravityLines_Init(t *testing.T) {
	lines := []string{`{"type":"init","session_id":"abc","model":"gemini-3.1-pro"}`}
	info := ExtractFromAntigravityLines(lines, "")
	if !info.Ok || info.Model != "gemini-3.1-pro" {
		t.Fatalf("got %+v", info)
	}
}

func TestExtractFromCodexLines_NoStreamModelUsesFlag(t *testing.T) {
	lines := []string{`{"type":"thread.started","thread_id":"t1"}`}
	info := ExtractFromCodexLines(lines, "codex-mini-latest")
	if !info.Ok || info.Model != "codex-mini-latest" || info.Source != SourceCliFlag {
		t.Fatalf("got %+v", info)
	}
}

func TestExtractFromKiro_ListModelsDefault(t *testing.T) {
	dir := t.TempDir()
	kiroDir := filepath.Join(dir, ".kiro", "settings")
	if err := os.MkdirAll(kiroDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 空 settings，走 list-models 文字 fallback
	t.Setenv("USERPROFILE", dir)
	t.Setenv("HOME", dir)

	text := "Available models (* = default):\n\n* auto                 1.00x credits\n  claude-sonnet-5      1.30x credits\n"
	info := ExtractFromKiro("", text)
	if !info.Ok || info.Model != "auto" || info.Source != SourceListModels {
		t.Fatalf("got %+v", info)
	}
}

func TestExtractFromKiro_CliFlag(t *testing.T) {
	info := ExtractFromKiro("claude-sonnet-4.6", "")
	if !info.Ok || info.Model != "claude-sonnet-4.6" {
		t.Fatalf("got %+v", info)
	}
}

func TestParseModelFromCliArgs(t *testing.T) {
	if got := ParseModelFromCliArgs([]string{"--verbose", "--model", "sonnet", "x"}); got != "sonnet" {
		t.Fatalf("got %q", got)
	}
	if got := ParseModelFromCliArgs([]string{"-m", "gpt-5"}); got != "gpt-5" {
		t.Fatalf("got %q", got)
	}
}

func TestReadClaudeSettingsModel(t *testing.T) {
	dir := t.TempDir()
	claudeDir := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(`{"model":"claude-haiku-4-5"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	home := os.Getenv("HOME")
	if home == "" {
		home = os.Getenv("USERPROFILE")
	}
	t.Setenv("USERPROFILE", dir)
	t.Setenv("HOME", dir)

	// readClaudeSettingsModel uses UserHomeDir
	if m := readClaudeSettingsModel(); m != "claude-haiku-4-5" {
		t.Fatalf("got %q", m)
	}
	_ = home
}
