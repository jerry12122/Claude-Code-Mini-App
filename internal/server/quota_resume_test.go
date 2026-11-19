package server

import (
	"context"
	"errors"
	"math"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fasthttp/websocket"
	fiberws "github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/quota"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/tg"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/ws"
)

type quotaFakeRunner struct{}

func (quotaFakeRunner) Name() string { return "fakeq" }

func (quotaFakeRunner) Run(_ context.Context, opts agent.RunOptions, cb agent.EventCallback) error {
	cb(agent.Event{Type: agent.EventDelta, Text: "ok:" + opts.Prompt})
	cb(agent.Event{Type: agent.EventDone})
	return nil
}

func init() {
	agent.Register("fakeq", func() agent.Runner { return quotaFakeRunner{} })
}

func TestQuotaResumeDueRunsFrontMessage(t *testing.T) {
	database := openQuotaResumeDB(t)
	s := newQuotaResumeSession(t, database)
	setQuotaResumeSwitch(t, database, true)
	pauseQuotaQueue(t, database, s.ID, "refactor X")
	setQuotaWaiting(t, database, s.ID, "2026-11-18T08:00:00Z")
	var wakes int
	var provider string
	rt := newRoomRuntime(database, 0)
	rt.resumeDue(context.Background(), quotaDueNow(), func(_ context.Context, got string) (quota.Snapshot, error) {
		provider = got
		return quotaSessionSnap(40, ""), nil
	}, func(context.Context, string) (bool, error) {
		wakes++
		return true, nil
	})
	if provider != "fakeq" || wakes != 1 {
		t.Fatalf("provider=%s wakes=%d", provider, wakes)
	}
	mustQuotaState(t, database, s.ID, db.QuotaResumeResuming, "2026-11-18T08:00:00Z", 1)
}

func TestQuotaResumeStillFullUpdatesTime(t *testing.T) {
	database := openQuotaResumeDB(t)
	s := newQuotaResumeSession(t, database)
	setQuotaResumeSwitch(t, database, true)
	setQuotaWaiting(t, database, s.ID, "2026-11-18T08:00:00Z")
	var wakes int
	rt := newRoomRuntime(database, 0)
	rt.resumeDue(context.Background(), quotaDueNow(), func(context.Context, string) (quota.Snapshot, error) {
		return quotaSessionSnap(100, "2026-11-18T10:00:00Z"), nil
	}, func(context.Context, string) (bool, error) {
		wakes++
		return true, nil
	})
	if wakes != 0 {
		t.Fatalf("wakes=%d", wakes)
	}
	mustQuotaState(t, database, s.ID, db.QuotaResumeWaiting, "2026-11-18T10:01:00Z", 0)
}

func TestQuotaResumeKeepsWaiting(t *testing.T) {
	nan := math.NaN()
	inf := math.Inf(1)
	cases := []struct {
		name string
		snap quota.Snapshot
		err  error
	}{
		{name: "refresh error", err: errors.New("refresh down")},
		{name: "cooldown", snap: quota.Snapshot{Error: "請稍後再試（60 秒冷卻）", Windows: quotaSessionSnap(40, "").Windows}},
		{name: "stale", snap: quota.Snapshot{Stale: true, Windows: quotaSessionSnap(40, "").Windows}},
		{name: "no window", snap: quota.Snapshot{}},
		{name: "weekly only", snap: quota.Snapshot{Windows: []quota.Window{{Key: "weekly", Percent: quotaPercent(40), ResetsAt: "2026-11-18T10:00:00Z"}}}},
		{name: "missing percent", snap: quota.Snapshot{Windows: []quota.Window{{Key: "session", ResetsAt: "2026-11-18T10:00:00Z"}}}},
		{name: "nan", snap: quota.Snapshot{Windows: []quota.Window{{Key: "session", Percent: &nan}}}},
		{name: "inf", snap: quota.Snapshot{Windows: []quota.Window{{Key: "session", Percent: &inf}}}},
		{name: "full without reset", snap: quotaSessionSnap(100, "")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			database := openQuotaResumeDB(t)
			s := newQuotaResumeSession(t, database)
			setQuotaResumeSwitch(t, database, true)
			setQuotaWaiting(t, database, s.ID, "2026-11-18T08:00:00Z")
			var wakes int
			rt := newRoomRuntime(database, 0)
			rt.resumeDue(context.Background(), quotaDueNow(), func(context.Context, string) (quota.Snapshot, error) {
				return tc.snap, tc.err
			}, func(context.Context, string) (bool, error) {
				wakes++
				return true, nil
			})
			if wakes != 0 {
				t.Fatalf("wakes=%d", wakes)
			}
			mustQuotaState(t, database, s.ID, db.QuotaResumeWaiting, "2026-11-18T08:00:00Z", 0)
		})
	}
}

