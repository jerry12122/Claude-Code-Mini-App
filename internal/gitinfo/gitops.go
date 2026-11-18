package gitinfo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/proc"
)

const queryTimeout = 10 * time.Second

type FileStatus struct {
	Path      string `json:"path"`
	Staged    bool   `json:"staged"`
	Unstaged  bool   `json:"unstaged"`
	Untracked bool   `json:"untracked"`
	Conflict  bool   `json:"conflict"`
}

type StatusReport struct {
	IsRepo    bool         `json:"is_repo"`
	Branch    string       `json:"branch"`
	Detached  bool         `json:"detached"`
	Upstream  string       `json:"upstream"`
	Ahead     int          `json:"ahead"`
	Behind    int          `json:"behind"`
	Files     []FileStatus `json:"files"`
	Total     int          `json:"total"`
	Truncated bool         `json:"truncated"`
}

type BranchList struct {
	Current  string   `json:"current"`
	Branches []string `json:"branches"`
}

type LineStats struct {
	Additions int `json:"additions"`
	Deletions int `json:"deletions"`
}

// ChangeStats 合計已追蹤文字檔相對 HEAD 的淨增刪與未忽略的新文字檔行數。
func ChangeStats(workDir string) (LineStats, error) {
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()
	prefix, _, err := runGitContext(ctx, workDir, queryTimeout, "rev-parse", "--show-cdup")
	if err != nil {
		return LineStats{}, err
	}
	root := filepath.Join(workDir, strings.TrimSpace(prefix))
	run := func(args ...string) (string, string, error) {
		return runGitContext(ctx, root, queryTimeout, args...)
	}
	args := []string{"diff", "--numstat", "-z", "--no-ext-diff", "--no-textconv", "HEAD", "--"}
	out, diagnostic, err := run(args...)
	if err != nil && strings.TrimSpace(diagnostic) == "fatal: bad revision 'HEAD'" {
		// 尚無提交時由 Git 計算目前物件格式的空 tree hash，支援 SHA-1 與 SHA-256。
		emptyTree, _, hashErr := run("hash-object", "-t", "tree", "--stdin")
		if hashErr != nil {
			return LineStats{}, hashErr
		}
		args[len(args)-2] = strings.TrimSpace(emptyTree)
		out, _, err = run(args...)
	}
	if err != nil {
		return LineStats{}, err
	}
	paths, _, err := run("ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return LineStats{}, err
	}
	var combined strings.Builder
	combined.WriteString(out)
	// ponytail: 每個新檔用一次 Git 保留二進位判斷，共用查詢期限。新檔量大時再導入批次比對。
	for path := range strings.SplitSeq(paths, "\x00") {
		if path == "" || strings.HasSuffix(path, "/") { // Git 以尾斜線標記內嵌 repo。
			continue
		}
		newFile, _, err := run("diff", "--no-index", "--numstat", "-z", "--no-ext-diff", "--no-textconv", "--", "/dev/null", "./"+path)
		if err != nil {
			var command *CommandError
			var exit *exec.ExitError
			// --no-index 用 1 表示有差異，也可能用 1 表示無法讀檔。
			if newFile == "" || !errors.As(err, &command) || command.Kind != "failed" || !errors.As(err, &exit) || exit.ExitCode() != 1 {
				return LineStats{}, err
			}
		}
		combined.WriteString(newFile)
	}
	return parseNumstat(combined.String())
}

func parseNumstat(out string) (LineStats, error) {
	var stats LineStats
	records := strings.Split(out, "\x00")
	for i := 0; i < len(records); i++ {
		if records[i] == "" {
			continue
		}
		fields := strings.SplitN(records[i], "\t", 3)
		if len(fields) != 3 {
			return LineStats{}, fmt.Errorf("invalid Git numstat record: %q", records[i])
		}
		if fields[2] == "" {
			// rename／copy 另以兩筆 NUL 分隔的資料帶出原始與目的路徑。
			if i+2 >= len(records) || records[i+1] == "" || records[i+2] == "" {
				return LineStats{}, fmt.Errorf("missing Git numstat rename paths")
			}
			i += 2
		}
		if fields[0] == "-" && fields[1] == "-" {
			continue
		}
		added, addErr := strconv.Atoi(fields[0])
		deleted, deleteErr := strconv.Atoi(fields[1])
		if addErr != nil || deleteErr != nil || added < 0 || deleted < 0 {
			return LineStats{}, fmt.Errorf("invalid Git numstat counts: %q", fields[:2])
		}
		stats.Additions += added
		stats.Deletions += deleted
		if stats.Additions < 0 || stats.Deletions < 0 {
			return LineStats{}, fmt.Errorf("Git numstat counts overflow")
		}
	}
	return stats, nil
}

