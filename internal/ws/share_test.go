package ws

import (
	"encoding/json"
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
	"github.com/jerry12122/Claude-Code-Mini-App/internal/tg"
)

// startShareServer 與 startTestServer 相同，但多一層模擬 authMiddleware 的 guest 分支：
// URL 帶 ?share=<token> 時解析訪客並寫入 Locals（與 internal/server/guest.go 一致）。
func startShareServer(t *testing.T) (*db.DB, func(sessionID, guestToken string) string) {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/ws.db")
	if err != nil {
		t.Fatal(err)
	}
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	lifecycle := newWSTestLifecycle(database)
	app.Get("/sessions/:id/ws", func(c *fiber.Ctx) error {
		if tok := c.Query("share"); tok != "" {
			ga, err := database.ResolveGuestToken(tok)
			if err != nil || !ga.Share.Active(time.Now()) || ga.Share.SessionID != c.Params("id") {
				return c.SendStatus(401)
			}
			c.Locals("share_id", ga.Share.ID)
			c.Locals("share_role", ga.Share.Role)
			c.Locals("share_mode", ga.Share.Mode)
			c.Locals("share_nickname", ga.Guest.Nickname)
			c.Locals("share_expires_at", ga.Share.ExpiresAt)
		}
		return c.Next()
	}, fiberws.New(lifecycle.wrap(NewHandler(database, "", ShellOpts{}, nil, tg.NotifyConfig{}))))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan struct{})
	go func() { defer close(served); _ = app.Listener(ln) }()
	t.Cleanup(func() { lifecycle.close(t, app, served) })
	port := ln.Addr().(*net.TCPAddr).Port
	return database, func(id, tok string) string {
		u := fmt.Sprintf("ws://127.0.0.1:%d/sessions/%s/ws", port, id)
		if tok != "" {
			u += "?share=" + tok
		}
		return u
	}
}

// wsClient 在背景持續收訊息並保存，方便斷言。
type wsClient struct {
	conn   *websocket.Conn
	mu     sync.Mutex
	msgs   []map[string]any
	closed chan struct{}
}

func dialClient(t *testing.T, url string) *wsClient {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	c := &wsClient{conn: conn, closed: make(chan struct{})}
	go func() {
		defer close(c.closed)
		for {
			_, b, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var m map[string]any
			if json.Unmarshal(b, &m) == nil {
				c.mu.Lock()
				c.msgs = append(c.msgs, m)
				c.mu.Unlock()
			}
		}
	}()
	t.Cleanup(func() { conn.Close() })
	return c
}

func (c *wsClient) send(t *testing.T, v map[string]any) {
	t.Helper()
	if err := c.conn.WriteJSON(v); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func (c *wsClient) count(pred func(map[string]any) bool) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, m := range c.msgs {
		if pred(m) {
			n++
		}
	}
	return n
}

func (c *wsClient) first(pred func(map[string]any) bool) map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, m := range c.msgs {
		if pred(m) {
			return m
		}
	}
	return nil
}

func (c *wsClient) await(t *testing.T, what string, pred func(map[string]any) bool) map[string]any {
	t.Helper()
	var got map[string]any
	waitFor(t, what, func() bool { got = c.first(pred); return got != nil })
	return got
}

func isType(typ string) func(map[string]any) bool {
	return func(m map[string]any) bool { return m["type"] == typ }
}

func presenceEvent(event, who string) func(map[string]any) bool {
	return func(m map[string]any) bool {
		return m["type"] == "presence" && m["value"] == event && m["author"] == who
	}
}

func (c *wsClient) waitClosed(t *testing.T, d time.Duration) {
	t.Helper()
	select {
	case <-c.closed:
	case <-time.After(d):
		t.Fatal("連線應被伺服器關閉")
	}
}

// ping 並等 pong：因讀迴圈是循序處理，收到 pong 代表先前送出的訊息都已處理完畢。
func (c *wsClient) barrier(t *testing.T) {
	t.Helper()
	before := c.count(isType("pong"))
	c.send(t, map[string]any{"type": "ping"})
	waitFor(t, "pong", func() bool { return c.count(isType("pong")) > before })
}

