package ws

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/claude"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

func TestCompactFakeCLI(t *testing.T) {
	bin := buildCompactFakeCLI(t)
	database, wsURL := startTestServer(t)
	agent.Register(agent.TypeClaude, func() agent.Runner { return &claude.Runner{} })
	t.Cleanup(func() {
		agent.Register(agent.TypeClaude, func() agent.Runner { return fakeClaude{} })
	})
	t.Setenv("PATH", filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))
	resolved, err := exec.LookPath("claude")
	if err != nil {
		t.Fatalf("LookPath: %v", err)
	}
	stubInfo, err := os.Stat(bin)
	if err != nil {
		t.Fatal(err)
	}
	gotInfo, err := os.Stat(resolved)
	if err != nil || !os.SameFile(stubInfo, gotInfo) {
		t.Fatalf("LookPath 解析到 %s，不是本次 stub %s", resolved, bin)
	}

	t.Run("success", func(t *testing.T) {
		logPath := filepath.Join(t.TempDir(), "prompt.txt")
		t.Setenv("COMPACT_FAKE", "success")
		t.Setenv("COMPACT_FAKE_LOG", logPath)
		s := pausedCompactSession(t, database)
		client := dialClient(t, wsURL(s.ID))
		client.await(t, "sync", isType("sync"))
		client.send(t, map[string]any{"type": "compact"})
		done := client.await(t, "compact done", compactValue("done"))
		if done["content"] != "已壓縮上下文" {
			t.Fatalf("content=%v", done["content"])
		}
		waitFor(t, "idle", func() bool {
			got, err := database.GetSession(s.ID)
			return err == nil && got.Status == db.SessionStatusIdle && !taskIsActive(s.ID)
		})
		body, err := os.ReadFile(logPath)
		if err != nil || strings.TrimSpace(string(body)) != "/compact" {
			t.Fatalf("fake CLI prompt=%q err=%v", body, err)
		}
		if strings.Contains(clientBlob(client), "summary secret") {
			t.Fatal("原生摘要進入 WS")
		}
		assertCompactPreserved(t, database, s)
	})

	t.Run("no boundary", func(t *testing.T) {
		t.Setenv("COMPACT_FAKE", "noboundary")
		s := pausedCompactSession(t, database)
		client := dialClient(t, wsURL(s.ID))
		client.await(t, "sync", isType("sync"))
		client.send(t, map[string]any{"type": "compact"})
		failed := client.await(t, "no boundary", compactValue("failed"))
		if failed["content"] != "沒有收到壓縮確認" {
			t.Fatalf("content=%v", failed["content"])
		}
		if strings.Contains(clientBlob(client), "summary secret") {
			t.Fatal("缺邊界仍露出摘要")
		}
		assertCompactPreserved(t, database, s)
	})

	t.Run("nonzero", func(t *testing.T) {
		t.Setenv("COMPACT_FAKE", "exit")
		s := pausedCompactSession(t, database)
		client := dialClient(t, wsURL(s.ID))
		client.await(t, "sync", isType("sync"))
		client.send(t, map[string]any{"type": "compact"})
		failed := client.await(t, "nonzero", compactValue("failed"))
		if failed["content"] != "壓縮行程失敗" {
			t.Fatalf("content=%v", failed["content"])
		}
		assertCompactPreserved(t, database, s)
	})
}

func pausedCompactSession(t *testing.T, database *db.DB) *db.Session {
	t.Helper()
	s := seedCompactSession(t, database, agent.TypeClaude, "agent", true)
	if err := database.SetQueuePaused(s.ID, true); err != nil {
		t.Fatal(err)
	}
	got, err := database.GetSession(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func buildCompactFakeCLI(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), compactStubName())
	cmd := exec.Command("go", "build", "-o", out, "./scripts/compactfake")
	cmd.Dir = moduleRoot(t)
	cmd.Env = os.Environ()
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fake claude: %v\n%s", err, output)
	}
	return out
}

func TestCompactStubNameSkipsProvider(t *testing.T) {
	if compactStubNameFor("linux") != "claude" || compactStubNameFor("darwin") != "claude" {
		t.Fatal("非 Windows 必須命名為 claude，避免找不到 stub 後落到系統上的真 CLI")
	}
	if compactStubNameFor("windows") != "claude.exe" {
		t.Fatal("Windows stub 名稱應為 claude.exe")
	}
}

func compactStubName() string { return compactStubNameFor(runtime.GOOS) }

func compactStubNameFor(goos string) string {
	if goos == "windows" {
		return "claude.exe"
	}
	return "claude"
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("找不到 go.mod")
		}
		dir = parent
	}
}
