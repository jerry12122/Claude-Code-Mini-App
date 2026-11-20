package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/quota"
)

// fakeQuota 只實作 Get：handler 型別上就碰不到 RefreshManual，等於證明「不觸發刷新」。
type fakeQuota struct {
	snaps map[string]quota.Snapshot
	calls []string
}

func (f *fakeQuota) Get(provider string) quota.Snapshot {
	f.calls = append(f.calls, provider)
	if s, ok := f.snaps[provider]; ok {
		return s
	}
	return quota.Snapshot{Provider: provider, DisplayText: "—"}
}

type roomsEnv struct {
	t     *testing.T
	app   *fiber.App
	db    *db.DB
	quota *fakeQuota
}

func TestRoomHandoffWithExhaustedQuota(t *testing.T) {
	e := newRoomsEnv(t)
	oldPM, nextPM := e.session("沒額度的 PM", "claude"), e.session("接手的 PM", "codex")
	rid := e.create("交接", member{"session_id": oldPM.ID, "role": "pm"}, member{"session_id": nextPM.ID, "role": "worker"})
	e.quota.snaps["claude"] = quota.Snapshot{Provider: "claude", DisplayText: "5h 100%", UpdatedAt: time.Now()}
	if err := e.db.AcceptRoomInput(rid, oldPM.ID, "pending", "額度恢復前請接手", nil); err != nil {
		t.Fatal(err)
	}
	if err := e.db.SetQueuePaused(oldPM.ID, true); err != nil {
		t.Fatal(err)
	}
	status, body := e.do("PUT", "/rooms/"+rid, map[string]any{
		"previous_pm_id": oldPM.ID,
		"members":        []member{{"session_id": oldPM.ID, "role": "worker"}, {"session_id": nextPM.ID, "role": "pm"}},
	})
	if status != 200 || decode(t, body)["pm_id"] != nextPM.ID {
		t.Fatalf("額度用盡不應阻止交接：%d %s", status, body)
	}
	queue, err := e.db.ListQueuedMessages(nextPM.ID)
	if err != nil || len(queue) != 1 || queue[0].Content != "額度恢復前請接手" {
		t.Fatalf("待處理訊息未移交：%+v %v", queue, err)
	}
}

func newRoomsEnv(t *testing.T) *roomsEnv {
	t.Helper()
	database := testDB(t)
	fq := &fakeQuota{snaps: map[string]quota.Snapshot{
		"claude": {Provider: "claude", DisplayText: "5h 40%", Plan: "max", UpdatedAt: time.Now()},
		"codex":  {Provider: "codex", DisplayText: "週 80%", UpdatedAt: time.Now().Add(-time.Hour), Stale: true},
	}}
	h := NewRoomsHandler(database, fq)
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Get("/rooms", h.List)
	app.Post("/rooms", h.Create)
	app.Get("/rooms/:id", h.Get)
	app.Put("/rooms/:id", h.Update)
	app.Delete("/rooms/:id", h.Delete)
	app.Get("/rooms/:id/timeline", h.Timeline)
	return &roomsEnv{t: t, app: app, db: database, quota: fq}
}

func (e *roomsEnv) do(method, path string, body any) (int, []byte) {
	e.t.Helper()
	var rd io.Reader
	if s, ok := body.(string); ok {
		rd = strings.NewReader(s)
	} else if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rd)
	if rd != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := e.app.Test(req, -1)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b
}

func (e *roomsEnv) session(name, agentType string) *db.Session {
	e.t.Helper()
	s, err := e.db.CreateSession(name, "", e.t.TempDir(), "default", agentType, nil, "agent")
	if err != nil {
		e.t.Fatal(err)
	}
	return s
}