func newShare(t *testing.T, database *db.DB, sessionID, role, mode, nick string, ttl time.Duration) (*db.Share, *db.GuestAccess) {
	t.Helper()
	sh, err := database.CreateShare(sessionID, role, mode, ttl)
	if err != nil {
		t.Fatal(err)
	}
	ga, err := database.TryJoin(sh.Token, sh.PIN, nick)
	if err != nil {
		t.Fatal(err)
	}
	return sh, ga
}

func claudeMessages(database *db.DB, sessionID string) int {
	msgs, _ := database.ListMessages(sessionID)
	n := 0
	for _, m := range msgs {
		if m.Role == "claude" {
			n++
		}
	}
	return n
}

// viewer 的 WS 只處理 ping：其餘指令全部被丟棄並回 error，且不產生任何副作用。
func TestShareWS_ViewerCommandsDropped(t *testing.T) {
	database, dial := startShareServer(t)
	s, err := database.CreateSession("v", "", t.TempDir(), "default", agent.TypeClaude, nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	owner := dialClient(t, dial(s.ID, ""))
	owner.send(t, map[string]any{"type": "input", "data": "needperm"})
	waitFor(t, "awaiting confirm", func() bool {
		x, _ := database.GetSession(s.ID)
		return x.PendingDenials != "" && !taskIsActive(s.ID)
	})
	before, _ := database.GetSession(s.ID)
	claudeBefore := claudeMessages(database, s.ID)
	usersBefore := len(userMessages(database, s.ID))

	_, ga := newShare(t, database, s.ID, db.ShareRoleViewer, db.ShareModeLive, "觀眾", time.Hour)
	viewer := dialClient(t, dial(s.ID, ga.Guest.Token))
	viewer.await(t, "viewer sync", isType("sync"))

	attacks := []map[string]any{
		{"type": "question_answer", "request_id": "q1", "answers": []map[string]any{{"text": "hijack"}}},
		{"type": "input", "data": "hijack"},
		{"type": "allow_once", "tools": []string{"Write"}},
		{"type": "deny_once"},
		{"type": "set_mode", "mode": "bypassPermissions"},
		{"type": "shell_exec", "data": "echo hi"},
		{"type": "shell_run", "data": "echo hi"},
		{"type": "set_input_mode", "mode": "shell"},
		{"type": "reset_context"},
		{"type": "interrupt"},
		{"type": "queue_resume"},
		{"type": "queue_remove", "id": 1},
		{"type": "set_model", "model": "x"},
		{"type": "set_effort", "effort": "high"},
		{"type": "refresh_quota"},
	}
	for _, a := range attacks {
		viewer.send(t, a)
	}
	viewer.barrier(t)

	if got := viewer.count(isType("error")); got != len(attacks) {
		t.Fatalf("每個被丟棄的指令都應回 error，got %d want %d", got, len(attacks))
	}
	after, _ := database.GetSession(s.ID)
	if after.PendingDenials != before.PendingDenials || after.PendingDenials == "" {
		t.Fatalf("授權請求不應被 viewer 處理: %q", after.PendingDenials)
	}
	if after.PermissionMode != "default" || after.InputMode != before.InputMode || after.Status != before.Status {
		t.Fatalf("session 狀態被 viewer 改動: %+v", after)
	}
	if n := claudeMessages(database, s.ID); n != claudeBefore {
		t.Fatalf("viewer 不應觸發 agent 執行: %d -> %d", claudeBefore, n)
	}
	if n := len(userMessages(database, s.ID)); n != usersBefore {
		t.Fatalf("viewer 不應寫入訊息: %d -> %d", usersBefore, n)
	}
	if taskIsActive(s.ID) {
		t.Fatal("不應有任務在執行")
	}
	// ping 仍可用（上面的 barrier 已驗證 pong）。
}

// editor 的訊息帶署名、prompt 帶 [暱稱] 前綴；presence 上線／離線廣播給其他連線。
func TestShareWS_EditorAuthorPrefixAndPresence(t *testing.T) {
	database, dial := startShareServer(t)
	s, err := database.CreateSession("e", "", t.TempDir(), "default", agent.TypeClaude, nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	owner := dialClient(t, dial(s.ID, ""))
	owner.await(t, "owner sync", isType("sync"))
	if n := owner.count(isType("presence")); n != 0 {
		t.Fatalf("沒有訪客時不應有 presence 廣播，got %d", n)
	}

	_, ga := newShare(t, database, s.ID, db.ShareRoleEditor, db.ShareModeLive, "小明", time.Hour)
	editor := dialClient(t, dial(s.ID, ga.Guest.Token))
	editor.await(t, "editor sync", isType("sync"))

	join := owner.await(t, "owner 收到 join", presenceEvent("join", "小明"))
	online, _ := join["online"].([]any)
	if len(online) != 2 || online[0] != ownerDisplayName || online[1] != "小明" {
		t.Fatalf("在線名單錯誤: %v", join["online"])
	}

	editor.send(t, map[string]any{"type": "input", "data": "hello"})
	um := owner.await(t, "owner 收到 user_message", func(m map[string]any) bool {
		return m["type"] == "user_message" && m["content"] == "hello"
	})
	if um["author"] != "小明" {
		t.Fatalf("user_message 應署名，got %v", um["author"])
	}
	waitFor(t, "agent 回覆", func() bool { return lastReply(database, s.ID) == "ok:[小明] hello" })

	msgs, _ := database.ListMessages(s.ID)
	var found bool
	for _, m := range msgs {
		if m.Role == "user" && m.Content == "hello" {
			found = true
			if m.Author != "小明" {
				t.Fatalf("DB author=%q", m.Author)
			}
		}
	}
	if !found {
		t.Fatal("找不到訪客訊息")
	}

	// 擁有者自己的訊息不署名、不加前綴。
	waitFor(t, "idle", func() bool { return !taskIsActive(s.ID) })
	owner.send(t, map[string]any{"type": "input", "data": "from owner"})
	waitFor(t, "owner 回覆", func() bool { return lastReply(database, s.ID) == "ok:from owner" })

	editor.conn.Close()
	leave := owner.await(t, "owner 收到 leave", presenceEvent("leave", "小明"))
	if lo, _ := leave["online"].([]any); len(lo) != 1 || lo[0] != ownerDisplayName {
		t.Fatalf("離線後名單錯誤: %v", leave["online"])
	}
}

// 排隊中的訪客訊息保留署名與前綴。
func TestShareWS_QueuedGuestMessageKeepsAuthor(t *testing.T) {
	database, dial := startShareServer(t)
	s, err := database.CreateSession("q", "", t.TempDir(), "default", "fakeq", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	owner := dialClient(t, dial(s.ID, ""))
	_, ga := newShare(t, database, s.ID, db.ShareRoleEditor, db.ShareModeLive, "阿花", time.Hour)
	editor := dialClient(t, dial(s.ID, ga.Guest.Token))
	editor.await(t, "editor sync", isType("sync"))

	owner.send(t, map[string]any{"type": "input", "data": "block-1"})
	waitFor(t, "running", func() bool { return taskIsActive(s.ID) })
	editor.send(t, map[string]any{"type": "input", "data": "second"})
	waitFor(t, "queued", func() bool { return queueLen(database, s.ID) == 1 })
	q, _ := database.ListQueuedMessages(s.ID)
	if q[0].Author != "阿花" {
		t.Fatalf("佇列項目應保留署名，got %q", q[0].Author)
	}

	fakeGate <- nil
	waitFor(t, "queue drained", func() bool {
		return queueLen(database, s.ID) == 0 && !taskIsActive(s.ID) && lastReply(database, s.ID) == "ok:[阿花] second"
	})
	msgs, _ := database.ListMessages(s.ID)
	for _, m := range msgs {
		if m.Role == "user" && m.Content == "second" && m.Author != "阿花" {
			t.Fatalf("升格後的訊息應保留署名，got %q", m.Author)
		}
	}
}

// 多人同時回應授權請求：先到者勝，agent 只重跑一次。
func TestShareWS_ConcurrentApprovalRunsOnce(t *testing.T) {
	database, dial := startShareServer(t)
	s, err := database.CreateSession("a", "", t.TempDir(), "default", agent.TypeClaude, nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	owner := dialClient(t, dial(s.ID, ""))
	_, ga := newShare(t, database, s.ID, db.ShareRoleEditor, db.ShareModeLive, "小明", time.Hour)
	editor := dialClient(t, dial(s.ID, ga.Guest.Token))
	editor.await(t, "editor sync", isType("sync"))

	for round := 0; round < 5; round++ {
		owner.send(t, map[string]any{"type": "input", "data": "needperm"})
		waitFor(t, "awaiting confirm", func() bool {
			x, _ := database.GetSession(s.ID)
			return x.PendingDenials != "" && !taskIsActive(s.ID)
		})
		base := claudeMessages(database, s.ID)

		start := make(chan struct{})
		var wg sync.WaitGroup
		for _, c := range []struct {
			cl  *wsClient
			msg map[string]any
		}{
			{owner, map[string]any{"type": "allow_once", "tools": []string{"Write"}}},
			{editor, map[string]any{"type": "allow_once", "tools": []string{"Write"}}},
			{editor, map[string]any{"type": "deny_once"}},
			{owner, map[string]any{"type": "deny_once"}},
		} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				c.cl.mu.Lock() // 序列化同一條連線的寫入
				_ = c.cl.conn.WriteJSON(c.msg)
				c.cl.mu.Unlock()
			}()
		}
		close(start)
		wg.Wait()

		waitFor(t, "retry 完成", func() bool {
			x, _ := database.GetSession(s.ID)
			return x.PendingDenials == "" && !taskIsActive(s.ID) && x.Status == db.SessionStatusIdle
		})
		time.Sleep(200 * time.Millisecond) // 給可能重複的第二次執行時間冒出來
		if got := claudeMessages(database, s.ID); got != base+1 {
			t.Fatalf("round %d: 授權應只執行一次，agent 訊息 %d -> %d", round, base, got)
		}
	}
}

// snapshot 分享不能連 WS；結束分享與到期都會踢線並送 share_ended。
func TestShareWS_SnapshotRefusedKickAndExpiry(t *testing.T) {
	database, dial := startShareServer(t)
	s, err := database.CreateSession("k", "", t.TempDir(), "default", agent.TypeClaude, nil, "agent")
	if err != nil {
		t.Fatal(err)
	}

	// 1) snapshot：即使繞過 middleware 帶著 Locals，handler 也拒絕（不送 sync）。
	_, snap := newShare(t, database, s.ID, db.ShareRoleViewer, db.ShareModeSnapshot, "快照", time.Hour)
	sc := dialClient(t, dial(s.ID, snap.Guest.Token))
	sc.waitClosed(t, 3*time.Second)
	if sc.count(isType("sync")) != 0 {
		t.Fatal("snapshot 訪客不應收到 sync")
	}

	// 2) 結束分享 → KickShare。
	sh, ga := newShare(t, database, s.ID, db.ShareRoleEditor, db.ShareModeLive, "小明", time.Hour)
	c := dialClient(t, dial(s.ID, ga.Guest.Token))
	c.await(t, "sync", isType("sync"))
	waitFor(t, "在線", func() bool { return strings.Join(ShareOnline(sh.ID), ",") == "小明" })
	if err := database.RevokeShare(sh.ID); err != nil {
		t.Fatal(err)
	}
	KickShare(sh.ID)
	ended := c.await(t, "share_ended", isType("share_ended"))
	if ended["content"] != shareEndRevoked {
		t.Fatalf("原因應為 revoked: %v", ended["content"])
	}
	c.waitClosed(t, 3*time.Second)
	waitFor(t, "離線", func() bool { return len(ShareOnline(sh.ID)) == 0 })

	// 3) 到期 timer。
	sh2, ga2 := newShare(t, database, s.ID, db.ShareRoleViewer, db.ShareModeLive, "過客", 2*time.Second)
	c2 := dialClient(t, dial(s.ID, ga2.Guest.Token))
	c2.await(t, "sync", isType("sync"))
	ended2 := c2.await(t, "到期 share_ended", isType("share_ended"))
	if ended2["content"] != shareEndExpired {
		t.Fatalf("原因應為 expired: %v", ended2["content"])
	}
	c2.waitClosed(t, 3*time.Second)
	_ = sh2
}
