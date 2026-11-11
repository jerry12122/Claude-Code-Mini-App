package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fasthttp/websocket"
	fiberws "github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/shell"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/tg"
)

// admCode 是維護中拒絕訊息的 code；測試端固定寫字串，確保規格值不會被實作常數悄悄改掉。
const admCode = "update_in_progress"

// admHits 記錄 runner／shell 替身被呼叫的次數（key: "agent:<prompt>" 或 "shell:<command>"）。
// 次數為 0 即代表沒有啟動子程序。
var admHits = struct {
	sync.Mutex
	m map[string]int
}{m: map[string]int{}}

func admHit(key string) {
	admHits.Lock()
	admHits.m[key]++
	admHits.Unlock()
}

func admHitCount(key string) int {
	admHits.Lock()
	defer admHits.Unlock()
	return admHits.m[key]
}

// admResetHits 每次測試開始時清空計數，避免 -count=N 時前一輪的次數被累加進來。
func admResetHits() {
	admHits.Lock()
	admHits.m = map[string]int{}
	admHits.Unlock()
}

// admGates：prompt／command → 放行 channel。替身等到放行才結束，且刻意不看 ctx，
// 用來模擬「cancel 之後子程序仍在清理」。
var admGates sync.Map

// admHold 登記一道閘，回傳放行函式（可重複呼叫）；測試結束前一定會放行。
func admHold(t *testing.T, key string) func() {
	t.Helper()
	g := make(chan struct{})
	admGates.Store(key, g)
	var once sync.Once
	open := func() { once.Do(func() { close(g) }) }
	t.Cleanup(open)
	return open
}

// admWait 若 key 有登記閘門就等到放行。
func admWait(key string) {
	if g, ok := admGates.Load(key); ok {
		<-g.(chan struct{})
	}
}

// admRunner：agent 替身，記錄啟動次數，完成後回覆 "ok:<prompt>"。
type admRunner struct{}

func (admRunner) Name() string { return "fakeadm" }

func (admRunner) Run(_ context.Context, opts agent.RunOptions, cb agent.EventCallback) error {
	admHit("agent:" + opts.Prompt)
	admWait(opts.Prompt)
	cb(agent.Event{Type: agent.EventDelta, Text: "ok:" + opts.Prompt})
	cb(agent.Event{Type: agent.EventDone})
	return nil
}

func init() { agent.Register("fakeadm", func() agent.Runner { return admRunner{} }) }

// admInstallShellStub 以假 shell 取代子程序啟動點：記錄啟動次數，無閘門的指令立即成功結束。
func admInstallShellStub(t *testing.T) {
	t.Helper()
	prev := runShellCommand
	runShellCommand = func(_ context.Context, opts shell.RunOptions, cb func(shell.Event)) error {
		admHit("shell:" + opts.Command)
		admWait(opts.Command)
		cb(shell.Event{Type: shell.EventDone, ExitCode: 0})
		return nil
	}
	t.Cleanup(func() { runShellCommand = prev })
}

// startAdmServer 起一個只掛 WS 的 fiber（與 startTestServer 相同，但 shell 設定可指定）。
// 測試結束時會確認閘門已回到空閒，避免一個測試的殘留狀態汙染下一個測試。
func startAdmServer(t *testing.T, shellOpts ShellOpts) (*db.DB, func(string) string) {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/adm.db")
	if err != nil {
		t.Fatal(err)
	}
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Get("/sessions/:id/ws", fiberws.New(NewHandler(database, "", shellOpts, nil, tg.NotifyConfig{})))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = app.Listener(ln) }()
	t.Cleanup(func() {
		_ = app.Shutdown()
		database.Close()
	})
	t.Cleanup(func() {
		deadline := time.Now().Add(5 * time.Second)
		for {
			rel, err := TryBeginDesktopUpdate()
			if err == nil {
				rel()
				return
			}
			if time.Now().After(deadline) {
				t.Errorf("測試結束後閘門仍忙碌（有未清理的狀態）: %v", err)
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	})
	port := ln.Addr().(*net.TCPAddr).Port
	return database, func(id string) string { return fmt.Sprintf("ws://127.0.0.1:%d/sessions/%s/ws", port, id) }
}

// admMsg 只取測試需要的欄位。
type admMsg struct {
	Type      string `json:"type"`
	Code      string `json:"code"`
	RequestID string `json:"request_id"`
	Content   string `json:"content"`
}

type admConn struct {
	t    *testing.T
	conn *websocket.Conn
	msgs chan admMsg
}

func admDial(t *testing.T, url string) *admConn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := &admConn{t: t, conn: conn, msgs: make(chan admMsg, 1024)}
	go func() {
		defer close(c.msgs)
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var m admMsg
			if json.Unmarshal(data, &m) == nil {
				select {
				case c.msgs <- m:
				default:
				}
			}
		}
	}()
	t.Cleanup(func() { _ = conn.Close() })
	return c
}