func (e *roomsEnv) msg(sessionID, role, content, status, createdAt string) int64 {
	e.t.Helper()
	res, err := e.db.Exec(`INSERT INTO messages (session_id, role, content, status, created_at, author) VALUES (?, ?, ?, ?, ?, 'au')`,
		sessionID, role, content, status, createdAt)
	if err != nil {
		e.t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

type member = map[string]any

func (e *roomsEnv) create(name string, members ...member) string {
	e.t.Helper()
	code, b := e.do("POST", "/rooms", map[string]any{"name": name, "members": members})
	if code != 201 {
		e.t.Fatalf("建立 %d: %s", code, b)
	}
	var out struct{ ID string }
	_ = json.Unmarshal(b, &out)
	return out.ID
}

func decode(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("回應不是 JSON 物件: %v: %.200s", err, b)
	}
	return m
}

func TestRoomsCreateAndGetDetail(t *testing.T) {
	e := newRoomsEnv(t)
	pm := e.session("PM 小隊長", "claude")
	w1 := e.session("執行者一", "codex")
	w2 := e.session("執行者二", "cursor")
	e.db.Exec(`UPDATE sessions SET status = 'running' WHERE id = ?`, w1.ID)

	id := e.create("工作室 A",
		member{"session_id": pm.ID, "role": "pm"},
		member{"session_id": w1.ID, "role": "worker"},
		member{"session_id": w2.ID, "role": "worker"})

	e.quota.calls = nil // 建立時的回應也會讀快取，這裡只數 GET 的
	code, b := e.do("GET", "/rooms/"+id, nil)
	if code != 200 {
		t.Fatalf("GET %d: %s", code, b)
	}
	room := decode(t, b)
	if room["id"] != id || room["name"] != "工作室 A" || room["created_at"] == "" {
		t.Fatalf("room 欄位不符: %v", room)
	}
	members := room["members"].([]any)
	if len(members) != 3 {
		t.Fatalf("members=%d", len(members))
	}
	m0, m1, m2 := members[0].(map[string]any), members[1].(map[string]any), members[2].(map[string]any)
	if m0["session_id"] != pm.ID || m0["role"] != "pm" || m0["session_name"] != "PM 小隊長" || m0["agent_type"] != "claude" || m0["status"] != "idle" {
		t.Errorf("m0=%v", m0)
	}
	if m1["status"] != "running" || m1["agent_type"] != "codex" || m1["role"] != "worker" {
		t.Errorf("m1=%v", m1)
	}

	// 額度：新鮮、過期、完全沒快取三種都要帶 stale 欄位（含 false）
	q0 := m0["quota"].(map[string]any)
	if q0["display_text"] != "5h 40%" || q0["plan"] != "max" || q0["stale"] != false {
		t.Errorf("claude quota=%v", q0)
	}
	if q1 := m1["quota"].(map[string]any); q1["display_text"] != "週 80%" || q1["stale"] != true {
		t.Errorf("codex quota=%v", q1)
	}
	if q2 := m2["quota"].(map[string]any); q2["display_text"] != "—" || q2["stale"] != true {
		t.Errorf("沒有快取的 provider 應標 stale=true，got %v", q2)
	}
	if fmt.Sprint(e.quota.calls) != "[claude codex cursor]" {
		t.Errorf("只應各讀一次快取，calls=%v", e.quota.calls)
	}
}

func TestRoomsCreateValidation(t *testing.T) {
	e := newRoomsEnv(t)
	a := e.session("A", "claude")
	ghost := "ghost-session"
	many := make([]member, 0, 51)
	for i := 0; i < 51; i++ {
		many = append(many, member{"session_id": fmt.Sprintf("s%d", i), "role": "worker"})
	}
	tests := []struct {
		name     string
		body     any
		wantCode int
		wantErr  string
	}{
		{"名稱空字串", map[string]any{"name": "", "members": []member{}}, 400, "名稱"},
		{"名稱全空白", map[string]any{"name": "  \t ", "members": []member{}}, 400, "名稱"},
		{"沒有名稱欄位", map[string]any{"members": []member{}}, 400, "名稱"},
		{"名稱過長", map[string]any{"name": strings.Repeat("長", 101)}, 400, "名稱"},
		{"role 不合法", map[string]any{"name": "r", "members": []member{{"session_id": a.ID, "role": "boss"}}}, 400, "role"},
		{"role 大小寫不同", map[string]any{"name": "r", "members": []member{{"session_id": a.ID, "role": "PM"}}}, 400, "role"},
		{"role 缺漏", map[string]any{"name": "r", "members": []member{{"session_id": a.ID}}}, 400, "role"},
		{"session_id 空", map[string]any{"name": "r", "members": []member{{"session_id": " ", "role": "pm"}}}, 400, "session_id"},
		{"成員 session 不存在", map[string]any{"name": "r", "members": []member{{"session_id": a.ID, "role": "pm"}, {"session_id": ghost, "role": "worker"}}}, 400, ghost},
		{"重複成員", map[string]any{"name": "r", "members": []member{{"session_id": a.ID, "role": "pm"}, {"session_id": a.ID, "role": "worker"}}}, 400, "重複"},
		{"成員過多", map[string]any{"name": "r", "members": many}, 400, "上限"},
		{"壞 JSON", "{not json", 400, ""},
	}
	for _, tc := range tests {
		code, b := e.do("POST", "/rooms", tc.body)
		if code != tc.wantCode {
			t.Errorf("%s: code=%d，預期 %d: %s", tc.name, code, tc.wantCode, b)
			continue
		}
		if tc.wantErr != "" && !strings.Contains(string(b), tc.wantErr) {
			t.Errorf("%s: 錯誤訊息應含 %q: %s", tc.name, tc.wantErr, b)
		}
	}
	if rooms, _ := e.db.ListRooms(); len(rooms) != 0 {
		t.Errorf("所有失敗的建立都不該留下工作室，got %d", len(rooms))
	}
}

func TestRoomsCreateTrimsNameAndAllowsEmptyMembers(t *testing.T) {
	e := newRoomsEnv(t)
	id := e.create("  空的  ")
	_, b := e.do("GET", "/rooms/"+id, nil)
	room := decode(t, b)
	if room["name"] != "空的" {
		t.Errorf("名稱應去除前後空白，got %q", room["name"])
	}
	if ms, ok := room["members"].([]any); !ok || len(ms) != 0 {
		t.Errorf("members 應為空陣列而非 null: %v", room["members"])
	}
}

func TestRoomsListUpdateDelete(t *testing.T) {
	e := newRoomsEnv(t)
	a := e.session("A", "claude")
	b := e.session("B", "claude")
	id := e.create("舊名", member{"session_id": a.ID, "role": "pm"})

	code, body := e.do("GET", "/rooms", nil)
	var list []map[string]any
	_ = json.Unmarshal(body, &list)
	if code != 200 || len(list) != 1 || list[0]["id"] != id || list[0]["name"] != "舊名" || list[0]["member_count"] != float64(1) {
		t.Fatalf("list: %d %s", code, body)
	}

	// 改名 + 換成員與角色
	code, body = e.do("PUT", "/rooms/"+id, map[string]any{"name": " 新名 ", "members": []member{
		{"session_id": b.ID, "role": "pm"}, {"session_id": a.ID, "role": "worker"},
	}})
	if code != 200 {
		t.Fatalf("PUT %d: %s", code, body)
	}
	room := decode(t, body)
	ms := room["members"].([]any)
	if room["name"] != "新名" || len(ms) != 2 || ms[0].(map[string]any)["session_id"] != b.ID || ms[0].(map[string]any)["role"] != "pm" || ms[1].(map[string]any)["role"] != "worker" {
		t.Fatalf("PUT 結果不符: %s", body)
	}

	// 只改名
	if code, _ = e.do("PUT", "/rooms/"+id, map[string]any{"name": "只改名"}); code != 200 {
		t.Fatalf("只改名 %d", code)
	}
	_, body = e.do("GET", "/rooms/"+id, nil)
	if r := decode(t, body); r["name"] != "只改名" || len(r["members"].([]any)) != 2 {
		t.Errorf("只改名不該動成員: %s", body)
	}

	// 已配置的工作室需先交接 PM，不能清空全部成員。
	if code, _ = e.do("PUT", "/rooms/"+id, map[string]any{"members": []member{}}); code != 409 {
		t.Fatalf("清空成員應被拒絕，狀態 %d", code)
	}
	_, body = e.do("GET", "/rooms/"+id, nil)
	if r := decode(t, body); len(r["members"].([]any)) != 2 || r["pm_id"] != b.ID {
		t.Errorf("清空失敗應保留成員與 PM: %s", body)
	}

	// 失敗案例：原狀不變
	for name, req := range map[string]map[string]any{
		"空名稱":         {"name": " "},
		"壞 role":      {"members": []member{{"session_id": a.ID, "role": "x"}}},
		"重複成員":        {"members": []member{{"session_id": a.ID, "role": "pm"}, {"session_id": a.ID, "role": "pm"}}},
		"session 不存在": {"members": []member{{"session_id": "ghost", "role": "pm"}}},
		"沒有任何欄位":      {},
	} {
		if code, b := e.do("PUT", "/rooms/"+id, req); code != 400 {
			t.Errorf("PUT %s: code=%d，預期 400: %s", name, code, b)
		}
	}
	_, body = e.do("GET", "/rooms/"+id, nil)
	if r := decode(t, body); r["name"] != "只改名" || len(r["members"].([]any)) != 2 {
		t.Errorf("失敗的更新不該改動任何東西: %s", body)
	}

	// 刪除工作室：204，且 session 與其訊息都還在
	e.msg(a.ID, "user", "keep", "done", "2026-11-18 10:00:00")
	if code, _ = e.do("DELETE", "/rooms/"+id, nil); code != 204 {
		t.Fatalf("DELETE %d", code)
	}
	if _, err := e.db.GetSession(a.ID); err != nil {
		t.Errorf("刪除工作室不該刪 session: %v", err)
	}
	if msgs, _ := e.db.ListMessages(a.ID); len(msgs) != 1 {
		t.Errorf("刪除工作室不該刪訊息: %d", len(msgs))
	}
	if code, _ = e.do("GET", "/rooms/"+id, nil); code != 404 {
		t.Errorf("刪除後 GET=%d，預期 404", code)
	}
}

func TestRoomsUnknownRoomIs404(t *testing.T) {
	e := newRoomsEnv(t)
	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{"GET", "/rooms/nope", nil},
		{"PUT", "/rooms/nope", map[string]any{"name": "x"}},
		{"DELETE", "/rooms/nope", nil},
		{"GET", "/rooms/nope/timeline", nil},
	} {
		if code, b := e.do(tc.method, tc.path, tc.body); code != 404 {
			t.Errorf("%s %s = %d，預期 404: %s", tc.method, tc.path, code, b)
		}
	}
}

