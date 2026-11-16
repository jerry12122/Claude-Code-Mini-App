package opener

import (
	"runtime"
	"testing"
)

func TestFolderCommand(t *testing.T) {
	cmd := folderCommand("/tmp")
	want := map[string]string{"windows": "explorer", "darwin": "open"}[runtime.GOOS]
	if want == "" {
		want = "xdg-open"
	}
	if cmd.Args[0] != want {
		t.Fatalf("args=%v want %s", cmd.Args, want)
	}
	if cmd.Args[1] != "/tmp" {
		t.Fatalf("workDir 未原樣傳入: args=%v", cmd.Args)
	}
}

func TestVscodeURI(t *testing.T) {
	if got := vscodeURI(`C:\Users\a b\proj`); got != "vscode://file/C:/Users/a b/proj" {
		t.Fatalf("got=%q", got)
	}
	if got := vscodeURI("/home/a b/proj"); got != "vscode://file/home/a b/proj" {
		t.Fatalf("got=%q", got)
	}
}

func TestVscodeNoAdminCommand(t *testing.T) {
	cmd := vscodeNoAdminCommand(`C:\Users\a b\proj`)
	if cmd.Args[0] != "explorer.exe" {
		t.Fatalf("args=%v", cmd.Args)
	}
	if cmd.Args[1] != "vscode://file/C:/Users/a b/proj" {
		t.Fatalf("args=%v", cmd.Args)
	}
}
