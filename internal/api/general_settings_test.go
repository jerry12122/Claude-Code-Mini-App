package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fasthttp/websocket"
	fiberws "github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/tg"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/ws"
)

func newGeneralApp(t *testing.T, configPercent float64) (*fiber.App, *db.DB) {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/general_api.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	h := NewSettingsHandler(database, configPercent)
	app.Get("/settings/general", h.GetGeneral)
	app.Put("/settings/general", h.PutGeneral)
	return app, database
}

func doGeneral(t *testing.T, app *fiber.App, method, body string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(method, "/settings/general", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	out, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, out
}

func armQuotaResume(t *testing.T, database *db.DB, id, state string) {
	t.Helper()
	const at = "2026-11-18T12:00:00Z"
	step := func(from, to, when string) {
		t.Helper()
		ok, err := database.TransitionQuotaResume(id, from, to, when, false)
		if err != nil || !ok {
			t.Fatalf("%s→%s ok=%v err=%v", from, to, ok, err)
		}
	}
	switch state {
	case db.QuotaResumeNone:
	case db.QuotaResumeWaiting:
		step(db.QuotaResumeNone, db.QuotaResumeWaiting, at)
	case db.QuotaResumeResuming:
		step(db.QuotaResumeNone, db.QuotaResumeWaiting, at)
		step(db.QuotaResumeWaiting, db.QuotaResumeResuming, "")
	case db.QuotaResumeExhausted:
		step(db.QuotaResumeNone, db.QuotaResumeExhausted, "")
	default:
		t.Fatalf("未知狀態 %s", state)
	}
}

func requireResume(t *testing.T, database *db.DB, id, state, at string, attempts int) {
	t.Helper()
	gotState, gotAt, gotAttempts, err := database.GetQuotaResume(id)
	if err != nil {
		t.Fatal(err)
	}
	if gotState != state || gotAt != at || gotAttempts != attempts {
		t.Fatalf("%s: got %s %q %d, want %s %q %d", id, gotState, gotAt, gotAttempts, state, at, attempts)
	}
}

func TestPutGeneralQuotaGuardPercentBoundaries(t *testing.T) {
	cases := []struct {
		percent float64
		accept  bool
	}{
		{0, false},
		{-5, false},
		{100.5, false},
		{100, true},
		{0.5, true},
		{90, true},
	}
	if len(cases) != 6 {
		t.Fatalf("邊界表應為 6 列，got %d", len(cases))
	}
	for _, tc := range cases {
		t.Run(fmt.Sprint(tc.percent), func(t *testing.T) {
			app, database := newGeneralApp(t, 90)
			if _, err := database.PutGeneralSettings([]byte(`{"vscodeNoAdmin":true,"claudeAutoAllowAsk":true,"autoResumeOnQuota":true,"quotaGuardEnabled":true,"quotaGuardPercent":80}`)); err != nil {
				t.Fatal(err)
			}
			s := newSession(t, database, "boundary")
			armQuotaResume(t, database, s.ID, db.QuotaResumeWaiting)
			body := `{"vscodeNoAdmin":true,"claudeAutoAllowAsk":true,"autoResumeOnQuota":false,"quotaGuardEnabled":true,"quotaGuardPercent":` + jsonNumber(tc.percent) + `}`
			code, raw := doGeneral(t, app, "PUT", body)
			stored, err := database.GetGeneralSettings()
			if err != nil {
				t.Fatal(err)
			}
			if tc.accept {
				if code != 200 {
					t.Fatalf("應接受，got %d %s", code, raw)
				}
				if stored.QuotaGuardPercent != tc.percent || !stored.QuotaGuardEnabled {
					t.Fatalf("寫入後 %+v", stored)
				}
				return
			}
			if code != 400 || !strings.Contains(string(raw), "quotaGuardPercent") {
				t.Fatalf("應 400 且指名 quotaGuardPercent，got %d %s", code, raw)
			}
			if !stored.VscodeNoAdmin || !stored.ClaudeAutoAllowAsk || !stored.AutoResumeOnQuota || stored.QuotaGuardPercent != 80 {
				t.Fatalf("被拒後既有設定被改掉：%+v", stored)
			}
			requireResume(t, database, s.ID, db.QuotaResumeWaiting, "2026-11-18T12:00:00Z", 0)
			if _, _, gen, gerr := database.SnapshotQuotaResume(s.ID); gerr != nil || gen != 0 {
				t.Fatalf("400 不該提交世代 gen=%d err=%v", gen, gerr)
			}
		})
	}
}

func TestPutGeneralMissingQuotaGuardPercentRejected(t *testing.T) {
	app, database := newGeneralApp(t, 90)
	if _, err := database.PutGeneralSettings([]byte(`{"vscodeNoAdmin":true,"autoResumeOnQuota":true,"quotaGuardEnabled":true,"quotaGuardPercent":80}`)); err != nil {
		t.Fatal(err)
	}
	s := newSession(t, database, "missing")
	armQuotaResume(t, database, s.ID, db.QuotaResumeResuming)
	code, raw := doGeneral(t, app, "PUT", `{"vscodeNoAdmin":false,"autoResumeOnQuota":false,"quotaGuardEnabled":true}`)
	if code != 400 || !strings.Contains(string(raw), "quotaGuardPercent") {
		t.Fatalf("缺欄位應被拒，got %d %s", code, raw)
	}
	stored, err := database.GetGeneralSettings()
	if err != nil {
		t.Fatal(err)
	}
	if !stored.VscodeNoAdmin || !stored.AutoResumeOnQuota || stored.QuotaGuardPercent != 80 {
		t.Fatalf("被拒後設定應不變：%+v", stored)
	}
	requireResume(t, database, s.ID, db.QuotaResumeResuming, "2026-11-18T12:00:00Z", 1)
	if _, _, gen, gerr := database.SnapshotQuotaResume(s.ID); gerr != nil || gen != 1 {
		t.Fatalf("400 不該提交世代 gen=%d err=%v", gen, gerr)
	}
}

func TestGetGeneralReturnsEffectiveGuard(t *testing.T) {
	t.Run("從未儲存 config 90", func(t *testing.T) {
		app, _ := newGeneralApp(t, 90)
		code, raw := doGeneral(t, app, "GET", "")
		if code != 200 {
			t.Fatal(code, string(raw))
		}
		var got db.GeneralSettings
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if got.AutoResumeOnQuota || !got.QuotaGuardEnabled || got.QuotaGuardPercent != 90 {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("從未儲存 config 0", func(t *testing.T) {
		app, _ := newGeneralApp(t, 0)
		_, raw := doGeneral(t, app, "GET", "")
		var got db.GeneralSettings
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if got.QuotaGuardEnabled || got.QuotaGuardPercent != 90 {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("已儲存 75 覆蓋 config 0", func(t *testing.T) {
		app, database := newGeneralApp(t, 0)
		if _, err := database.PutGeneralSettings([]byte(`{"quotaGuardEnabled":true,"quotaGuardPercent":75}`)); err != nil {
			t.Fatal(err)
		}
		_, raw := doGeneral(t, app, "GET", "")
		var got db.GeneralSettings
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if !got.QuotaGuardEnabled || got.QuotaGuardPercent != 75 || got.AutoResumeOnQuota {
			t.Fatalf("%+v", got)
		}
	})
}

func TestPutGeneralAutoResumeSwitchClearsOnlyWhenTurningOff(t *testing.T) {
	app, database := newGeneralApp(t, 90)
	code, raw := doGeneral(t, app, "PUT", `{"autoResumeOnQuota":true,"quotaGuardEnabled":true,"quotaGuardPercent":90}`)
	if code != 200 {
		t.Fatalf("開啟開關失敗 %d %s", code, raw)
	}

	waiting := newSession(t, database, "waiting")
	resuming := newSession(t, database, "resuming")
	exhausted := newSession(t, database, "exhausted")
	none := newSession(t, database, "none")
	armQuotaResume(t, database, waiting.ID, db.QuotaResumeWaiting)
	armQuotaResume(t, database, resuming.ID, db.QuotaResumeResuming)
	armQuotaResume(t, database, exhausted.ID, db.QuotaResumeExhausted)
	if _, err := database.Exec(`UPDATE sessions SET quota_resume_at = ?, quota_resume_attempts = ? WHERE id = ?`, "keep", 4, none.ID); err != nil {
		t.Fatal(err)
	}

	code, raw = doGeneral(t, app, "PUT", `{"autoResumeOnQuota":true,"quotaGuardEnabled":true,"quotaGuardPercent":80}`)
	if code != 200 {
		t.Fatalf("同為 true 的 PUT 失敗 %d %s", code, raw)
	}
	requireResume(t, database, waiting.ID, db.QuotaResumeWaiting, "2026-11-18T12:00:00Z", 0)
	requireResume(t, database, resuming.ID, db.QuotaResumeResuming, "2026-11-18T12:00:00Z", 1)
	requireResume(t, database, exhausted.ID, db.QuotaResumeExhausted, "", 0)
	requireResume(t, database, none.ID, db.QuotaResumeNone, "keep", 4)
	if _, _, gen, err := database.SnapshotQuotaResume(none.ID); err != nil || gen != 0 {
		t.Fatalf("true→true 不該推進世代 gen=%d err=%v", gen, err)
	}

	code, raw = doGeneral(t, app, "PUT", `{"autoResumeOnQuota":false,"quotaGuardEnabled":false,"quotaGuardPercent":90}`)
	if code != 200 {
		t.Fatalf("true→false 失敗 %d %s", code, raw)
	}
	requireResume(t, database, waiting.ID, db.QuotaResumeNone, "", 0)
	requireResume(t, database, resuming.ID, db.QuotaResumeNone, "", 0)
	requireResume(t, database, exhausted.ID, db.QuotaResumeNone, "", 0)
	requireResume(t, database, none.ID, db.QuotaResumeNone, "", 0)
	if _, _, gen, err := database.SnapshotQuotaResume(none.ID); err != nil || gen != 1 {
		t.Fatalf("none 世代=%d err=%v", gen, err)
	}
	if _, _, gen, err := database.SnapshotQuotaResume(resuming.ID); err != nil || gen != 2 {
		t.Fatalf("resuming 世代=%d err=%v", gen, err)
	}

	fresh := newSession(t, database, "stay")
	armQuotaResume(t, database, fresh.ID, db.QuotaResumeWaiting)
	code, raw = doGeneral(t, app, "PUT", `{"autoResumeOnQuota":true,"quotaGuardEnabled":true,"quotaGuardPercent":90}`)
	if code != 200 {
		t.Fatalf("false→true 失敗 %d %s", code, raw)
	}
	requireResume(t, database, fresh.ID, db.QuotaResumeWaiting, "2026-11-18T12:00:00Z", 0)
	requireResume(t, database, waiting.ID, db.QuotaResumeNone, "", 0)
}

func TestPutGeneralOffBroadcastsQueueUpdate(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/general_broadcast.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	h := NewSettingsHandler(database, 90)
	app.Put("/settings/general", h.PutGeneral)
	app.Get("/sessions/:id/ws", fiberws.New(ws.NewHandler(database, "", ws.ShellOpts{}, nil, tg.NotifyConfig{})))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = app.Listener(ln) }()
	t.Cleanup(func() { _ = app.Shutdown() })

	if code, raw := doGeneral(t, app, "PUT", `{"autoResumeOnQuota":true,"quotaGuardEnabled":true,"quotaGuardPercent":90}`); code != 200 {
		t.Fatalf("開啟開關 %d %s", code, raw)
	}
	s := newSession(t, database, "live")
	if _, err := database.EnqueueMessage(s.ID, "A", ""); err != nil {
		t.Fatal(err)
	}
	if err := database.SetQueuePaused(s.ID, true); err != nil {
		t.Fatal(err)
	}
	if ok, err := database.TransitionQuotaResume(s.ID, db.QuotaResumeNone, db.QuotaResumeWaiting, "2026-11-18T10:01:00Z", false); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if ok, err := database.TransitionQuotaResume(s.ID, db.QuotaResumeWaiting, db.QuotaResumeResuming, "", false); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if ok, err := database.TransitionQuotaResume(s.ID, db.QuotaResumeResuming, db.QuotaResumeWaiting, "2026-11-18T10:01:00Z", false); err != nil || !ok {
		t.Fatal(ok, err)
	}
	url := "ws://" + ln.Addr().String() + "/sessions/" + s.ID + "/ws"
	watch := func() (ready <-chan struct{}, got <-chan map[string]any) {
		t.Helper()
		conn, _, err := websocket.DefaultDialer.Dial(url, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		readyCh := make(chan struct{})
		gotCh := make(chan map[string]any, 1)
		go func() {
			_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			synced := false
			for {
				_, payload, err := conn.ReadMessage()
				if err != nil {
					return
				}
				var msg map[string]any
				if json.Unmarshal(payload, &msg) != nil {
					continue
				}
				if !synced && msg["type"] == "sync" {
					synced = true
					close(readyCh)
				}
				if msg["type"] == "queue_update" && msg["quota_resume_state"] == db.QuotaResumeNone && msg["quota_resume_at"] == "" {
					gotCh <- msg
					return
				}
			}
		}()
		return readyCh, gotCh
	}
	ready1, first := watch()
	ready2, second := watch()
	select {
	case <-ready1:
	case <-time.After(5 * time.Second):
		t.Fatal("第一條連線沒有 sync")
	}
	select {
	case <-ready2:
	case <-time.After(5 * time.Second):
		t.Fatal("第二條連線沒有 sync")
	}
	if code, raw := doGeneral(t, app, "PUT", `{"autoResumeOnQuota":false,"quotaGuardEnabled":true,"quotaGuardPercent":90}`); code != 200 {
		t.Fatalf("關閉開關 %d %s", code, raw)
	}
	for _, ch := range []<-chan map[string]any{first, second} {
		select {
		case msg := <-ch:
			queue, _ := msg["queue"].([]any)
			if len(queue) != 1 || msg["queue_paused"] != true {
				t.Fatalf("queue update=%v", msg)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("已連線 client 沒有收到取消後的 queue_update")
		}
	}
	_, _, generation, err := database.SnapshotQuotaResume(s.ID)
	if err != nil || generation != 2 {
		t.Fatalf("關閉後世代=%d err=%v", generation, err)
	}
	requireResume(t, database, s.ID, db.QuotaResumeNone, "", 0)
}

func jsonNumber(v float64) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func newSession(t *testing.T, database *db.DB, name string) *db.Session {
	t.Helper()
	s, err := database.CreateSession(name, "", t.TempDir(), "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	return s
}
