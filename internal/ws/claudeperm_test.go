package ws

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"sync"
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
	}, nil)
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
	}, nil)
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
	}, nil)
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