func TestQuotaResumeNotDueDoesNotRefresh(t *testing.T) {
	database := openQuotaResumeDB(t)
	s := newQuotaResumeSession(t, database)
	setQuotaResumeSwitch(t, database, true)
	setQuotaWaiting(t, database, s.ID, "2026-11-18T10:00:00Z")
	var refreshes, wakes int
	rt := newRoomRuntime(database, 0)
	now := time.Date(2026, 11, 18, 9, 59, 59, 0, time.UTC)
	rt.resumeDue(context.Background(), now, func(context.Context, string) (quota.Snapshot, error) {
		refreshes++
		return quotaSessionSnap(40, ""), nil
	}, func(context.Context, string) (bool, error) {
		wakes++
		return true, nil
	})
	if refreshes != 0 || wakes != 0 {
		t.Fatalf("refreshes=%d wakes=%d", refreshes, wakes)
	}
	mustQuotaState(t, database, s.ID, db.QuotaResumeWaiting, "2026-11-18T10:00:00Z", 0)
}

func TestQuotaResumeSkipsResuming(t *testing.T) {
	database := openQuotaResumeDB(t)
	s := newQuotaResumeSession(t, database)
	setQuotaResumeSwitch(t, database, true)
	setQuotaWaiting(t, database, s.ID, "2026-11-18T08:00:00Z")
	if ok, err := database.TransitionQuotaResume(s.ID, db.QuotaResumeWaiting, db.QuotaResumeResuming, "", false); err != nil || !ok {
		t.Fatalf("resuming ok=%v err=%v", ok, err)
	}
	var refreshes, wakes int
	rt := newRoomRuntime(database, 0)
	rt.resumeDue(context.Background(), quotaDueNow(), func(context.Context, string) (quota.Snapshot, error) {
		refreshes++
		return quotaSessionSnap(40, ""), nil
	}, func(context.Context, string) (bool, error) {
		wakes++
		return true, nil
	})
	if refreshes != 0 || wakes != 0 {
		t.Fatalf("refreshes=%d wakes=%d", refreshes, wakes)
	}
	mustQuotaState(t, database, s.ID, db.QuotaResumeResuming, "2026-11-18T08:00:00Z", 1)
}

func TestQuotaResumeSwitchOffDoesNotStart(t *testing.T) {
	database := openQuotaResumeDB(t)
	s := newQuotaResumeSession(t, database)
	setQuotaWaiting(t, database, s.ID, "2026-11-18T08:00:00Z")
	var refreshes, wakes int
	rt := newRoomRuntime(database, 0)
	rt.resumeDue(context.Background(), quotaDueNow(), func(context.Context, string) (quota.Snapshot, error) {
		refreshes++
		return quotaSessionSnap(40, ""), nil
	}, func(context.Context, string) (bool, error) {
		wakes++
		return true, nil
	})
	if refreshes != 0 || wakes != 0 {
		t.Fatalf("refreshes=%d wakes=%d", refreshes, wakes)
	}
	mustQuotaState(t, database, s.ID, db.QuotaResumeWaiting, "2026-11-18T08:00:00Z", 0)
}

func TestQuotaResumeSwitchOffDuringRefresh(t *testing.T) {
	database := openQuotaResumeDB(t)
	s := newQuotaResumeSession(t, database)
	setQuotaResumeSwitch(t, database, true)
	setQuotaWaiting(t, database, s.ID, "2026-11-18T08:00:00Z")
	var wakes int
	rt := newRoomRuntime(database, 0)
	rt.resumeDue(context.Background(), quotaDueNow(), func(context.Context, string) (quota.Snapshot, error) {
		setQuotaResumeSwitch(t, database, false)
		return quotaSessionSnap(40, ""), nil
	}, func(context.Context, string) (bool, error) {
		wakes++
		return true, nil
	})
	if wakes != 0 {
		t.Fatalf("wakes=%d", wakes)
	}
	mustQuotaState(t, database, s.ID, db.QuotaResumeNone, "", 0)
}