// CommandError 保留錯誤原因與長度受限的診斷訊息，供 API 回應使用。
type CommandError struct {
	Kind    string
	Output  string
	Timeout time.Duration
	Err     error
}

func (e *CommandError) Error() string {
	switch e.Kind {
	case "missing_git":
		return "請在伺服器 PATH 安裝 git"
	case "timeout":
		return fmt.Sprintf("Git 執行逾時（%g 秒）", e.Timeout.Seconds())
	default:
		if e.Output != "" {
			return strings.TrimSpace(e.Output)
		}
		return e.Err.Error()
	}
}

func (e *CommandError) Unwrap() error { return e.Err }

func runGit(workDir string, timeout time.Duration, args ...string) (stdout, stderr string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return runGitContext(ctx, workDir, timeout, args...)
}

func runGitContext(ctx context.Context, workDir string, timeout time.Duration, args ...string) (stdout, stderr string, err error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", workDir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never", "LC_ALL=C")
	cmd.SysProcAttr = proc.SysProcAttr()
	cmd.WaitDelay = time.Second
	var out, diagnostic bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &diagnostic
	err = cmd.Run()
	stdout, stderr = out.String(), diagnostic.String()
	if err == nil {
		return stdout, stderr, nil
	}
	kind := "failed"
	if errors.Is(err, exec.ErrNotFound) {
		kind = "missing_git"
	} else if ctx.Err() != nil || errors.Is(err, exec.ErrWaitDelay) {
		kind = "timeout"
	}
	output := stdout + stderr
	if len(output) > 4096 {
		output = output[len(output)-4096:]
	}
	return stdout, stderr, &CommandError{Kind: kind, Output: output, Timeout: timeout, Err: err}
}

// Status 以一次 porcelain 查詢取得狀態，包含個別未追蹤檔案。
func Status(workDir string) (StatusReport, error) {
	out, diagnostic, err := runGit(workDir, queryTimeout, "status", "--porcelain=v2", "--branch", "-z", "--untracked-files=all")
	if err != nil {
		// LC_ALL=C 固定診斷訊息語系，不需再啟動一次 Git。
		if strings.HasPrefix(diagnostic, "fatal: not a git repository") || strings.HasPrefix(diagnostic, "fatal: this operation must be run in a work tree") {
			return StatusReport{}, nil
		}
		return StatusReport{}, err
	}
	return parseStatus(out)
}

func parseStatus(out string) (StatusReport, error) {
	s := StatusReport{IsRepo: true, Files: []FileStatus{}}
	records := strings.Split(out, "\x00")
	var oid string
	for i := 0; i < len(records); i++ {
		record := records[i]
		if record == "" {
			continue
		}
		if strings.HasPrefix(record, "# ") {
			key, value, _ := strings.Cut(record[2:], " ")
			switch key {
			case "branch.oid":
				oid = value
			case "branch.head":
				s.Branch = value
				s.Detached = value == "(detached)"
			case "branch.upstream":
				s.Upstream = value
			case "branch.ab":
				if n, err := fmt.Sscanf(value, "+%d -%d", &s.Ahead, &s.Behind); err != nil || n != 2 {
					return StatusReport{}, fmt.Errorf("invalid Git branch counts: %q", value)
				}
			}
			continue
		}
		var f FileStatus
		var fields []string
		switch record[0] {
		case '1', '2', 'u':
			count := 9
			if record[0] == '2' {
				count = 10
			} else if record[0] == 'u' {
				count = 11
			}
			fields = strings.SplitN(record, " ", count)
			if len(fields) != count || len(fields[1]) != 2 || fields[count-1] == "" {
				return StatusReport{}, fmt.Errorf("invalid Git status record: %q", record)
			}
			f.Path = fields[count-1]
			f.Conflict = record[0] == 'u'
			if !f.Conflict {
				f.Staged = fields[1][0] != '.'
				f.Unstaged = fields[1][1] != '.'
			}
			if record[0] == '2' {
				i++ // -z 重新命名項目的下一筆資料是原始路徑。
				if i >= len(records) || records[i] == "" {
					return StatusReport{}, fmt.Errorf("missing Git rename source path")
				}
			}
		case '?':
			if !strings.HasPrefix(record, "? ") || len(record) == 2 {
				return StatusReport{}, fmt.Errorf("invalid Git untracked record: %q", record)
			}
			f = FileStatus{Path: record[2:], Untracked: true}
		case '!':
			continue
		default:
			return StatusReport{}, fmt.Errorf("unknown Git status record: %q", record)
		}
		s.Total++
		if len(s.Files) < 200 {
			s.Files = append(s.Files, f)
		}
	}
	if s.Detached {
		if len(oid) < 7 || !isHex(oid) {
			return StatusReport{}, fmt.Errorf("invalid Git detached HEAD: %q", oid)
		}
		s.Branch = oid[:7]
	}
	s.Truncated = s.Total > len(s.Files)
	return s, nil
}

