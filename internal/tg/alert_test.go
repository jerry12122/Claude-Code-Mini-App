package tg

import (
	"strings"
	"testing"
)

func TestFormatTaskAlert_error(t *testing.T) {
	cfg := DefaultNotifyConfig()
	exit := 1
	text := FormatTaskAlert(TaskAlert{
		SessionName: "my*proj",
		AgentType:   "claude",
		Outcome:     OutcomeError,
		Prompt:      "fix auth",
		Error:       "exit status 1",
		ExitCode:    &exit,
	}, cfg)
	if !strings.Contains(text, "❌") {
		t.Fatalf("expected error emoji, got %q", text)
	}
	if strings.Contains(text, "my*proj") {
		t.Fatalf("session name should be escaped: %q", text)
	}
	if !strings.Contains(text, "exit status 1") {
		t.Fatalf("missing error text: %q", text)
	}
}

func TestFormatTaskAlert_success(t *testing.T) {
	text := FormatTaskAlert(TaskAlert{
		SessionName: "demo",
		Outcome:     OutcomeSuccess,
	}, DefaultNotifyConfig())
	if text != "✅ *demo* 任務完成" {
		t.Fatalf("unexpected: %q", text)
	}
}

func TestNotifyTask_skipsWhenDisabled(t *testing.T) {
	cfg := DefaultNotifyConfig()
	cfg.OnError = false
	err := NotifyTask("token", 123, cfg, TaskAlert{
		SessionName: "x",
		Outcome:     OutcomeError,
		Error:       "boom",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestNotifyTask_skipsZeroChat(t *testing.T) {
	err := NotifyTask("token", 0, DefaultNotifyConfig(), TaskAlert{
		SessionName: "x",
		Outcome:     OutcomeSuccess,
	})
	if err != nil {
		t.Fatal(err)
	}
}
