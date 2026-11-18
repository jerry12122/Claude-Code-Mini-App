package api

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/gitinfo"
)

func apiGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git 未安裝")
	}
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s: %v", args, out, err)
	}
	return string(out)
}

func TestGitWriteRoutes(t *testing.T) {
	database := testDB(t)
	dir := t.TempDir()
	apiGit(t, dir, "init", "--initial-branch=main")
	apiGit(t, dir, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "initial")
	apiGit(t, dir, "branch", "dev")
	sess, err := database.CreateSession("Git 寫入", "", dir, "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	h := NewGitHandler(database)
	app := fiber.New()
	app.Post("/sessions/:id/git/checkout", h.Checkout)
	app.Post("/sessions/:id/git/pull", h.Pull)
	app.Post("/sessions/:id/git/push", h.Push)
	app.Get("/sessions/:id/git/status", h.Status)
	app.Get("/sessions/:id/git/branches", h.Branches)
	app.Get("/sessions", NewSessionHandler(database).List)
	request := func(method, path, body string, want int) string {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("%s %s: want=%d got=%d %s", method, path, want, resp.StatusCode, raw)
		}
		return string(raw)
	}
	path := "/sessions/" + sess.ID + "/git/"
	if err := database.UpdateSessionStatus(sess.ID, db.SessionStatusRunning); err != nil {
		t.Fatal(err)
	}
	for _, op := range []string{"pull", "push", "checkout"} {
		request("POST", path+op, `{"branch":"dev"}`, 409)
	}
	request("GET", path+"status", "", 200)
	request("GET", path+"branches", "", 200)
	if err := database.UpdateSessionStatus(sess.ID, db.SessionStatusIdle); err != nil {
		t.Fatal(err)
	}
	other, err := database.CreateSession("同目錄", "", filepath.Join(dir, "."), "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateSessionStatus(other.ID, db.SessionStatusAwaitingConfirm); err != nil {
		t.Fatal(err)
	}
	request("POST", path+"push", "", 409)
	if err := database.UpdateSessionStatus(other.ID, db.SessionStatusIdle); err != nil {
		t.Fatal(err)
	}
	request("POST", path+"pull", "", 422)
	request("POST", path+"push", "", 422)
	for _, body := range []string{`{"branch":"missing"}`, `{"branch":"-x"}`, `{"branch":""}`, `{"branch":null}`, `{"branch":123}`, `{`} {
		request("POST", path+"checkout", body, 400)
	}
	request("GET", "/sessions", "", 200) // 先填入分支快取。
	request("POST", path+"checkout", `{"branch":"dev"}`, 200)
	raw := request("GET", "/sessions", "", 200)
	var sessions []db.Session
	if err := json.Unmarshal([]byte(raw), &sessions); err != nil {
		t.Fatal(err)
	}
	for _, s := range sessions {
		if s.WorkDir == dir && s.GitBranch != "dev" {
			t.Fatalf("stale list=%s", raw)
		}
	}
	request("POST", "/sessions/missing/git/pull", "", 404)
}

func TestGitErrorResponses(t *testing.T) {
	for _, tc := range []struct {
		kind string
		want int
	}{
		{"invalid_branch", 400}, {"busy", 409}, {"no_upstream", 422}, {"failed", 422}, {"timeout", 504}, {"missing_git", 500},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			app := fiber.New()
			app.Get("/", func(c *fiber.Ctx) error {
				return gitError(c, &gitinfo.CommandError{Kind: tc.kind, Output: "details\n\nGit 拒絕訊息", Timeout: 90 * time.Second})
			})
			resp, err := app.Test(httptest.NewRequest("GET", "/", nil))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			var data map[string]string
			if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != tc.want || data["error"] == "" {
				t.Fatalf("%s: %d %+v", tc.kind, resp.StatusCode, data)
			}
			if tc.kind == "failed" && (data["error"] != "Git 拒絕訊息" || data["output"] != "details\n\nGit 拒絕訊息") {
				t.Fatalf("git diagnostic=%+v", data)
			}
			if tc.kind == "timeout" && !strings.Contains(data["error"], "90") {
				t.Fatalf("timeout=%+v", data)
			}
		})
	}
}

