package server

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fasthttp/websocket"
	"github.com/spf13/viper"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/quota"
)

type e2eRunner struct {
	mu    sync.Mutex
	fails int
	gate  chan error
	enter chan struct{}
}

func (r *e2eRunner) Name() string { return "fakeq" }

func (r *e2eRunner) Run(ctx context.Context, opts agent.RunOptions, cb agent.EventCallback) error {
	if strings.HasPrefix(opts.Prompt, "quota-fail") {
		r.mu.Lock()
		fail := r.fails == 0
		if fail {
			r.fails++
		}
		r.mu.Unlock()
		if fail {
			return errors.New("disk full")
		}
	}
	if strings.HasPrefix(opts.Prompt, "block") && r.gate != nil {
		if r.enter != nil {
			select {
			case <-r.enter:
			default:
				close(r.enter)
			}
		}
		select {
		case err := <-r.gate:
			if err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	cb(agent.Event{Type: agent.EventDelta, Text: "ok:" + opts.Prompt})
	cb(agent.Event{Type: agent.EventDone})
	return nil
}

func useE2ERunner(t *testing.T, runner agent.Runner) {
	t.Helper()
	agent.Register("fakeq", func() agent.Runner { return runner })
	t.Cleanup(func() {
		agent.Register("fakeq", func() agent.Runner { return quotaFakeRunner{} })
	})
}

type wsSeen struct {
	mu   sync.Mutex
	msgs []map[string]any
}

func (s *wsSeen) add(msg map[string]any) {
	s.mu.Lock()
	s.msgs = append(s.msgs, msg)
	s.mu.Unlock()
}

func (s *wsSeen) seen(state string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, msg := range s.msgs {
		typ, _ := msg["type"].(string)
		if typ != "sync" && typ != "queue_update" {
			continue
		}
		if msg["quota_resume_state"] == state {
			return true
		}
	}
	return false
}

func watchSocket(t *testing.T, base, id string) *wsSeen {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(strings.Replace(base, "http", "ws", 1)+"/sessions/"+id+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	seen := &wsSeen{}
	go func() {
		for {
			var msg map[string]any
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			seen.add(msg)
		}
	}()
	return seen
}

func startQuotaE2E(t *testing.T, refresh func(context.Context, string) (quota.Snapshot, error), seed quota.Snapshot, prepare func(*db.DB)) (*db.DB, string) {
	t.Helper()
	t.Setenv("PATH", "")
	t.Setenv("CC_AGY_BIN", "agy")
	viper.Reset()
	t.Cleanup(viper.Reset)

	dir := t.TempDir()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	dbPath := filepath.Join(dir, "app.db")
	body := "no_auth: true\nserver:\n  port: " + strconv.Itoa(port) + "\ndb:\n  path: " + quote(filepath.ToSlash(dbPath)) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if prepare != nil {
		prepare(database)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	srv, err := StartIsolated(context.Background(), refresh, "fakeq", seed)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
	})
	opened, err := db.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { opened.Close() })
	return opened, srv.URL()
}

func quote(s string) string { return `"` + s + `"` }

func waitState(t *testing.T, database *db.DB, id, want string, timeout time.Duration) (string, string, int) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var state, at string
	var attempts int
	var err error
	for time.Now().Before(deadline) {
		state, at, attempts, err = database.GetQuotaResume(id)
		if err != nil {
			t.Fatal(err)
		}
		if state == want {
			return state, at, attempts
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("session %s state=%s at=%s want %s", id, state, at, want)
	return state, at, attempts
}

func putGeneral(t *testing.T, base, body string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, base+"/settings/general", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 {
		t.Fatalf("PUT settings %d %s", res.StatusCode, raw)
	}
}

func TestQuotaE2ERealWatchContinues(t *testing.T) {
	runner := &e2eRunner{}
	useE2ERunner(t, runner)
	var calls atomic.Int32
	var recovered atomic.Bool
	refresh := func(context.Context, string) (quota.Snapshot, error) {
		calls.Add(1)
		if !recovered.Load() {
			reset := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Second).Format(time.RFC3339)
			return quotaSessionSnap(100, reset), nil
		}
		return quotaSessionSnap(40, ""), nil
	}
	past := time.Now().UTC().Add(-time.Minute).Truncate(time.Second).Format(time.RFC3339)
	var waitingID, resumingID, liveID string
	database, base := startQuotaE2E(t, refresh, quota.Snapshot{}, func(d *db.DB) {
		setQuotaResumeSwitch(t, d, true)
		work := t.TempDir()
		waiting := newNamedQuotaSession(t, d, work, "waiting-restart")
		resuming := newNamedQuotaSession(t, d, work, "resuming-restart")
		live := newNamedQuotaSession(t, d, work, "live")
		waitingID, resumingID, liveID = waiting.ID, resuming.ID, live.ID
		pauseQuotaQueue(t, d, waiting.ID, "waiting-restart")
		pauseQuotaQueue(t, d, resuming.ID, "resuming-restart")
		setQuotaWaiting(t, d, waiting.ID, past)
		setQuotaWaiting(t, d, resuming.ID, past)
		if ok, err := d.TransitionQuotaResume(resuming.ID, db.QuotaResumeWaiting, db.QuotaResumeResuming, past, false); err != nil || !ok {
			t.Fatalf("resuming ok=%v err=%v", ok, err)
		}
	})
	time.Sleep(300 * time.Millisecond)
	if calls.Load() != 0 {
		t.Fatalf("startup refresh calls=%d", calls.Load())
	}
	waitingSeen := watchSocket(t, base, waitingID)
	resumingSeen := watchSocket(t, base, resumingID)
	liveSeen := watchSocket(t, base, liveID)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && (!waitingSeen.seen("waiting") || !resumingSeen.seen("waiting")) {
		time.Sleep(50 * time.Millisecond)
	}
	if !waitingSeen.seen("waiting") || !resumingSeen.seen("waiting") {
		t.Fatal("重啟後的 sync 沒有帶 waiting")
	}
	conn, _, err := websocket.DefaultDialer.Dial(strings.Replace(base, "http", "ws", 1)+"/sessions/"+liveID+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := conn.WriteJSON(map[string]string{"type": "input", "data": "quota-fail continue"}); err != nil {
		t.Fatal(err)
	}
	waitState(t, database, liveID, db.QuotaResumeWaiting, 8*time.Second)
	if quotaQueueText(t, database, liveID) != "quota-fail continue" {
		t.Fatalf("queue=%s", quotaQueueText(t, database, liveID))
	}
	recovered.Store(true)
	waitState(t, database, liveID, db.QuotaResumeNone, 45*time.Second)
	waitState(t, database, waitingID, db.QuotaResumeNone, 5*time.Second)
	waitState(t, database, resumingID, db.QuotaResumeNone, 5*time.Second)
	if quotaReply(database, liveID) != "ok:quota-fail continue" || !strings.Contains(quotaUserText(database, liveID), "quota-fail continue") || quotaQueueText(t, database, liveID) != "" {
		t.Fatalf("live reply=%q user=%q queue=%q", quotaReply(database, liveID), quotaUserText(database, liveID), quotaQueueText(t, database, liveID))
	}
	if quotaReply(database, waitingID) != "ok:waiting-restart" || quotaReply(database, resumingID) != "ok:resuming-restart" {
		t.Fatalf("restart replies waiting=%q resuming=%q", quotaReply(database, waitingID), quotaReply(database, resumingID))
	}
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !liveSeen.seen("none") {
		time.Sleep(50 * time.Millisecond)
	}
	if !liveSeen.seen("waiting") || !liveSeen.seen("none") {
		t.Fatal("live socket 沒有走完 waiting 到 none")
	}
}

func TestQuotaE2ERealWatchDelays(t *testing.T) {
	useE2ERunner(t, quotaFakeRunner{})
	reset := time.Now().UTC().Add(45 * time.Second).Truncate(time.Second)
	wantAt := reset.Add(time.Minute).Truncate(time.Second).Format(time.RFC3339)
	refresh := func(context.Context, string) (quota.Snapshot, error) {
		return quotaSessionSnap(100, reset.Format(time.RFC3339)), nil
	}
	past := time.Now().UTC().Add(-time.Minute).Truncate(time.Second).Format(time.RFC3339)
	var id string
	database, base := startQuotaE2E(t, refresh, quota.Snapshot{}, func(d *db.DB) {
		setQuotaResumeSwitch(t, d, true)
		s := newNamedQuotaSession(t, d, t.TempDir(), "delay")
		id = s.ID
		pauseQuotaQueue(t, d, s.ID, "keep")
		setQuotaWaiting(t, d, s.ID, past)
	})
	seen := watchSocket(t, base, id)
	deadline := time.Now().Add(45 * time.Second)
	var at string
	var attempts int
	for {
		state, gotAt, gotAttempts, err := database.GetQuotaResume(id)
		if err != nil {
			t.Fatal(err)
		}
		at, attempts = gotAt, gotAttempts
		if state == db.QuotaResumeWaiting && at == wantAt {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("at=%s attempts=%d queue=%s reply=%q", at, attempts, quotaQueueText(t, database, id), quotaReply(database, id))
		}
		time.Sleep(100 * time.Millisecond)
	}
	if attempts != 0 || quotaQueueText(t, database, id) != "keep" || quotaReply(database, id) != "" {
		t.Fatalf("at=%s attempts=%d queue=%s reply=%q", at, attempts, quotaQueueText(t, database, id), quotaReply(database, id))
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !seen.seen("waiting") {
		time.Sleep(20 * time.Millisecond)
	}
	if !seen.seen("waiting") {
		t.Fatal("delay 連線沒有收到 waiting")
	}
	again, _, err := websocket.DefaultDialer.Dial(strings.Replace(base, "http", "ws", 1)+"/sessions/"+id+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	_ = again.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		var msg map[string]any
		if err := again.ReadJSON(&msg); err != nil {
			t.Fatal(err)
		}
		if msg["type"] != "sync" {
			continue
		}
		if msg["quota_resume_state"] != "waiting" || msg["quota_resume_at"] != wantAt {
			t.Fatalf("reconnect sync state=%v at=%v", msg["quota_resume_state"], msg["quota_resume_at"])
		}
		break
	}
}

func TestQuotaE2ESwitchOffStays(t *testing.T) {
	useE2ERunner(t, quotaFakeRunner{})
	refresh := func(context.Context, string) (quota.Snapshot, error) {
		return quotaSessionSnap(40, ""), nil
	}
	past := time.Now().UTC().Add(-time.Minute).Truncate(time.Second).Format(time.RFC3339)
	var id string
	database, base := startQuotaE2E(t, refresh, quota.Snapshot{}, func(d *db.DB) {
		setQuotaResumeSwitch(t, d, true)
		s := newNamedQuotaSession(t, d, t.TempDir(), "off")
		id = s.ID
		pauseQuotaQueue(t, d, s.ID, "stay")
		setQuotaWaiting(t, d, s.ID, past)
	})
	seen := watchSocket(t, base, id)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !seen.seen("waiting") {
		time.Sleep(20 * time.Millisecond)
	}
	if !seen.seen("waiting") {
		t.Fatal("關開關前沒有 waiting")
	}
	putGeneral(t, base, `{"autoResumeOnQuota":false,"quotaGuardEnabled":true,"quotaGuardPercent":90}`)
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !seen.seen("none") {
		time.Sleep(20 * time.Millisecond)
	}
	if !seen.seen("none") {
		t.Fatal("關開關後連線沒有收到 none")
	}
	time.Sleep(35 * time.Second)
	state, at, attempts, err := database.GetQuotaResume(id)
	if err != nil {
		t.Fatal(err)
	}
	if state != db.QuotaResumeNone || at != "" || attempts != 0 || quotaQueueText(t, database, id) != "stay" || quotaReply(database, id) != "" {
		t.Fatalf("state=%s at=%s attempts=%d queue=%s reply=%q", state, at, attempts, quotaQueueText(t, database, id), quotaReply(database, id))
	}
}

func TestQuotaE2EGuardThreshold(t *testing.T) {
	runner := &e2eRunner{gate: make(chan error, 1), enter: make(chan struct{})}
	useE2ERunner(t, runner)
	refresh := func(context.Context, string) (quota.Snapshot, error) {
		return quotaSessionSnap(50, ""), nil
	}
	var id string
	database, base := startQuotaE2E(t, refresh, quotaSessionSnap(50, ""), func(d *db.DB) {
		setQuotaResumeSwitch(t, d, false)
		s := newNamedQuotaSession(t, d, t.TempDir(), "guard")
		id = s.ID
	})
	conn, _, err := websocket.DefaultDialer.Dial(strings.Replace(base, "http", "ws", 1)+"/sessions/"+id+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	go func() {
		for {
			var msg map[string]any
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
		}
	}()
	if err := conn.WriteJSON(map[string]string{"type": "input", "data": "block A"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runner.enter:
	case <-time.After(5 * time.Second):
		t.Fatal("runner 沒有進入 block")
	}
	if err := conn.WriteJSON(map[string]string{"type": "input", "data": "next"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && quotaQueueText(t, database, id) != "next" {
		time.Sleep(20 * time.Millisecond)
	}
	if quotaQueueText(t, database, id) != "next" {
		t.Fatalf("queue=%s", quotaQueueText(t, database, id))
	}
	putGeneral(t, base, `{"autoResumeOnQuota":false,"quotaGuardEnabled":true,"quotaGuardPercent":40}`)
	runner.gate <- nil
	deadline = time.Now().Add(5 * time.Second)
	var paused bool
	for time.Now().Before(deadline) {
		paused, err = database.QueuePaused(id)
		if err != nil {
			t.Fatal(err)
		}
		if paused && quotaQueueText(t, database, id) == "next" && quotaUserText(database, id) == "block A" {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if !paused || quotaQueueText(t, database, id) != "next" || quotaUserText(database, id) != "block A" {
		t.Fatalf("paused=%v queue=%s user=%s", paused, quotaQueueText(t, database, id), quotaUserText(database, id))
	}
}

func newNamedQuotaSession(t *testing.T, database *db.DB, work, name string) *db.Session {
	t.Helper()
	s, err := database.CreateSession(name, "", work, "default", "fakeq", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestQuotaE2EPMLiveDelay(t *testing.T) {
	useE2ERunner(t, quotaFakeRunner{})
	now := time.Now().UTC().Truncate(time.Second)
	reset := now.Add(45 * time.Second)
	wantAt := reset.Add(time.Minute).Format(time.RFC3339)
	refresh := func(context.Context, string) (quota.Snapshot, error) {
		return quotaSessionSnap(100, reset.Format(time.RFC3339)), nil
	}
	var id string
	database, base := startQuotaE2E(t, refresh, quota.Snapshot{}, func(d *db.DB) {
		setQuotaResumeSwitch(t, d, true)
		s := newNamedQuotaSession(t, d, t.TempDir(), "live-delay")
		id = s.ID
		pauseQuotaQueue(t, d, id, "keep")
		setQuotaWaiting(t, d, id, now.Add(-time.Minute).Format(time.RFC3339))
	})
	seen := watchSocket(t, base, id)
	deadline := time.Now().Add(time.Second)
	for !seen.seen("waiting") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	(roomRuntime{db: database}).resumeDue(context.Background(), now, refresh, nil)
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		seen.mu.Lock()
		found := false
		for _, m := range seen.msgs {
			if m["type"] == "queue_update" && m["quota_resume_at"] == wantAt {
				found = true
			}
		}
		seen.mu.Unlock()
		if found {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	_, at, _, _ := database.GetQuotaResume(id)
	t.Fatalf("connected client kept old planned time; database at=%s want=%s", at, wantAt)
}

func TestQuotaE2EPMCancelRearmDuringRefresh(t *testing.T) {
	database := openQuotaResumeDB(t)
	setQuotaResumeSwitch(t, database, true)
	s := newNamedQuotaSession(t, database, t.TempDir(), "rearm")
	pauseQuotaQueue(t, database, s.ID, "keep")
	now := time.Now().UTC().Truncate(time.Second)
	future := now.Add(time.Hour).Format(time.RFC3339)
	setQuotaWaiting(t, database, s.ID, now.Add(-time.Minute).Format(time.RFC3339))
	refresh := func(context.Context, string) (quota.Snapshot, error) {
		if err := database.CancelQuotaResume(s.ID); err != nil {
			t.Fatal(err)
		}
		setQuotaWaiting(t, database, s.ID, future)
		return quotaSessionSnap(40, ""), nil
	}
	wakes := 0
	(roomRuntime{db: database}).resumeDue(context.Background(), now, refresh, func(context.Context, string) (bool, error) {
		wakes++
		return true, nil
	})
	state, at, _, err := database.GetQuotaResume(s.ID)
	if err != nil || wakes != 0 || state != db.QuotaResumeWaiting || at != future {
		t.Fatalf("stale refresh started new schedule: wakes=%d state=%s at=%s err=%v", wakes, state, at, err)
	}
}

func TestQuotaE2EPMDelayCancelRearm(t *testing.T) {
	database := openQuotaResumeDB(t)
	setQuotaResumeSwitch(t, database, true)
	s := newNamedQuotaSession(t, database, t.TempDir(), "delay-rearm")
	pauseQuotaQueue(t, database, s.ID, "keep")
	now := time.Now().UTC().Truncate(time.Second)
	oldAt := now.Add(-time.Minute).Format(time.RFC3339)
	future := now.Add(time.Hour).Format(time.RFC3339)
	setQuotaWaiting(t, database, s.ID, oldAt)
	reset := now.Add(2 * time.Minute)
	refresh := func(context.Context, string) (quota.Snapshot, error) {
		if err := database.CancelQuotaResume(s.ID); err != nil {
			t.Fatal(err)
		}
		setQuotaWaiting(t, database, s.ID, future)
		return quotaSessionSnap(100, reset.Format(time.RFC3339)), nil
	}
	(roomRuntime{db: database}).resumeDue(context.Background(), now, refresh, nil)
	state, at, attempts, err := database.GetQuotaResume(s.ID)
	if err != nil || state != db.QuotaResumeWaiting || at != future || attempts != 0 {
		t.Fatalf("stale delay rewrote new schedule: state=%s at=%s attempts=%d want=%s err=%v", state, at, attempts, future, err)
	}
	if quotaQueueText(t, database, s.ID) != "keep" {
		t.Fatalf("queue=%s", quotaQueueText(t, database, s.ID))
	}
}

func TestQuotaE2EPMParallelDelayThenStaleStart(t *testing.T) {
	database := openQuotaResumeDB(t)
	setQuotaResumeSwitch(t, database, true)
	s := newNamedQuotaSession(t, database, t.TempDir(), "parallel")
	pauseQuotaQueue(t, database, s.ID, "keep")
	now := time.Now().UTC().Truncate(time.Second)
	setQuotaWaiting(t, database, s.ID, now.Add(-time.Minute).Format(time.RFC3339))
	reset := now.Add(45 * time.Second)
	wantAt := reset.Add(time.Minute).Truncate(time.Second).Format(time.RFC3339)
	entered := make(chan struct{})
	delayDone := make(chan struct{})
	finished := make(chan struct{})
	var wakes atomic.Int32
	go func() {
		defer close(finished)
		(roomRuntime{db: database}).resumeDue(context.Background(), now, func(context.Context, string) (quota.Snapshot, error) {
			close(entered)
			<-delayDone
			return quotaSessionSnap(40, ""), nil
		}, func(context.Context, string) (bool, error) {
			wakes.Add(1)
			return true, nil
		})
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("start refresh did not observe the due row")
	}
	(roomRuntime{db: database}).resumeDue(context.Background(), now, func(context.Context, string) (quota.Snapshot, error) {
		return quotaSessionSnap(100, reset.Format(time.RFC3339)), nil
	}, nil)
	close(delayDone)
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("stale start did not finish")
	}
	state, at, attempts, err := database.GetQuotaResume(s.ID)
	if err != nil || state != db.QuotaResumeWaiting || at != wantAt || attempts != 0 || wakes.Load() != 0 {
		t.Fatalf("stale start applied after delay: wakes=%d state=%s at=%s attempts=%d want=%s err=%v", wakes.Load(), state, at, attempts, wantAt, err)
	}
}

func TestQuotaE2EPMOffOnDuringRefresh(t *testing.T) {
	database := openQuotaResumeDB(t)
	setQuotaResumeSwitch(t, database, true)
	s := newNamedQuotaSession(t, database, t.TempDir(), "off-on")
	pauseQuotaQueue(t, database, s.ID, "keep")
	now := time.Now().UTC().Truncate(time.Second)
	future := now.Add(time.Hour).Format(time.RFC3339)
	setQuotaWaiting(t, database, s.ID, now.Add(-time.Minute).Format(time.RFC3339))
	refresh := func(context.Context, string) (quota.Snapshot, error) {
		setQuotaResumeSwitch(t, database, false)
		setQuotaResumeSwitch(t, database, true)
		setQuotaWaiting(t, database, s.ID, future)
		return quotaSessionSnap(40, ""), nil
	}
	wakes := 0
	(roomRuntime{db: database}).resumeDue(context.Background(), now, refresh, func(context.Context, string) (bool, error) {
		wakes++
		return true, nil
	})
	state, at, _, err := database.GetQuotaResume(s.ID)
	if err != nil || wakes != 0 || state != db.QuotaResumeWaiting || at != future {
		t.Fatalf("off/on refresh started new schedule: wakes=%d state=%s at=%s err=%v", wakes, state, at, err)
	}
}

func TestQuotaE2EIsolatedLoopback(t *testing.T) {
	_, base := startQuotaE2E(t, func(context.Context, string) (quota.Snapshot, error) {
		return quotaSessionSnap(40, ""), nil
	}, quota.Snapshot{}, nil)
	if !strings.HasPrefix(base, "http://127.0.0.1:") {
		t.Fatalf("url=%s", base)
	}
	res, err := http.Get(base + "/robots.txt")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	_, port, err := net.SplitHostPort(strings.TrimPrefix(base, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	ip := nonLoopbackIPv4(t)
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(ip, port), 300*time.Millisecond)
	if err == nil {
		conn.Close()
		t.Fatalf("non-loopback %s accepted on %s", ip, port)
	}
}

func nonLoopbackIPv4(t *testing.T) string {
	t.Helper()
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok || ipnet.IP.IsLoopback() {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil {
				continue
			}
			return ip4.String()
		}
	}
	t.Fatal("no non-loopback ipv4")
	return ""
}