func TestQuotaResumeCancelDuringRefresh(t *testing.T) {
	database := openQuotaResumeDB(t)
	s := newQuotaResumeSession(t, database)
	setQuotaResumeSwitch(t, database, true)
	setQuotaWaiting(t, database, s.ID, "2026-11-18T08:00:00Z")
	var wakes int
	rt := newRoomRuntime(database, 0)
	rt.resumeDue(context.Background(), quotaDueNow(), func(context.Context, string) (quota.Snapshot, error) {
		ok, err := database.TransitionQuotaResume(s.ID, db.QuotaResumeWaiting, db.QuotaResumeNone, "", false)
		if err != nil || !ok {
			t.Errorf("cancel ok=%v err=%v", ok, err)
		}
		return quotaSessionSnap(40, ""), nil
	}, func(context.Context, string) (bool, error) {
		wakes++
		return true, nil
	})
	if wakes != 0 {
		t.Fatalf("wakes=%d", wakes)
	}
	mustQuotaState(t, database, s.ID, db.QuotaResumeNone, "", 0)
}

func TestQuotaResumeReleaseBeforeDial(t *testing.T) {
	database := openQuotaResumeDB(t)
	s := newQuotaResumeSession(t, database)
	setQuotaResumeSwitch(t, database, true)
	setQuotaWaiting(t, database, s.ID, "2026-11-18T08:00:00Z")
	if ok, err := database.TransitionQuotaResume(s.ID, db.QuotaResumeWaiting, db.QuotaResumeResuming, "", false); err != nil || !ok {
		t.Fatalf("resuming ok=%v err=%v", ok, err)
	}
	if _, err := database.Exec(`UPDATE settings SET value = ? WHERE key = ?`,
		`{"autoResumeOnQuota":false,"quotaGuardEnabled":true,"quotaGuardPercent":90}`, db.SettingKeyGeneral); err != nil {
		t.Fatal(err)
	}
	var wakes int
	rt := newRoomRuntime(database, 0)
	_, _, generation, err := database.SnapshotQuotaResume(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	rt.deliverQuotaResume(context.Background(), s.ID, generation, func(context.Context, string) (bool, error) {
		wakes++
		return true, nil
	})
	if wakes != 0 {
		t.Fatalf("wakes=%d", wakes)
	}
	mustQuotaState(t, database, s.ID, db.QuotaResumeNone, "", 0)

	started, err := rt.wakeQuota(context.Background(), s.ID)
	if started || !errors.Is(err, errQuotaResumeNotHeld) {
		t.Fatalf("started=%v err=%v", started, err)
	}
}

func TestQuotaResumeParallelTicksSendOnce(t *testing.T) {
	database := openQuotaResumeDB(t)
	s := newQuotaResumeSession(t, database)
	setQuotaResumeSwitch(t, database, true)
	setQuotaWaiting(t, database, s.ID, "2026-11-18T08:00:00Z")
	var wakes atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	rt := newRoomRuntime(database, 0)
	wake := func(context.Context, string) (bool, error) {
		if wakes.Add(1) == 1 {
			close(entered)
		}
		<-release
		return true, nil
	}
	refresh := func(context.Context, string) (quota.Snapshot, error) {
		return quotaSessionSnap(40, ""), nil
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		rt.resumeDue(context.Background(), quotaDueNow(), refresh, wake)
	}()
	go func() {
		defer wg.Done()
		rt.resumeDue(context.Background(), quotaDueNow(), refresh, wake)
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("沒有喚醒")
	}
	close(release)
	wg.Wait()
	if wakes.Load() != 1 {
		t.Fatalf("wakes=%d", wakes.Load())
	}
	mustQuotaState(t, database, s.ID, db.QuotaResumeResuming, "2026-11-18T08:00:00Z", 1)
}

func TestQuotaResumeStartedFalse(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		database := openQuotaResumeDB(t)
		s := newQuotaResumeSession(t, database)
		setQuotaResumeSwitch(t, database, true)
		setQuotaWaiting(t, database, s.ID, "2026-11-18T08:00:00Z")
		rt := newRoomRuntime(database, 0)
		rt.resumeDue(context.Background(), quotaDueNow(), func(context.Context, string) (quota.Snapshot, error) {
			return quotaSessionSnap(40, ""), nil
		}, func(context.Context, string) (bool, error) {
			return false, nil
		})
		mustQuotaState(t, database, s.ID, db.QuotaResumeNone, "", 0)
	})
	t.Run("queued", func(t *testing.T) {
		database := openQuotaResumeDB(t)
		s := newQuotaResumeSession(t, database)
		setQuotaResumeSwitch(t, database, true)
		pauseQuotaQueue(t, database, s.ID, "still here")
		setQuotaWaiting(t, database, s.ID, "2026-11-18T08:00:00Z")
		rt := newRoomRuntime(database, 0)
		rt.resumeDue(context.Background(), quotaDueNow(), func(context.Context, string) (quota.Snapshot, error) {
			return quotaSessionSnap(40, ""), nil
		}, func(context.Context, string) (bool, error) {
			return false, nil
		})
		mustQuotaState(t, database, s.ID, db.QuotaResumeWaiting, "2026-11-18T08:00:00Z", 0)
		paused, err := database.QueuePaused(s.ID)
		if err != nil || !paused || quotaQueueText(t, database, s.ID) != "still here" {
			t.Fatalf("paused=%v queue=%s err=%v", paused, quotaQueueText(t, database, s.ID), err)
		}
	})
}

