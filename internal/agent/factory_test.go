package agent

import "testing"

func TestIsEnabledGeminiAntigravityDisabled(t *testing.T) {
	for _, typ := range []string{TypeGemini, TypeAntigravity, "gemini", "antigravity"} {
		if IsEnabled(typ) {
			t.Fatalf("%q should be disabled", typ)
		}
		if reason := DisabledReason(typ); reason == "" {
			t.Fatalf("%q should have disabled reason", typ)
		}
	}
}

func TestNewRunnerGeminiRejected(t *testing.T) {
	for _, typ := range []string{TypeGemini, TypeAntigravity} {
		if _, err := NewRunner(typ); err == nil {
			t.Fatalf("NewRunner(%q) should fail", typ)
		}
	}
}

func TestIsEnabledClaude(t *testing.T) {
	if !IsEnabled(TypeClaude) {
		t.Fatal("claude should be enabled")
	}
}
