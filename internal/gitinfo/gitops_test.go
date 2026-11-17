package gitinfo

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func testGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never", "LC_ALL=C")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func testRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	testGit(t, dir, "init", "-b", "main")
	testGit(t, dir, "config", "user.email", "test@example.com")
	testGit(t, dir, "config", "user.name", "Git test")
	testGit(t, dir, "config", "core.autocrlf", "false")
	testGit(t, dir, "commit", "--allow-empty", "-m", "initial")
	return dir
}

func testFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestStatusAndLocalBranches(t *testing.T) {
	dir := testRepo(t)
	s, err := Status(dir)
	if err != nil || !s.IsRepo || s.Branch != "main" || s.Detached || s.Upstream != "" || s.Ahead != 0 || s.Behind != 0 || s.Total != 0 || s.Files == nil {
		t.Fatalf("clean status: %+v, %v", s, err)
	}
	testGit(t, dir, "branch", "dev")
	testGit(t, dir, "update-ref", "refs/remotes/origin/remote-only", "HEAD")
	b, err := Branches(dir)
	if err != nil || b.Current != "main" || !reflect.DeepEqual(b.Branches, []string{"dev", "main"}) {
		t.Fatalf("branches: %+v, %v", b, err)
	}
	oid := testGit(t, dir, "rev-parse", "HEAD")
	testGit(t, dir, "checkout", "--detach", "HEAD")
	s, err = Status(dir)
	if err != nil || !s.Detached || s.Branch != oid[:7] || s.Upstream != "" {
		t.Fatalf("detached status: %+v, %v", s, err)
	}
	s, err = Status(t.TempDir())
	if err != nil || s.IsRepo {
		t.Fatalf("non-repo status: %+v, %v", s, err)
	}
}

func TestStatusFileCategories(t *testing.T) {
	dir := testRepo(t)
	for _, name := range []string{"unstaged", "staged", "both", "old name"} {
		testFile(t, dir, name, "before\n")
	}
	testGit(t, dir, "add", ".")
	testGit(t, dir, "commit", "-m", "files")
	testFile(t, dir, "unstaged", "after\n")
	testFile(t, dir, "staged", "after\n")
	testFile(t, dir, "both", "staged\n")
	testGit(t, dir, "add", "staged", "both")
	testFile(t, dir, "both", "unstaged\n")
	testFile(t, dir, "untracked file", "new\n")
	testGit(t, dir, "mv", "old name", "renamed file")
	s, err := Status(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]FileStatus{
		"unstaged":       {Path: "unstaged", Unstaged: true},
		"staged":         {Path: "staged", Staged: true},
		"both":           {Path: "both", Staged: true, Unstaged: true},
		"untracked file": {Path: "untracked file", Untracked: true},
		"renamed file":   {Path: "renamed file", Staged: true},
	}
	if s.Total != len(want) {
		t.Fatalf("total=%d, files=%+v", s.Total, s.Files)
	}
	for _, f := range s.Files {
		if expected, ok := want[f.Path]; !ok || f != expected {
			t.Errorf("file: %+v, expected %+v", f, expected)
		}
	}
	// 以真實合併建立規格表格中的第五種狀態：僅標示衝突。
	testGit(t, dir, "reset", "--hard", "HEAD")
	testGit(t, dir, "clean", "-fd")
	testGit(t, dir, "checkout", "-b", "other")
	testFile(t, dir, "both", "other side\n")
	testGit(t, dir, "commit", "-am", "other")
	testGit(t, dir, "checkout", "main")
	testFile(t, dir, "both", "main side\n")
	testGit(t, dir, "commit", "-am", "main")
	if out, err := exec.Command("git", "-C", dir, "merge", "other").CombinedOutput(); err == nil {
		t.Fatalf("expected conflict: %s", out)
	}
	s, err = Status(dir)
	if err != nil || s.Total != 1 || s.Files[0] != (FileStatus{Path: "both", Conflict: true}) {
		t.Fatalf("conflict: %+v, %v", s, err)
	}
}

