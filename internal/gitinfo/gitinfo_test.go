package gitinfo

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBranchFromGitDir(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/feat/SAS\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, ok := Branch(dir)
	if !ok || b != "feat/SAS" {
		t.Fatalf("got %q ok=%v", b, ok)
	}
	// second call should hit cache
	t0 := time.Now()
	b2, ok2 := Branch(dir)
	if !ok2 || b2 != "feat/SAS" {
		t.Fatalf("cache got %q ok=%v", b2, ok2)
	}
	if time.Since(t0) > 50*time.Millisecond {
		t.Fatalf("cache path too slow: %v", time.Since(t0))
	}
}

func TestBranchEmptyWorkDir(t *testing.T) {
	if _, ok := Branch(""); ok {
		t.Fatal("expected false")
	}
}