// Branches 僅列出本機分支，同一次 Git 查詢也標示目前 HEAD。
func Branches(workDir string) (BranchList, error) {
	out, _, err := runGit(workDir, queryTimeout, "for-each-ref", "--sort=refname", "--format=%(HEAD)%(refname:strip=2)", "refs/heads/")
	if err != nil {
		return BranchList{}, err
	}
	b := BranchList{Branches: []string{}}
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		name := line[1:]
		b.Branches = append(b.Branches, name)
		if line[0] == '*' {
			b.Current = name
		}
	}
	return b, nil
}

// ponytail: 每個使用過的目錄保留一把鎖；大量臨時目錄出現時再回收閒置鎖。
var writeLocks sync.Map

func canonicalWorkDir(workDir string) (string, error) {
	abs, err := filepath.Abs(workDir)
	if err != nil {
		return "", err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err == nil && runtime.GOOS == "windows" {
		abs = strings.ToLower(abs)
	}
	return abs, err
}

func beginWrite(workDir string) (func(), error) {
	path, err := canonicalWorkDir(workDir)
	if err != nil {
		return nil, &CommandError{Kind: "failed", Err: err}
	}
	value, _ := writeLocks.LoadOrStore(path, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	if !mu.TryLock() {
		return nil, &CommandError{Kind: "busy", Output: "此工作目錄已有 Git 操作進行中"}
	}
	return mu.Unlock, nil
}

func writeOutput(workDir string, timeout time.Duration, args ...string) (string, error) {
	out, diagnostic, err := runGit(workDir, timeout, args...)
	output := out + diagnostic
	if len(output) > 4096 {
		output = output[len(output)-4096:]
	}
	return output, err
}

// Switch 僅接受完全符合既有本機分支的名稱，不建立分支或強制切換。
func Switch(workDir, branch string) (string, error) {
	if branch == "" || strings.HasPrefix(branch, "-") {
		return "", &CommandError{Kind: "invalid_branch", Output: "請選擇有效的本機分支名稱"}
	}
	unlock, err := beginWrite(workDir)
	if err != nil {
		return "", err
	}
	defer unlock()
	branches, err := Branches(workDir)
	if err != nil {
		return "", err
	}
	found := false
	for _, name := range branches.Branches {
		if name == branch {
			found = true
			break
		}
	}
	if !found {
		return "", &CommandError{Kind: "invalid_branch", Output: "找不到指定的本機分支"}
	}
	output, err := writeOutput(workDir, 30*time.Second, "switch", "--", branch)
	if err == nil {
		path, _ := canonicalWorkDir(workDir)
		cacheMu.Lock()
		for key := range cache {
			if canonical, keyErr := canonicalWorkDir(key); keyErr == nil && canonical == path {
				delete(cache, key)
			}
		}
		cacheMu.Unlock()
	}
	return output, err
}

func requireUpstream(workDir string) (StatusReport, error) {
	status, err := Status(workDir)
	if err != nil {
		return StatusReport{}, err
	}
	if !status.IsRepo || status.Detached || status.Upstream == "" {
		return StatusReport{}, &CommandError{Kind: "no_upstream", Output: "目前分支未設定上游，無法執行此操作"}
	}
	return status, nil
}

// Pull 僅允許快轉更新，不受 pull.rebase 設定影響。
func Pull(workDir string) (string, error) {
	unlock, err := beginWrite(workDir)
	if err != nil {
		return "", err
	}
	defer unlock()
	if _, err := requireUpstream(workDir); err != nil {
		return "", err
	}
	return writeOutput(workDir, 90*time.Second, "pull", "--ff-only")
}

// Push 僅推送目前分支到既有上游，不沿用可能擴大範圍的推送設定。
func Push(workDir string) (string, error) {
	unlock, err := beginWrite(workDir)
	if err != nil {
		return "", err
	}
	defer unlock()
	status, err := requireUpstream(workDir)
	if err != nil {
		return "", err
	}
	out, _, err := runGit(workDir, queryTimeout, "for-each-ref", "--format=%(upstream:remotename)%00%(upstream:remoteref)", "refs/heads/"+status.Branch)
	if err != nil {
		return "", err
	}
	upstream := strings.Split(strings.TrimSuffix(out, "\n"), "\x00")
	if len(upstream) != 2 || upstream[0] == "" || !strings.HasPrefix(upstream[1], "refs/") {
		return "", &CommandError{Kind: "no_upstream", Output: "目前分支未設定有效的上游，無法推送"}
	}
	return writeOutput(workDir, 90*time.Second, "-c", "remote."+upstream[0]+".mirror=false", "push", "--no-follow-tags", "--", upstream[0], "HEAD:"+upstream[1])
}
