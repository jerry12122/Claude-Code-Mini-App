package ws

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
)

func TestClaudeAsker_allowDenyAndDropAnswered(t *testing.T) {
	t.Parallel()
	decision := make(chan bool, 1)
	a, unreg := registerClaudeAsker("s-allow", context.Background(), func(ctx context.Context, tools any) bool {
		if ds, ok := tools.([]agent.PermissionDenial); !ok || len(ds) != 1 || ds[0].ToolName != "Bash" || ds[0].ToolUseID == "" {
			t.Errorf("tools 應與 Claude denial 同形: %#v", tools)
		}
		return <-decision
	}, nil, nil, nil)
	defer unreg()

	decision <- true
	if ok, _ := AskClaudePermission(context.Background(), "s-allow", "Bash", json.RawMessage(`{"command":"x"}`), "u1"); !ok {
		t.Fatal("使用者允許應回 true")
	}
	decision <- false
	ok, msg := AskClaudePermission(context.Background(), "s-allow", "Bash", json.RawMessage(`{}`), "u2")
	if ok || msg == "" {
		t.Fatalf("使用者拒絕應回 false＋原因: %v %q", ok, msg)
	}

	// result.permission_denials 仍會列出已回答的 u2，須剔除；未回答的 u3 保留。
	got := a.dropAnswered([]agent.PermissionDenial{{ToolUseID: "u2"}, {ToolUseID: "u3"}, {ToolUseID: ""}})
	if len(got) != 2 || got[0].ToolUseID != "u3" || got[1].ToolUseID != "" {
		t.Fatalf("dropAnswered=%+v", got)
	}
	var nilAsker *claudeAsker
	if len(nilAsker.dropAnswered([]agent.PermissionDenial{{ToolUseID: "x"}})) != 1 {
		t.Fatal("nil asker（非 bypass 的 run）不應過濾任何 denial")
	}
}

func TestClaudeAsker_noActiveRunDenies(t *testing.T) {
	t.Parallel()
	if ok, msg := AskClaudePermission(context.Background(), "no-such-session", "Bash", nil, "u"); ok || msg == "" {
		t.Fatalf("沒有進行中的 run 必須拒絕: %v %q", ok, msg)
	}
}

// 同一 session 並行的詢問必須一題一題問（pendingPerms 每個 session 只有一格）。
func TestClaudeAsker_serializesPrompts(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	cur, max := 0, 0
	release := make(chan struct{})
	_, unreg := registerClaudeAsker("s-ser", context.Background(), func(ctx context.Context, tools any) bool {
		mu.Lock()
		cur++
		if cur > max {
			max = cur
		}
		mu.Unlock()
		<-release
		mu.Lock()
		cur--
		mu.Unlock()
		return true
	}, nil, nil, nil)
	defer unreg()

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			AskClaudePermission(context.Background(), "s-ser", "Bash", nil, "u")
		}()
	}
	time.Sleep(100 * time.Millisecond)
	for i := 0; i < 3; i++ {
		release <- struct{}{}
	}
	wg.Wait()
	if max != 1 {
		t.Fatalf("同時進行的詢問數=%d，應為 1", max)
	}
}

// run 被中斷（runCtx 取消）時，等待中的詢問要解除，不可卡住。
func TestClaudeAsker_runCancelUnblocks(t *testing.T) {
	t.Parallel()
	runCtx, cancel := context.WithCancel(context.Background())
	_, unreg := registerClaudeAsker("s-cancel", runCtx, func(ctx context.Context, tools any) bool {
		<-ctx.Done()
		return false
	}, nil, nil, nil)
	defer unreg()
	done := make(chan bool, 1)
	go func() {
		ok, _ := AskClaudePermission(context.Background(), "s-cancel", "Bash", nil, "u")
		done <- ok
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case ok := <-done:
		if ok {
			t.Fatal("中斷不可視為允許")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("run 取消後詢問仍卡住")
	}
}

func TestWritePermMCPConfig(t *testing.T) {
	SetClaudePermMCP("http://127.0.0.1:1/mcp/perm", "tok123")
	defer SetClaudePermMCP("", "")
	path, cleanup, err := writePermMCPConfig("sess-9")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`"miniapp_perm"`, `"type":"http"`, "http://127.0.0.1:1/mcp/perm", "Bearer tok123", `"X-Miniapp-Session":"sess-9"`} {
		if !strings.Contains(s, want) {
			t.Errorf("設定檔缺少 %s:\n%s", want, s)
		}
	}
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("cleanup 後暫存檔應被刪除")
	}

	SetClaudePermMCP("", "")
	if _, _, err := writePermMCPConfig("x"); err == nil {
		t.Error("未設定 mcp_token 時不應建立設定檔")
	}
}