func TestRoomsMemberSessionDeletedKeepsRoomUsable(t *testing.T) {
	e := newRoomsEnv(t)
	a := e.session("A", "claude")
	b := e.session("B", "codex")
	e.msg(a.ID, "user", "from-a", "done", "2026-11-18 10:00:00")
	e.msg(b.ID, "claude", "from-b", "done", "2026-11-18 10:00:01")
	id := e.create("r", member{"session_id": a.ID, "role": "pm"}, member{"session_id": b.ID, "role": "worker"})

	if err := e.db.DeleteSession(b.ID); err != nil {
		t.Fatal(err)
	}
	code, body := e.do("GET", "/rooms/"+id, nil)
	if code != 200 || len(decode(t, body)["members"].([]any)) != 1 {
		t.Fatalf("成員清單應剩 1 人: %d %s", code, body)
	}
	code, body = e.do("GET", "/rooms/"+id+"/timeline", nil)
	tl := decode(t, body)
	msgs, _ := tl["messages"].([]any)
	if code != 200 || len(msgs) != 1 || msgs[0].(map[string]any)["text"] != "from-a" {
		t.Fatalf("時間軸應只剩 A 的訊息: %d %s", code, body)
	}
}

func TestRoomsTimelineShapeAndMerge(t *testing.T) {
	e := newRoomsEnv(t)
	a := e.session("PM", "claude")
	b := e.session("W", "codex")
	e.msg(a.ID, "user", "q", "done", "2026-11-18 10:00:00")
	e.msg(b.ID, "claude", "ans", "done", "2026-11-18 10:00:00")
	e.msg(b.ID, db.RoleShell, "ls output", "done", "2026-11-18 10:00:01")
	id := e.create("r", member{"session_id": a.ID, "role": "pm"}, member{"session_id": b.ID, "role": "worker"})

	code, body := e.do("GET", "/rooms/"+id+"/timeline", nil)
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	tl := decode(t, body)
	msgs := tl["messages"].([]any)
	if len(msgs) != 2 || tl["has_more"] != false {
		t.Fatalf("messages=%d has_more=%v（shell 訊息應被排除）", len(msgs), tl["has_more"])
	}
	top := msgs[0].(map[string]any) // 同秒：id 較大的在前
	if top["text"] != "ans" || top["session_id"] != b.ID || top["session_name"] != "W" || top["member_role"] != "worker" ||
		top["agent_type"] != "codex" || top["role"] != "claude" || top["author"] != "au" || top["created_at"] != "2026-11-18 10:00:00" || top["status"] != "done" {
		t.Errorf("欄位不符: %v", top)
	}
	if _, ok := top["id"].(float64); !ok {
		t.Errorf("缺少訊息 id: %v", top)
	}
	if next, _ := tl["next_cursor"].(string); next != "" {
		t.Errorf("沒有更早訊息時 next_cursor 應為空，got %q", next)
	}
}

