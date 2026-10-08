package shell

import (
	"context"
	"runtime"
	"strings"
	"testing"
)

func TestDetectType(t *testing.T) {
	typ := DetectType()
	if runtime.GOOS == "windows" && typ != TypePowerShell {
		t.Fatalf("windows want powershell, got %s", typ)
	}
	if runtime.GOOS != "windows" && typ != TypeBash && typ != TypeSh {
		t.Fatalf("unix want bash or sh, got %s", typ)
	}
}

func TestRunEcho(t *testing.T) {
	dir := t.TempDir()
	var out strings.Builder
	var shellErr string
	ctx := context.Background()
	// GitHub Windows runner 冷啟動 powershell.exe 可能超過 10 秒；逾時時 Run 仍回 nil，只送 EventError。
	err := Run(ctx, RunOptions{Command: echoCmd(), WorkDir: dir, Timeout: 60}, func(e Event) {
		switch e.Type {
		case EventDeltaStdout, EventDeltaStderr:
			out.WriteString(e.Text)
		case EventError:
			shellErr = e.Text
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if shellErr != "" {
		t.Fatalf("shell error: %s; output: %q", shellErr, out.String())
	}
	if !strings.Contains(out.String(), "hi_shell") {
		t.Fatalf("output: %q", out.String())
	}
}

func echoCmd() string {
	if runtime.GOOS == "windows" {
		return "Write-Output hi_shell"
	}
	return "echo hi_shell"
}

func TestValidateWorkDirEmpty(t *testing.T) {
	err := Run(context.Background(), RunOptions{Command: "echo x", WorkDir: "", Timeout: 5}, func(Event) {})
	if err == nil {
		t.Fatal("expected error")
	}
}