func TestStatusAheadBehindAndTruncation(t *testing.T) {
	dir := testRepo(t)
	remote := t.TempDir()
	testGit(t, remote, "init", "--bare")
	testGit(t, dir, "remote", "add", "origin", remote)
	testGit(t, dir, "push", "-u", "origin", "main")
	s, err := Status(dir)
	if err != nil || s.Upstream != "origin/main" || s.Ahead != 0 || s.Behind != 0 {
		t.Fatalf("in-sync status: %+v, %v", s, err)
	}
	testGit(t, dir, "checkout", "-b", "remote-tip")
	testGit(t, dir, "commit", "--allow-empty", "-m", "remote")
	testGit(t, dir, "push", "origin", "HEAD:main")
	testGit(t, dir, "checkout", "main")
	for i := 0; i < 2; i++ {
		testGit(t, dir, "commit", "--allow-empty", "-m", fmt.Sprintf("local %d", i))
	}
	s, err = Status(dir)
	if err != nil || s.Upstream != "origin/main" || s.Ahead != 2 || s.Behind != 1 {
		t.Fatalf("diverged status: %+v, %v", s, err)
	}
	if err := os.Mkdir(filepath.Join(dir, "new files"), 0o700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 205; i++ {
		testFile(t, dir, fmt.Sprintf("new files/%03d", i), "new\n")
	}
	s, err = Status(dir)
	if err != nil || s.Total != 205 || len(s.Files) != 200 || !s.Truncated {
		t.Fatalf("truncated status: %+v, %v", s, err)
	}
}

func TestStatusParserPreservesPaths(t *testing.T) {
	s, err := parseStatus("# branch.oid abcdef0123456789\x00# branch.head main\x002 R. N... 100644 100644 100644 abc def R100 new\nname\x00old\nname\x00? file with spaces\n\x00")
	if err != nil || s.Total != 2 || s.Files[0].Path != "new\nname" || s.Files[1].Path != "file with spaces\n" {
		t.Fatalf("paths: %+v, %v", s, err)
	}
	if _, err := parseStatus("1 broken\x00"); err == nil {
		t.Fatal("malformed porcelain must fail")
	}
}

func TestGitCommandErrors(t *testing.T) {
	dir := testRepo(t)
	// 使用已知子指令的錯誤路徑，避免 Git 掃描 CI 的 PATH 搜尋相似指令。
	_, _, err := runGit(dir, queryTimeout, "rev-parse", "--verify", "refs/heads/missing")
	var commandErr *CommandError
	if !errors.As(err, &commandErr) || commandErr.Kind != "failed" || commandErr.Output == "" {
		t.Fatalf("command failure: %v", err)
	}
	_, _, err = runGit(dir, time.Nanosecond, "status")
	if !errors.As(err, &commandErr) || commandErr.Kind != "timeout" {
		t.Fatalf("timeout: %v", err)
	}
	t.Setenv("PATH", t.TempDir())
	_, err = Status(dir)
	if !errors.As(err, &commandErr) || commandErr.Kind != "missing_git" {
		t.Fatalf("missing git: %v", err)
	}
}

func TestSwitchAndCacheInvalidation(t *testing.T) {
	dir := testRepo(t)
	testFile(t, dir, "file", "main\n")
	testGit(t, dir, "add", "file")
	testGit(t, dir, "commit", "-m", "main file")
	testGit(t, dir, "checkout", "-b", "dev")
	testFile(t, dir, "file", "dev\n")
	testGit(t, dir, "commit", "-am", "dev file")
	testGit(t, dir, "checkout", "main")
	if branch, ok := Branch(dir); !ok || branch != "main" {
		t.Fatalf("initial cache: %q %v", branch, ok)
	}
	for _, branch := range []string{"", "-x", "missing"} {
		_, err := Switch(dir, branch)
		var commandErr *CommandError
		if !errors.As(err, &commandErr) || commandErr.Kind != "invalid_branch" {
			t.Errorf("unsafe branch %q: %v", branch, err)
		}
	}
	testFile(t, dir, "file", "uncommitted\n")
	if _, err := Switch(dir, "dev"); err == nil {
		t.Fatal("switch should preserve local modifications")
	}
	if got := testGit(t, dir, "branch", "--show-current"); got != "main" {
		t.Fatalf("blocked switch changed branch: %s", got)
	}
	testGit(t, dir, "restore", "file")
	if _, err := Switch(dir, "dev"); err != nil {
		t.Fatal(err)
	}
	if branch, ok := Branch(dir); !ok || branch != "dev" {
		t.Fatalf("stale branch cache: %q %v", branch, ok)
	}
}

func testUpstream(t *testing.T) (string, string) {
	t.Helper()
	dir := testRepo(t)
	remote := t.TempDir()
	testGit(t, remote, "init", "--bare")
	testGit(t, dir, "remote", "add", "origin", remote)
	testGit(t, dir, "push", "-u", "origin", "main")
	return dir, remote
}

func TestPullFastForwardAndRejectDivergence(t *testing.T) {
	dir, _ := testUpstream(t)
	testGit(t, dir, "checkout", "-b", "remote-tip")
	testGit(t, dir, "commit", "--allow-empty", "-m", "remote ahead")
	remoteHead := testGit(t, dir, "rev-parse", "HEAD")
	testGit(t, dir, "push", "origin", "HEAD:main")
	testGit(t, dir, "checkout", "main")
	if _, err := Pull(dir); err != nil {
		t.Fatal(err)
	}
	if got := testGit(t, dir, "rev-parse", "HEAD"); got != remoteHead {
		t.Fatalf("pull did not fast-forward: %s", got)
	}
	testGit(t, dir, "checkout", "remote-tip")
	testGit(t, dir, "commit", "--allow-empty", "-m", "remote diverged")
	testGit(t, dir, "push", "origin", "HEAD:main")
	testGit(t, dir, "checkout", "main")
	testGit(t, dir, "commit", "--allow-empty", "-m", "local diverged")
	localHead := testGit(t, dir, "rev-parse", "HEAD")
	testGit(t, dir, "config", "pull.rebase", "true")
	if _, err := Pull(dir); err == nil {
		t.Fatal("diverged pull should fail")
	}
	if got := testGit(t, dir, "rev-parse", "HEAD"); got != localHead {
		t.Fatalf("diverged pull modified HEAD: %s", got)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "MERGE_HEAD")); !os.IsNotExist(err) {
		t.Fatal("diverged pull started a merge")
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "rebase-merge")); !os.IsNotExist(err) {
		t.Fatal("diverged pull started a rebase")
	}
}