func TestClaudeRoomBridgeAllowsQuotaOnlyForPM(t *testing.T) {
	SetClaudePermMCP("http://127.0.0.1:1/mcp/perm", "test-perm")
	t.Cleanup(func() { SetClaudePermMCP("", "") })
	opts := handlerOptions{roomBridge: func(sessionID string) (string, string) {
		if sessionID == "pm" {
			return "http://127.0.0.1:1/mcp/room", "test-room"
		}
		return "", ""
	}}
	for _, sessionID := range []string{"pm", "worker"} {
		args := map[string]string{agent.ArgAllowedTools: "Read"}
		_, clean, err := opts.writeClaudeMCPConfig(sessionID, args)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(clean)
		tools := strings.Split(args[agent.ArgAllowedTools], ",")
		if tools[0] != "Read" || slices.Contains(tools, "mcp__miniapp_room__get_quota") != (sessionID == "pm") {
			t.Fatalf("%s tools = %v", sessionID, tools)
		}
	}
}

// askUser 是唯一會登記 pendingPerms、改 session 狀態、廣播授權面板的路徑。
// 自動允許不呼叫它，就等於沒有待決詢問。
func TestClaudeAsker_applicability(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		pm       string
		on       bool
		question bool
	}{
		{"on-bypass-tool", "bypassPermissions", true, false},
		{"off-bypass-tool", "bypassPermissions", false, false},
		{"on-default", "default", true, false},
		{"on-acceptEdits", "acceptEdits", true, false},
		{"on-bypass-question", "bypassPermissions", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var asked atomic.Bool
			var questioned atomic.Bool
			sid := "apply-" + tc.name
			_, unreg := registerClaudeAsker(sid, context.Background(), func(ctx context.Context, tools any) bool {
				asked.Store(true)
				return false
			}, func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
				questioned.Store(true)
				return json.RawMessage(`{"answers":{"q":"yes"}}`), nil
			}, func() bool {
				return claudeAutoAllow(tc.pm, func() (bool, error) { return tc.on, nil })
			}, nil)
			defer unreg()

			if tc.question {
				out, err := AskClaudeQuestion(context.Background(), sid, json.RawMessage(`{"questions":[]}`), "q1")
				if err != nil || !questioned.Load() || asked.Load() {
					t.Fatalf("AskUserQuestion 仍應詢問: out=%s err=%v asked=%v questioned=%v", out, err, asked.Load(), questioned.Load())
				}
				return
			}
			ok, _ := AskClaudePermission(context.Background(), sid, "Bash", json.RawMessage(`{"command":"curl --version"}`), "u1")
			wantAllow := tc.pm == "bypassPermissions" && tc.on
			if ok != wantAllow || asked.Load() == wantAllow {
				t.Fatalf("allow=%v asked=%v，wantAllow=%v", ok, asked.Load(), wantAllow)
			}
		})
	}

	t.Run("deny-still-blocked", func(t *testing.T) {
		t.Parallel()
		// deny 規則在 CLI 內擋下，不進授權通道；開關開也不把這筆改成允許。
		pending, blocked := splitDenials([]agent.PermissionDenial{{
			ToolName: "Bash", ToolUseID: "deny-1",
			ToolInput:     json.RawMessage(`{"command":"rm -rf /"}`),
			BlockedByRule: true,
		}})
		if len(pending) != 0 || len(blocked) != 1 || !blocked[0].BlockedByRule {
			t.Fatalf("deny 應仍被擋: pending=%+v blocked=%+v", pending, blocked)
		}
	})
}

func TestClaudeAsker_readFailureAsks(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	var asked atomic.Bool
	_, unreg := registerClaudeAsker("s-readerr", context.Background(), func(ctx context.Context, tools any) bool {
		asked.Store(true)
		return false
	}, nil, func() bool {
		return claudeAutoAllow("bypassPermissions", func() (bool, error) {
			return true, errors.New("db down")
		})
	}, nil)
	defer unreg()

	ok, _ := AskClaudePermission(context.Background(), "s-readerr", "Bash", json.RawMessage(`{"command":"curl --version"}`), "u-read")
	if ok || !asked.Load() {
		t.Fatalf("讀設定失敗應走詢問: allow=%v asked=%v", ok, asked.Load())
	}
	if !strings.Contains(buf.String(), "claudeAutoAllowAsk") {
		t.Fatalf("讀設定失敗應寫 log: %s", buf.String())
	}
}

