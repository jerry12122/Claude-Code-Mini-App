package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/fasthttp/websocket"
	fiberws "github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/api"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/media"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/tg"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/ws"
)

// guestEnv 以和 Start() 相同的方式掛 guestAuth，再接上真正的 handler。
// 「擁有者」在這裡用「沒帶 guest token」代表（Start() 內走 TG／Web 驗證，與訪客分支無關）。
type guestEnv struct {
	t        *testing.T
	database *db.DB
	base     string
	wsBase   string
	sessions *api.SessionHandler // 測試可注入 TelegramHistory
}

func newGuestEnv(t *testing.T) *guestEnv {
	t.Helper()
	old := media.WorkspaceDir
	media.WorkspaceDir = t.TempDir()
	t.Cleanup(func() { media.WorkspaceDir = old })

	database, err := db.Open(t.TempDir() + "/guest.db")
	if err != nil {
		t.Fatal(err)
	}
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	auth := func(c *fiber.Ctx) error {
		if tok := guestToken(c); tok != "" {
			return guestAuth(database, c, tok)
		}
		return c.Next() // 擁有者
	}
	shareH := api.NewShareHandler(database, api.ShareHooks{Kick: ws.KickShare, Online: ws.ShareOnline}, os.DirFS("../static"))
	sh := api.NewSessionHandler(database)
	uh := api.NewUploadHandler(database)
	app.Get("/sessions/:id/messages", auth, sh.Messages)
	app.Patch("/sessions/:id", auth, sh.Patch)
	app.Delete("/sessions/:id", auth, sh.Delete)
	app.Get("/sessions", auth, sh.List)
	app.Get("/model-options/:agentType", auth, sh.ModelOptions)
	app.Post("/sessions/:id/uploads", auth, uh.Upload)
	app.Get("/sessions/:id/uploads/:attachmentId/details", auth, uh.Details)
	app.Get("/sessions/:id/uploads/:attachmentId", auth, uh.Content)
	app.Get("/sessions/:id/tg-media/:msgId", auth, sh.TelegramMediaHTTP)
	app.Post("/sessions/:id/shares", auth, shareH.Create)
	app.Get("/sessions/:id/shares", auth, shareH.ListBySession)
	app.Get("/shares", auth, shareH.ListAll)
	app.Delete("/shares/ended", auth, shareH.ClearEnded)
	app.Delete("/shares/ended/:id", auth, shareH.ClearEnded)
	app.Delete("/shares/:id", auth, shareH.Revoke)
	app.Delete("/shares", auth, shareH.RevokeAll)
	app.Get("/guest/me", auth, shareH.Me)
	app.Post("/share/:token/join", api.JoinLimiter(), shareH.Join)
	app.Use("/sessions/:id/ws", func(c *fiber.Ctx) error {
		if fiberws.IsWebSocketUpgrade(c) {
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})
	app.Get("/sessions/:id/ws", auth, fiberws.New(ws.NewHandler(database, "", ws.ShellOpts{}, nil, tg.NotifyConfig{})))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = app.Listener(ln) }()
	t.Cleanup(func() { _ = app.Shutdown(); database.Close() })
	addr := ln.Addr().String()
	return &guestEnv{t: t, database: database, base: "http://" + addr, wsBase: "ws://" + addr, sessions: sh}
}

func (e *guestEnv) session(name string) *db.Session {
	e.t.Helper()
	s, err := e.database.CreateSession(name, "", e.t.TempDir(), "default", "claude", nil, "agent")
	if err != nil {
		e.t.Fatal(err)
	}
	return s
}

// guest 建立分享並加入，回傳 guest token 與分享。
func (e *guestEnv) guest(sessionID, role, mode, nick string) (string, *db.Share) {
	e.t.Helper()
	sh, err := e.database.CreateShare(sessionID, role, mode, time.Hour)
	if err != nil {
		e.t.Fatal(err)
	}
	ga, err := e.database.TryJoin(sh.Token, sh.PIN, nick)
	if err != nil {
		e.t.Fatal(err)
	}
	return ga.Guest.Token, sh
}