func TestPushOnlyCurrentBranchAndRemoteReject(t *testing.T) {
	dir, remote := testUpstream(t)
	testGit(t, dir, "branch", "other")
	testGit(t, dir, "push", "origin", "other")
	otherBefore := testGit(t, remote, "rev-parse", "refs/heads/other")
	testGit(t, dir, "checkout", "other")
	testGit(t, dir, "commit", "--allow-empty", "-m", "other ahead")
	testGit(t, dir, "checkout", "main")
	testGit(t, dir, "commit", "--allow-empty", "-m", "main ahead")
	testGit(t, dir, "config", "push.default", "matching")
	if _, err := Push(dir); err != nil {
		t.Fatal(err)
	}
	if got := testGit(t, remote, "rev-parse", "refs/heads/main"); got != testGit(t, dir, "rev-parse", "HEAD") {
		t.Fatalf("push did not update main: %s", got)
	}
	if got := testGit(t, remote, "rev-parse", "refs/heads/other"); got != otherBefore {
		t.Fatalf("push modified other branch: %s", got)
	}
	testGit(t, dir, "checkout", "-b", "remote-tip")
	testGit(t, dir, "commit", "--allow-empty", "-m", "remote ahead")
	testGit(t, dir, "push", "origin", "HEAD:main")
	remoteHead := testGit(t, remote, "rev-parse", "refs/heads/main")
	testGit(t, dir, "checkout", "main")
	testGit(t, dir, "commit", "--allow-empty", "-m", "local diverged")
	if _, err := Push(dir); err == nil {
		t.Fatal("non-fast-forward push must fail")
	}
	if got := testGit(t, remote, "rev-parse", "refs/heads/main"); got != remoteHead {
		t.Fatalf("rejected push modified remote: %s", got)
	}
}

