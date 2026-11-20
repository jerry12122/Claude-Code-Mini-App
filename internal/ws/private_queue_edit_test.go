package ws

import (
	"fmt"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/fasthttp/websocket"
	fiberws "github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/tg"
)

// 普通（私人）會話的 queue_edit：真 loopback WS＋真 runner gate，不直接呼叫 handler。

type privateEditRig struct {
	*runNowRig
	dial  func(string, string) string
	owner *wsClient
	a     *runNowTurn
	q     []db.QueuedMessage // A 執行中，B、C 由 owner 的 input 排入
}

func newPrivateEditRig(t *testing.T) *privateEditRig {
	t.Helper()
	x, dial := newRunNowWSRig(t)
	r := &privateEditRig{runNowRig: x, dial: dial, owner: dialClient(t, dial(x.s.ID, ""))}
	r.owner.barrier(t)
	r.a = x.plan("hold-cancel")
	r.owner.send(t, map[string]any{"type": "input", "data": "A"})
	readyRunNow(t, r.a)
	r.owner.send(t, map[string]any{"type": "input", "data": "B", "request_id": "rid-B"})
	r.owner.send(t, map[string]any{"type": "input", "data": "C", "request_id": "rid-C"})
	r.owner.barrier(t)
	waitFor(t, "A 的 partial 寫入穩定", func() bool {
		m, err := x.database.ListMessages(x.s.ID)
		return err == nil && len(m) == 2 && m[1].Content == "partial"
	})
	r.q = x.queue(t)
	if len(r.q) != 2 {
		t.Fatalf("queue=%+v", r.q)
	}
	return r
}

func resultFor(rid string) func(map[string]any) bool {
	return func(m map[string]any) bool {
		return (m["type"] == "queue_edit_success" || m["type"] == "queue_edit_error") && m["request_id"] == rid
	}
}

// unchanged 確認 queue、歷史、session 與 runner 都沒被這次拒絕碰到。
func (r *privateEditRig) snapshot(t *testing.T) func() {
	t.Helper()
	q, m, s := r.queue(t), r.messages(t), r.session(t)
	return func() {
		t.Helper()
		if !reflect.DeepEqual(q, r.queue(t)) || !reflect.DeepEqual(m, r.messages(t)) || !reflect.DeepEqual(s, r.session(t)) {
			t.Fatal("拒絕不得改動 queue／歷史／session")
		}
		select {
		case <-r.a.cancelled:
			t.Fatal("編輯不得取消執行中的 runner")
		default:
		}
	}
}

