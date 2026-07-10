package model

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
)

func TestResolveForSession_StoredPreferred(t *testing.T) {
	info := ResolveForSession(agent.TypeClaude, nil, "claude-opus-4-6", "init_event")
	if info.Model != "claude-opus-4-6" || info.Source != SourceInitEvent {
		t.Fatalf("got %+v", info)
	}
}

func TestResolveForSession_CursorCLI(t *testing.T) {
	info := ResolveForSession(agent.TypeCursor, []string{"--model", "gpt-5"}, "", "")
	if !info.Ok || info.Model != "gpt-5" {
		t.Fatalf("got %+v", info)
	}
}

func TestResolveKiro_SettingsBeforeListModels(t *testing.T) {
	dir := t.TempDir()
	kiroDir := filepath.Join(dir, ".kiro", "settings")
	if err := os.MkdirAll(kiroDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kiroDir, "cli.json"), []byte(`{"chat.defaultModel":"claude-sonnet-5"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("USERPROFILE", dir)
	t.Setenv("HOME", dir)

	info := ResolveKiro("")
	if !info.Ok || info.Model != "claude-sonnet-5" || info.Source != SourceGlobalConfig {
		t.Fatalf("got %+v", info)
	}
}

func TestInfoToPayload(t *testing.T) {
	p := InfoFromStream(agent.TypeClaude, "claude-sonnet-5").ToPayload()
	if p.DisplayText != "claude-sonnet-5" || p.Source != string(SourceInitEvent) {
		t.Fatalf("got %+v", p)
	}
}

func TestAgentSnapshot(t *testing.T) {
	s := AgentSnapshot(InfoFromStream(agent.TypeCursor, "Composer 2.5 Fast"))
	if s == nil || s.DisplayText != "Composer 2.5 Fast" {
		t.Fatalf("got %+v", s)
	}
}