func TestRoomsTimelineLimitDefaultAndCap(t *testing.T) {
	e := newRoomsEnv(t)
	a := e.session("A", "claude")
	id := e.create("r", member{"session_id": a.ID, "role": "pm"})
	tx, _ := e.db.Begin()
	for i := 0; i < 230; i++ {
		tx.Exec(`INSERT INTO messages (session_id, role, content, status, created_at) VALUES (?, 'user', ?, 'done', ?)`,
			a.ID, fmt.Sprintf("m%d", i), fmt.Sprintf("2026-11-18 10:%02d:%02d", i/60, i%60))
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	count := func(q string) (int, bool) {
		code, body := e.do("GET", "/rooms/"+id+"/timeline"+q, nil)
		if code != 200 {
			t.Fatalf("%q → %d %s", q, code, body)
		}
		tl := decode(t, body)
		return len(tl["messages"].([]any)), tl["has_more"].(bool)
	}
	if n, more := count(""); n != 50 || !more {
		t.Errorf("預設 limit：n=%d more=%v，預期 50/true", n, more)
	}
	if n, more := count("?limit=10"); n != 10 || !more {
		t.Errorf("limit=10：n=%d more=%v", n, more)
	}
	if n, more := count("?limit=200"); n != 200 || !more {
		t.Errorf("limit=200：n=%d more=%v", n, more)
	}
	if n, more := count("?limit=9999"); n != 200 || !more {
		t.Errorf("limit 超過上限應夾到 200：n=%d more=%v", n, more)
	}
	for _, bad := range []string{"?limit=0", "?limit=-1", "?limit=abc"} {
		if code, _ := e.do("GET", "/rooms/"+id+"/timeline"+bad, nil); code != 400 {
			t.Errorf("%s 應 400，got %d", bad, code)
		}
	}
}

func TestRoomsTimelineCursorPagingOverHTTP(t *testing.T) {
	e := newRoomsEnv(t)
	ss := []*db.Session{e.session("A", "claude"), e.session("B", "codex"), e.session("C", "cursor")}
	var want []int64
	for i := 0; i < 25; i++ {
		id := e.msg(ss[(i*5)%3].ID, "user", fmt.Sprintf("m%d", i), "done", fmt.Sprintf("2026-11-18 10:00:%02d", i/4))
		want = append([]int64{id}, want...)
	}
	rid := e.create("r", member{"session_id": ss[0].ID, "role": "pm"}, member{"session_id": ss[1].ID, "role": "worker"}, member{"session_id": ss[2].ID, "role": "worker"})

	var got []int64
	cursor := ""
	for page := 0; page < 20; page++ {
		path := "/rooms/" + rid + "/timeline?limit=4"
		if cursor != "" {
			path += "&before=" + url.QueryEscape(cursor)
		}
		code, body := e.do("GET", path, nil)
		if code != 200 {
			t.Fatalf("%d %s", code, body)
		}
		tl := decode(t, body)
		for _, m := range tl["messages"].([]any) {
			got = append(got, int64(m.(map[string]any)["id"].(float64)))
		}
		if tl["has_more"] != true {
			break
		}
		cursor, _ = tl["next_cursor"].(string)
		if cursor == "" {
			t.Fatal("has_more=true 但 next_cursor 為空")
		}
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("分頁串起來應等於完整排序:\n got=%v\nwant=%v", got, want)
	}

	for _, bad := range []string{"garbage", "!!!", "YWJj"} { // 非 base64／不是游標格式
		if code, _ := e.do("GET", "/rooms/"+rid+"/timeline?before="+url.QueryEscape(bad), nil); code != 400 {
			t.Errorf("before=%q 應 400，got %d", bad, code)
		}
	}
}

func TestRoomsTimelinePendingUsesOriginalID(t *testing.T) {
	e := newRoomsEnv(t)
	a := e.session("A", "claude")
	rid := e.create("r", member{"session_id": a.ID, "role": "pm"})
	pid, err := e.db.CreatePendingMessage(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	read := func() []any {
		_, body := e.do("GET", "/rooms/"+rid+"/timeline", nil)
		return decode(t, body)["messages"].([]any)
	}
	msgs := read()
	if len(msgs) != 1 || msgs[0].(map[string]any)["status"] != "pending" || int64(msgs[0].(map[string]any)["id"].(float64)) != pid {
		t.Fatalf("pending 應以原 id 出現: %v", msgs)
	}
	_ = e.db.AppendMessageContent(pid, "串流中")
	_ = e.db.FinalizeMessage(pid)
	msgs = read()
	m := msgs[0].(map[string]any)
	if len(msgs) != 1 || int64(m["id"].(float64)) != pid || m["text"] != "串流中" || m["status"] != "done" {
		t.Fatalf("同一 id 就地更新、不重複: %v", msgs)
	}
}

func TestRoomsTimelineMemberFilterOverHTTP(t *testing.T) {
	e := newRoomsEnv(t)
	pm, wa, wb := e.session("PM", "claude"), e.session("同名", "codex"), e.session("同名", "codex")
	outside := e.session("別間", "claude")
	rid := e.create("r", member{"session_id": pm.ID, "role": "pm"}, member{"session_id": wa.ID, "role": "worker"}, member{"session_id": wb.ID, "role": "worker"})
	e.create("別間", member{"session_id": outside.ID, "role": "pm"})
	for i := 0; i < 5; i++ {
		e.msg(wa.ID, "claude", fmt.Sprintf("a%d", i), "done", fmt.Sprintf("2026-11-18 10:00:0%d", i))
	}
	for i := 0; i < 8; i++ {
		e.msg(wb.ID, "claude", fmt.Sprintf("b%d", i), "done", fmt.Sprintf("2026-11-18 10:01:0%d", i))
	}
	get := func(q string) (int, map[string]any) {
		code, body := e.do("GET", "/rooms/"+rid+"/timeline"+q, nil)
		if code != 200 {
			return code, nil
		}
		return code, decode(t, body)
	}
	// 相關訊息全在最新 8 則之外；分頁仍要走完 A 的完整歷史
	var texts []string
	q := "?member=" + wa.ID + "&limit=2"
	for pages := 0; ; pages++ {
		code, tl := get(q)
		if code != 200 || pages > 5 {
			t.Fatalf("code=%d pages=%d", code, pages)
		}
		for _, m := range tl["messages"].([]any) {
			mm := m.(map[string]any)
			if mm["session_id"] != wa.ID {
				t.Fatalf("leaked %v", mm)
			}
			texts = append(texts, mm["text"].(string))
		}
		if tl["has_more"] != true {
			break
		}
		q = "?member=" + wa.ID + "&limit=2&before=" + url.QueryEscape(tl["next_cursor"].(string))
	}
	if strings.Join(texts, ",") != "a4,a3,a2,a1,a0" {
		t.Fatalf("texts=%v", texts)
	}
	if code, tl := get("?member="); code != 200 || len(tl["messages"].([]any)) != 13 {
		t.Fatalf("空 member 等於總覽：%d", code)
	}
	for _, bad := range []string{outside.ID, "no-such", url.QueryEscape("x' OR 1=1 --")} {
		code, body := e.do("GET", "/rooms/"+rid+"/timeline?member="+bad, nil)
		if code != 404 || !strings.Contains(string(body), "error") {
			t.Fatalf("member=%s → %d %s，非成員要 404", bad, code, body)
		}
	}
	if code, _ := e.do("GET", "/rooms/nope/timeline?member="+wa.ID, nil); code != 404 {
		t.Fatalf("missing room = %d", code)
	}
}