func TestNoUpstreamAndConcurrentWrites(t *testing.T) {
	dir := testRepo(t)
	trace := filepath.Join(t.TempDir(), "git-trace.log")
	t.Setenv("GIT_TRACE", filepath.ToSlash(trace))
	for _, detached := range []bool{false, true} {
		if detached {
			testGit(t, dir, "checkout", "--detach", "HEAD")
		}
		for _, op := range []func(string) (string, error){Pull, Push} {
			_, err := op(dir)
			var commandErr *CommandError
			if !errors.As(err, &commandErr) || commandErr.Kind != "no_upstream" {
				t.Fatalf("no upstream detached=%v: %v", detached, err)
			}
		}
	}
	traceOutput, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(traceOutput), "built-in: git pull") || strings.Contains(string(traceOutput), "built-in: git push") {
		t.Fatalf("network command without upstream: %s", traceOutput)
	}
	unlock, err := beginWrite(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	started := time.Now()
	_, err = Switch(filepath.Join(dir, "."), "main")
	var commandErr *CommandError
	if !errors.As(err, &commandErr) || commandErr.Kind != "busy" || time.Since(started) > time.Second {
		t.Fatalf("concurrent write did not reject promptly: %v", err)
	}
	if _, err := Status(dir); err != nil {
		t.Fatalf("write lock should not block reads: %v", err)
	}
}

func TestPushConfigCannotExpandScope(t *testing.T) {
	dir, remote := testUpstream(t)
	testGit(t, dir, "branch", "other")
	testGit(t, dir, "push", "origin", "other", "HEAD:remote-only")
	otherBefore := testGit(t, remote, "rev-parse", "refs/heads/other")
	testGit(t, dir, "checkout", "other")
	testGit(t, dir, "commit", "--allow-empty", "-m", "other ahead")
	testGit(t, dir, "checkout", "main")
	testGit(t, dir, "commit", "--allow-empty", "-m", "main ahead")
	testGit(t, dir, "tag", "-a", "extra-tag", "-m", "must not push")
	decoy := t.TempDir()
	testGit(t, decoy, "init", "--bare")
	testGit(t, dir, "remote", "add", "decoy", decoy)
	testGit(t, dir, "config", "branch.main.pushRemote", "decoy")
	testGit(t, dir, "config", "remote.pushDefault", "decoy")
	testGit(t, dir, "config", "remote.origin.push", "+refs/heads/other:refs/heads/other")
	testGit(t, dir, "config", "remote.origin.mirror", "true")
	testGit(t, dir, "config", "push.followTags", "true")
	if _, err := Push(dir); err != nil {
		t.Fatal(err)
	}
	if got := testGit(t, remote, "rev-parse", "refs/heads/main"); got != testGit(t, dir, "rev-parse", "HEAD") {
		t.Fatalf("push did not update upstream: %s", got)
	}
	if got := testGit(t, remote, "rev-parse", "refs/heads/other"); got != otherBefore {
		t.Fatalf("push changed other branch: %s", got)
	}
	if got := testGit(t, remote, "rev-parse", "refs/heads/remote-only"); got != otherBefore {
		t.Fatalf("push deleted or changed remote-only branch: %s", got)
	}
	if got := testGit(t, remote, "for-each-ref", "--format=%(refname)", "refs/tags/"); got != "" {
		t.Fatalf("push added tags: %s", got)
	}
	if got := testGit(t, decoy, "for-each-ref", "--format=%(refname)"); got != "" {
		t.Fatalf("push used pushRemote or pushDefault: %s", got)
	}
}

func TestGitRunnerEnvironmentAndOutputCap(t *testing.T) {
	dir := testRepo(t)
	t.Setenv("GIT_TERMINAL_PROMPT", "1")
	t.Setenv("GCM_INTERACTIVE", "always")
	t.Setenv("GIT_OPTIONAL_LOCKS", "1")
	out, _, err := runGit(dir, queryTimeout, "-c", `alias.runner-env=!printf '%s|%s|%s' "$GIT_TERMINAL_PROMPT" "$GCM_INTERACTIVE" "$GIT_OPTIONAL_LOCKS"`, "runner-env")
	if err != nil || out != "0|never|0" {
		t.Fatalf("runner environment: %q, %v", out, err)
	}
	out, err = writeOutput(dir, queryTimeout, "-c", "test.output="+strings.Repeat("x", 5000), "config", "--get", "test.output")
	if err != nil || len(out) != 4096 {
		t.Fatalf("success output length: %d, %v", len(out), err)
	}
	_, err = writeOutput(dir, queryTimeout, "-c", "alias.runner-fail=!printf '"+strings.Repeat("x", 5000)+"' >&2; exit 1", "runner-fail")
	var commandErr *CommandError
	if !errors.As(err, &commandErr) || commandErr.Kind != "failed" || len(commandErr.Output) != 4096 {
		t.Fatalf("failure output is not capped: %v", err)
	}
}