func TestQuotaResumeDeliveryFailureRestoresAttempts(t *testing.T) {
	database := openQuotaResumeDB(t)
	s := newQuotaResumeSession(t, database)
	setQuotaResumeSwitch(t, database, true)
	setQuotaWaiting(t, database, s.ID, "2026-11-18T08:00:00Z")
	rt := newRoomRuntime(database, 0)
	rt.resumeDue(context.Background(), quotaDueNow(), func(context.Context, string) (quota.Snapshot, error) {
		return quotaSessionSnap(40, ""), nil
	}, func(context.Context, string) (bool, error) {
		return false, errors.New("dial failed")
	})
	mustQuotaState(t, database, s.ID, db.QuotaResumeWaiting, "2026-11-18T08:00:00Z", 0)
}

func TestQuotaResumeWeeklyFullSessionOpenStarts(t *testing.T) {
	database := openQuotaResumeDB(t)
	s := newQuotaResumeSession(t, database)
	setQuotaResumeSwitch(t, database, true)
	setQuotaWaiting(t, database, s.ID, "2026-11-18T08:00:00Z")
	var wakes int
	weekly, session := 100.0, 40.0
	rt := newRoomRuntime(database, 0)
	rt.resumeDue(context.Background(), quotaDueNow(), func(context.Context, string) (quota.Snapshot, error) {
		return quota.Snapshot{Windows: []quota.Window{
			{Key: "weekly", Percent: &weekly},
			{Key: "session", Percent: &session},
		}}, nil
	}, func(context.Context, string) (bool, error) {
		wakes++
		return true, nil
	})
	if wakes != 1 {
		t.Fatalf("wakes=%d", wakes)
	}
	mustQuotaState(t, database, s.ID, db.QuotaResumeResuming, "2026-11-18T08:00:00Z", 1)
}

func TestQuotaResumeLoopExitsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		var rt roomRuntime
		rt.watchQuota(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("取消後輪詢沒有退出")
	}
	var refreshes int
	newRoomRuntime(nil, 0).resumeDue(ctx, quotaDueNow(), func(context.Context, string) (quota.Snapshot, error) {
		refreshes++
		return quota.Snapshot{}, nil
	}, nil)
	if refreshes != 0 {
		t.Fatalf("refreshes=%d", refreshes)
	}
}

func TestQuotaResumeListenWaitsForLoop(t *testing.T) {
	database := openQuotaResumeDB(t)
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		errc <- newRoomRuntime(database, 0).listen(ctx, app, ln)
	}()
	time.Sleep(30 * time.Millisecond)
	cancel()
	if err := app.Shutdown(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errc:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("listen 沒有在輪詢結束後返回")
	}
}

