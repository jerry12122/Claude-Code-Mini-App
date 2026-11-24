package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

// compactScript 是 fake Runner，不是子進程 CLI。原生 stream-json 整鏈另見 TestCompactFakeCLI。
type compactScript struct {
	mu       sync.Mutex
	mode     string
	opts     []agent.RunOptions
	started  chan struct{}
	release  chan struct{}
	started2 chan struct{}
	release2 chan struct{}
	exitErr  error
}

func (s *compactScript) Name() string { return "claude" }

func (s *compactScript) Run(ctx context.Context, opts agent.RunOptions, cb agent.EventCallback) error {
	s.mu.Lock()
	s.opts = append(s.opts, opts)
	mode := s.mode
	n := len(s.opts)
	s.mu.Unlock()
	if opts.Prompt != "/compact" {
		if mode == "hold" && s.release2 != nil && n == 2 {
			close(s.started2)
			<-s.release2
		}
		cb(agent.Event{Type: agent.EventDelta, Text: "ok:" + opts.Prompt})
		cb(agent.Event{Type: agent.EventDone})
		return nil
	}
	if mode == "hold" || mode == "late" {
		if n == 1 {
			close(s.started)
			if mode == "late" {
				<-s.release
				if ctx.Err() != nil {
					return ctx.Err()
				}
			} else {
				select {
				case <-s.release:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		} else if mode == "late" {
			close(s.started2)
			<-s.release2
		}
		mode = "ok"
	}
	switch mode {
	case "ok":
		cb(agent.Event{Type: agent.EventCompactBoundary, SessionID: opts.SessionID})
		cb(agent.Event{Type: agent.EventDone, SessionID: opts.SessionID, CompactNative: true})
		return nil
	case "text":
		cb(agent.Event{Type: agent.EventDelta, Text: "compacted summary secret"})
		cb(agent.Event{Type: agent.EventDone, SessionID: opts.SessionID, ResultText: "summary secret"})
		return nil
	case "none":
		cb(agent.Event{Type: agent.EventDone, SessionID: opts.SessionID})
		return nil
	case "lie":
		cb(agent.Event{Type: agent.EventDone, SessionID: opts.SessionID, CompactNative: true})
		return nil
	case "wrong":
		cb(agent.Event{Type: agent.EventCompactBoundary, SessionID: "other-session"})
		cb(agent.Event{Type: agent.EventSessionInit, SessionID: "other-session"})
		cb(agent.Event{Type: agent.EventDone, SessionID: "other-session", CompactNative: true})
		return nil
	case "exit":
		cb(agent.Event{Type: agent.EventCompactBoundary, SessionID: opts.SessionID})
		cb(agent.Event{Type: agent.EventDone, SessionID: opts.SessionID, CompactNative: true})
		return errors.New("exit status 1")
	case "result":
		cb(agent.Event{Type: agent.EventCompactBoundary, SessionID: opts.SessionID})
		cb(agent.Event{Type: agent.EventError, Err: errors.New("claude: boom")})
		cb(agent.Event{Type: agent.EventDone, SessionID: opts.SessionID})
		return nil
	default:
		return errors.New("unknown compact fixture")
	}
}

func (s *compactScript) prompts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.opts))
	for i, o := range s.opts {
		out[i] = o.Prompt
	}
	return out
}

func useCompactScript(t *testing.T, script *compactScript) {
	t.Helper()
	agent.Register(agent.TypeClaude, func() agent.Runner { return script })
	t.Cleanup(func() {
		agent.Register(agent.TypeClaude, func() agent.Runner { return fakeClaude{} })
	})
}

