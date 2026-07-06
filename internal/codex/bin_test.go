package codex

import (
	"os"
	"runtime"
	"testing"
)

func TestResolveBin(t *testing.T) {
	p, err := ResolveBin()
	if err != nil {
		t.Skipf("codex not installed: %v", err)
	}
	if p == "" {
		t.Fatal("empty path")
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("stat %q: %v", p, err)
	}
}

func TestHasAuthConfig(t *testing.T) {
	// 僅確認函式可呼叫；結果依本機環境而異。
	_ = HasAuthConfig()
}

func TestWindowsDefaultBin(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows only")
	}
	if p := windowsDefaultBin(); p == "" {
		t.Skip("default codex.exe not found")
	}
}