func (r *privateEditRig) messages(t *testing.T) []*db.Message {
	t.Helper()
	m, err := r.database.ListMessages(r.s.ID)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func (r *privateEditRig) session(t *testing.T) *db.Session {
	t.Helper()
	s, err := r.database.GetSession(r.s.ID)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPrivateQueueEditOwnerSuccessBroadcastsAndKeepsEverythingElse(t *testing.T) {
	r := newPrivateEditRig(t)
	other := dialClient(t, r.dial(r.s.ID, ""))
	other.barrier(t)
	if _, err := r.database.Exec(`UPDATE sessions SET quota_resume_state='waiting',quota_resume_attempts=2,quota_resume_generation=7,quota_resume_at='2099-01-01T00:00:00Z' WHERE id=?`, r.s.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.database.SetQueuePaused(r.s.ID, true); err != nil {
		t.Fatal(err)
	}
	beforeQ, beforeM, beforeS := r.queue(t), r.messages(t), r.session(t)
	want := append([]db.QueuedMessage(nil), beforeQ...)
	want[1].Content = "  新的 C\n保留空白  " + strings.Repeat("長", 30000) // 私人會話不套 Room 的 64000 bytes
	r.owner.send(t, map[string]any{"type": "queue_edit", "id": beforeQ[1].ID, "request_id": "edit-1", "previous": "C", "content": want[1].Content})

	for _, c := range []*wsClient{r.owner, other} {
		c.await(t, "queue_update 帶新文字與 request_id", func(m map[string]any) bool {
			q, _ := m["queue"].([]any)
			if m["type"] != "queue_update" || len(q) != 2 || m["request_id"] != "edit-1" || m["queue_paused"] != true {
				return false
			}
			item, _ := q[1].(map[string]any)
			return item["id"] == float64(beforeQ[1].ID) && item["content"] == want[1].Content
		})
	}
	ack := r.owner.await(t, "queue_edit_success", resultFor("edit-1"))
	if ack["type"] != "queue_edit_success" || ack["id"] != float64(beforeQ[1].ID) {
		t.Fatalf("ack=%v", ack)
	}
	if other.first(resultFor("edit-1")) != nil {
		t.Fatal("其他裝置不該收到不屬於它的 request 結果")
	}
	if got := r.queue(t); !reflect.DeepEqual(got, want) {
		t.Fatalf("只該改 C 的 content，FIFO／附件／來源不變：%+v", got)
	}
	afterS := r.session(t)
	if !reflect.DeepEqual(beforeS, afterS) || !reflect.DeepEqual(beforeM, r.messages(t)) {
		t.Fatal("編輯改了 session（paused／quota）或歷史")
	}
	select {
	case <-r.a.cancelled:
		t.Fatal("編輯不得取消 A")
	default:
	}
	if paused, _ := r.database.QueuePaused(r.s.ID); !paused {
		t.Fatal("paused 被改")
	}
	// 原 receipt 不變：同 request_id 重送原 payload 不覆蓋編輯、不新增 queue。
	r.owner.send(t, map[string]any{"type": "input", "data": "C", "request_id": "rid-C"})
	r.owner.barrier(t)
	if got := r.queue(t); !reflect.DeepEqual(got, want) {
		t.Fatalf("舊 request 重送覆蓋了編輯或新增 queue：%+v", got)
	}
}

func TestPrivateQueueEditRejectsInvalidAndConflicting(t *testing.T) {
	r := newPrivateEditRig(t)
	same := r.snapshot(t)
	b := r.q[0].ID
	other, err := r.database.CreateSession("別的", "", t.TempDir(), "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := r.database.EnqueueMessage(other.ID, "別人的", "")
	if err != nil {
		t.Fatal(err)
	}
	foreignBefore, err := r.database.ListQueuedMessages(other.ID)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, rid, code string
		msg             map[string]any
	}{
		{"缺 previous", "m1", "invalid", map[string]any{"id": b, "content": "x"}},
		{"缺 content", "m2", "invalid", map[string]any{"id": b, "previous": "B"}},
		{"previous null", "m3", "invalid", map[string]any{"id": b, "previous": nil, "content": "x"}},
		{"content null", "m4", "invalid", map[string]any{"id": b, "previous": "B", "content": nil}},
		{"content 型別錯", "m5", "invalid", map[string]any{"id": b, "previous": "B", "content": 123}},
		{"previous 型別錯", "m6", "invalid", map[string]any{"id": b, "previous": []string{"B"}, "content": "x"}},
		{"id 型別錯", "m7", "invalid", map[string]any{"id": "1", "previous": "B", "content": "x"}},
		{"id 為 0", "m8", "invalid", map[string]any{"id": 0, "previous": "B", "content": "x"}},
		{"id 為負", "m9", "invalid", map[string]any{"id": -3, "previous": "B", "content": "x"}},
		{"NUL", "m10", "invalid", map[string]any{"id": b, "previous": "B", "content": "含\x00字"}},
		{"空白且無附件", "m11", "invalid", map[string]any{"id": b, "previous": "B", "content": " \n"}},
		{"前值變動", "m12", "conflict", map[string]any{"id": b, "previous": "舊稿", "content": "x"}},
		{"不存在 id", "m13", "conflict", map[string]any{"id": 987654, "previous": "B", "content": "x"}},
		{"別的 session 的 id", "m14", "conflict", map[string]any{"id": foreign.ID, "previous": "別人的", "content": "越權"}},
	}
	for _, tc := range cases {
		msg := map[string]any{"type": "queue_edit", "request_id": tc.rid}
		for k, v := range tc.msg {
			msg[k] = v
		}
		r.owner.send(t, msg)
		got := r.owner.await(t, tc.name, resultFor(tc.rid))
		if got["type"] != "queue_edit_error" || got["code"] != tc.code {
			t.Fatalf("%s：%v", tc.name, got)
		}
		if text, _ := got["content"].(string); strings.TrimSpace(text) == "" || strings.Contains(strings.ToLower(text), "sql") {
			t.Fatalf("%s 的錯誤文字：%q", tc.name, text)
		}
	}
	// 沒有 request_id 就無法關聯，也要明確拒絕，不能讓 UI 永久等待。
	before := r.owner.count(isType("queue_edit_error"))
	r.owner.send(t, map[string]any{"type": "queue_edit", "id": b, "previous": "B", "content": "x"})
	waitFor(t, "缺 request_id 的拒絕", func() bool { return r.owner.count(isType("queue_edit_error")) > before })
	if r.owner.count(isType("queue_edit_success")) != 0 {
		t.Fatal("失敗不可回成功")
	}
	same()
	if got, err := r.database.ListQueuedMessages(other.ID); err != nil || !reflect.DeepEqual(got, foreignBefore) {
		t.Fatalf("別的 session 的 queue 被動到：%v", err)
	}
	if r.owner.count(func(m map[string]any) bool { return m["type"] == "queue_update" && m["request_id"] != nil }) != 0 {
		t.Fatal("失敗不可廣播帶 request_id 的 queue_update")
	}
}

func TestPrivateQueueEditOnlyOwnerPrivateSource(t *testing.T) {
	r := newPrivateEditRig(t)
	_, editor := newShare(t, r.database, r.s.ID, db.ShareRoleEditor, db.ShareModeLive, "編輯者", time.Hour)
	_, viewer := newShare(t, r.database, r.s.ID, db.ShareRoleViewer, db.ShareModeLive, "觀眾", time.Hour)
	ec := dialClient(t, r.dial(r.s.ID, editor.Guest.Token))
	vc := dialClient(t, r.dial(r.s.ID, viewer.Guest.Token))
	ec.barrier(t)
	vc.barrier(t)
	ec.send(t, map[string]any{"type": "input", "data": "G 的輸入", "request_id": "rid-G"})
	ec.barrier(t)
	var guestItem db.QueuedMessage
	for _, q := range r.queue(t) {
		if q.Content == "G 的輸入" {
			guestItem = q
		}
	}
	if guestItem.ID == 0 || guestItem.Author == "" {
		t.Fatalf("editor 的輸入應排入且帶暱稱作者：%+v", r.queue(t))
	}
	// 其他來源：room-user／job／report／agent，payload 夾帶假 author／source 也不能變可編。
	seed := map[string]struct{ author, source string }{
		"room-user": {db.RoomUserAuthorPrefix + "r1", ""}, "job": {"room-job:1", ""}, "report": {"room-report:r1", ""}, "agent": {"", r.s.ID},
	}
	ids := map[string]int64{}
	for name, s := range seed {
		var q *db.QueuedMessage
		var err error
		if s.source == "" {
			q, err = r.database.EnqueueMessage(r.s.ID, name, s.author)
		} else {
			q, err = r.database.EnqueueMessageFromCancelQuota(r.s.ID, name, s.author, s.source)
		}
		if err != nil {
			t.Fatal(err)
		}
		ids[name] = q.ID
	}
	same := r.snapshot(t)
	ownerBefore := r.owner.count(isType("queue_update"))
	for name, id := range ids {
		r.owner.send(t, map[string]any{"type": "queue_edit", "id": id, "request_id": "src-" + name, "previous": name, "content": "越權",
			"author": "", "source_session_id": ""})
		if got := r.owner.await(t, name, resultFor("src-"+name)); got["type"] != "queue_edit_error" || got["code"] != "forbidden" {
			t.Fatalf("%s 來源：%v", name, got)
		}
	}
	// guest（含 editor）不能改 owner 或其他 guest 的文字；viewer 由 readLoop 唯讀拒絕。
	for _, tc := range []struct {
		who string
		c   *wsClient
		id  int64
		pre string
	}{{"editor 改 owner", ec, r.q[0].ID, "B"}, {"editor 改自己的輸入", ec, guestItem.ID, "G 的輸入"}} {
		tc.c.send(t, map[string]any{"type": "queue_edit", "id": tc.id, "request_id": "g-" + tc.who, "previous": tc.pre, "content": "越權"})
		if got := tc.c.await(t, tc.who, resultFor("g-"+tc.who)); got["type"] != "queue_edit_error" || got["code"] != "forbidden" {
			t.Fatalf("%s：%v", tc.who, got)
		}
	}
	errs := vc.count(isType("error"))
	vc.send(t, map[string]any{"type": "queue_edit", "id": r.q[0].ID, "request_id": "viewer", "previous": "B", "content": "越權"})
	waitFor(t, "viewer 唯讀錯誤", func() bool { return vc.count(isType("error")) > errs })
	if vc.first(resultFor("viewer")) != nil {
		t.Fatal("viewer 不該進入編輯處理")
	}
	same()
	if r.owner.count(isType("queue_update")) != ownerBefore {
		t.Fatal("拒絕不得廣播 queue_update")
	}
}

// 工作室喚醒連線與 MCP agent 連線不是人類 owner，不能經 WS 改私人 queue。
func TestPrivateQueueEditRejectsInternalConnections(t *testing.T) {
	for _, local := range []string{"room_wake", "mcp_agent"} {
		t.Run(local, func(t *testing.T) {
			database, url := startFlaggedServer(t, local)
			s, err := database.CreateSession("內部", "", t.TempDir(), "default", "claude", nil, "agent")
			if err != nil {
				t.Fatal(err)
			}
			q, err := database.EnqueueMessage(s.ID, "私人", "")
			if err != nil {
				t.Fatal(err)
			}
			c := dialClient(t, url(s.ID))
			c.barrier(t)
			c.send(t, map[string]any{"type": "queue_edit", "id": q.ID, "request_id": "internal", "previous": "私人", "content": "偽造"})
			if got := c.await(t, "內部連線", resultFor("internal")); got["type"] != "queue_edit_error" || got["code"] != "forbidden" {
				t.Fatalf("%v", got)
			}
			if got, err := database.ListQueuedMessages(s.ID); err != nil || len(got) != 1 || got[0].Content != "私人" {
				t.Fatalf("內部連線改了私人 queue：%v %+v", err, got)
			}
		})
	}
}

// 編輯與 RunNow／Promote 共用 dispatchLock：RunNow 持鎖等舊回合收尾時編輯必須排後面，
// 收尾後被取走的項目編輯得到 conflict，沒被取走的項目編輯成功且舊 snapshot 不會覆蓋已送出的 prompt。
func TestPrivateQueueEditSharesDispatchLockWithRunNow(t *testing.T) {
	r := newPrivateEditRig(t)
	c := r.plan("block")
	second := dialClient(t, r.dial(r.s.ID, ""))
	second.barrier(t)
	r.owner.send(t, map[string]any{"type": "queue_run_now", "id": r.q[1].ID})
	select {
	case <-r.a.cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("RunNow 未進入持鎖等待舊回合")
	}
	if dl := dispatchLock(r.s.ID); dl.TryLock() {
		dl.Unlock()
		t.Fatal("RunNow 應持有 dispatchLock")
	}
	second.send(t, map[string]any{"type": "queue_edit", "id": r.q[1].ID, "request_id": "late-C", "previous": "C", "content": "C 被改"})
	second.send(t, map[string]any{"type": "queue_edit", "id": r.q[0].ID, "request_id": "ok-B", "previous": "B", "content": "B 被改"})
	// 先確認 RunNow 持鎖（上面 TryLock 失敗），再給編輯一個機會穿過去；它必須仍在等鎖。
	time.Sleep(50 * time.Millisecond)
	if second.first(resultFor("ok-B")) != nil || second.first(resultFor("late-C")) != nil {
		t.Fatal("編輯穿過 RunNow 持有的 dispatchLock")
	}
	r.a.release()
	readyRunNow(t, c)
	if got := second.await(t, "C 已被取走", resultFor("late-C")); got["type"] != "queue_edit_error" || got["code"] != "conflict" {
		t.Fatalf("RunNow 提升 C 後編輯應 conflict：%v", got)
	}
	if got := second.await(t, "B 仍可編輯", resultFor("ok-B")); got["type"] != "queue_edit_success" {
		t.Fatalf("B：%v", got)
	}
	if !strings.Contains(c.opts.Prompt, "C") || strings.Contains(c.opts.Prompt, "C 被改") {
		t.Fatalf("送出的 prompt 必須是 C 原文：%q", c.opts.Prompt)
	}
	q := r.queue(t)
	if len(q) != 1 || q[0].ID != r.q[0].ID || q[0].Content != "B 被改" {
		t.Fatalf("B 應保留原位且為新文字：%+v", q)
	}
}

// startFlaggedServer 與 startTestServer 相同，但連線帶指定 Locals（room_wake／mcp_agent），模擬內部連線。
func startFlaggedServer(t *testing.T, local string) (*db.DB, func(string) string) {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/ws.db")
	if err != nil {
		t.Fatal(err)
	}
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Get("/sessions/:id/ws", func(c *fiber.Ctx) error {
		c.Locals(local, true)
		return c.Next()
	}, fiberws.New(NewHandler(database, "", ShellOpts{}, nil, tg.NotifyConfig{})))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = app.Listener(ln) }()
	t.Cleanup(func() { _ = app.Shutdown(); database.Close() })
	port := ln.Addr().(*net.TCPAddr).Port
	return database, func(id string) string { return fmt.Sprintf("ws://127.0.0.1:%d/sessions/%s/ws", port, id) }
}

// 無效 UTF-8 bytes 在 JSON 解碼時會被換成 U+FFFD；queue_edit 必須在替換前就拒絕，不能悄悄改字。
func TestPrivateQueueEditRejectsRawInvalidUTF8(t *testing.T) {
	r := newPrivateEditRig(t)
	same := r.snapshot(t)
	c := r.q[1]
	updates := r.owner.count(isType("queue_update"))
	frame := func(rid, previous, content string) []byte {
		return []byte(fmt.Sprintf(`{"type":"queue_edit","id":%d,"request_id":"%s","previous":"%s","content":"%s"}`, c.ID, rid, previous, content))
	}
	for name, raw := range map[string][]byte{
		"content 單 byte 0xff":  frame("u1", "C", "\xff"),
		"content 截斷的多 byte 序列": frame("u2", "C", "好\xe5\xa5"),
		"previous 0xff":        frame("u3", "C\xff", "x"),
		"request_id 0xff":      frame("u4\xff", "C", "x"),
	} {
		before := r.owner.count(isType("queue_edit_error"))
		if err := r.owner.conn.WriteMessage(websocket.TextMessage, raw); err != nil {
			t.Fatal(err)
		}
		waitFor(t, name, func() bool { return r.owner.count(isType("queue_edit_error")) > before })
	}
	for _, rid := range []string{"u1", "u2", "u3"} {
		if got := r.owner.await(t, rid, resultFor(rid)); got["type"] != "queue_edit_error" || got["code"] != "invalid" || got["id"] != float64(c.ID) {
			t.Fatalf("%s：%v", rid, got)
		}
	}
	if r.owner.count(isType("queue_edit_success")) != 0 || r.owner.count(isType("queue_update")) != updates {
		t.Fatal("無效 UTF-8 不得回成功或廣播 queue_update")
	}
	same()
}

// 真正的 U+FFFD 是合法字元，不能因為上面的檢查被禁止（字面字元與 � 跳脫都要能存）。
func TestPrivateQueueEditAllowsLegitimateReplacementCharacter(t *testing.T) {
	r := newPrivateEditRig(t)
	for i, tc := range []struct{ rid, raw, want string }{
		{"f1", "字面�字元", "字面�字元"},
		{"f2", `跳脫�字元`, "跳脫�字元"},
	} {
		q := r.queue(t)
		prev := q[1].Content
		frame := fmt.Sprintf(`{"type":"queue_edit","id":%d,"request_id":%q,"previous":%q,"content":"%s"}`, q[1].ID, tc.rid, prev, tc.raw)
		if err := r.owner.conn.WriteMessage(websocket.TextMessage, []byte(frame)); err != nil {
			t.Fatal(err)
		}
		if got := r.owner.await(t, tc.rid, resultFor(tc.rid)); got["type"] != "queue_edit_success" {
			t.Fatalf("case %d：%v", i, got)
		}
		if got := r.queue(t)[1].Content; got != tc.want {
			t.Fatalf("case %d 存成 %q，應為 %q", i, got, tc.want)
		}
	}
}

// 附件-only：原附件還在，文字可從有字編成空白；其餘一切不變。
func TestPrivateQueueEditAttachmentOnlyBlank(t *testing.T) {
	r := newPrivateEditRig(t)
	if err := r.database.AddAttachment(&db.Attachment{ID: "priv-edit-file", SessionID: r.s.ID, Name: "保留.txt", StorageName: "stored", MimeType: "text/plain", Size: 7}); err != nil {
		t.Fatal(err)
	}
	item, err := r.database.EnqueueMessage(r.s.ID, "有字有附件", "", "priv-edit-file")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.database.SetQueuePaused(r.s.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := r.database.Exec(`UPDATE sessions SET quota_resume_state='waiting',quota_resume_attempts=2,quota_resume_generation=7,quota_resume_at='2099-01-01T00:00:00Z' WHERE id=?`, r.s.ID); err != nil {
		t.Fatal(err)
	}
	beforeQ, beforeM, beforeS := r.queue(t), r.messages(t), r.session(t)
	want := append([]db.QueuedMessage(nil), beforeQ...)
	want[len(want)-1].Content = ""
	r.owner.send(t, map[string]any{"type": "queue_edit", "id": item.ID, "request_id": "att-blank", "previous": "有字有附件", "content": ""})
	if got := r.owner.await(t, "附件-only 空白", resultFor("att-blank")); got["type"] != "queue_edit_success" {
		t.Fatalf("%v", got)
	}
	after := r.queue(t)
	if !reflect.DeepEqual(after, want) || len(after[len(after)-1].Attachments) != 1 || after[len(after)-1].Attachments[0].ID != "priv-edit-file" {
		t.Fatalf("只該清空文字，附件／FIFO 不變：%+v", after)
	}
	if !reflect.DeepEqual(beforeS, r.session(t)) || !reflect.DeepEqual(beforeM, r.messages(t)) {
		t.Fatal("編輯改了 session（paused／quota）或歷史")
	}
	select {
	case <-r.a.cancelled:
		t.Fatal("編輯不得取消 A")
	default:
	}
	// 沒附件的項目編成空白仍然 invalid（既有規則不放寬）。
	r.owner.send(t, map[string]any{"type": "queue_edit", "id": beforeQ[0].ID, "request_id": "no-att-blank", "previous": "B", "content": ""})
	if got := r.owner.await(t, "無附件空白", resultFor("no-att-blank")); got["code"] != "invalid" {
		t.Fatalf("%v", got)
	}
}
