package server

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/spf13/viper"
)

var roomEditRuns atomic.Int32

type roomEditFake struct{}

func (roomEditFake) Name() string { return "room-edit-fake" }
func (roomEditFake) Run(context.Context, agent.RunOptions, agent.EventCallback) error {
	roomEditRuns.Add(1)
	return fmt.Errorf("編輯不可啟動 runner")
}
func init() { agent.Register("room-edit-fake", func() agent.Runner { return roomEditFake{} }) }

type roomEditHTTP struct {
	t                             *testing.T
	d                             *db.DB
	base, token, room, pm, worker string
	client                        *http.Client
}

func newRoomEditHTTP(t *testing.T) *roomEditHTTP {
	t.Helper()
	t.Setenv("MINIAPP_TEST_LOOPBACK", "1")
	password := rand.Text()
	base := startTestServer(t, "web:\n  password: "+password+"\n")
	database, err := db.Open(viper.GetString("db.path"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	session := func(name string) string {
		s, err := database.CreateSession(name, "", t.TempDir(), "default", "room-edit-fake", nil, "agent")
		if err != nil {
			t.Fatal(err)
		}
		return s.ID
	}
	pm, worker := session("PM"), session("worker")
	room, err := database.CreateRoom("佇列編輯", []db.RoomMember{{SessionID: pm, Role: db.RoomRolePM}, {SessionID: worker, Role: db.RoomRoleWorker}})
	if err != nil {
		t.Fatal(err)
	}
	e := &roomEditHTTP{t: t, d: database, base: base, room: room.ID, pm: pm, worker: worker, client: &http.Client{Timeout: 5 * time.Second}}
	status, body := e.request("POST", "/auth/login", map[string]string{"password": password}, "")
	var login struct{ Token string }
	if status != 200 || json.Unmarshal(body, &login) != nil || login.Token == "" {
		t.Fatal("測試登入失敗")
	}
	e.token = login.Token
	return e
}

func (e *roomEditHTTP) request(method, path string, payload any, token string) (int, []byte) {
	e.t.Helper()
	var data []byte
	if raw, ok := payload.([]byte); ok {
		data = raw
	} else {
		var err error
		data, err = json.Marshal(payload)
		if err != nil {
			e.t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, e.base+path, bytes.NewReader(data))
	if err != nil {
		e.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		e.t.Fatal(err)
	}
	return resp.StatusCode, body
}

func TestRoomQueueEditHTTP(t *testing.T) {
	e := newRoomEditHTTP(t)
	runsBefore := roomEditRuns.Load()
	if err := e.d.AddAttachment(&db.Attachment{ID: "room-edit-file", SessionID: e.pm, Name: "保留.txt", StorageName: "stored", MimeType: "text/plain", Size: 7}); err != nil {
		t.Fatal(err)
	}
	ids := []string{"room-edit-file"}
	if err := e.d.AcceptRoomInput(e.room, e.pm, "original-request", "原訊息", ids); err != nil {
		t.Fatal(err)
	}
	if _, err := e.d.EnqueueMessage(e.pm, "下一筆私人", ""); err != nil {
		t.Fatal(err)
	}
	if err := e.d.SetQueuePaused(e.pm, true); err != nil {
		t.Fatal(err)
	}
	items, err := e.d.ListQueuedMessages(e.pm)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.d.Exec(`UPDATE sessions SET quota_resume_state='waiting',quota_resume_attempts=2,quota_resume_generation=7,quota_resume_at='2099-01-01T00:00:00Z' WHERE id=?`, e.pm); err != nil {
		t.Fatal(err)
	}
	stateBefore, err := e.d.GetSession(e.pm)
	if err != nil {
		t.Fatal(err)
	}
	want := append([]db.QueuedMessage(nil), items...)
	want[0].Content = "  新訊息\n保留空白  "
	path := fmt.Sprintf("/rooms/%s/messages/%d", e.room, items[0].ID)
	status, body := e.request("PATCH", path, map[string]string{"expected_pm_id": e.pm, "previous": "原訊息", "content": want[0].Content}, e.token)
	if status != 204 {
		t.Fatalf("已登入 owner PATCH 應成功 204，得到 %d", status)
	}
	items, err = e.d.ListQueuedMessages(e.pm)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(items, want) || len(body) != 0 {
		t.Fatal("真 HTTP 應只原地修改 content；保留 FIFO／附件，回204空body")
	}
	stateAfter, err := e.d.GetSession(e.pm)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stateBefore, stateAfter) {
		t.Fatal("編輯改變paused／quota／session狀態")
	}
	status, _ = e.request("PATCH", path, map[string]string{"expected_pm_id": e.pm, "previous": "原訊息", "content": "另一裝置舊稿"}, e.token)
	if status != 409 {
		t.Fatalf("CAS應409：%d", status)
	}
	status, _ = e.request("POST", "/rooms/"+e.room+"/messages", map[string]any{"pm_id": e.pm, "request_id": "original-request", "text": "原訊息", "attachment_ids": ids}, e.token)
	if status != 202 {
		t.Fatalf("原payload重送：%d", status)
	}
	status, _ = e.request("POST", "/rooms/"+e.room+"/messages", map[string]any{"pm_id": e.pm, "request_id": "original-request", "text": want[0].Content, "attachment_ids": ids}, e.token)
	if status != 409 {
		t.Fatalf("新payload不可改receipt：%d", status)
	}
	items, err = e.d.ListQueuedMessages(e.pm)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(items, want) {
		t.Fatal("原重送覆寫編輯或新增queue")
	}
	status, _ = e.request("PATCH", path, map[string]string{"expected_pm_id": e.pm, "previous": want[0].Content, "content": ""}, e.token)
	if status != 204 {
		t.Fatalf("原附件存在時可空文字：%d", status)
	}
	items, err = e.d.ListQueuedMessages(e.pm)
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.d.PromoteQueuedMessage(e.pm, items[0])
	if err != nil {
		t.Fatal(err)
	}
	status, _ = e.request("PATCH", path, map[string]string{"expected_pm_id": e.pm, "previous": "", "content": "已取走"}, e.token)
	if status != 409 {
		t.Fatalf("出列應409：%d", status)
	}
	if roomEditRuns.Load() != runsBefore {
		t.Fatal("PATCH啟動了fake runner")
	}
	parsed, _ := url.Parse(e.base)
	if parsed.Hostname() != "127.0.0.1" || parsed.Port() == "8080" {
		t.Fatal("測試未隔離loopback")
	}
	t.Logf("HTTP證據：pid=%d address=%s auth=owner success=204 CAS=409 popped=409 fake_runner_starts=0", os.Getpid(), parsed.Host)
}

func TestRoomQueueEditHTTPAuthScopeAndHandoff(t *testing.T) {
	e := newRoomEditHTTP(t)
	if err := e.d.AcceptRoomInput(e.room, e.pm, "scope-request", "原文", nil); err != nil {
		t.Fatal(err)
	}
	if err := e.d.SetQueuePaused(e.pm, true); err != nil {
		t.Fatal(err)
	}
	queue, err := e.d.ListQueuedMessages(e.pm)
	if err != nil {
		t.Fatal(err)
	}
	id := queue[0].ID
	path := fmt.Sprintf("/rooms/%s/messages/%d", e.room, id)
	valid := map[string]string{"expected_pm_id": e.pm, "previous": "原文", "content": "修改"}
	if status, _ := e.request("PATCH", path, valid, ""); status != 401 {
		t.Fatalf("未登入：%d", status)
	}
	share, err := e.d.CreateShare(e.pm, db.ShareRoleEditor, db.ShareModeLive, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	guest, err := e.d.TryJoin(share.Token, share.PIN, "fixture-guest")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(valid)
	req, _ := http.NewRequest("PATCH", e.base+path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Share-Token", guest.Guest.Token)
	res, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatalf("editor guest不可改room：%d", res.StatusCode)
	}
	for _, tc := range []struct{ author, source string }{{"", ""}, {"room-user:foreign-room", ""}, {"room-job:1", ""}, {"room-report:" + e.room, ""}, {"", e.worker}, {db.RoomUserAuthorPrefix + e.room, e.worker}} {
		var q *db.QueuedMessage
		if tc.source == "" {
			q, err = e.d.EnqueueMessage(e.pm, "隔離", tc.author)
		} else {
			q, err = e.d.EnqueueMessageFromCancelQuota(e.pm, "隔離", tc.author, tc.source)
		}
		if err != nil {
			t.Fatal(err)
		}
		before, err := e.d.ListQueuedMessages(e.pm)
		if err != nil {
			t.Fatal(err)
		}
		p := fmt.Sprintf("/rooms/%s/messages/%d", e.room, q.ID)
		status, _ := e.request("PATCH", p, map[string]string{"expected_pm_id": e.pm, "previous": "隔離", "content": "越權"}, e.token)
		if status != 404 {
			t.Fatalf("私人／外room／job／report／agent來源統一404：%d", status)
		}
		after, err := e.d.ListQueuedMessages(e.pm)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, after) {
			t.Fatal("拒絕來源時改了queue")
		}
	}
	if status, _ := e.request("PATCH", "/rooms/absent/messages/1", valid, e.token); status != 404 {
		t.Fatalf("無room：%d", status)
	}
	members := []db.RoomMember{{SessionID: e.pm, Role: db.RoomRoleWorker}, {SessionID: e.worker, Role: db.RoomRolePM}}
	if err := e.d.UpdateRoom(e.room, nil, &members, e.pm); err != nil {
		t.Fatal(err)
	}
	if err := e.d.SetQueuePaused(e.worker, true); err != nil {
		t.Fatal(err)
	}
	if status, _ := e.request("PATCH", path, valid, e.token); status != 409 {
		t.Fatalf("舊PM應409：%d", status)
	}
	valid["expected_pm_id"] = e.worker
	if status, _ := e.request("PATCH", path, valid, e.token); status != 204 {
		t.Fatalf("新PM應可編輯：%d", status)
	}
}

func TestRoomQueueEditHTTPContentLimits(t *testing.T) {
	e := newRoomEditHTTP(t)
	if err := e.d.AcceptRoomInput(e.room, e.pm, "limits", "原", nil); err != nil {
		t.Fatal(err)
	}
	if err := e.d.SetQueuePaused(e.pm, true); err != nil {
		t.Fatal(err)
	}
	queue, err := e.d.ListQueuedMessages(e.pm)
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/rooms/%s/messages/%d", e.room, queue[0].ID)
	for _, content := range []string{"", " \n", strings.Repeat("x", 64001), "含\x00字"} {
		status, _ := e.request("PATCH", path, map[string]string{"expected_pm_id": e.pm, "previous": "原", "content": content}, e.token)
		if status != 400 {
			t.Fatalf("無效新文字應400，bytes=%d status=%d", len(content), status)
		}
	}
	large := strings.Repeat("x", 64000)
	if status, _ := e.request("PATCH", path, map[string]string{"expected_pm_id": e.pm, "previous": "原", "content": large}, e.token); status != 204 {
		t.Fatalf("合法64000bytes不裁切：%d", status)
	}
	// 全站 envelope 上限（server.max_message_bytes，預設 apiBodyLimit）原樣保留：Content-Length 超過就在讀 body 前回 413 並關線。
	// 不能用 http.Client 送滿超大 body：伺服器回 413 就關連線，客戶端還在寫就會得到 wsasend／connection reset，
	// 讀不到狀態碼（既有 TestHTTPBodyLimits 同理，用原始 TCP 只送一小段）。
	conn, err := net.DialTimeout("tcp", strings.TrimPrefix(e.base, "http://"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	fmt.Fprintf(conn, "PATCH %s HTTP/1.1\r\nHost: localhost\r\nAuthorization: Bearer %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n", path, e.token, apiBodyLimit+1)
	_, _ = conn.Write(bytes.Repeat([]byte("x"), 8<<10))
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 413 {
		t.Fatalf("envelope應413：%d", resp.StatusCode)
	}
	// 上限內的整包（小 content，其他欄位讓整包變大）不被 413；content 規則仍只看 content。
	status, _ := e.request("PATCH", path, map[string]string{"expected_pm_id": e.pm, "previous": large, "content": "y", "extra": strings.Repeat("x", 1<<20)}, e.token)
	if status != 204 {
		t.Fatalf("上限內的大 envelope：%d", status)
	}
	queue, err = e.d.ListQueuedMessages(e.pm)
	if err != nil || len(queue) != 1 || queue[0].Content != "y" {
		t.Fatalf("413 不得改 queue，其後合法編輯才生效：%v %+v", err, queue)
	}
}

// PM 決策：不存在、別的 session 的 id、已出列、CAS 不符都統一 409，且不動任何佇列。
func TestRoomQueueEditHTTPUnknownOrOtherSessionID409(t *testing.T) {
	e := newRoomEditHTTP(t)
	if err := e.d.AcceptRoomInput(e.room, e.pm, "mine", "我的", nil); err != nil {
		t.Fatal(err)
	}
	other, err := e.d.EnqueueMessage(e.worker, "別人的", db.RoomUserAuthorPrefix+e.room)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := func() ([]db.QueuedMessage, []db.QueuedMessage) {
		a, err := e.d.ListQueuedMessages(e.pm)
		if err != nil {
			t.Fatal(err)
		}
		b, err := e.d.ListQueuedMessages(e.worker)
		if err != nil {
			t.Fatal(err)
		}
		return a, b
	}
	pmBefore, workerBefore := snapshot()
	for name, id := range map[string]int64{"不存在": 987654, "別的 session": other.ID} {
		path := fmt.Sprintf("/rooms/%s/messages/%d", e.room, id)
		status, _ := e.request("PATCH", path, map[string]string{"expected_pm_id": e.pm, "previous": "別人的", "content": "越權"}, e.token)
		if status != 409 {
			t.Fatalf("%s應409：%d", name, status)
		}
	}
	pmAfter, workerAfter := snapshot()
	if !reflect.DeepEqual(pmBefore, pmAfter) || !reflect.DeepEqual(workerBefore, workerAfter) {
		t.Fatal("409 不得改任何 session 的佇列")
	}
}