func TestChangeStatsCombinedNetChanges(t *testing.T) {
	dir := testRepo(t)
	testFile(t, dir, "same file", "one\ntwo\n")
	testFile(t, dir, "deleted", "one\ntwo\nthree\n")
	testGit(t, dir, "add", ".")
	testGit(t, dir, "commit", "-m", "baseline")
	testFile(t, dir, "same file", "one\nstaged\ntwo\n")
	testGit(t, dir, "add", "same file")
	testFile(t, dir, "same file", "one\nfinal\ntwo\n")
	stats, err := ChangeStats(dir)
	if err != nil || stats != (LineStats{Additions: 1}) {
		t.Fatalf("staged + unstaged net counts: %+v, %v", stats, err)
	}
	testGit(t, dir, "rm", "deleted")
	testFile(t, dir, "new staged file", "four\nfive\n")
	testGit(t, dir, "add", "new staged file")
	stats, err = ChangeStats(dir)
	if err != nil || stats != (LineStats{Additions: 3, Deletions: 3}) {
		t.Fatalf("tracked addition + deletion counts: %+v, %v", stats, err)
	}
}

func TestChangeStatsUnborn(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git 未安裝")
	}
	for _, format := range []string{"sha1", "sha256"} {
		t.Run(format, func(t *testing.T) {
			dir := t.TempDir()
			if out, err := exec.Command("git", "-C", dir, "init", "-b", "main", "--object-format="+format).CombinedOutput(); err != nil {
				t.Skipf("Git 不支援 %s: %v %s", format, err, out)
			}
			testGit(t, dir, "config", "core.autocrlf", "false")
			testFile(t, dir, "staged file with whitespace", "one\ntwo\n")
			testGit(t, dir, "add", ".")
			stats, err := ChangeStats(dir)
			if err != nil || stats != (LineStats{Additions: 2}) {
				t.Fatalf("unborn counts: %+v, %v", stats, err)
			}
		})
	}
}

func TestChangeStatsBinaryAndUntrackedExcluded(t *testing.T) {
	dir := testRepo(t)
	testFile(t, dir, "binary", "before\x00\n")
	testGit(t, dir, "add", ".")
	testGit(t, dir, "commit", "-m", "binary baseline")
	testFile(t, dir, "binary", "after\x00\nmore\n")
	testFile(t, dir, "untracked text", "one\ntwo\nthree\n")
	stats, err := ChangeStats(dir)
	if err != nil || stats != (LineStats{}) {
		t.Fatalf("binary or untracked text contributed lines: %+v, %v", stats, err)
	}
}

func TestChangeStatsRename(t *testing.T) {
	dir := testRepo(t)
	content := "one\ntwo\nthree\nfour\nfive\nsix\n"
	testFile(t, dir, "old name", content)
	testGit(t, dir, "add", ".")
	testGit(t, dir, "commit", "-m", "rename baseline")
	testGit(t, dir, "mv", "old name", "new name")
	testFile(t, dir, "new name", content+"seven\n")
	stats, err := ChangeStats(dir)
	if err != nil || stats != (LineStats{Additions: 1}) {
		t.Fatalf("rename counts: %+v, %v", stats, err)
	}
}

func TestNumstatSpecialPaths(t *testing.T) {
	out := "1\t0\tregular\tname\n\x003\t1\t\x00old\tname\n\x001\t999\tfake\n\x00-\t-\tbinary\x002\t2\ttail\x00"
	stats, err := parseNumstat(out)
	if err != nil || stats != (LineStats{Additions: 6, Deletions: 3}) {
		t.Fatalf("special paths or rename records confused counts: %+v, %v", stats, err)
	}
	for _, malformed := range []string{"missing tabs\x00", "1\t2\t\x00old\x00", "-\t1\tfile\x00", "-1\t0\tfile\x00"} {
		if _, err := parseNumstat(malformed); err == nil {
			t.Errorf("malformed numstat accepted: %q", malformed)
		}
	}
}
