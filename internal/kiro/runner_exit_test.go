package kiro

import (
	"strings"
	"testing"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
)

func TestExitErrorFromDiagnostics(t *testing.T) {
	err := agent.NewExitError("kiro-cli", "Error: not logged in. Please run `kiro login`", nil)
	got := err.Error()
	if strings.Contains(got, "exit status") {
		t.Fatalf("should not be generic exit status: %q", got)
	}
	if !strings.Contains(got, "not logged in") {
		t.Fatalf("missing auth detail: %q", got)
	}
}

func TestExitErrorStdoutFallback(t *testing.T) {
	st := &kiroStreamState{}
	st.thinkingBuf.WriteString("Authentication required")
	detail := strings.TrimSpace(st.thinkingBuf.String())
	err := agent.NewExitError("kiro-cli", detail, nil)
	if !strings.Contains(err.Error(), "Authentication required") {
		t.Fatalf("got %q", err.Error())
	}
}
