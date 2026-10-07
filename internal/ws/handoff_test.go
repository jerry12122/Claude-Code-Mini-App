package ws

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fasthttp/websocket"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

// fakeHandoffRunner：永遠回 agent.ErrHandoff（模擬 kiroacp 重試後仍救不回來），不送任何事件。
type fakeHandoffRunner struct{}

func (fakeHandoffRunner) Name() string { return "fakeho" }
func (fakeHandoffRunner) Run(context.Context, agent.RunOptions, agent.EventCallback) error {
	return fmt.Errorf("%w: jsonrpc error -32603: Internal error", agent.ErrHandoff)
}

func init() { agent.Register("fakeho", func() agent.Runner { return fakeHandoffRunner{} }) }

type startCall struct{ id, text string }

// startCalls 包著 mutex 的呼叫記錄，讀寫都要過鎖；避免測試斷言端未同步讀取 slice（-race 會抓到）。
type startCalls struct {
	mu    sync.Mutex
	calls []startCall
}

func (s *startCalls) len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func (s *startCalls) at(i int) startCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[i]
}

func captureStarter(t *testing.T, err error) *startCalls {
	t.Helper()
	calls := &startCalls{}
	SetHandoffStarter(func(id, text string) error {
		calls.mu.Lock()
		defer calls.mu.Unlock()
		calls.calls = append(calls.calls, startCall{id, text})
		return err
	})
	t.Cleanup(func() { SetHandoffStarter(nil) })
	return calls
}

// 端到端：runner 回 ErrHandoff → 原會話保留並留下說明、新會話以同設定建立，且收到含舊會話 id 與原需求的交接訊息。
func TestHandoff_EndToEnd(t *testing.T) {
	database, wsURL := startTestServer(t)
	calls := captureStarter(t, nil)
	old, err := database.CreateSession("舊", "", t.TempDir(), "bypassPermissions", "fakeho", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	_ = database.UpdateModel(old.ID, "claude-sonnet-5.5")
	_ = database.UpdateEffort(old.ID, "high")

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(old.ID), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(map[string]string{"type": "input", "data": "fix it"}); err != nil {
		t.Fatal(err)
	}

	waitFor(t, "handoff session created and started", func() bool { return calls.len() == 1 && !taskIsActive(old.ID) })
	all, _ := database.ListSessions()
	if len(all) != 2 {
		t.Fatalf("sessions=%d want 2", len(all))
	}
	call := calls.at(0)
	ns, err := database.GetSession(call.id)
	if err != nil || ns.ID == old.ID {
		t.Fatalf("new session not created: %v", err)
	}
	if ns.WorkDir != old.WorkDir || ns.AgentType != "fakeho" || ns.PermissionMode != "bypassPermissions" || ns.Model != "claude-sonnet-5.5" || ns.Effort != "high" {
		t.Errorf("新會話設定未沿用舊會話: %+v", ns)
	}
	for _, want := range []string{handoffMarker, old.ID, "get_messages", "fix it"} {
		if !strings.Contains(call.text, want) {
			t.Errorf("交接訊息缺少 %q:\n%s", want, call.text)
		}
	}
	got, _ := database.GetSession(old.ID)
	if got.Status != db.SessionStatusIdle {
		t.Errorf("舊會話 status=%s want idle", got.Status)
	}
	if r := lastReply(database, old.ID); !strings.Contains(r, "自動建立新會話") {
		t.Errorf("舊會話應留下交接說明，got %q", r)
	}
}

// 自動交接本身失敗（starter 回錯）時：原會話收到結構化 handoff_available，欄位直接取自已知的 Go 值
// （session id／name／work_dir／agent_type／permission_mode／model／cli_extra_args／reason），
// 不是從錯誤訊息或任何中文顯示字串解析出來的。
func TestHandoff_ManualFallback_WhenAutoHandoffFails(t *testing.T) {
	database, wsURL := startTestServer(t)
	captureStarter(t, errors.New("starter 掛了"))
	old, err := database.CreateSession("舊2", "", t.TempDir(), "bypassPermissions", "fakeho", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	_ = database.UpdateModel(old.ID, "claude-sonnet-5.5")

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(old.ID), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(map[string]string{"type": "input", "data": "fix it"}); err != nil {
		t.Fatal(err)
	}

	var got struct {
		Type    string             `json:"type"`
		Handoff *manualHandoffInfo `json:"handoff"`
	}
	// 一次設好有界 deadline（5s），逐則讀到 handoff_available 為止；ReadJSON 失敗（含逾時）直接
	// Fatal，不在同一個 conn 上重複設 timeout 再讀——逾時後底層連線就不保證可再讀，重試只會疊加錯誤。
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	for {
		if err := conn.ReadJSON(&got); err != nil {
			t.Fatalf("ReadJSON: %v（最後一則 type=%q）", err, got.Type)
		}
		if got.Type == "handoff_available" {
			break
		}
	}
	if got.Handoff == nil {
		t.Fatalf("handoff_available 訊息沒有 handoff 欄位: %+v", got)
	}
	h := got.Handoff
	if h.OldSessionID != old.ID || h.OldName != old.Name || h.OldWorkDir != old.WorkDir ||
		h.AgentType != "fakeho" || h.PermissionMode != "bypassPermissions" || h.Model != "claude-sonnet-5.5" ||
		h.LastPrompt != "fix it" || !h.MCPEnabled {
		t.Errorf("handoff 結構化欄位不符: %+v", h)
	}
	// 只保留一個與自動交接相同的連鎖防護；原會話仍只有自己一筆（自動交接沒建出半成品會話）。
	all, _ := database.ListSessions()
	if len(all) != 1 {
		t.Errorf("自動交接失敗不應留下半成品會話，sessions=%d", len(all))
	}
}

func TestStartHandoff_Guards(t *testing.T) {
	database, _ := startTestServer(t)
	old, _ := database.CreateSession("舊", "", t.TempDir(), "default", "fakeho", nil, "agent")
	cause := errors.New("boom")
	count := func() int { l, _ := database.ListSessions(); return len(l) }

	SetHandoffStarter(nil)
	if _, err := startHandoff(database, old, "x", cause); err == nil || count() != 1 {
		t.Fatalf("未注入 starter 應失敗且不建會話: err=%v sessions=%d", err, count())
	}

	captureStarter(t, nil)
	if _, err := startHandoff(database, old, handoffMarker+" 已經是交接會話", cause); err == nil || count() != 1 {
		t.Fatalf("交接會話自己失敗不可連鎖: err=%v sessions=%d", err, count())
	}

	captureStarter(t, errors.New("ws down"))
	if _, err := startHandoff(database, old, "x", cause); err == nil || count() != 1 {
		t.Fatalf("starter 失敗要清掉半成品會話: err=%v sessions=%d", err, count())
	}
}