func TestClaudeAsker_switchOnMidRunLeavesFirstPanel(t *testing.T) {
	t.Parallel()
	var on atomic.Bool
	started := make(chan struct{})
	release := make(chan bool)
	defer close(release)
	var asks atomic.Int32
	_, unreg := registerClaudeAsker("s-mid", context.Background(), func(ctx context.Context, tools any) bool {
		asks.Add(1)
		select {
		case <-started:
		default:
			close(started)
		}
		return <-release
	}, nil, func() bool {
		return claudeAutoAllow("bypassPermissions", func() (bool, error) { return on.Load(), nil })
	}, nil)
	defer unreg()

	done1 := make(chan bool, 1)
	go func() {
		ok, _ := AskClaudePermission(context.Background(), "s-mid", "Bash", json.RawMessage(`{"command":"first"}`), "u-first")
		done1 <- ok
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("第一個詢問沒有進入面板")
	}
	on.Store(true)

	ctx2, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ok2, _ := AskClaudePermission(ctx2, "s-mid", "Bash", json.RawMessage(`{"command":"second"}`), "u-second")
	if !ok2 {
		t.Fatal("第二個詢問應自動允許")
	}
	select {
	case <-done1:
		t.Fatal("第一個面板不應被自動答覆")
	case <-time.After(50 * time.Millisecond):
	}
	if asks.Load() != 1 {
		t.Fatalf("askUser 次數=%d，第一個面板應保持等待", asks.Load())
	}
	release <- true
	select {
	case ok := <-done1:
		if !ok {
			t.Fatal("第一個仍應等使用者允許")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("第一個詢問沒有在使用者回答後結束")
	}
}

func TestClaudeAskAudit_noticeAndTruncation(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	var lines []string
	audit := newClaudeAskAudit("sess-audit")
	audit.bindNotice(func(line string) { lines = append(lines, line) })
	_, unreg := registerClaudeAsker("s-audit", context.Background(), func(ctx context.Context, tools any) bool {
		t.Error("自動允許不應呼叫 askUser")
		return false
	}, nil, func() bool { return true }, audit.record)
	defer unreg()

	ok, _ := AskClaudePermission(context.Background(), "s-audit", "Bash", json.RawMessage(`{"command":"curl --version"}`), "u-curl")
	if !ok || len(lines) != 1 || !strings.Contains(lines[0], "Bash") || !strings.Contains(lines[0], "curl --version") {
		t.Fatalf("稽核行應含 Bash 與命令: allow=%v lines=%q", ok, lines)
	}
	if lines[0] != "\n\n已自動允許 Bash：curl --version\n\n" || !strings.HasSuffix(lines[0], "\n") {
		t.Fatalf("稽核行前後應各空一行，行尾要有換行: %q", lines[0])
	}
	logText := buf.String()
	if !strings.Contains(logText, "sess-audit") || !strings.Contains(logText, "Bash") || !strings.Contains(logText, "curl --version") {
		t.Fatalf("log 應含 session、工具與命令: %s", logText)
	}

	long := strings.Repeat("a", 500)
	body, err := json.Marshal(map[string]string{"command": long})
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := AskClaudePermission(context.Background(), "s-audit", "Bash", body, "u-long"); !ok {
		t.Fatal("長命令仍應允許")
	}
	if len(lines) != 2 {
		t.Fatalf("lines=%q", lines)
	}
	summary := strings.TrimSuffix(strings.TrimPrefix(lines[1], "\n\n已自動允許 Bash："), "\n\n")
	if summary == lines[1] || !strings.HasSuffix(summary, "…") || strings.Count(summary, "a") != 200 {
		t.Fatalf("500 字元應截成 200 字元加標記: %q", summary)
	}
	if strings.Contains(buf.String(), strings.Repeat("a", 201)) || !strings.Contains(buf.String(), strings.Repeat("a", 200)) {
		t.Fatal("log 摘要應與稽核行同一段截斷")
	}

	wide := strings.Repeat("字", 250)
	body, err = json.Marshal(map[string]string{"command": wide})
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := AskClaudePermission(context.Background(), "s-audit", "Bash", body, "u-wide"); !ok {
		t.Fatal("中文長命令仍應允許")
	}
	summary = strings.TrimSuffix(strings.TrimPrefix(lines[2], "\n\n已自動允許 Bash："), "\n\n")
	if summary != strings.Repeat("字", 200)+"…" {
		t.Fatalf("應按字元截斷: %q", summary)
	}

	if ok, _ := AskClaudePermission(context.Background(), "s-audit", "Read", json.RawMessage(`{"file_path": "a.go"}`), "u-read"); !ok {
		t.Fatal("非 Bash 仍應允許")
	}
	if !strings.Contains(lines[3], "Read") || !strings.Contains(lines[3], `{"file_path":"a.go"}`) {
		t.Fatalf("非 Bash 應使用壓縮 input JSON: %q", lines[3])
	}
}

func TestClaudeAskAudit_noticeUnavailableStillAllows(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	audit := newClaudeAskAudit("sess-nolog-notice")
	_, unreg := registerClaudeAsker("s-nonotice", context.Background(), func(ctx context.Context, tools any) bool {
		t.Error("不應詢問")
		return false
	}, nil, func() bool { return true }, audit.record)
	defer unreg()

	ok, _ := AskClaudePermission(context.Background(), "s-nonotice", "Bash", json.RawMessage(`{"command":"curl --version"}`), "u-bare")
	if !ok {
		t.Fatal("回覆附加尚未就緒時仍應允許")
	}
	text := buf.String()
	if !strings.Contains(text, "sess-nolog-notice") || !strings.Contains(text, "Bash") || !strings.Contains(text, "curl --version") {
		t.Fatalf("仍應寫 log: %s", text)
	}
}