func seedCompactSession(t *testing.T, database *db.DB, agentType, inputMode string, withProvider bool) *db.Session {
	t.Helper()
	s, err := database.CreateSession("compact", "", t.TempDir(), "acceptEdits", agentType, nil, inputMode)
	if err != nil {
		t.Fatal(err)
	}
	if withProvider {
		if err := database.UpdateAgentSessionID(s.ID, "provider-session"); err != nil {
			t.Fatal(err)
		}
	}
	if err := database.UpdateModel(s.ID, "opus-test"); err != nil {
		t.Fatal(err)
	}
	if err := database.AddMessage(s.ID, "user", "keep-me", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := database.EnqueueMessage(s.ID, "queued-note", ""); err != nil {
		t.Fatal(err)
	}
	got, err := database.GetSession(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func compactValue(value string) func(map[string]any) bool {
	return func(m map[string]any) bool { return m["type"] == "compact" && m["value"] == value }
}

func TestCompactClaudeChain(t *testing.T) {
	database, wsURL := startTestServer(t)
	script := &compactScript{mode: "ok"}
	useCompactScript(t, script)
	s := seedCompactSession(t, database, agent.TypeClaude, "agent", true)
	if err := database.SetQueuePaused(s.ID, true); err != nil {
		t.Fatal(err)
	}
	before, _ := database.GetSession(s.ID)
	client := dialClient(t, wsURL(s.ID))
	client.await(t, "sync", isType("sync"))
	client.send(t, map[string]any{"type": "compact"})
	done := client.await(t, "compact done", compactValue("done"))
	if done["content"] != "已壓縮上下文" {
		t.Fatalf("成功文案=%v", done["content"])
	}
	waitFor(t, "compact idle", func() bool {
		got, err := database.GetSession(s.ID)
		return err == nil && got.Status == db.SessionStatusIdle && !taskIsActive(s.ID)
	})
	assertCompactPreserved(t, database, before)
	if paused, err := database.QueuePaused(s.ID); err != nil || !paused {
		t.Fatalf("既有暫停被解除: paused=%v err=%v", paused, err)
	}
	blob := clientBlob(client)
	if strings.Contains(blob, "summary secret") {
		t.Fatalf("不該送出摘要: %s", blob)
	}
	if prompts := script.prompts(); len(prompts) != 1 || prompts[0] != "/compact" {
		t.Fatalf("prompts=%v", prompts)
	}
	script.mu.Lock()
	gotOpts := script.opts[0]
	script.mu.Unlock()
	if gotOpts.SessionID != "provider-session" || gotOpts.SystemInstruction != "" || gotOpts.ExtraArgs[agent.ArgPermissionMode] != "acceptEdits" || gotOpts.ExtraArgs[agent.ArgModel] != "opus-test" {
		t.Fatalf("runner opts=%+v extra=%v", gotOpts, gotOpts.ExtraArgs)
	}
	msgs, _ := database.ListMessages(s.ID)
	if len(msgs) != 1 || msgs[0].Content != "keep-me" {
		t.Fatalf("history=%+v", msgs)
	}
}

func TestCompactRejectsWithoutNativeSuccess(t *testing.T) {
	database, wsURL := startTestServer(t)
	script := &compactScript{}
	useCompactScript(t, script)
	cases := []struct {
		mode    string
		content string
	}{
		{mode: "text", content: "沒有收到壓縮確認"},
		{mode: "none", content: "沒有收到壓縮確認"},
		{mode: "lie", content: "沒有收到壓縮確認"},
		{mode: "wrong", content: "壓縮後的會話不一致"},
		{mode: "exit", content: "壓縮行程失敗"},
		{mode: "result", content: "壓縮結果失敗"},
	}
	for _, tc := range cases {
		t.Run(tc.mode, func(t *testing.T) {
			script.mu.Lock()
			script.mode = tc.mode
			script.mu.Unlock()
			s := seedCompactSession(t, database, agent.TypeClaude, "agent", true)
			before, _ := database.GetSession(s.ID)
			client := dialClient(t, wsURL(s.ID))
			client.await(t, "sync", isType("sync"))
			client.send(t, map[string]any{"type": "compact"})
			failed := client.await(t, "compact failed", compactValue("failed"))
			if failed["content"] != tc.content {
				t.Fatalf("content=%v", failed["content"])
			}
			if strings.Contains(clientBlob(client), "summary secret") || strings.Contains(clientBlob(client), "other-session") {
				t.Fatalf("失敗訊息洩漏原生內容: %s", clientBlob(client))
			}
			waitFor(t, "idle", func() bool {
				got, _ := database.GetSession(s.ID)
				return got.Status == db.SessionStatusIdle && !taskIsActive(s.ID)
			})
			assertCompactPreserved(t, database, before)
		})
	}
}

func TestCompactGuards(t *testing.T) {
	database, wsURL := startTestServer(t)
	script := &compactScript{mode: "ok"}
	useCompactScript(t, script)

	t.Run("busy", func(t *testing.T) {
		s := seedCompactSession(t, database, agent.TypeClaude, "agent", true)
		taskStart(s.ID, func() {}, 42)
		t.Cleanup(func() { taskEnd(s.ID, 42) })
		client := dialClient(t, wsURL(s.ID))
		client.await(t, "sync", isType("sync"))
		client.send(t, map[string]any{"type": "compact"})
		failed := client.await(t, "busy", compactValue("failed"))
		if failed["content"] != "請等目前工作結束再壓縮" {
			t.Fatalf("content=%v", failed["content"])
		}
		if prompts := script.prompts(); len(prompts) != 0 {
			t.Fatalf("忙碌時不該啟動 runner: %v", prompts)
		}
	})

	t.Run("missing provider", func(t *testing.T) {
		s := seedCompactSession(t, database, agent.TypeClaude, "agent", false)
		expectCompactReject(t, wsURL, s.ID, "沒有可續接的會話")
	})
	t.Run("unsupported", func(t *testing.T) {
		s := seedCompactSession(t, database, agent.TypeCursor, "agent", true)
		expectCompactReject(t, wsURL, s.ID, "此 runner 尚未驗證，不能壓縮")
	})
	t.Run("shell", func(t *testing.T) {
		s := seedCompactSession(t, database, agent.TypeClaude, "shell", true)
		expectCompactReject(t, wsURL, s.ID, "Shell 模式不能壓縮")
	})
	t.Run("room", func(t *testing.T) {
		s := seedCompactSession(t, database, agent.TypeClaude, "agent", true)
		if _, err := database.CreateRoom("compact-room", []db.RoomMember{{SessionID: s.ID, Role: db.RoomRolePM}}); err != nil {
			t.Fatal(err)
		}
		expectCompactReject(t, wsURL, s.ID, "聊天室不能壓縮")
	})
}

func TestCompactMutualExclusion(t *testing.T) {
	database, wsURL := startTestServer(t)
	script := &compactScript{mode: "hold", started: make(chan struct{}), release: make(chan struct{})}
	useCompactScript(t, script)
	s := seedCompactSession(t, database, agent.TypeClaude, "agent", true)
	client := dialClient(t, wsURL(s.ID))
	client.await(t, "sync", isType("sync"))
	client.send(t, map[string]any{"type": "compact"})
	select {
	case <-script.started:
	case <-time.After(5 * time.Second):
		t.Fatal("第一筆 compact 沒有啟動")
	}
	client.send(t, map[string]any{"type": "input", "data": "同時送出"})
	client.send(t, map[string]any{"type": "compact"})
	failed := client.await(t, "second compact", compactValue("failed"))
	if failed["content"] != "請等目前工作結束再壓縮" {
		t.Fatalf("content=%v", failed["content"])
	}
	if queueLen(database, s.ID) < 2 || len(script.prompts()) != 1 || script.prompts()[0] != "/compact" {
		t.Fatalf("運行中不該重疊：queue=%d prompts=%v", queueLen(database, s.ID), script.prompts())
	}
	msgs, _ := database.ListMessages(s.ID)
	for _, m := range msgs {
		if m.Content == "同時送出" || m.Content == "queued-note" || m.Content == "/compact" {
			t.Fatalf("compact 期間不該啟動普通輸入: %+v", msgs)
		}
	}
	close(script.release)
	client.await(t, "first done", compactValue("done"))
	waitFor(t, "queued work continues in order", func() bool {
		p := script.prompts()
		return len(p) >= 3 && !taskIsActive(s.ID)
	})
	p := script.prompts()
	if p[0] != "/compact" || !strings.Contains(p[1], "queued-note") || !strings.Contains(p[2], "同時送出") {
		t.Fatalf("續跑順序=%v", p)
	}
	got, _ := database.GetSession(s.ID)
	if got.AgentSessionID != "provider-session" || got.Model != "opus-test" || got.PermissionMode != "acceptEdits" {
		t.Fatalf("續跑後 session 被改動: %+v", got)
	}
	msgs, _ = database.ListMessages(s.ID)
	kept := false
	for _, m := range msgs {
		if m.Content == "keep-me" {
			kept = true
		}
		if m.Content == "/compact" || strings.Contains(m.Content, "summary secret") {
			t.Fatalf("compact 寫進歷史: %+v", msgs)
		}
	}
	if !kept {
		t.Fatalf("history=%+v", msgs)
	}
}

func TestCompactGuestAndShareRejected(t *testing.T) {
	database, dial := startShareServer(t)
	s := seedCompactSession(t, database, agent.TypeClaude, "agent", true)
	_, editor := newShare(t, database, s.ID, db.ShareRoleEditor, db.ShareModeLive, "編輯", time.Hour)
	ed := dialClient(t, dial(s.ID, editor.Guest.Token))
	ed.await(t, "editor sync", isType("sync"))
	ed.send(t, map[string]any{"type": "compact"})
	failed := ed.await(t, "editor compact", compactValue("failed"))
	if failed["content"] != "分享連線不能壓縮" {
		t.Fatalf("content=%v", failed["content"])
	}
	_, viewer := newShare(t, database, s.ID, db.ShareRoleViewer, db.ShareModeLive, "觀眾", time.Hour)
	vw := dialClient(t, dial(s.ID, viewer.Guest.Token))
	vw.await(t, "viewer sync", isType("sync"))
	vw.send(t, map[string]any{"type": "compact"})
	errMsg := vw.await(t, "viewer error", func(m map[string]any) bool {
		return m["type"] == "error" && m["content"] == "唯讀分享，無法執行此操作"
	})
	if errMsg == nil {
		t.Fatal("viewer 應被拒絕")
	}
	got, _ := database.GetSession(s.ID)
	if got.AgentSessionID != "provider-session" || got.Model != "opus-test" || got.PermissionMode != "acceptEdits" {
		t.Fatalf("分享拒絕後 session 被改動: %+v", got)
	}
}

func expectCompactReject(t *testing.T, wsURL func(string) string, sessionID, content string) {
	t.Helper()
	client := dialClient(t, wsURL(sessionID))
	client.await(t, "sync", isType("sync"))
	client.send(t, map[string]any{"type": "compact"})
	failed := client.await(t, content, compactValue("failed"))
	if failed["content"] != content {
		t.Fatalf("content=%v", failed["content"])
	}
}

func assertCompactPreserved(t *testing.T, database *db.DB, before *db.Session) {
	t.Helper()
	got, err := database.GetSession(before.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.AgentSessionID != before.AgentSessionID || got.Model != before.Model || got.PermissionMode != before.PermissionMode || got.InputMode != before.InputMode {
		t.Fatalf("session identity changed: before=%+v after=%+v", before, got)
	}
	if queueLen(database, before.ID) != 1 {
		t.Fatalf("queue=%d", queueLen(database, before.ID))
	}
	msgs, _ := database.ListMessages(before.ID)
	if len(msgs) != 1 || msgs[0].Content != "keep-me" {
		t.Fatalf("history=%+v", msgs)
	}
}

func TestCompactLateFinishDoesNotClearSuccessor(t *testing.T) {
	database, wsURL := startTestServer(t)
	script := &compactScript{
		mode: "late", started: make(chan struct{}), release: make(chan struct{}),
		started2: make(chan struct{}), release2: make(chan struct{}),
	}
	useCompactScript(t, script)
	closeOnce := func(ch chan struct{}) {
		defer func() { _ = recover() }()
		close(ch)
	}
	t.Cleanup(func() {
		closeOnce(script.release)
		closeOnce(script.release2)
	})
	s := seedCompactSession(t, database, agent.TypeClaude, "agent", true)
	client := dialClient(t, wsURL(s.ID))
	client.await(t, "sync", isType("sync"))
	client.send(t, map[string]any{"type": "compact"})
	select {
	case <-script.started:
	case <-time.After(time.Second):
		t.Fatal("first compact not started")
	}
	taskManager.mu.Lock()
	oldDone := taskManager.tasks[s.ID].done
	oldID := taskManager.tasks[s.ID].msgID
	taskManager.mu.Unlock()
	taskCancel(s.ID)
	if err := database.UpdateSessionStatus(s.ID, db.SessionStatusIdle); err != nil {
		t.Fatal(err)
	}
	client.send(t, map[string]any{"type": "compact"})
	select {
	case <-script.started2:
	case <-time.After(time.Second):
		t.Fatal("successor compact not started")
	}
	taskManager.mu.Lock()
	successor := taskManager.tasks[s.ID]
	successorDone := successor.done
	successorID := successor.msgID
	taskManager.mu.Unlock()
	if successorID == oldID {
		t.Fatalf("successor reused cancelled turn id %d", oldID)
	}
	close(script.release)
	select {
	case <-oldDone:
	case <-time.After(time.Second):
		t.Fatal("cancelled turn did not close its own done")
	}
	taskManager.mu.Lock()
	still := taskManager.tasks[s.ID]
	taskManager.mu.Unlock()
	if still == nil || still.msgID != successorID {
		t.Fatalf("late cleanup cleared successor: %+v", still)
	}
	got, err := database.GetSession(s.ID)
	if err != nil || got.Status != db.SessionStatusRunning {
		t.Fatalf("old turn overwrote successor status: %+v err=%v", got, err)
	}
	select {
	case <-successorDone:
		t.Fatal("old turn closed the successor done channel")
	case <-time.After(150 * time.Millisecond):
	}
	close(script.release2)
	waitFor(t, "successor finished", func() bool { return !taskIsActive(s.ID) })
}

func TestCompactInterruptClosesDoneAndPauses(t *testing.T) {
	database, wsURL := startTestServer(t)
	script := &compactScript{mode: "hold", started: make(chan struct{}), release: make(chan struct{})}
	useCompactScript(t, script)
	s := seedCompactSession(t, database, agent.TypeClaude, "agent", true)
	client := dialClient(t, wsURL(s.ID))
	client.await(t, "sync", isType("sync"))
	client.send(t, map[string]any{"type": "compact"})
	select {
	case <-script.started:
	case <-time.After(time.Second):
		t.Fatal("compact not started")
	}
	taskManager.mu.Lock()
	done := taskManager.tasks[s.ID].done
	taskManager.mu.Unlock()
	client.send(t, map[string]any{"type": "interrupt"})
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("interrupt did not close compact done")
	}
	waitFor(t, "idle after interrupt", func() bool {
		got, err := database.GetSession(s.ID)
		return err == nil && got.Status == db.SessionStatusIdle && !taskIsActive(s.ID)
	})
	paused, err := database.QueuePaused(s.ID)
	if err != nil || !paused || queueLen(database, s.ID) != 1 {
		t.Fatalf("取消應暫停並保留佇列: paused=%v queue=%d err=%v", paused, queueLen(database, s.ID), err)
	}
	msgs, _ := database.ListMessages(s.ID)
	if len(msgs) != 1 || msgs[0].Content != "keep-me" {
		t.Fatalf("history=%+v", msgs)
	}
}

func TestPMCompactRetainsAdmission(t *testing.T) {
	database, wsURL := startTestServer(t)
	script := &compactScript{mode: "hold", started: make(chan struct{}), release: make(chan struct{})}
	useCompactScript(t, script)
	s := seedCompactSession(t, database, agent.TypeClaude, "agent", true)
	c := dialClient(t, wsURL(s.ID))
	c.await(t, "sync", isType("sync"))
	c.barrier(t)
	gate.mu.Lock()
	before := gate.inflight
	gate.mu.Unlock()
	c.send(t, map[string]any{"type": "compact"})
	select {
	case <-script.started:
	case <-time.After(time.Second):
		t.Fatal("compact not started")
	}
	t.Cleanup(func() {
		close(script.release)
		waitFor(t, "compact cleanup", func() bool { return !taskIsActive(s.ID) })
	})
	c.barrier(t)
	gate.mu.Lock()
	during := gate.inflight
	gate.mu.Unlock()
	if during <= before {
		t.Fatalf("background compact lost admission lease: before=%d during=%d", before, during)
	}
}

func TestPMCompactClosesTaskDone(t *testing.T) {
	database, wsURL := startTestServer(t)
	script := &compactScript{mode: "hold", started: make(chan struct{}), release: make(chan struct{})}
	useCompactScript(t, script)
	s := seedCompactSession(t, database, agent.TypeClaude, "agent", true)
	c := dialClient(t, wsURL(s.ID))
	c.await(t, "sync", isType("sync"))
	c.send(t, map[string]any{"type": "compact"})
	select {
	case <-script.started:
	case <-time.After(time.Second):
		t.Fatal("compact not started")
	}
	taskManager.mu.Lock()
	done := taskManager.tasks[s.ID].done
	taskManager.mu.Unlock()
	close(script.release)
	c.await(t, "compact done", compactValue("done"))
	waitFor(t, "compact task removed", func() bool { return !taskIsActive(s.ID) })
	select {
	case <-done:
	case <-time.After(150 * time.Millisecond):
		t.Fatal("compact finished but task done channel never closed; RunNow waiter cannot resume")
	}
}

func TestPMCompactFinishCannotPauseNewlyStartedSuccessor(t *testing.T) {
	database, _ := startTestServer(t)
	s := seedCompactSession(t, database, agent.TypeClaude, "agent", true)
	if err := database.SetQueuePaused(s.ID, false); err != nil {
		t.Fatal(err)
	}
	h := &wsConn{database: database, sessionID: s.ID}
	done := taskStartWithDone(s.ID, func() {}, -101)
	defer taskCancel(s.ID)
	reached, release, finished, newStarted := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var armed atomic.Bool
	armed.Store(true)
	database.OnSessionChange = func() {
		if armed.CompareAndSwap(true, false) {
			close(reached)
			<-release
		}
	}
	defer func() { database.OnSessionChange = nil }()
	go func() {
		defer close(finished)
		h.finishCompactTurn(done, -101, false)
	}()
	select {
	case <-reached:
	case <-time.After(time.Second):
		t.Fatal("old idle update not reached")
	}
	go func() {
		lock := dispatchLock(s.ID)
		lock.Lock()
		defer lock.Unlock()
		taskStart(s.ID, func() {}, 12345)
		if err := database.UpdateSessionStatus(s.ID, db.SessionStatusRunning); err != nil {
			t.Error(err)
		}
		if err := database.SetQueuePaused(s.ID, false); err != nil {
			t.Error(err)
		}
		close(newStarted)
	}()
	// 正確的 dispatchLock 會把新回合延到舊收尾做完。
	select {
	case <-newStarted:
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("old cleanup blocked")
	}
	select {
	case <-newStarted:
	case <-time.After(time.Second):
		t.Fatal("new start blocked")
	}
	paused, err := database.QueuePaused(s.ID)
	if err != nil || paused {
		t.Fatalf("old cleanup paused successor queue after successor registered: paused=%v err=%v", paused, err)
	}
}

func TestCompactRunNowHandoffKeepsSuccessor(t *testing.T) {
	database, wsURL := startTestServer(t)
	script := &compactScript{
		mode: "hold", started: make(chan struct{}), release: make(chan struct{}),
		started2: make(chan struct{}), release2: make(chan struct{}),
	}
	useCompactScript(t, script)
	closeOnce := func(ch chan struct{}) {
		defer func() { _ = recover() }()
		close(ch)
	}
	t.Cleanup(func() {
		closeOnce(script.release)
		closeOnce(script.release2)
	})
	s := seedCompactSession(t, database, agent.TypeClaude, "agent", true)
	if _, err := database.EnqueueMessage(s.ID, "stay-queued", ""); err != nil {
		t.Fatal(err)
	}
	if err := database.SetQueuePaused(s.ID, false); err != nil {
		t.Fatal(err)
	}
	client := dialClient(t, wsURL(s.ID))
	client.await(t, "sync", isType("sync"))
	client.send(t, map[string]any{"type": "compact"})
	select {
	case <-script.started:
	case <-time.After(time.Second):
		t.Fatal("compact not started")
	}
	dispatcher := liveDispatcher(t, s.ID)
	dispatcher.runNowWait = 20 * time.Millisecond
	queued, err := database.ListQueuedMessages(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	var runID int64
	for _, q := range queued {
		if q.Content == "queued-note" {
			runID = q.ID
		}
	}
	if runID == 0 {
		t.Fatal("找不到 queued-note")
	}
	client.send(t, map[string]any{"type": "queue_run_now", "id": runID})
	select {
	case <-script.started2:
	case <-time.After(time.Second):
		t.Fatal("RunNow 沒有在短等待內交接")
	}
	if client.count(func(m map[string]any) bool {
		return m["type"] == "error" && strings.Contains(fmt.Sprint(m["content"]), "仍在收尾")
	}) != 0 {
		t.Fatal("20ms 等待不該逾時，也不該放大成 15 秒")
	}
	paused, err := database.QueuePaused(s.ID)
	if err != nil || paused {
		t.Fatalf("舊收尾暫停了新回合的佇列: paused=%v err=%v", paused, err)
	}
	got, err := database.GetSession(s.ID)
	if err != nil || got.Status != db.SessionStatusRunning {
		t.Fatalf("status=%v err=%v", got, err)
	}
	taskManager.mu.Lock()
	cur := taskManager.tasks[s.ID]
	taskManager.mu.Unlock()
	if cur == nil || cur.msgID < 0 {
		t.Fatalf("後繼回合被舊收尾清掉: %+v", cur)
	}
	left, err := database.ListQueuedMessages(s.ID)
	if err != nil || len(left) != 1 || left[0].Content != "stay-queued" {
		t.Fatalf("佇列=%+v err=%v", left, err)
	}
	close(script.release2)
	waitFor(t, "handoff finished", func() bool { return !taskIsActive(s.ID) })
}

func liveDispatcher(t *testing.T, sessionID string) *sessionDispatcher {
	t.Helper()
	liveConns.mu.Lock()
	defer liveConns.mu.Unlock()
	for conn := range liveConns.m[sessionID] {
		if conn.dispatcher != nil {
			return conn.dispatcher
		}
	}
	t.Fatal("找不到連線 dispatcher")
	return nil
}

func clientBlob(c *wsClient) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, _ := json.Marshal(c.msgs)
	return string(b)
}