func TestQuotaResumeInternalSocketRunsFrontMessage(t *testing.T) {
	database := openQuotaResumeDB(t)
	s := newQuotaResumeSession(t, database)
	setQuotaResumeSwitch(t, database, true)
	pauseQuotaQueue(t, database, s.ID, "refactor X")
	setQuotaWaiting(t, database, s.ID, "2026-11-18T08:00:00Z")
	rt, base := startQuotaResumeApp(t, database)
	rt.resumeDue(context.Background(), quotaDueNow(), func(context.Context, string) (quota.Snapshot, error) {
		return quotaSessionSnap(40, ""), nil
	}, rt.wakeQuota)
	deadline := time.Now().Add(5 * time.Second)
	var reply, users string
	for time.Now().Before(deadline) {
		reply = quotaReply(database, s.ID)
		users = quotaUserText(database, s.ID)
		state, _, attempts, err := database.GetQuotaResume(s.ID)
		if err == nil && reply == "ok:refactor X" && users == "refactor X" && quotaQueueText(t, database, s.ID) == "" && state == db.QuotaResumeNone && attempts == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if reply != "ok:refactor X" || users != "refactor X" || quotaQueueText(t, database, s.ID) != "" {
		t.Fatalf("reply=%q users=%q queue=%q", reply, users, quotaQueueText(t, database, s.ID))
	}
	mustQuotaState(t, database, s.ID, db.QuotaResumeNone, "", 0)
	if base == "" {
		t.Fatal("missing address")
	}
}

func TestQuotaResumeOrdinaryConnectionsDoNotStart(t *testing.T) {
	database := openQuotaResumeDB(t)
	s := newQuotaResumeSession(t, database)
	setQuotaResumeSwitch(t, database, true)
	pauseQuotaQueue(t, database, s.ID, "do not run")
	setQuotaWaiting(t, database, s.ID, "2026-11-18T08:00:00Z")
	if ok, err := database.TransitionQuotaResume(s.ID, db.QuotaResumeWaiting, db.QuotaResumeResuming, "", false); err != nil || !ok {
		t.Fatalf("resuming ok=%v err=%v", ok, err)
	}
	_, base := startQuotaResumeApp(t, database)
	url := base + "/sessions/" + s.ID + "/ws"
	owner, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	if err := owner.WriteJSON(map[string]string{"type": "quota_resume"}); err != nil {
		t.Fatal(err)
	}
	rejectQuotaResumed(t, owner)

	share, err := database.CreateShare(s.ID, db.ShareRoleEditor, db.ShareModeLive, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	guestAccess, err := database.TryJoin(share.Token, share.PIN, "訪客")
	if err != nil {
		t.Fatal(err)
	}
	guest, _, err := websocket.DefaultDialer.Dial(url+"?share="+guestAccess.Guest.Token, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer guest.Close()
	if err := guest.WriteJSON(map[string]string{"type": "quota_resume"}); err != nil {
		t.Fatal(err)
	}
	rejectQuotaResumed(t, guest)

	_, _, err = websocket.DefaultDialer.Dial(url, http.Header{roomWakeHeader: {"nope"}})
	if err == nil {
		t.Fatal("錯誤的內部 token 不該連上")
	}
	paused, err := database.QueuePaused(s.ID)
	if err != nil || !paused || quotaQueueText(t, database, s.ID) != "do not run" || quotaReply(database, s.ID) != "" {
		t.Fatalf("paused=%v queue=%q reply=%q err=%v", paused, quotaQueueText(t, database, s.ID), quotaReply(database, s.ID), err)
	}
	mustQuotaState(t, database, s.ID, db.QuotaResumeResuming, "2026-11-18T08:00:00Z", 1)
}

func TestQuotaResumeCancelledBeforeStartDoesNotRun(t *testing.T) {
	database := openQuotaResumeDB(t)
	s := newQuotaResumeSession(t, database)
	setQuotaResumeSwitch(t, database, true)
	pauseQuotaQueue(t, database, s.ID, "stay queued")
	rt, base := startQuotaResumeApp(t, database)
	conn, _, err := websocket.DefaultDialer.Dial(base+"/sessions/"+s.ID+"/ws", http.Header{roomWakeHeader: {rt.wakeToken}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(map[string]string{"type": "quota_resume"}); err != nil {
		t.Fatal(err)
	}
	raw := readQuotaResumed(t, conn)
	if !strings.Contains(string(raw), `"started":false`) {
		t.Fatalf("payload=%s", raw)
	}
	paused, err := database.QueuePaused(s.ID)
	if err != nil || !paused || quotaQueueText(t, database, s.ID) != "stay queued" || quotaReply(database, s.ID) != "" {
		t.Fatalf("paused=%v queue=%q reply=%q err=%v", paused, quotaQueueText(t, database, s.ID), quotaReply(database, s.ID), err)
	}
	mustQuotaState(t, database, s.ID, db.QuotaResumeNone, "", 0)
}

func TestQuotaResumeFastSuccessDoesNotRestore(t *testing.T) {
	database := openQuotaResumeDB(t)
	s := newQuotaResumeSession(t, database)
	setQuotaResumeSwitch(t, database, true)
	pauseQuotaQueue(t, database, s.ID, "first")
	if _, err := database.EnqueueMessage(s.ID, "second", ""); err != nil {
		t.Fatal(err)
	}
	if err := database.SetQueuePaused(s.ID, true); err != nil {
		t.Fatal(err)
	}
	setQuotaWaiting(t, database, s.ID, "2026-11-18T08:00:00Z")
	rt, _ := startQuotaResumeApp(t, database)
	var started bool
	rt.resumeDue(context.Background(), quotaDueNow(), func(context.Context, string) (quota.Snapshot, error) {
		return quotaSessionSnap(40, ""), nil
	}, func(ctx context.Context, id string) (bool, error) {
		ok, err := rt.wakeQuota(ctx, id)
		started = ok
		return ok, err
	})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state, _, attempts, err := database.GetQuotaResume(s.ID)
		if err == nil && strings.Contains(quotaReply(database, s.ID), "ok:second") && quotaUserText(database, s.ID) == "first,second" && quotaQueueText(t, database, s.ID) == "" && state == db.QuotaResumeNone && attempts == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !started || quotaUserText(database, s.ID) != "first,second" || quotaQueueText(t, database, s.ID) != "" || !strings.Contains(quotaReplies(database, s.ID), "ok:first") || !strings.Contains(quotaReplies(database, s.ID), "ok:second") {
		t.Fatalf("started=%v users=%q replies=%q queue=%q", started, quotaUserText(database, s.ID), quotaReplies(database, s.ID), quotaQueueText(t, database, s.ID))
	}
	mustQuotaState(t, database, s.ID, db.QuotaResumeNone, "", 0)
}

func TestQuotaResumeStartupReportsError(t *testing.T) {
	database := openQuotaResumeDB(t)
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	err := recoverSessionsAfterStartup(database, time.Now())
	if err == nil || !strings.Contains(err.Error(), "重啟任務狀態恢復失敗") {
		t.Fatalf("err=%v", err)
	}
}

func TestQuotaResumeStartupContinues(t *testing.T) {
	path := t.TempDir() + "/restart.db"
	database, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	setQuotaResumeSwitch(t, database, true)
	resuming := newQuotaResumeSession(t, database)
	waiting := newQuotaResumeSession(t, database)
	exhausted := newQuotaResumeSession(t, database)
	pauseQuotaQueue(t, database, resuming.ID, "continue me")
	setQuotaWaiting(t, database, resuming.ID, "2026-11-18T08:00:00Z")
	if ok, err := database.TransitionQuotaResume(resuming.ID, db.QuotaResumeWaiting, db.QuotaResumeResuming, "", false); err != nil || !ok {
		t.Fatalf("resuming ok=%v err=%v", ok, err)
	}
	if ok, err := database.TransitionQuotaResume(resuming.ID, db.QuotaResumeResuming, db.QuotaResumeWaiting, "", false); err != nil || !ok {
		t.Fatalf("back ok=%v err=%v", ok, err)
	}
	if ok, err := database.TransitionQuotaResume(resuming.ID, db.QuotaResumeWaiting, db.QuotaResumeResuming, "", false); err != nil || !ok {
		t.Fatalf("resuming again ok=%v err=%v", ok, err)
	}
	setQuotaWaiting(t, database, waiting.ID, "2026-11-18T18:00:00Z")
	if ok, err := database.TransitionQuotaResume(exhausted.ID, db.QuotaResumeNone, db.QuotaResumeExhausted, "2026-11-18T09:00:00Z", false); err != nil || !ok {
		t.Fatalf("exhausted ok=%v err=%v", ok, err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	database, err = db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	startup := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	if err := recoverSessionsAfterStartup(database, startup); err != nil {
		t.Fatal(err)
	}
	mustQuotaState(t, database, resuming.ID, db.QuotaResumeWaiting, "2026-11-18T12:00:00Z", 2)
	mustQuotaState(t, database, waiting.ID, db.QuotaResumeWaiting, "2026-11-18T18:00:00Z", 0)
	mustQuotaState(t, database, exhausted.ID, db.QuotaResumeExhausted, "2026-11-18T09:00:00Z", 0)

	rt, _ := startQuotaResumeApp(t, database)
	var refreshes int
	rt.resumeDue(context.Background(), startup, func(context.Context, string) (quota.Snapshot, error) {
		refreshes++
		return quotaSessionSnap(40, ""), nil
	}, rt.wakeQuota)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state, _, attempts, err := database.GetQuotaResume(resuming.ID)
		if err == nil && quotaReply(database, resuming.ID) == "ok:continue me" && state == db.QuotaResumeNone && attempts == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if refreshes != 1 || quotaReply(database, resuming.ID) != "ok:continue me" || quotaUserText(database, resuming.ID) != "continue me" {
		t.Fatalf("refreshes=%d reply=%q users=%q", refreshes, quotaReply(database, resuming.ID), quotaUserText(database, resuming.ID))
	}
	mustQuotaState(t, database, resuming.ID, db.QuotaResumeNone, "", 0)
	mustQuotaState(t, database, waiting.ID, db.QuotaResumeWaiting, "2026-11-18T18:00:00Z", 0)
	mustQuotaState(t, database, exhausted.ID, db.QuotaResumeExhausted, "2026-11-18T09:00:00Z", 0)
}

func TestQuotaResumeOldWakeDoesNotStartNewGeneration(t *testing.T) {
	database := openQuotaResumeDB(t)
	s := newQuotaResumeSession(t, database)
	setQuotaResumeSwitch(t, database, true)
	pauseQuotaQueue(t, database, s.ID, "stay queued")
	setQuotaWaiting(t, database, s.ID, "2026-11-18T08:00:00Z")
	if ok, err := database.TransitionQuotaResume(s.ID, db.QuotaResumeWaiting, db.QuotaResumeResuming, "", false); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if err := database.CancelQuotaResume(s.ID); err != nil {
		t.Fatal(err)
	}
	if ok, err := database.TransitionQuotaResume(s.ID, db.QuotaResumeNone, db.QuotaResumeWaiting, "2026-11-18T09:00:00Z", false); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if ok, err := database.TransitionQuotaResume(s.ID, db.QuotaResumeWaiting, db.QuotaResumeResuming, "", false); err != nil || !ok {
		t.Fatal(ok, err)
	}
	rt, base := startQuotaResumeApp(t, database)
	conn, _, err := websocket.DefaultDialer.Dial(base+"/sessions/"+s.ID+"/ws", http.Header{roomWakeHeader: {rt.wakeToken}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(map[string]any{"type": "quota_resume", "generation": 1}); err != nil {
		t.Fatal(err)
	}
	raw := readQuotaResumed(t, conn)
	if !strings.Contains(string(raw), `"started":false`) {
		t.Fatalf("payload=%s", raw)
	}
	if quotaReply(database, s.ID) != "" || quotaQueueText(t, database, s.ID) != "stay queued" {
		t.Fatalf("reply=%q queue=%q", quotaReply(database, s.ID), quotaQueueText(t, database, s.ID))
	}
	mustQuotaState(t, database, s.ID, db.QuotaResumeResuming, "2026-11-18T09:00:00Z", 1)
	_, _, generation, err := database.SnapshotQuotaResume(s.ID)
	if err != nil || generation != 3 {
		t.Fatalf("generation=%d err=%v", generation, err)
	}
}

func TestQuotaResumeStaleDeliveryDoesNotRestoreNewGeneration(t *testing.T) {
	database := openQuotaResumeDB(t)
	s := newQuotaResumeSession(t, database)
	setQuotaResumeSwitch(t, database, true)
	setQuotaWaiting(t, database, s.ID, "2026-11-18T08:00:00Z")
	rt := newRoomRuntime(database, 0)
	rt.resumeDue(context.Background(), quotaDueNow(), func(context.Context, string) (quota.Snapshot, error) {
		return quotaSessionSnap(40, ""), nil
	}, func(context.Context, string) (bool, error) {
		if _, err := database.TransitionQuotaResume(s.ID, db.QuotaResumeResuming, db.QuotaResumeNone, "", false); err != nil {
			return false, err
		}
		if _, err := database.TransitionQuotaResume(s.ID, db.QuotaResumeNone, db.QuotaResumeWaiting, "2026-11-18T09:00:00Z", false); err != nil {
			return false, err
		}
		if _, err := database.TransitionQuotaResume(s.ID, db.QuotaResumeWaiting, db.QuotaResumeResuming, "", false); err != nil {
			return false, err
		}
		return false, errors.New("dial failed")
	})
	mustQuotaState(t, database, s.ID, db.QuotaResumeResuming, "2026-11-18T09:00:00Z", 1)
	_, _, generation, err := database.SnapshotQuotaResume(s.ID)
	if err != nil || generation != 2 {
		t.Fatalf("generation=%d err=%v", generation, err)
	}
}

func startQuotaResumeApp(t *testing.T, database *db.DB) (*roomRuntime, string) {
	t.Helper()
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	rt := newRoomRuntime(database, 0)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rt.address = ln.Addr().String()
	app.Get("/sessions/:id/ws", rt.wsAuth(func(c *fiber.Ctx) error {
		if token := strings.TrimSpace(c.Query("share")); token != "" {
			return guestAuth(database, c, token)
		}
		return c.Next()
	}), fiberws.New(ws.NewHandler(database, "", ws.ShellOpts{}, nil, tg.NotifyConfig{})))
	go func() { _ = app.Listener(ln) }()
	t.Cleanup(func() { _ = app.Shutdown() })
	return &rt, "ws://" + rt.address
}

func rejectQuotaResumed(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	for {
		_, payload, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if strings.Contains(string(payload), `"quota_resumed"`) {
			t.Fatalf("一般連線不該收到 quota_resumed: %s", payload)
		}
	}
}

func readQuotaResumed(t *testing.T, conn *websocket.Conn) []byte {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		_, payload, err := conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(payload), `"quota_resumed"`) {
			return payload
		}
	}
}

func openQuotaResumeDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/quota-resume.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func newQuotaResumeSession(t *testing.T, database *db.DB) *db.Session {
	t.Helper()
	s, err := database.CreateSession("resume", "", t.TempDir(), "default", "fakeq", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func setQuotaResumeSwitch(t *testing.T, database *db.DB, on bool) {
	t.Helper()
	raw := `{"autoResumeOnQuota":false,"quotaGuardEnabled":true,"quotaGuardPercent":90}`
	if on {
		raw = `{"autoResumeOnQuota":true,"quotaGuardEnabled":true,"quotaGuardPercent":90}`
	}
	if _, err := database.PutGeneralSettings([]byte(raw)); err != nil {
		t.Fatal(err)
	}
}

func pauseQuotaQueue(t *testing.T, database *db.DB, sessionID, content string) {
	t.Helper()
	if _, err := database.EnqueueMessage(sessionID, content, ""); err != nil {
		t.Fatal(err)
	}
	if err := database.SetQueuePaused(sessionID, true); err != nil {
		t.Fatal(err)
	}
}

func setQuotaWaiting(t *testing.T, database *db.DB, sessionID, at string) {
	t.Helper()
	ok, err := database.TransitionQuotaResume(sessionID, db.QuotaResumeNone, db.QuotaResumeWaiting, at, false)
	if err != nil || !ok {
		t.Fatalf("waiting ok=%v err=%v", ok, err)
	}
}

func mustQuotaState(t *testing.T, database *db.DB, sessionID, state, at string, attempts int) {
	t.Helper()
	gotState, gotAt, gotAttempts, err := database.GetQuotaResume(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if gotState != state || gotAt != at || gotAttempts != attempts {
		t.Fatalf("state=%s at=%s attempts=%d", gotState, gotAt, gotAttempts)
	}
}

func quotaDueNow() time.Time {
	return time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
}

func quotaPercent(v float64) *float64 { return &v }

func quotaSessionSnap(percent float64, reset string) quota.Snapshot {
	return quota.Snapshot{Windows: []quota.Window{{Key: "session", Percent: quotaPercent(percent), ResetsAt: reset}}}
}

func quotaQueueText(t *testing.T, database *db.DB, sessionID string) string {
	t.Helper()
	list, err := database.ListQueuedMessages(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	parts := make([]string, len(list))
	for i, item := range list {
		parts[i] = item.Content
	}
	return strings.Join(parts, ",")
}

func quotaUserText(database *db.DB, sessionID string) string {
	msgs, _ := database.ListMessages(sessionID)
	var parts []string
	for _, m := range msgs {
		if m.Role == "user" {
			parts = append(parts, m.Content)
		}
	}
	return strings.Join(parts, ",")
}

func quotaReplies(database *db.DB, sessionID string) string {
	msgs, _ := database.ListMessages(sessionID)
	var parts []string
	for _, m := range msgs {
		if m.Role == "claude" && m.Status == db.MessageStatusDone {
			parts = append(parts, m.Content)
		}
	}
	return strings.Join(parts, "|")
}

func quotaReply(database *db.DB, sessionID string) string {
	msgs, _ := database.ListMessages(sessionID)
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "claude" && msgs[i].Status == db.MessageStatusDone {
			return msgs[i].Content
		}
	}
	return ""
}