func (c *admConn) mustSend(v map[string]any) {
	c.t.Helper()
	if err := c.conn.WriteJSON(v); err != nil {
		c.t.Fatal(err)
	}
}

// await 等到第一則符合條件的訊息；逾時或連線關閉就失敗。只能在測試 goroutine 呼叫。
func (c *admConn) await(what string, pred func(admMsg) bool) admMsg {
	c.t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case m, ok := <-c.msgs:
			if !ok {
				c.t.Fatalf("%s: ws 連線已關閉", what)
			}
			if pred(m) {
				return m
			}
		case <-deadline:
			c.t.Fatalf("%s: 等待逾時", what)
		}
	}
}

func admRejected(m admMsg) bool { return m.Code == admCode }

func admSession(t *testing.T, database *db.DB, agentType, mode string) string {
	t.Helper()
	s, err := database.CreateSession("adm", "", t.TempDir(), mode, agentType, nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	return s.ID
}

// admMaint 取得維護權，測試結束前一定釋放；回傳的函式可提早釋放（可重複呼叫）。
func admMaint(t *testing.T) func() {
	t.Helper()
	rel, err := TryBeginDesktopUpdate()
	if err != nil {
		t.Fatalf("空閒時應能取得維護權: %v", err)
	}
	var once sync.Once
	release := func() { once.Do(rel) }
	t.Cleanup(release)
	return release
}

// admWaitIdle 確認閘門能再次取得（即已回到空閒），取得後立即釋放。
func admWaitIdle(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		rel, err := TryBeginDesktopUpdate()
		if err == nil {
			rel()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("閘門未回到空閒: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// admAssertNoSideEffect 驗證被拒的路徑沒有任何副作用：不啟動子程序、status 沒改成執行中、訊息數不變。
func admAssertNoSideEffect(t *testing.T, database *db.DB, sid string, wantMsgs int, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if n := admHitCount(k); n != 0 {
			t.Errorf("%s 不應被啟動，實際 %d 次", k, n)
		}
	}
	s, err := database.GetSession(sid)
	if err != nil {
		t.Fatal(err)
	}
	if s.Status == db.SessionStatusRunning || s.Status == db.SessionStatusAwaitingShellConfirm || s.Status == db.SessionStatusAwaitingInput {
		t.Errorf("被拒後 status 不應變成忙碌狀態，實際 %s", s.Status)
	}
	msgs, _ := database.ListMessages(sid)
	if len(msgs) != wantMsgs {
		t.Errorf("訊息數應為 %d，實際 %d", wantMsgs, len(msgs))
	}
}

func TestDesktopUpdateAdmission(t *testing.T) {
	admResetHits()
	t.Run("idle admits maintenance and release restores it", func(t *testing.T) {
		startAdmServer(t, ShellOpts{})
		rel, err := TryBeginDesktopUpdate()
		if err != nil {
			t.Fatalf("空閒應可取得維護權: %v", err)
		}
		rel()
		rel2, err := TryBeginDesktopUpdate()
		if err != nil {
			t.Fatalf("釋放後應可再次取得: %v", err)
		}
		rel2()
	})

	// 每一種忙碌來源各自一個 server，確認 TryBegin 都回 ErrUpdateBusy。
	busy := []struct {
		name  string
		setup func(t *testing.T, database *db.DB, wsURL func(string) string, sid string) (undo func())
	}{
		{"agent 執行中", func(t *testing.T, database *db.DB, wsURL func(string) string, sid string) func() {
			open := admHold(t, "adm-busy-run")
			c := admDial(t, wsURL(sid))
			c.mustSend(map[string]any{"type": "input", "data": "adm-busy-run"})
			waitFor(t, "agent running", func() bool { return taskIsActive(sid) })
			return open
		}},
		{"shell 執行中（記憶體）", func(t *testing.T, database *db.DB, wsURL func(string) string, sid string) func() {
			shellTaskStart(sid, func() {}, 1)
			return func() { shellTaskEnd(sid, 1) }
		}},
		{"shell 待確認（記憶體）", func(t *testing.T, database *db.DB, wsURL func(string) string, sid string) func() {
			setShellPending(sid, &shellPendingInfo{Command: "ls", WorkDir: "."})
			return func() { clearInMemoryShellApproval(sid) }
		}},
		{"kiroacp 授權等待（記憶體）", func(t *testing.T, database *db.DB, wsURL func(string) string, sid string) func() {
			e := &pendingPermEntry{ch: make(chan bool, 1), tools: "Write"}
			permSet(sid, e)
			return func() { permClear(sid, e) }
		}},
		{"問題等待（記憶體）", func(t *testing.T, database *db.DB, wsURL func(string) string, sid string) func() {
			pendingQuestions.Lock()
			pendingQuestions.m[sid] = &pendingQuestion{RequestID: "adm-q", ch: make(chan json.RawMessage, 1)}
			pendingQuestions.Unlock()
			return func() {
				pendingQuestions.Lock()
				delete(pendingQuestions.m, sid)
				pendingQuestions.Unlock()
			}
		}},
		{"佇列中（DB）", func(t *testing.T, database *db.DB, wsURL func(string) string, sid string) func() {
			if _, err := database.EnqueueMessage(sid, "adm-later", ""); err != nil {
				t.Fatal(err)
			}
			return func() { _ = database.ClearQueuedMessages(sid) }
		}},
		{"暫停佇列（DB）", func(t *testing.T, database *db.DB, wsURL func(string) string, sid string) func() {
			if _, err := database.EnqueueMessage(sid, "keep this queued", ""); err != nil {
				t.Fatal(err)
			}
			if err := database.SetQueuePaused(sid, true); err != nil {
				t.Fatal(err)
			}
			return func() { _ = database.ClearQueuedMessages(sid) }
		}},
		{"pending_denials（DB）", func(t *testing.T, database *db.DB, wsURL func(string) string, sid string) func() {
			if err := database.UpdatePendingDenials(sid, `[{"tool_name":"Write"}]`); err != nil {
				t.Fatal(err)
			}
			return func() { _ = database.UpdatePendingDenials(sid, "") }
		}},
		{"shell_pending（DB）", func(t *testing.T, database *db.DB, wsURL func(string) string, sid string) func() {
			if err := database.UpdateShellPending(sid, `{"line":"ls","cmd":"ls"}`); err != nil {
				t.Fatal(err)
			}
			return func() { _ = database.UpdateShellPending(sid, "") }
		}},
		{"DB 狀態 running（無執行緒）", func(t *testing.T, database *db.DB, wsURL func(string) string, sid string) func() {
			if err := database.UpdateSessionStatus(sid, db.SessionStatusRunning); err != nil {
				t.Fatal(err)
			}
			return func() { _ = database.UpdateSessionStatus(sid, db.SessionStatusIdle) }
		}},
	}
	// 每種忙碌來源的原因分類（BusyReason）：前端據此顯示「為何忙碌」。
	wantBusyReason := map[string]string{
		"agent 執行中":           "running_task",
		"shell 執行中（記憶體）":      "shell_task",
		"shell 待確認（記憶體）":      "pending_confirm",
		"kiroacp 授權等待（記憶體）":   "pending_confirm",
		"問題等待（記憶體）":           "pending_confirm",
		"佇列中（DB）":             "queued",
		"暫停佇列（DB）":            "queued",
		"pending_denials（DB）": "pending_confirm",
		"shell_pending（DB）":   "pending_confirm",
		"DB 狀態 running（無執行緒）": "running_task",
	}
	for _, tc := range busy {
		t.Run("busy: "+tc.name, func(t *testing.T) {
			database, wsURL := startAdmServer(t, ShellOpts{Enabled: true})
			sid := admSession(t, database, "fakeadm", "default")
			undo := tc.setup(t, database, wsURL, sid)
			_, err := TryBeginDesktopUpdate()
			if !errors.Is(err, ErrUpdateBusy) {
				t.Fatalf("應回傳 ErrUpdateBusy，實際 %v", err)
			}
			if got, want := BusyReason(err), wantBusyReason[tc.name]; got != want {
				t.Errorf("BusyReason = %q, want %q", got, want) // Errorf：確保下方 undo 仍會清理種子狀態
			}
			if tc.name == "暫停佇列（DB）" {
				if !strings.Contains(err.Error(), sid) || !strings.Contains(err.Error(), "暫停") || !strings.Contains(err.Error(), "1 筆") {
					t.Errorf("missing actionable blocker: %v", err)
				}
				if queueLen(database, sid) != 1 {
					t.Error("diagnostics changed the queue")
				}
			}
			undo()
			admWaitIdle(t)
		})
	}

	t.Run("cancel map entry removed but cleanup pending is still busy", func(t *testing.T) {
		database, wsURL := startAdmServer(t, ShellOpts{Enabled: true})
		sid := admSession(t, database, "fakeadm", "default")
		open := admHold(t, "adm-stuck")
		c := admDial(t, wsURL(sid))
		c.mustSend(map[string]any{"type": "input", "data": "adm-stuck"})
		waitFor(t, "stuck running", func() bool { return taskIsActive(sid) })
		taskCancel(sid) // 與第二次按停止相同：先刪 map 再 cancel；替身不看 ctx，清理遲遲不結束
		waitFor(t, "map entry removed", func() bool { return !taskIsActive(sid) })
		if _, err := TryBeginDesktopUpdate(); !errors.Is(err, ErrUpdateBusy) {
			t.Fatalf("map 已刪但清理未完應忙碌，實際 %v", err)
		}
		open()
		admWaitIdle(t)
	})

	t.Run("shell cancel map entry removed but cleanup pending is still busy", func(t *testing.T) {
		database, wsURL := startAdmServer(t, ShellOpts{Enabled: true})
		admInstallShellStub(t)
		sid := admSession(t, database, "fakeadm", "bypassPermissions")
		open := admHold(t, "adm-stuck-shell")
		c := admDial(t, wsURL(sid))
		c.mustSend(map[string]any{"type": "shell_run", "data": "adm-stuck-shell"})
		waitFor(t, "shell running", func() bool { return shellTaskActive(sid) })
		shellTaskCancel(sid)
		waitFor(t, "shell map entry removed", func() bool { return !shellTaskActive(sid) })
		if _, err := TryBeginDesktopUpdate(); !errors.Is(err, ErrUpdateBusy) {
			t.Fatalf("shell map 已刪但清理未完應忙碌，實際 %v", err)
		}
		open()
		admWaitIdle(t)
	})

	t.Run("two concurrent restart attempts: exactly one wins", func(t *testing.T) {
		startAdmServer(t, ShellOpts{})
		const n = 16
		start := make(chan struct{})
		var wg sync.WaitGroup
		var mu sync.Mutex
		var releases []func()
		wins, busyCount := 0, 0
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				rel, err := TryBeginDesktopUpdate()
				mu.Lock()
				defer mu.Unlock()
				switch {
				case err == nil:
					wins++
					releases = append(releases, rel)
				case errors.Is(err, ErrUpdateBusy):
					busyCount++
				default:
					t.Errorf("非忙碌錯誤: %v", err)
				}
			}()
		}
		close(start)
		wg.Wait()
		if wins != 1 || busyCount != n-1 {
			t.Fatalf("應只有一個成功，實際 成功=%d 忙碌=%d", wins, busyCount)
		}
		for _, r := range releases {
			r()
		}
		admWaitIdle(t)
	})

	t.Run("after release normal work is admitted", func(t *testing.T) {
		database, wsURL := startAdmServer(t, ShellOpts{Enabled: true})
		sid := admSession(t, database, "fakeadm", "default")
		admMaint(t)()
		c := admDial(t, wsURL(sid))
		c.mustSend(map[string]any{"type": "input", "data": "adm-after", "request_id": "r-after"})
		c.await("input accepted", func(m admMsg) bool { return m.RequestID == "r-after" && m.Type == "input_accepted" })
		waitFor(t, "run finished", func() bool { return lastReply(database, sid) == "ok:adm-after" && !taskIsActive(sid) })
		if n := admHitCount("agent:adm-after"); n != 1 {
			t.Fatalf("應執行一次，實際 %d", n)
		}
	})

	t.Run("maintenance rejects WS input without side effects", func(t *testing.T) {
		database, wsURL := startAdmServer(t, ShellOpts{Enabled: true})
		sid := admSession(t, database, "fakeadm", "default")
		admMaint(t)
		c := admDial(t, wsURL(sid))
		c.mustSend(map[string]any{"type": "input", "data": "adm-rej-input", "request_id": "r-in"})
		c.await("update_in_progress", admRejected)
		admAssertNoSideEffect(t, database, sid, 0, "agent:adm-rej-input")
		if n := queueLen(database, sid); n != 0 {
			t.Errorf("被拒的輸入不應進佇列，實際 %d", n)
		}
	})

	t.Run("maintenance rejects loopback message (MCP send, same WS input entry)", func(t *testing.T) {
		database, wsURL := startAdmServer(t, ShellOpts{Enabled: true})
		sid := admSession(t, database, "fakeadm", "default")
		admMaint(t)
		c := admDial(t, wsURL(sid))
		c.mustSend(map[string]any{"type": "input", "data": "adm-rej-mcp"})
		c.await("update_in_progress", admRejected)
		admAssertNoSideEffect(t, database, sid, 0, "agent:adm-rej-mcp")
	})

	t.Run("maintenance keeps queue; queue_resume rejected; runs once after release", func(t *testing.T) {
		database, wsURL := startAdmServer(t, ShellOpts{Enabled: true})
		sid := admSession(t, database, "fakeadm", "default")
		release := admMaint(t)
		if _, err := database.EnqueueMessage(sid, "adm-kept", ""); err != nil {
			t.Fatal(err)
		}
		if err := database.SetQueuePaused(sid, true); err != nil {
			t.Fatal(err)
		}
		c := admDial(t, wsURL(sid))
		c.mustSend(map[string]any{"type": "queue_resume"})
		c.await("update_in_progress", admRejected)
		if paused, _ := database.QueuePaused(sid); !paused {
			t.Error("閘門拒絕時不應解除暫停")
		}
		if n := queueLen(database, sid); n != 1 {
			t.Errorf("原佇列應保留 1 筆，實際 %d", n)
		}
		admAssertNoSideEffect(t, database, sid, 0, "agent:adm-kept")

		release()
		c.mustSend(map[string]any{"type": "queue_resume"})
		waitFor(t, "kept item runs once", func() bool {
			return admHitCount("agent:adm-kept") == 1 && queueLen(database, sid) == 0 && !taskIsActive(sid)
		})
	})

	t.Run("maintenance rejects shell_run (bypass) without subprocess", func(t *testing.T) {
		database, wsURL := startAdmServer(t, ShellOpts{Enabled: true})
		admInstallShellStub(t)
		sid := admSession(t, database, "fakeadm", "bypassPermissions")
		admMaint(t)
		c := admDial(t, wsURL(sid))
		c.mustSend(map[string]any{"type": "shell_run", "data": "adm-rej-shell"})
		c.await("update_in_progress", admRejected)
		admAssertNoSideEffect(t, database, sid, 0, "shell:adm-rej-shell")
		if shellTaskActive(sid) {
			t.Error("被拒後不應登記 shell 任務")
		}
	})

	t.Run("maintenance rejects shell_exec without writing shell_pending", func(t *testing.T) {
		database, wsURL := startAdmServer(t, ShellOpts{Enabled: true})
		admInstallShellStub(t)
		sid := admSession(t, database, "fakeadm", "default")
		admMaint(t)
		c := admDial(t, wsURL(sid))
		c.mustSend(map[string]any{"type": "shell_exec", "data": "adm-rej-exec"})
		c.await("update_in_progress", admRejected)
		admAssertNoSideEffect(t, database, sid, 0, "shell:adm-rej-exec")
		if s, _ := database.GetSession(sid); s.ShellPending != "" {
			t.Errorf("被拒後不應寫入 shell_pending，實際 %q", s.ShellPending)
		}
	})

	t.Run("maintenance rejects shell_approve (in-memory pending kept)", func(t *testing.T) {
		database, wsURL := startAdmServer(t, ShellOpts{Enabled: true})
		admInstallShellStub(t)
		sid := admSession(t, database, "fakeadm", "default")
		admMaint(t)
		setShellPending(sid, &shellPendingInfo{Command: "adm-rej-approve", WorkDir: t.TempDir()})
		t.Cleanup(func() { clearInMemoryShellApproval(sid) })
		c := admDial(t, wsURL(sid))
		c.mustSend(map[string]any{"type": "shell_approve"})
		c.await("update_in_progress", admRejected)
		if peekShellPending(sid) == nil {
			t.Error("被拒後待確認指令不應被取走")
		}
		admAssertNoSideEffect(t, database, sid, 0, "shell:adm-rej-approve")
	})

	t.Run("maintenance rejects shell_allow_once and keeps shell_pending (DB)", func(t *testing.T) {
		database, wsURL := startAdmServer(t, ShellOpts{Enabled: true})
		admInstallShellStub(t)
		sid := admSession(t, database, "fakeadm", "default")
		admMaint(t)
		if err := database.UpdateShellPending(sid, `{"line":"adm-rej-allow","cmd":"adm-rej-allow"}`); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = database.UpdateShellPending(sid, "") }) // 種子狀態需清掉，閘門才能回到空閒
		c := admDial(t, wsURL(sid))
		c.mustSend(map[string]any{"type": "shell_allow_once"})
		c.await("update_in_progress", admRejected)
		if s, _ := database.GetSession(sid); s.ShellPending == "" {
			t.Error("被拒後 shell_pending 不應被清除")
		}
		admAssertNoSideEffect(t, database, sid, 0, "shell:adm-rej-allow")
	})

	t.Run("maintenance rejects allow_once without claiming pending_denials", func(t *testing.T) {
		database, wsURL := startAdmServer(t, ShellOpts{Enabled: true})
		sid := admSession(t, database, "claude", "default")
		admMaint(t)
		// 閘門已開始後才出現的待授權：閘門必須拒絕，不可認領（否則 pending 會在沒有 run 的情況下消失）。
		if err := database.UpdatePendingDenials(sid, `[{"tool_name":"Write"}]`); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = database.UpdatePendingDenials(sid, "") })
		c := admDial(t, wsURL(sid))
		c.mustSend(map[string]any{"type": "allow_once", "tools": []string{"Write"}})
		c.await("update_in_progress", admRejected)
		if s, _ := database.GetSession(sid); s.PendingDenials != `[{"tool_name":"Write"}]` {
			t.Errorf("被拒後 pending_denials 不應被認領，實際 %q", s.PendingDenials)
		}
		admAssertNoSideEffect(t, database, sid, 0)
	})

	t.Run("maintenance rejects deny_once without claiming or pausing queue", func(t *testing.T) {
		database, wsURL := startAdmServer(t, ShellOpts{Enabled: true})
		sid := admSession(t, database, "claude", "default")
		admMaint(t)
		if err := database.UpdatePendingDenials(sid, `[{"tool_name":"Write"}]`); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = database.UpdatePendingDenials(sid, "") })
		c := admDial(t, wsURL(sid))
		c.mustSend(map[string]any{"type": "deny_once"})
		c.await("update_in_progress", admRejected)
		if s, _ := database.GetSession(sid); s.PendingDenials != `[{"tool_name":"Write"}]` {
			t.Errorf("被拒後 pending_denials 不應被認領，實際 %q", s.PendingDenials)
		}
		if paused, _ := database.QueuePaused(sid); paused {
			t.Error("被拒後不應暫停佇列")
		}
		admAssertNoSideEffect(t, database, sid, 0)
	})

	t.Run("maintenance rejects reset_context and keeps messages and queue", func(t *testing.T) {
		database, wsURL := startAdmServer(t, ShellOpts{Enabled: true})
		sid := admSession(t, database, "fakeadm", "default")
		admMaint(t)
		if err := database.AddMessage(sid, "user", "keep", ""); err != nil {
			t.Fatal(err)
		}
		if _, err := database.EnqueueMessage(sid, "adm-queued-keep", ""); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = database.ClearQueuedMessages(sid) })
		c := admDial(t, wsURL(sid))
		c.mustSend(map[string]any{"type": "reset_context"})
		c.await("update_in_progress", admRejected)
		admAssertNoSideEffect(t, database, sid, 1)
		if n := queueLen(database, sid); n != 1 {
			t.Errorf("reset 被拒後佇列應保留，實際 %d", n)
		}
	})

	t.Run("restart succeeded: maintenance never released, WS input keeps being rejected", func(t *testing.T) {
		database, wsURL := startAdmServer(t, ShellOpts{Enabled: true})
		sid := admSession(t, database, "fakeadm", "default")
		rel, err := TryBeginDesktopUpdate()
		if err != nil {
			t.Fatalf("空閒應可取得維護權: %v", err)
		}
		// 模擬 Restart 成功：測試中不主動釋放（程序將結束）；cleanup 只為了讓 server 清理時回到空閒。
		t.Cleanup(rel)
		c := admDial(t, wsURL(sid))
		for i := 0; i < 3; i++ {
			text := fmt.Sprintf("adm-after-restart-%d", i)
			reqID := fmt.Sprintf("after-restart-%d", i)
			c.mustSend(map[string]any{"type": "input", "data": text, "request_id": reqID})
			reply := c.await("after-restart reply", func(m admMsg) bool {
				return m.RequestID == reqID && (m.Type == "input_accepted" || m.Type == "input_rejected" || m.Code == admCode)
			})
			if reply.Code != admCode {
				t.Fatalf("第 %d 次輸入應被維護拒絕，實際 %+v", i, reply)
			}
			admAssertNoSideEffect(t, database, sid, 0, "agent:"+text)
		}
		if _, err := TryBeginDesktopUpdate(); BusyReason(err) != "maintenance" {
			t.Errorf("維護中再次 TryBegin 應回 maintenance 忙碌，實際 %v", err)
		}
	})

	t.Run("race: restart vs new work — once restart wins, no admission succeeds", func(t *testing.T) {
		startAdmServer(t, ShellOpts{})
		for i := 0; i < 50; i++ {
			const workers = 8
			start := make(chan struct{})
			var wg sync.WaitGroup
			var restartRel func()
			var restartErr error
			for j := 0; j < workers; j++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					if rel, err := admitWork(); err == nil {
						rel()
					}
				}()
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				restartRel, restartErr = TryBeginDesktopUpdate()
			}()
			close(start)
			wg.Wait()
			if restartErr != nil {
				// 工作先取得閘門：Restart 應被拒（忙碌），且沒有維護權要釋放。
				if !errors.Is(restartErr, ErrUpdateBusy) {
					t.Fatalf("iter %d: 非預期錯誤 %v", i, restartErr)
				}
				admWaitIdle(t)
				continue
			}
			// Restart 先取得維護權：此後任何新工作都必須被拒，且不產生 inflight。
			for k := 0; k < workers; k++ {
				if _, err := admitWork(); !errors.Is(err, errUpdateInProgress) {
					t.Errorf("iter %d: 維護成功後 admitWork 應被拒，實際 %v", i, err)
				}
			}
			restartRel()
			admWaitIdle(t)
		}
	})

	t.Run("race: enqueue vs task finishing and drain", func(t *testing.T) {
		database, wsURL := startAdmServer(t, ShellOpts{Enabled: true})
		for i := 0; i < 20; i++ {
			sid := admSession(t, database, "fakeadm", "default")
			keyA := fmt.Sprintf("adm-race-a-%d", i)
			keyB := fmt.Sprintf("adm-race-b-%d", i)
			openA := admHold(t, keyA)
			c := admDial(t, wsURL(sid))
			c.mustSend(map[string]any{"type": "input", "data": keyA})
			waitFor(t, "A running", func() bool { return taskIsActive(sid) })
			c.mustSend(map[string]any{"type": "input", "data": keyB})
			waitFor(t, "B queued", func() bool { return queueLen(database, sid) == 1 })

			// A 結束（觸發 drain 取出 B）與 TryBegin 同時進行。
			res := make(chan error, 1)
			var wg sync.WaitGroup
			wg.Add(2)
			go func() { defer wg.Done(); openA() }()
			go func() {
				defer wg.Done()
				rel, err := TryBeginDesktopUpdate()
				if err == nil {
					rel()
				}
				res <- err
			}()
			wg.Wait()
			err := <-res
			if err == nil {
				// 取得成功代表 B 已完整執行（不會遺失、也不會卡在佇列）。
				if admHitCount("agent:"+keyB) != 1 || queueLen(database, sid) != 0 {
					t.Fatalf("iter %d: 維護成功但 B 未恰好執行一次（hits=%d queue=%d）", i, admHitCount("agent:"+keyB), queueLen(database, sid))
				}
			} else if !errors.Is(err, ErrUpdateBusy) {
				t.Fatalf("iter %d: 應為 ErrUpdateBusy，實際 %v", i, err)
			}
			waitFor(t, "B finished exactly once", func() bool {
				return admHitCount("agent:"+keyB) == 1 && queueLen(database, sid) == 0 && !taskIsActive(sid)
			})
			if admHitCount("agent:"+keyA) != 1 {
				t.Fatalf("iter %d: A 應只執行一次", i)
			}
			admWaitIdle(t)
		}
	})

	t.Run("race: WS input vs TryBegin", func(t *testing.T) {
		database, wsURL := startAdmServer(t, ShellOpts{Enabled: true})
		for i := 0; i < 20; i++ {
			sid := admSession(t, database, "fakeadm", "default")
			text := fmt.Sprintf("adm-race-in-%d", i)
			reqID := fmt.Sprintf("race-%d", i)
			c := admDial(t, wsURL(sid))

			sent := make(chan error, 1)
			go func() {
				sent <- c.conn.WriteJSON(map[string]any{"type": "input", "data": text, "request_id": reqID})
			}()
			rel, err := TryBeginDesktopUpdate()
			if werr := <-sent; werr != nil {
				t.Fatal(werr)
			}
			reply := c.await("input reply", func(m admMsg) bool {
				return m.RequestID == reqID && (m.Type == "input_accepted" || m.Type == "input_rejected" || m.Code == admCode)
			})
			if reply.Code == admCode {
				// 被拒代表閘門當時已由本次 TryBegin 取得，必須成功，且沒有任何副作用。
				if err != nil {
					t.Fatalf("iter %d: 輸入被拒時維護權應已取得，實際 %v", i, err)
				}
				admAssertNoSideEffect(t, database, sid, 0, "agent:"+text)
				if n := queueLen(database, sid); n != 0 {
					t.Fatalf("iter %d: 被拒的輸入不應進佇列", i)
				}
				rel()
			} else {
				// 已接受：必須恰好執行一次；維護權取得與否都合法（取得時代表執行已完成）。
				if err == nil {
					rel()
				} else if !errors.Is(err, ErrUpdateBusy) {
					t.Fatalf("iter %d: 非預期錯誤 %v", i, err)
				}
				waitFor(t, "accepted input finished", func() bool {
					return admHitCount("agent:"+text) == 1 && !taskIsActive(sid) && queueLen(database, sid) == 0
				})
			}
			admWaitIdle(t)
		}
	})
}
