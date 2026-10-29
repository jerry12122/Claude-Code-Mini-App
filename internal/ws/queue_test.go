package ws

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/fasthttp/websocket"
	fiberws "github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/tg"
)

// fakeGate：prompt 以 "block" 開頭時，runner 會卡住直到從這裡收到結果（nil＝成功）。
var fakeGate = make(chan error)

type fakeRunner struct{}

func (fakeRunner) Name() string { return "fakeq" }

func (fakeRunner) Run(ctx context.Context, opts agent.RunOptions, cb agent.EventCallback) error {
	if strings.HasPrefix(opts.Prompt, "block") {
		select {
		case err := <-fakeGate:
			if err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	cb(agent.Event{Type: agent.EventDelta, Text: "ok:" + opts.Prompt})
	cb(agent.Event{Type: agent.EventDone})
	// "linger" 開頭：result 後程序不退出（模擬等 ScheduleWakeup），直到 gate 放行。
	if strings.HasPrefix(opts.Prompt, "linger") {
		select {
		case <-fakeGate:
		case <-ctx.Done():
		}
	}
	return nil
}

func init() { agent.Register("fakeq", func() agent.Runner { return fakeRunner{} }) }

// fakeClaude：handler 的授權流程寫死只對 agent_type=claude 生效（isClaude），無法改用專屬 type，
// 因此在 ws 測試二進位內覆寫 claude runner。能覆寫成功是因為被 import 的 claude 套件 init 先跑、這裡的 init 後跑。
// 注意：本套件任何測試若建 claude session 並送 input，拿到的都是這個假 runner，不會真的跑 CLI。
// prompt 含 "needperm" 時回 permission_denied。
type fakeClaude struct{}

func (fakeClaude) Name() string { return "claude" }

func (fakeClaude) Run(_ context.Context, opts agent.RunOptions, cb agent.EventCallback) error {
	if strings.Contains(opts.Prompt, "needperm") {
		cb(agent.Event{Type: agent.EventPermDenied, Denials: []agent.PermissionDenial{{ToolName: "Write"}}})
		cb(agent.Event{Type: agent.EventDone})
		return nil
	}
	cb(agent.Event{Type: agent.EventDelta, Text: "ok:" + opts.Prompt})
	cb(agent.Event{Type: agent.EventDone})
	return nil
}

func init() { agent.Register(agent.TypeClaude, func() agent.Runner { return fakeClaude{} }) }

// startTestServer 起一個只掛 WS 的 fiber，回傳 DB 與撥號用 URL 產生器。
func startTestServer(t *testing.T) (*db.DB, func(sessionID string) string) {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/ws.db")
	if err != nil {
		t.Fatal(err)
	}
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Get("/sessions/:id/ws", fiberws.New(NewHandler(database, "", ShellOpts{}, nil, tg.NotifyConfig{})))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = app.Listener(ln) }()
	t.Cleanup(func() { _ = app.Shutdown(); database.Close() })
	port := ln.Addr().(*net.TCPAddr).Port
	return database, func(id string) string { return fmt.Sprintf("ws://127.0.0.1:%d/sessions/%s/ws", port, id) }
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", what)
}

func userMessages(database *db.DB, sessionID string) []string {
	msgs, _ := database.ListMessages(sessionID)
	var out []string
	for _, m := range msgs {
		if m.Role == "user" {
			out = append(out, m.Content)
		}
	}
	return out
}

func queueLen(database *db.DB, sessionID string) int {
	q, _ := database.ListQueuedMessages(sessionID)
	return len(q)
}

func TestRemoveQueuedMessageBroadcasts(t *testing.T) {
	database, _ := startTestServer(t)
	s, err := database.CreateSession("queue-delete", "", t.TempDir(), "default", "fakeq", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	q, err := database.EnqueueMessage(s.ID, "remove me", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.SetQueuePaused(s.ID, true); err != nil {
		t.Fatal(err)
	}
	events := make(chan serverMsg, 1)
	unsub := hub.Subscribe(s.ID, func(msg serverMsg) bool { events <- msg; return true })
	defer unsub()
	if err := RemoveQueuedMessage(database, s.ID, q.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-events:
		if ev.Type != "queue_update" || len(ev.Queue) != 0 || !ev.QueuePaused {
			t.Fatalf("wrong queue update: %+v", ev)
		}
	default:
		t.Fatal("queue deletion must notify subscribers")
	}
}

func TestQueue_RunsInOrderAndPausesOnFailure(t *testing.T) {
	database, wsURL := startTestServer(t)
	s, err := database.CreateSession("q", "", t.TempDir(), "default", "fakeq", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(s.ID), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	go func() { // 丟棄 server 推播，避免寫入端卡住
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	input := func(text string) {
		if err := conn.WriteJSON(map[string]string{"type": "input", "data": text}); err != nil {
			t.Fatal(err)
		}
	}

	// 1) 執行中送出的訊息排隊，不打斷進行中的任務；成功後依序執行。
	input("block-1")
	waitFor(t, "block-1 running", func() bool { return taskIsActive(s.ID) })
	input("q1")
	input("q2")
	waitFor(t, "2 queued", func() bool { return queueLen(database, s.ID) == 2 })
	fakeGate <- nil
	waitFor(t, "queue drained", func() bool {
		return queueLen(database, s.ID) == 0 && !taskIsActive(s.ID) && lastReply(database, s.ID) == "ok:q2"
	})
	if got := strings.Join(userMessages(database, s.ID), ","); got != "block-1,q1,q2" {
		t.Fatalf("執行順序錯誤: %s", got)
	}

	// 2) 失敗時暫停，排隊的不自動執行；queue_resume 後才執行。
	input("block-2")
	waitFor(t, "block-2 running", func() bool { return taskIsActive(s.ID) })
	input("q3")
	waitFor(t, "q3 queued", func() bool { return queueLen(database, s.ID) == 1 })
	fakeGate <- errors.New("boom")
	waitFor(t, "paused", func() bool {
		p, _ := database.QueuePaused(s.ID)
		return p && !taskIsActive(s.ID)
	})
	time.Sleep(150 * time.Millisecond)
	if n := queueLen(database, s.ID); n != 1 {
		t.Fatalf("失敗後不應續跑，queue=%d", n)
	}
	if err := conn.WriteJSON(map[string]string{"type": "queue_resume"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "q3 ran after resume", func() bool {
		u := userMessages(database, s.ID)
		return queueLen(database, s.ID) == 0 && len(u) == 5 && u[4] == "q3" && !taskIsActive(s.ID) && lastReply(database, s.ID) == "ok:q3"
	})
}

// lastReply 回傳最後一則已完成的 agent 回覆內容（尚在串流中則回空字串）。
func lastReply(database *db.DB, sessionID string) string {
	msgs, _ := database.ListMessages(sessionID)
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "claude" {
			if msgs[i].Status != db.MessageStatusDone {
				return ""
			}
			return msgs[i].Content
		}
	}
	return ""
}

// 回歸：Claude 授權請求 →「允許並記住」(set_mode) 後 DB status 不可殘留 awaiting_confirm，
// 否則之後的 input 會被誤判忙碌而永遠卡在佇列。等授權期間排的訊息在 set_mode 後應接著執行。
func TestQueue_SetModeAfterPermissionUnblocks(t *testing.T) {
	database, wsURL := startTestServer(t)
	s, err := database.CreateSession("p", "", t.TempDir(), "default", agent.TypeClaude, nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(s.ID), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	write := func(v map[string]string) {
		if err := conn.WriteJSON(v); err != nil {
			t.Fatal(err)
		}
	}

	write(map[string]string{"type": "input", "data": "needperm"})
	waitFor(t, "awaiting confirm", func() bool {
		x, _ := database.GetSession(s.ID)
		return x.PendingDenials != "" && !taskIsActive(s.ID)
	})
	write(map[string]string{"type": "input", "data": "while-waiting"})
	waitFor(t, "queued during confirm", func() bool { return queueLen(database, s.ID) == 1 })

	write(map[string]string{"type": "set_mode", "mode": "acceptEdits"})
	waitFor(t, "queued ran after set_mode", func() bool {
		return queueLen(database, s.ID) == 0 && lastReply(database, s.ID) == "ok:while-waiting" && !taskIsActive(s.ID)
	})

	write(map[string]string{"type": "input", "data": "after"})
	waitFor(t, "idle input runs immediately", func() bool {
		x, _ := database.GetSession(s.ID)
		return lastReply(database, s.ID) == "ok:after" && x.Status == db.SessionStatusIdle
	})
	if n := queueLen(database, s.ID); n != 0 {
		t.Fatalf("閒置時的 input 不應排隊，queue=%d", n)
	}
}

// 回歸（reviewer H1）：佇列暫停且仍有項目時，閒置狀態的新 input 不可插隊；應排到尾端並維持暫停，
// queue_resume 後依送出順序執行。
func TestQueue_IdleInputDoesNotJumpPausedQueue(t *testing.T) {
	database, wsURL := startTestServer(t)
	s, err := database.CreateSession("h1", "", t.TempDir(), "default", "fakeq", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(s.ID), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	write := func(v map[string]string) {
		if err := conn.WriteJSON(v); err != nil {
			t.Fatal(err)
		}
	}

	write(map[string]string{"type": "input", "data": "block-fail"})
	waitFor(t, "running", func() bool { return taskIsActive(s.ID) })
	write(map[string]string{"type": "input", "data": "old"})
	waitFor(t, "old queued", func() bool { return queueLen(database, s.ID) == 1 })
	fakeGate <- errors.New("boom")
	waitFor(t, "paused & idle", func() bool {
		p, _ := database.QueuePaused(s.ID)
		return p && !taskIsActive(s.ID)
	})

	write(map[string]string{"type": "input", "data": "new"})
	waitFor(t, "new appended", func() bool { return queueLen(database, s.ID) == 2 })
	time.Sleep(150 * time.Millisecond)
	if lastReply(database, s.ID) == "ok:new" {
		t.Fatal("new 不應在暫停佇列前插隊執行")
	}

	write(map[string]string{"type": "queue_resume"})
	waitFor(t, "both ran", func() bool {
		return queueLen(database, s.ID) == 0 && lastReply(database, s.ID) == "ok:new" && !taskIsActive(s.ID)
	})
	u := userMessages(database, s.ID)
	if got := strings.Join(u[len(u)-2:], ","); got != "old,new" {
		t.Fatalf("順序錯誤: %s", got)
	}
}

// result 後程序仍活著：status 已 IDLE，但要通知前端 lingering on（顯示中斷鈕）；程序退出後 lingering off。
func TestLingering_NotifiedWhileProcessOutlivesResult(t *testing.T) {
	old := lingerNoticeDelay
	lingerNoticeDelay = 50 * time.Millisecond
	t.Cleanup(func() { lingerNoticeDelay = old })

	database, wsURL := startTestServer(t)
	s, err := database.CreateSession("l", "", t.TempDir(), "default", "fakeq", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(s.ID), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	events := make(chan string, 64)
	go func() {
		for {
			var m struct{ Type, Value string }
			if err := conn.ReadJSON(&m); err != nil {
				return
			}
			if m.Type == "lingering" || (m.Type == "status" && m.Value == StateIdle) {
				events <- m.Type + ":" + m.Value
			}
		}
	}()
	expect := func(want string) {
		t.Helper()
		select {
		case got := <-events:
			if got != want {
				t.Fatalf("事件順序錯誤: got %s want %s", got, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("timeout waiting for %s", want)
		}
	}

	if err := conn.WriteJSON(map[string]string{"type": "input", "data": "linger-1"}); err != nil {
		t.Fatal(err)
	}
	expect("status:IDLE")
	expect("lingering:on")
	if !taskIsActive(s.ID) {
		t.Fatal("程序未退出時任務應仍登記為執行中")
	}
	fakeGate <- nil
	expect("lingering:off")
}