func (e *guestEnv) do(method, path, token string, body io.Reader, hdr map[string]string) (int, []byte) {
	e.t.Helper()
	req, _ := http.NewRequest(method, e.base+path, body)
	if token != "" {
		req.Header.Set("X-Share-Token", token)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b
}

func (e *guestEnv) status(method, path, token string) int {
	e.t.Helper()
	code, _ := e.do(method, path, token, nil, nil)
	return code
}

var jsonHdr = map[string]string{"Content-Type": "application/json"}

// dialWS 嘗試連線；握手被拒時回傳 HTTP 狀態碼，連上則回傳 conn。
func (e *guestEnv) dialWS(sessionID, token string, viaQuery bool) (*websocket.Conn, int) {
	e.t.Helper()
	url := e.wsBase + "/sessions/" + sessionID + "/ws"
	hdr := http.Header{}
	if viaQuery {
		url += "?share=" + token
	} else if token != "" {
		hdr.Set("X-Share-Token", token)
	}
	conn, res, err := websocket.DefaultDialer.Dial(url, hdr)
	if err != nil {
		if res != nil {
			return nil, res.StatusCode
		}
		e.t.Fatalf("dial: %v", err)
	}
	return conn, 101
}

// attachment 在磁碟與 DB 建一個附件，回傳附件 id。
func (e *guestEnv) attachment(sessionID, name string) string {
	e.t.Helper()
	p, err := media.SaveUpload(sessionID, name, strings.NewReader("hello "+name))
	if err != nil {
		e.t.Fatal(err)
	}
	id := "att-" + name
	storage := p[strings.LastIndexAny(p, `/\`)+1:]
	if err := e.database.AddAttachment(&db.Attachment{ID: id, SessionID: sessionID, Name: name, StorageName: storage, MimeType: "text/plain", Size: 8}); err != nil {
		e.t.Fatal(err)
	}
	return id
}

func TestGuest_InvalidTokenAndOwnerRoutesForbidden(t *testing.T) {
	e := newGuestEnv(t)
	a := e.session("A")
	tok, _ := e.guest(a.ID, db.ShareRoleEditor, db.ShareModeLive, "小明")

	if code := e.status("GET", "/sessions/"+a.ID+"/messages", "not-a-token"); code != 401 {
		t.Fatalf("無效 token 應 401，got %d", code)
	}
	// 擁有者端路由與其他 session 管理路由：即使是 editor 也一律 403
	for _, tc := range []struct{ method, path string }{
		{"GET", "/sessions"},
		{"PATCH", "/sessions/" + a.ID},
		{"DELETE", "/sessions/" + a.ID},
		{"POST", "/sessions/" + a.ID + "/shares"},
		{"GET", "/sessions/" + a.ID + "/shares"},
		{"GET", "/shares"},
		{"DELETE", "/shares"},
		{"DELETE", "/shares/1"},
	} {
		if code := e.status(tc.method, tc.path, tok); code != 403 {
			t.Errorf("%s %s 訪客應 403，got %d", tc.method, tc.path, code)
		}
	}
	// 沒帶 token 的請求走原本驗證流程（測試中視為擁有者）
	if code := e.status("GET", "/sessions", ""); code != 200 {
		t.Errorf("擁有者 /sessions got %d", code)
	}
	if code := e.status("GET", "/sessions/"+a.ID+"/messages", tok); code != 200 {
		t.Errorf("訪客讀綁定 session 的 messages got %d", code)
	}
	// query 形式（WebSocket 用）同樣有效
	if code := e.status("GET", "/sessions/"+a.ID+"/messages?share="+tok, ""); code != 200 {
		t.Errorf("?share= got %d", code)
	}
}

func TestGuest_CrossSessionBlocked(t *testing.T) {
	e := newGuestEnv(t)
	a, b := e.session("A"), e.session("B")
	tok, _ := e.guest(a.ID, db.ShareRoleEditor, db.ShareModeLive, "小明")

	if code := e.status("GET", "/sessions/"+b.ID+"/messages", tok); code != 403 {
		t.Fatalf("跨 session messages 應 403，got %d", code)
	}
	if code := e.status("POST", "/sessions/"+b.ID+"/uploads", tok); code != 403 {
		t.Fatalf("跨 session uploads 應 403，got %d", code)
	}
	if code := e.status("GET", "/sessions/"+b.ID+"/uploads/x", tok); code != 403 {
		t.Fatalf("跨 session 附件應 403，got %d", code)
	}
	if conn, code := e.dialWS(b.ID, tok, false); conn != nil || code != 403 {
		t.Fatalf("跨 session ws 應 403，got conn=%v code=%d", conn != nil, code)
	}
	// 綁定的 session 仍可連
	conn, code := e.dialWS(a.ID, tok, true)
	if conn == nil {
		t.Fatalf("綁定 session 應可連線，code=%d", code)
	}
	conn.Close()
}

func TestGuest_ExpiredAndRevokedTokensRejected(t *testing.T) {
	e := newGuestEnv(t)
	a := e.session("A")
	tokExp, shExp := e.guest(a.ID, db.ShareRoleViewer, db.ShareModeLive, "甲")
	tokRev, shRev := e.guest(a.ID, db.ShareRoleViewer, db.ShareModeLive, "乙")
	path := "/sessions/" + a.ID + "/messages"

	for _, tok := range []string{tokExp, tokRev} {
		if code := e.status("GET", path, tok); code != 200 {
			t.Fatalf("有效期內應 200，got %d", code)
		}
	}

	past := time.Now().UTC().Add(-time.Minute).Format("2006-01-02T15:04:05Z")
	if _, err := e.database.Exec(`UPDATE shares SET expires_at = ? WHERE id = ?`, past, shExp.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.database.RevokeShare(shRev.ID); err != nil {
		t.Fatal(err)
	}
	for name, tok := range map[string]string{"到期": tokExp, "撤銷": tokRev} {
		code, body := e.do("GET", path, tok, nil, nil)
		if code != 401 || !strings.Contains(string(body), "share_ended") {
			t.Errorf("%s後 REST 應 401 share_ended，got %d %s", name, code, body)
		}
		if conn, code := e.dialWS(a.ID, tok, true); conn != nil || code != 401 {
			t.Errorf("%s後 ws 應 401，got conn=%v code=%d", name, conn != nil, code)
		}
	}
}

func TestGuest_SnapshotLimitedToSnapshotMessagesAndNoWS(t *testing.T) {
	e := newGuestEnv(t)
	a := e.session("A")
	oldAtt, newAtt := e.attachment(a.ID, "old.txt"), e.attachment(a.ID, "new.txt")

	_, _ = e.database.AddUserMessageWithAttachments(a.ID, "before-1", "", []string{oldAtt})
	_ = e.database.AddMessage(a.ID, "claude", "before-2", "")
	tok, sh := e.guest(a.ID, db.ShareRoleViewer, db.ShareModeSnapshot, "小明")
	_, _ = e.database.AddUserMessageWithAttachments(a.ID, "after-1", "", []string{newAtt})
	_ = e.database.AddMessage(a.ID, "claude", "after-2", "")

	code, body := e.do("GET", "/sessions/"+a.ID+"/messages", tok, nil, nil)
	if code != 200 {
		t.Fatalf("snapshot messages got %d", code)
	}
	var msgs []db.Message
	if err := json.Unmarshal(body, &msgs); err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("snapshot 應只有 2 則，got %d: %s", len(msgs), body)
	}
	for _, m := range msgs {
		if m.ID > *sh.SnapshotMsgID || strings.HasPrefix(m.Content, "after") {
			t.Fatalf("讀到 snapshot 之後的訊息: %+v", m)
		}
	}
	// 擁有者看得到全部
	_, ownerBody := e.do("GET", "/sessions/"+a.ID+"/messages", "", nil, nil)
	var all []db.Message
	_ = json.Unmarshal(ownerBody, &all)
	if len(all) != 4 {
		t.Fatalf("擁有者應看到 4 則，got %d", len(all))
	}

	// 附件：快照內可讀，快照之後的不行
	if code := e.status("GET", "/sessions/"+a.ID+"/uploads/"+oldAtt, tok); code != 200 {
		t.Errorf("快照內附件應可讀，got %d", code)
	}
	if code := e.status("GET", "/sessions/"+a.ID+"/uploads/"+newAtt, tok); code != 403 {
		t.Errorf("快照之後的附件應 403，got %d", code)
	}
	if code := e.status("GET", "/sessions/"+a.ID+"/uploads/"+oldAtt+"/details", tok); code != 403 {
		t.Errorf("details 會洩漏路徑，訪客應 403，got %d", code)
	}

	// snapshot 不開 ws（兩種傳 token 的方式）
	for _, viaQuery := range []bool{false, true} {
		if conn, code := e.dialWS(a.ID, tok, viaQuery); conn != nil || code != 403 {
			t.Fatalf("snapshot 應連不上 ws (403)，got conn=%v code=%d", conn != nil, code)
		}
	}
}

func TestGuest_UploadOnlyForEditor(t *testing.T) {
	e := newGuestEnv(t)
	a := e.session("A")
	viewer, _ := e.guest(a.ID, db.ShareRoleViewer, db.ShareModeLive, "觀眾")
	editor, _ := e.guest(a.ID, db.ShareRoleEditor, db.ShareModeLive, "協作者")

	upload := func(tok string) int {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		f, _ := w.CreateFormFile("file", "note.txt")
		f.Write([]byte("hi"))
		w.Close()
		code, _ := e.do("POST", "/sessions/"+a.ID+"/uploads", tok, &buf, map[string]string{"Content-Type": w.FormDataContentType()})
		return code
	}
	if code := upload(viewer); code != 403 {
		t.Fatalf("viewer 上傳應 403，got %d", code)
	}
	if code := upload(editor); code != 200 {
		t.Fatalf("editor 上傳應 200，got %d", code)
	}
	// model-options 僅 editor 可讀
	if code := e.status("GET", "/model-options/claude", viewer); code != 403 {
		t.Fatalf("viewer model-options got %d", code)
	}
	if code := e.status("GET", "/model-options/claude", editor); code != 200 {
		t.Fatalf("editor model-options got %d", code)
	}
}

func TestGuest_EndToEndCreateJoinAndMe(t *testing.T) {
	e := newGuestEnv(t)
	a := e.session("專案")

	// 擁有者建立分享：editor + snapshot → 400；ttl 字串與秒數皆可
	if code, _ := e.do("POST", "/sessions/"+a.ID+"/shares", "", strings.NewReader(`{"ttl":3600,"role":"editor","mode":"snapshot"}`), jsonHdr); code != 400 {
		t.Fatalf("editor+snapshot 應 400，got %d", code)
	}
	code, body := e.do("POST", "/sessions/"+a.ID+"/shares", "", strings.NewReader(`{"ttl":"2h","role":"editor","mode":"live"}`), jsonHdr)
	if code != 201 {
		t.Fatalf("建立分享 got %d %s", code, body)
	}
	var created struct {
		ID    int64  `json:"id"`
		URL   string `json:"url"`
		Path  string `json:"path"`
		PIN   string `json:"pin"`
		Token string `json:"token"`
	}
	_ = json.Unmarshal(body, &created)
	if len(created.PIN) != 6 || !strings.HasSuffix(created.URL, "/share/"+created.Token) || created.Path != "/share/"+created.Token {
		t.Fatalf("created=%+v", created)
	}

	// 錯 PIN 5 次後鎖定（第 5 次起回 423）
	wrong := "000000"
	if created.PIN == wrong {
		wrong = "111111"
	}
	join := func(pin, nick string) (int, map[string]any) {
		b, _ := json.Marshal(map[string]string{"pin": pin, "nickname": nick})
		code, body := e.do("POST", "/share/"+created.Token+"/join", "", bytes.NewReader(b), jsonHdr)
		var out map[string]any
		_ = json.Unmarshal(body, &out)
		return code, out
	}
	for i := 1; i <= db.MaxPINAttempts; i++ {
		code, _ := join(wrong, "駭客")
		want := 401
		if i == db.MaxPINAttempts {
			want = 423
		}
		if code != want {
			t.Fatalf("第 %d 次錯誤 PIN 應 %d，got %d", i, want, code)
		}
	}
	if code, _ := join(created.PIN, "小明"); code != 423 {
		t.Fatalf("鎖定後正確 PIN 也應 423，got %d", code)
	}
	lockedID := created.ID

	// 另建一個分享測試正常加入與 /guest/me
	_, body = e.do("POST", "/sessions/"+a.ID+"/shares", "", strings.NewReader(`{"role":"viewer","mode":"live"}`), jsonHdr)
	_ = json.Unmarshal(body, &created)
	code, out := join(created.PIN, "小明")
	if code != 200 || out["guest_token"] == "" || out["nickname"] != "小明" || out["role"] != "viewer" || out["mode"] != "live" || out["session_id"] != a.ID {
		t.Fatalf("join got %d %v", code, out)
	}
	if info, _ := out["session"].(map[string]any); info == nil || info["work_dir"] != nil {
		t.Fatalf("session 資訊不該包含 work_dir: %v", out["session"])
	}
	_, out2 := join(created.PIN, "小明")
	if out2["nickname"] != "小明2" {
		t.Fatalf("重複暱稱應加後綴，got %v", out2["nickname"])
	}
	tok := out["guest_token"].(string)
	code, body = e.do("GET", "/guest/me", tok, nil, nil)
	if code != 200 || !strings.Contains(string(body), `"nickname":"小明"`) {
		t.Fatalf("/guest/me got %d %s", code, body)
	}
	if code := e.status("GET", "/guest/me", ""); code != 401 {
		t.Fatalf("擁有者呼叫 /guest/me 應 401，got %d", code)
	}

	// 擁有者列表含明文 PIN、在線資訊與 session 名稱
	code, body = e.do("GET", "/shares", "", nil, nil)
	if code != 200 || !strings.Contains(string(body), created.PIN) || !strings.Contains(string(body), `"session_name":"專案"`) {
		t.Fatalf("GET /shares got %d %s", code, body)
	}

	// 擁有者結束分享 → token 立刻失效
	if code := e.status("DELETE", fmt.Sprintf("/shares/%d", created.ID), ""); code != 204 {
		t.Fatalf("結束分享 got %d", code)
	}
	if code := e.status("GET", "/sessions/"+a.ID+"/messages", tok); code != 401 {
		t.Fatalf("結束後 token 應失效，got %d", code)
	}
	// 全部結束：只剩「已鎖定但仍有效」的第一個分享（已結束者不計）
	code, body = e.do("DELETE", "/shares", "", nil, nil)
	if code != 200 || !strings.Contains(string(body), `"revoked":1`) {
		t.Fatalf("全部結束 got %d %s", code, body)
	}
	if got, _ := e.database.GetShare(lockedID); !got.Revoked {
		t.Fatal("全部結束後第一個分享也應撤銷")
	}
}

func TestGuest_RevokeKicksOpenWebSocket(t *testing.T) {
	e := newGuestEnv(t)
	a := e.session("A")
	tok, sh := e.guest(a.ID, db.ShareRoleViewer, db.ShareModeLive, "小明")
	conn, code := e.dialWS(a.ID, tok, true)
	if conn == nil {
		t.Fatalf("dial code=%d", code)
	}
	defer conn.Close()

	// 等到在線名單出現
	deadline := time.Now().Add(3 * time.Second)
	for len(ws.ShareOnline(sh.ID)) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if got := ws.ShareOnline(sh.ID); len(got) != 1 || got[0] != "小明" {
		t.Fatalf("online=%v", got)
	}

	if code := e.status("DELETE", fmt.Sprintf("/shares/%d", sh.ID), ""); code != 204 {
		t.Fatalf("revoke got %d", code)
	}
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	gotEnded := false
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			break // 連線被關閉
		}
		var m struct{ Type string }
		if json.Unmarshal(data, &m) == nil && m.Type == "share_ended" {
			gotEnded = true
		}
	}
	if !gotEnded {
		t.Fatal("結束分享時應先收到 share_ended 再被關閉")
	}
}