func TestGitReadRoutes(t *testing.T) {
	database := testDB(t)
	repo := t.TempDir()
	apiGit(t, repo, "init", "--initial-branch=main")
	apiGit(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "initial")
	apiGit(t, repo, "branch", "dev")
	h := NewGitHandler(database)
	app := fiber.New()
	app.Get("/sessions/:id/git/status", h.Status)
	app.Get("/sessions/:id/git/branches", h.Branches)
	for _, tc := range []struct {
		name, dir string
		want      int
	}{
		{"repo", repo, 200}, {"plain", t.TempDir(), 200}, {"empty", "", 400},
		{"missingDir", filepath.Join(t.TempDir(), "missing"), 400},
	} {
		s, err := database.CreateSession(tc.name, "", tc.dir, "default", "claude", nil, "agent")
		if err != nil {
			t.Fatal(err)
		}
		resp, err := app.Test(httptest.NewRequest("GET", "/sessions/"+s.ID+"/git/status", nil), -1)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Fatalf("%s: status=%d body=%s", tc.name, resp.StatusCode, raw)
		}
		if tc.name == "plain" && string(raw) != `{"is_repo":false}` {
			t.Fatalf("nonrepo=%s", raw)
		}
		if tc.name == "repo" {
			var got map[string]any
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatal(err)
			}
			if got["branch"] != "main" || got["is_repo"] != true || got["total"] != float64(0) || got["additions"] != float64(0) || got["deletions"] != float64(0) {
				t.Fatalf("status=%s", raw)
			}
			resp, err = app.Test(httptest.NewRequest("GET", "/sessions/"+s.ID+"/git/branches", nil), -1)
			if err != nil {
				t.Fatal(err)
			}
			raw, _ = io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != 200 || string(raw) != `{"current":"main","branches":["dev","main"]}` {
				t.Fatalf("branches=%d %s", resp.StatusCode, raw)
			}
		}
	}
	for _, path := range []string{"status", "branches"} {
		resp, err := app.Test(httptest.NewRequest("GET", "/sessions/missing/git/"+path, nil), -1)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Fatalf("missing %s=%d", path, resp.StatusCode)
		}
	}
}

func TestGitStatusLineCounts(t *testing.T) {
	database := testDB(t)
	dir := t.TempDir()
	apiGit(t, dir, "init", "--initial-branch=main")
	apiGit(t, dir, "config", "core.autocrlf", "false")
	name := filepath.Join(dir, "file")
	if err := os.WriteFile(name, []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	apiGit(t, dir, "add", ".")
	apiGit(t, dir, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "baseline")
	if err := os.WriteFile(name, []byte("one\nstaged\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	apiGit(t, dir, "add", ".")
	if err := os.WriteFile(name, []byte("one\nfinal\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "untracked"), []byte("new\ntext"), 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := database.CreateSession("Git 行數", "", dir, "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	app.Get("/sessions/:id/git/status", NewGitHandler(database).Status)
	resp, err := app.Test(httptest.NewRequest("GET", "/sessions/"+session.ID+"/git/status", nil), -1)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || got["is_repo"] != true || got["branch"] != "main" || got["additions"] != float64(3) || got["deletions"] != float64(0) {
		t.Fatalf("status line counts: %d %+v", resp.StatusCode, got)
	}
	// 移除測試 repo 的 HEAD blob；status 仍可讀取中繼資料，行數 diff 必須明確失敗。
	blob := strings.TrimSpace(apiGit(t, dir, "rev-parse", "HEAD:file"))
	if err := os.Remove(filepath.Join(dir, ".git", "objects", blob[:2], blob[2:])); err != nil {
		t.Fatal(err)
	}
	if _, err := gitinfo.Status(dir); err != nil {
		t.Fatalf("porcelain should still succeed: %v", err)
	}
	failed, err := app.Test(httptest.NewRequest("GET", "/sessions/"+session.ID+"/git/status", nil), -1)
	if err != nil {
		t.Fatal(err)
	}
	defer failed.Body.Close()
	var diagnostic map[string]any
	if err := json.NewDecoder(failed.Body).Decode(&diagnostic); err != nil {
		t.Fatal(err)
	}
	if failed.StatusCode != 422 || diagnostic["error"] == "" || diagnostic["additions"] != nil {
		t.Fatalf("diff error should not fabricate counts: %d %+v", failed.StatusCode, diagnostic)
	}
}
