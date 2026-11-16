package cursor

import (
	"os"
	"path/filepath"
	"testing"
)

func mkVersion(t *testing.T, root, name string, withFiles bool) string {
	t.Helper()
	dir := filepath.Join(root, "versions", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if withFiles {
		for _, f := range []string{"node.exe", "index.js"} {
			if err := os.WriteFile(filepath.Join(dir, f), nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	return dir
}

func TestFindNodePicksNewestVersion(t *testing.T) {
	root := t.TempDir()
	mkVersion(t, root, "2026.10.22-27dcf5f", true)
	mkVersion(t, root, "2026.11.13-eacade1", true)
	mkVersion(t, root, "2026.9.28-64d2043", true)          // 單位數月份：不能字串排序
	mkVersion(t, root, "2026.11.12-09-30-00-abc123", true) // 新格式
	mkVersion(t, root, "2026.12.01-ffff.new", true)        // 暫存更新不可選
	mkVersion(t, root, "junk", true)

	node, index, ok := findNode(root)
	want := filepath.Join(root, "versions", "2026.11.13-eacade1")
	if !ok || node != filepath.Join(want, "node.exe") || index != filepath.Join(want, "index.js") {
		t.Fatalf("got %q %q %v, want dir %q", node, index, ok, want)
	}
}

func TestFindNodePrefersSiblingLayout(t *testing.T) {
	root := t.TempDir()
	mkVersion(t, root, "2026.11.13-eacade1", true)
	for _, f := range []string{"node.exe", "index.js"} {
		os.WriteFile(filepath.Join(root, f), nil, 0o644)
	}
	node, _, ok := findNode(root)
	if !ok || node != filepath.Join(root, "node.exe") {
		t.Fatalf("got %q %v", node, ok)
	}
}

func TestFindNodeFallsBackWhenMissing(t *testing.T) {
	root := t.TempDir()
	if _, _, ok := findNode(root); ok {
		t.Fatal("no versions dir should fail")
	}
	mkVersion(t, root, "2026.11.13-eacade1", false)
	if _, _, ok := findNode(root); ok {
		t.Fatal("newest dir without node.exe should fail, not fall to older")
	}
}

func TestShimPrompt(t *testing.T) {
	got := shimPrompt("a \"$(npm root -g)\" b\nc")
	want := `a \"$(npm root -g)\" b\nc`
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
