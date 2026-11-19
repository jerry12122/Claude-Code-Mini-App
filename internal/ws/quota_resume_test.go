package ws

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fasthttp/websocket"
	fiberws "github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/quota"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/tg"
)

func openQuotaDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/quota-resume.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func quotaSession(t *testing.T, database *db.DB, name, agentType string) *db.Session {
	t.Helper()
	s, err := database.CreateSession(name, "", t.TempDir(), "default", agentType, nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func setAutoResume(t *testing.T, database *db.DB, on bool) {
	t.Helper()
	raw := `{"autoResumeOnQuota":false,"quotaGuardEnabled":true,"quotaGuardPercent":90}`
	if on {
		raw = `{"autoResumeOnQuota":true,"quotaGuardEnabled":true,"quotaGuardPercent":90}`
	}
	if _, err := database.PutGeneralSettings([]byte(raw)); err != nil {
		t.Fatal(err)
	}
}

func percentOf(v float64) *float64 { return &v }

func fullSessionSnap() quota.Snapshot {
	return quota.Snapshot{Windows: []quota.Window{{
		Key: "session", Percent: percentOf(100), ResetsAt: "2026-11-18T10:00:00Z",
	}}}
}

func queueText(t *testing.T, database *db.DB, sessionID string) string {
	t.Helper()
	list, err := database.ListQueuedMessages(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	parts := make([]string, len(list))
	for i, q := range list {
		parts[i] = q.Content
	}
	return strings.Join(parts, ",")
}

type refreshSpy struct {
	mu    sync.Mutex
	snap  quota.Snapshot
	err   error
	calls int
}

func (r *refreshSpy) call(context.Context, string) (quota.Snapshot, error) {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	return r.snap, r.err
}

func (r *refreshSpy) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func TestQuotaResumeDetectionBoundaries(t *testing.T) {
	now := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	rows := []struct {
		name    string
		on      bool
		failed  bool
		cancel  bool
		perm    bool
		userID  int64
		agent   string
		snap    quota.Snapshot
		refresh error
		want    bool
		calls   int
	}{
		{name: "100與有效reset", on: true, failed: true, userID: 1, agent: "fakeq", snap: fullSessionSnap(), want: true, calls: 1},
		{name: "99", on: true, failed: true, userID: 1, agent: "fakeq", snap: quota.Snapshot{Windows: []quota.Window{{Key: "session", Percent: percentOf(99), ResetsAt: "2026-11-18T10:00:00Z"}}}, calls: 1},
		{name: "100缺reset", on: true, failed: true, userID: 1, agent: "fakeq", snap: quota.Snapshot{Windows: []quota.Window{{Key: "session", Percent: percentOf(100)}}}, calls: 1},
		{name: "只有weekly", on: true, failed: true, userID: 1, agent: "fakeq", snap: quota.Snapshot{Windows: []quota.Window{{Key: "weekly", Percent: percentOf(100), ResetsAt: "2026-11-18T10:00:00Z"}}}, calls: 1},
		{name: "stale", on: true, failed: true, userID: 1, agent: "fakeq", snap: quota.Snapshot{Stale: true, Windows: fullSessionSnap().Windows}, calls: 1},
		{name: "missing", on: true, failed: true, userID: 1, agent: "fakeq", refresh: errors.New("refresh down"), calls: 1},
		{name: "使用者取消", on: true, failed: true, cancel: true, userID: 1, agent: "fakeq", snap: fullSessionSnap()},
		{name: "等待授權", on: true, failed: true, perm: true, userID: 1, agent: "fakeq", snap: fullSessionSnap()},
		{name: "開關關", on: false, failed: true, userID: 1, agent: "fakeq", snap: fullSessionSnap()},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			database := openQuotaDB(t)
			s := quotaSession(t, database, "det", row.agent)
			setAutoResume(t, database, row.on)
			msg, err := database.AddUserMessageWithAttachments(s.ID, "refactor X", "甲", nil)
			if err != nil {
				t.Fatal(err)
			}
			userID := row.userID
			if userID > 0 {
				userID = msg.ID
			}
			spy := &refreshSpy{snap: row.snap, err: row.refresh}
			err = finishQuotaResume(database, s.ID, row.agent, userID, row.failed, row.cancel, row.perm, spy.call, now)
			if row.want {
				if err != nil {
					t.Fatal(err)
				}
				state, at, attempts, err := database.GetQuotaResume(s.ID)
				if err != nil {
					t.Fatal(err)
				}
				if state != db.QuotaResumeWaiting || at != "2026-11-18T10:01:00Z" || attempts != 0 {
					t.Fatalf("state=%s at=%s attempts=%d", state, at, attempts)
				}
				if queueText(t, database, s.ID) != "refactor X" {
					t.Fatalf("queue=%s", queueText(t, database, s.ID))
				}
				paused, err := database.QueuePaused(s.ID)
				if err != nil || !paused {
					t.Fatalf("paused=%v err=%v", paused, err)
				}
			} else if err != nil {
				t.Fatal(err)
			} else {
				state, _, _, err := database.GetQuotaResume(s.ID)
				if err != nil {
					t.Fatal(err)
				}
				if state != db.QuotaResumeNone || queueText(t, database, s.ID) != "" {
					t.Fatalf("不該武裝 state=%s queue=%s", state, queueText(t, database, s.ID))
				}
			}
			if spy.count() != row.calls {
				t.Fatalf("refresh %d 次，want %d", spy.count(), row.calls)
			}
		})
	}
}

func TestQuotaResumeIgnoresUnusableWindows(t *testing.T) {
	now := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	nan := math.NaN()
	inf := math.Inf(1)
	database := openQuotaDB(t)
	s := quotaSession(t, database, "badwin", "fakeq")
	setAutoResume(t, database, true)
	msg, err := database.AddUserMessageWithAttachments(s.ID, "A", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	snaps := []quota.Snapshot{
		{Windows: []quota.Window{{Key: "session", Percent: percentOf(100), ResetsAt: "someday"}}},
		{Windows: []quota.Window{{Key: "session", ResetsAt: "2026-11-18T10:00:00Z"}}},
		{Windows: []quota.Window{{Key: "session", Percent: &nan, ResetsAt: "2026-11-18T10:00:00Z"}}},
		{Windows: []quota.Window{{Key: "session", Percent: &inf, ResetsAt: "2026-11-18T10:00:00Z"}}},
	}
	for i, snap := range snaps {
		spy := &refreshSpy{snap: snap}
		if err := finishQuotaResume(database, s.ID, "fakeq", msg.ID, true, false, false, spy.call, now); err != nil {
			t.Fatal(err)
		}
		state, _, _, err := database.GetQuotaResume(s.ID)
		if err != nil || state != db.QuotaResumeNone || spy.count() != 1 {
			t.Fatalf("case %d state=%s calls=%d err=%v", i, state, spy.count(), err)
		}
	}
}

func TestQuotaResumeQueueOrderAuthorAndAgain(t *testing.T) {
	now := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	database := openQuotaDB(t)
	s := quotaSession(t, database, "order", "fakeq")
	setAutoResume(t, database, true)
	if err := database.AddAttachment(&db.Attachment{
		ID: "att1", SessionID: s.ID, Name: "note.txt", StorageName: "att1.txt", MimeType: "text/plain", Size: 4,
	}); err != nil {
		t.Fatal(err)
	}
	msg, err := database.AddUserMessageWithAttachments(s.ID, "A", "訪客", []string{"att1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{"B", "C"} {
		if _, err := database.EnqueueMessage(s.ID, content, ""); err != nil {
			t.Fatal(err)
		}
	}
	spy := &refreshSpy{snap: fullSessionSnap()}
	if err := finishQuotaResume(database, s.ID, "fakeq", msg.ID, true, false, false, spy.call, now); err != nil {
		t.Fatal(err)
	}
	list, err := database.ListQueuedMessages(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if queueText(t, database, s.ID) != "A,B,C" || list[0].Author != "訪客" || len(list[0].AttachmentIDs) != 1 || list[0].AttachmentIDs[0] != "att1" {
		t.Fatalf("queue=%+v", list)
	}
	paused, err := database.QueuePaused(s.ID)
	if err != nil || !paused {
		t.Fatalf("paused=%v err=%v", paused, err)
	}

	if _, err := database.PopQueuedMessage(s.ID); err != nil {
		t.Fatal(err)
	}
	ok, err := database.TransitionQuotaResume(s.ID, db.QuotaResumeWaiting, db.QuotaResumeResuming, "2026-11-18T10:01:00Z", false)
	if err != nil || !ok {
		t.Fatalf("轉 resuming ok=%v err=%v", ok, err)
	}
	owned, generation := claimQuotaResume(database, s.ID)
	if !owned {
		t.Fatal("再次用盡時應持有這一代 resuming")
	}
	if err := finishHeldQuotaResume(database, s.ID, "fakeq", msg.ID, true, false, false, spy.call, now, generation, generation); err != nil {
		t.Fatal(err)
	}
	state, _, attempts, err := database.GetQuotaResume(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state != db.QuotaResumeWaiting || attempts != 1 || queueText(t, database, s.ID) != "A,B,C" {
		t.Fatalf("再次用盡 state=%s attempts=%d queue=%s", state, attempts, queueText(t, database, s.ID))
	}
}

func TestQuotaResumeEmptyQueueAndLimit(t *testing.T) {
	now := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	database := openQuotaDB(t)
	s := quotaSession(t, database, "empty", "fakeq")
	setAutoResume(t, database, true)
	if err := database.SetSessionLastError(s.ID, "原始失敗"); err != nil {
		t.Fatal(err)
	}
	msg, err := database.AddUserMessageWithAttachments(s.ID, "refactor X", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	spy := &refreshSpy{snap: fullSessionSnap()}
	if err := finishQuotaResume(database, s.ID, "fakeq", msg.ID, true, false, false, spy.call, now); err != nil {
		t.Fatal(err)
	}
	if queueText(t, database, s.ID) != "refactor X" {
		t.Fatal(queueText(t, database, s.ID))
	}
	paused, _ := database.QueuePaused(s.ID)
	if !paused {
		t.Fatal("空佇列也要暫停")
	}
	text, _ := lastError(database, s.ID)
	if text != "原始失敗" {
		t.Fatalf("失敗原因被清掉: %q", text)
	}

	if _, err := database.PopQueuedMessage(s.ID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if i > 0 {
			if _, err := database.TransitionQuotaResume(s.ID, db.QuotaResumeResuming, db.QuotaResumeWaiting, "2026-11-18T10:01:00Z", false); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := database.TransitionQuotaResume(s.ID, db.QuotaResumeWaiting, db.QuotaResumeResuming, "2026-11-18T10:01:00Z", false); err != nil {
			t.Fatal(err)
		}
	}
	_, _, attempts, err := database.GetQuotaResume(s.ID)
	if err != nil || attempts != 3 {
		t.Fatalf("attempts=%d err=%v", attempts, err)
	}
	owned, generation := claimQuotaResume(database, s.ID)
	if !owned {
		t.Fatal("第三次應持有 resuming")
	}
	if err := finishHeldQuotaResume(database, s.ID, "fakeq", msg.ID, true, false, false, spy.call, now, generation, generation); err != nil {
		t.Fatal(err)
	}
	state, _, attempts, err := database.GetQuotaResume(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	paused, _ = database.QueuePaused(s.ID)
	text, _ = lastError(database, s.ID)
	if state != db.QuotaResumeExhausted || attempts != 3 || !paused || queueText(t, database, s.ID) != "refactor X" || text != "原始失敗" {
		t.Fatalf("上限 state=%s attempts=%d paused=%v queue=%s text=%s", state, attempts, paused, queueText(t, database, s.ID), text)
	}
}

func TestQuotaResumeDuplicateAndDataFailure(t *testing.T) {
	now := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	database := openQuotaDB(t)
	s := quotaSession(t, database, "dup", "fakeq")
	other := quotaSession(t, database, "other", "fakeq")
	setAutoResume(t, database, true)
	msg, err := database.AddUserMessageWithAttachments(s.ID, "A", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := database.AddUserMessageWithAttachments(other.ID, "別的", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	spy := &refreshSpy{snap: fullSessionSnap()}
	if err := finishQuotaResume(database, s.ID, "fakeq", msg.ID, true, false, false, spy.call, now); err != nil {
		t.Fatal(err)
	}
	if err := finishQuotaResume(database, s.ID, "fakeq", msg.ID, true, false, false, spy.call, now); err != nil {
		t.Fatal(err)
	}
	if queueText(t, database, s.ID) != "A" {
		t.Fatalf("重複收尾入列 %s", queueText(t, database, s.ID))
	}

	fresh := quotaSession(t, database, "miss", "fakeq")
	err = finishQuotaResume(database, fresh.ID, "fakeq", foreign.ID, true, false, false, spy.call, now)
	if err == nil {
		t.Fatal("別的 session 的訊息不該假裝成功")
	}
	state, _, _, gerr := database.GetQuotaResume(fresh.ID)
	if gerr != nil || state != db.QuotaResumeNone || queueText(t, database, fresh.ID) != "" {
		t.Fatalf("失敗後 state=%s queue=%s err=%v", state, queueText(t, database, fresh.ID), gerr)
	}

	var wg sync.WaitGroup
	raceDB := openQuotaDB(t)
	raceSession := quotaSession(t, raceDB, "race", "fakeq")
	setAutoResume(t, raceDB, true)
	raceMsg, err := raceDB.AddUserMessageWithAttachments(raceSession.ID, "A", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = finishQuotaResume(raceDB, raceSession.ID, "fakeq", raceMsg.ID, true, false, false, (&refreshSpy{snap: fullSessionSnap()}).call, now)
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if queueText(t, raceDB, raceSession.ID) != "A" {
		t.Fatalf("並行收尾 queue=%s", queueText(t, raceDB, raceSession.ID))
	}
}

func TestQuotaResumeSkipsSystemAndTelegram(t *testing.T) {
	now := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	database := openQuotaDB(t)
	setAutoResume(t, database, true)
	sys := quotaSession(t, database, "sys", "fakeq")
	spy := &refreshSpy{snap: fullSessionSnap()}
	if err := finishQuotaResume(database, sys.ID, "fakeq", 0, true, false, false, spy.call, now); err != nil {
		t.Fatal(err)
	}
	tg := quotaSession(t, database, "tg", agent.TypeTelegram)
	msg, err := database.AddUserMessageWithAttachments(tg.ID, "hi", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := finishQuotaResume(database, tg.ID, agent.TypeTelegram, msg.ID, true, false, false, spy.call, now); err != nil {
		t.Fatal(err)
	}
	if spy.count() != 0 {
		t.Fatalf("系統重試與 Telegram 不該刷新，calls=%d", spy.count())
	}
	for _, id := range []string{sys.ID, tg.ID} {
		state, _, _, err := database.GetQuotaResume(id)
		if err != nil || state != db.QuotaResumeNone {
			t.Fatalf("%s state=%s err=%v", id, state, err)
		}
	}
}

func TestQuotaResumeHandlerFinishDoesNotRearmAfterNewInput(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/ws.db")
	if err != nil {
		t.Fatal(err)
	}
	spy := &refreshSpy{snap: fullSessionSnap()}
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Get("/sessions/:id/ws", fiberws.New(NewHandler(database, "", ShellOpts{}, nil, tg.NotifyConfig{}, WithQuotaResumeRefresh(spy.call))))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = app.Listener(ln) }()
	t.Cleanup(func() { _ = app.Shutdown(); database.Close() })
	port := ln.Addr().(*net.TCPAddr).Port
	s, err := database.CreateSession("handler", "", t.TempDir(), "default", "fakeq", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	setAutoResume(t, database, true)
	conn, _, err := websocket.DefaultDialer.Dial(fmt.Sprintf("ws://127.0.0.1:%d/sessions/%s/ws", port, s.ID), nil)
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
	events := make(chan serverMsg, 8)
	unsub := hub.Subscribe(s.ID, func(msg serverMsg) bool {
		if msg.Type == "queue_update" {
			events <- msg
		}
		return true
	})
	defer unsub()
	input := func(text string) {
		if err := conn.WriteJSON(map[string]string{"type": "input", "data": text}); err != nil {
			t.Fatal(err)
		}
	}
	input("block-quota")
	waitFor(t, "running", func() bool { return taskIsActive(s.ID) })
	input("B")
	input("C")
	waitFor(t, "queued", func() bool { return queueLen(database, s.ID) == 2 })
	fakeGate <- errors.New("usage limit")
	waitFor(t, "not rearmed", func() bool {
		state, at, _, err := database.GetQuotaResume(s.ID)
		return err == nil && !taskIsActive(s.ID) && state == db.QuotaResumeNone && at == "" && queueText(t, database, s.ID) == "B,C"
	})
	paused, err := database.QueuePaused(s.ID)
	if err != nil || !paused {
		t.Fatalf("paused=%v err=%v", paused, err)
	}
	if spy.count() == 0 {
		t.Fatal("handler 收尾沒有刷新額度")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		select {
		case ev := <-events:
			if ev.QuotaResumeState != nil && *ev.QuotaResumeState == db.QuotaResumeWaiting {
				t.Fatal("新訊息之後又武裝")
			}
			if ev.QueuePaused && len(ev.Queue) == 2 && ev.Queue[0].Content == "B" && ev.QuotaResumeState != nil && *ev.QuotaResumeState == db.QuotaResumeNone && ev.QuotaResumeAt != nil && *ev.QuotaResumeAt == "" {
				return
			}
		default:
			if time.Now().After(deadline) {
				t.Fatal("沒有廣播維持 none 的佇列")
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
}

func TestQuotaResumePMCloseDuringRefresh(t *testing.T) {
	database := openQuotaDB(t)
	s := quotaSession(t, database, "switch-race", "fakeq")
	setAutoResume(t, database, true)
	msg, err := database.AddUserMessageWithAttachments(s.ID, "A", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	refresh := func(context.Context, string) (quota.Snapshot, error) {
		setAutoResume(t, database, false)
		return fullSessionSnap(), nil
	}
	if err := finishQuotaResume(database, s.ID, "fakeq", msg.ID, true, false, false, refresh, time.Now()); err != nil {
		t.Fatal(err)
	}
	state, _, _, err := database.GetQuotaResume(s.ID)
	if err != nil || state != db.QuotaResumeNone || queueText(t, database, s.ID) != "" {
		t.Fatalf("switch off during refresh: state=%s queue=%s err=%v", state, queueText(t, database, s.ID), err)
	}
}

func TestQuotaResumeArmOnlyExhaustionEdges(t *testing.T) {
	database := openQuotaDB(t)
	s := quotaSession(t, database, "edge", "fakeq")
	setAutoResume(t, database, true)
	msg, err := database.AddUserMessageWithAttachments(s.ID, "A", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := database.TransitionQuotaResume(s.ID, db.QuotaResumeNone, db.QuotaResumeWaiting, "2026-11-18T10:01:00Z", false)
	if err != nil || !ok {
		t.Fatalf("waiting ok=%v err=%v", ok, err)
	}
	ok, err = database.ArmQuotaResume(s.ID, msg.ID, db.QuotaResumeWaiting, db.QuotaResumeWaiting, "2026-11-18T10:02:00Z", 0)
	if err != nil || ok || queueText(t, database, s.ID) != "" {
		t.Fatalf("waiting→waiting 不該入列 ok=%v err=%v queue=%s", ok, err, queueText(t, database, s.ID))
	}
	state, at, _, err := database.GetQuotaResume(s.ID)
	if err != nil || state != db.QuotaResumeWaiting || at != "2026-11-18T10:01:00Z" {
		t.Fatalf("狀態被改掉 state=%s at=%s err=%v", state, at, err)
	}
}

func TestQuotaResumeInsertFailureRollsBack(t *testing.T) {
	database := openQuotaDB(t)
	s := quotaSession(t, database, "insert-fail", "fakeq")
	setAutoResume(t, database, true)
	msg, err := database.AddUserMessageWithAttachments(s.ID, "A", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TRIGGER quota_arm_insert_fail BEFORE INSERT ON queued_messages
		BEGIN SELECT RAISE(ABORT, 'injected insert failure'); END`); err != nil {
		t.Fatal(err)
	}
	ok, err := database.ArmQuotaResume(s.ID, msg.ID, db.QuotaResumeNone, db.QuotaResumeWaiting, "2026-11-18T10:01:00Z", 0)
	if err == nil || ok {
		t.Fatalf("入列失敗應回錯誤 ok=%v err=%v", ok, err)
	}
	state, at, attempts, gerr := database.GetQuotaResume(s.ID)
	paused, perr := database.QueuePaused(s.ID)
	if gerr != nil || perr != nil || state != db.QuotaResumeNone || at != "" || attempts != 0 || paused || queueText(t, database, s.ID) != "" {
		t.Fatalf("半成功 state=%s at=%s attempts=%d paused=%v queue=%s err=%v %v", state, at, attempts, paused, queueText(t, database, s.ID), gerr, perr)
	}
}

func TestQuotaResumeTurningOffSerializesWithArm(t *testing.T) {
	database := openQuotaDB(t)
	s := quotaSession(t, database, "serialize", "fakeq")
	msg, err := database.AddUserMessageWithAttachments(s.ID, "A", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	off := []byte(`{"autoResumeOnQuota":false,"quotaGuardEnabled":true,"quotaGuardPercent":90}`)
	for i := 0; i < 40; i++ {
		setAutoResume(t, database, true)
		if _, err := database.Exec(`UPDATE sessions SET quota_resume_state = 'none', quota_resume_at = '', quota_resume_attempts = 0, queue_paused = 0 WHERE id = ?`, s.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec(`DELETE FROM queued_messages WHERE session_id = ?`, s.ID); err != nil {
			t.Fatal(err)
		}
		errc := make(chan error, 2)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			errc <- finishQuotaResume(database, s.ID, "fakeq", msg.ID, true, false, false, func(context.Context, string) (quota.Snapshot, error) {
				return fullSessionSnap(), nil
			}, time.Now())
		}()
		go func() {
			defer wg.Done()
			_, err := database.PutGeneralSettings(off)
			errc <- err
		}()
		wg.Wait()
		close(errc)
		for err := range errc {
			if err != nil {
				t.Fatal(err)
			}
		}
		settings, err := database.GetGeneralSettings()
		if err != nil {
			t.Fatal(err)
		}
		state, _, _, err := database.GetQuotaResume(s.ID)
		if err != nil {
			t.Fatal(err)
		}
		if settings.AutoResumeOnQuota || state != db.QuotaResumeNone {
			t.Fatalf("第 %d 次關開關後 state=%s on=%v", i, state, settings.AutoResumeOnQuota)
		}
	}
}

func TestQuotaResumeOwnerSocketDoesNotStart(t *testing.T) {
	database, wsURL := startTestServer(t)
	s := quotaSession(t, database, "owner-resume", "fakeq")
	setAutoResume(t, database, true)
	if _, err := database.EnqueueMessage(s.ID, "do not run", ""); err != nil {
		t.Fatal(err)
	}
	if err := database.SetQueuePaused(s.ID, true); err != nil {
		t.Fatal(err)
	}
	if ok, err := database.TransitionQuotaResume(s.ID, db.QuotaResumeNone, db.QuotaResumeWaiting, "2026-11-18T08:00:00Z", false); err != nil || !ok {
		t.Fatalf("waiting ok=%v err=%v", ok, err)
	}
	if ok, err := database.TransitionQuotaResume(s.ID, db.QuotaResumeWaiting, db.QuotaResumeResuming, "", false); err != nil || !ok {
		t.Fatalf("resuming ok=%v err=%v", ok, err)
	}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(s.ID), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(map[string]string{"type": "quota_resume"}); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	for {
		_, payload, err := conn.ReadMessage()
		if err != nil {
			break
		}
		if strings.Contains(string(payload), `"quota_resumed"`) {
			t.Fatalf("擁有者連線不該啟動: %s", payload)
		}
	}
	paused, err := database.QueuePaused(s.ID)
	state, _, attempts, gerr := database.GetQuotaResume(s.ID)
	if err != nil || gerr != nil || !paused || queueText(t, database, s.ID) != "do not run" || taskIsActive(s.ID) || state != db.QuotaResumeResuming || attempts != 1 || lastReply(database, s.ID) != "" {
		t.Fatalf("paused=%v queue=%q state=%s attempts=%d active=%v reply=%q err=%v %v", paused, queueText(t, database, s.ID), state, attempts, taskIsActive(s.ID), lastReply(database, s.ID), err, gerr)
	}
}

func TestQuotaResumeOtherFailureClearsResuming(t *testing.T) {
	database, wsURL := startTestServer(t)
	s := quotaSession(t, database, "other-fail", "fakeq")
	setAutoResume(t, database, true)
	armQuotaAttempts(t, database, s.ID, 2)
	pauseQuotaText(t, database, s.ID, "quota-fail now")
	if _, err := database.EnqueueMessage(s.ID, "later", ""); err != nil {
		t.Fatal(err)
	}
	if err := database.SetQueuePaused(s.ID, true); err != nil {
		t.Fatal(err)
	}
	conn := dialQuota(t, wsURL(s.ID))
	defer conn.Close()
	if err := conn.WriteJSON(map[string]string{"type": "queue_resume"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "other failure cleared", func() bool {
		state, _, attempts, err := database.GetQuotaResume(s.ID)
		paused, perr := database.QueuePaused(s.ID)
		return err == nil && perr == nil && paused && state == db.QuotaResumeNone && attempts == 0 && queueText(t, database, s.ID) == "later"
	})
}

func TestQuotaResumeCancelClearsResuming(t *testing.T) {
	database, wsURL := startTestServer(t)
	s := quotaSession(t, database, "cancel", "fakeq")
	setAutoResume(t, database, true)
	armQuotaAttempts(t, database, s.ID, 1)
	pauseQuotaText(t, database, s.ID, "block-quota-cancel")
	conn := dialQuota(t, wsURL(s.ID))
	defer conn.Close()
	if err := conn.WriteJSON(map[string]string{"type": "queue_resume"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "running", func() bool { return taskIsActive(s.ID) })
	if err := conn.WriteJSON(map[string]string{"type": "interrupt"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "cancel cleared", func() bool {
		state, _, attempts, err := database.GetQuotaResume(s.ID)
		return err == nil && !taskIsActive(s.ID) && state == db.QuotaResumeNone && attempts == 0
	})
}

func armQuotaAttempts(t *testing.T, database *db.DB, sessionID string, attempts int) {
	t.Helper()
	if attempts < 1 {
		t.Fatalf("attempts=%d", attempts)
	}
	ok, err := database.TransitionQuotaResume(sessionID, db.QuotaResumeNone, db.QuotaResumeWaiting, "2026-11-18T08:00:00Z", false)
	if err != nil || !ok {
		t.Fatalf("waiting ok=%v err=%v", ok, err)
	}
	for i := 1; i <= attempts; i++ {
		if i > 1 {
			ok, err = database.TransitionQuotaResume(sessionID, db.QuotaResumeResuming, db.QuotaResumeWaiting, "", false)
			if err != nil || !ok {
				t.Fatalf("back waiting ok=%v err=%v", ok, err)
			}
		}
		ok, err = database.TransitionQuotaResume(sessionID, db.QuotaResumeWaiting, db.QuotaResumeResuming, "", false)
		if err != nil || !ok {
			t.Fatalf("resuming ok=%v err=%v", ok, err)
		}
	}
}

func pauseQuotaText(t *testing.T, database *db.DB, sessionID, content string) {
	t.Helper()
	if _, err := database.EnqueueMessage(sessionID, content, ""); err != nil {
		t.Fatal(err)
	}
	if err := database.SetQueuePaused(sessionID, true); err != nil {
		t.Fatal(err)
	}
}

func TestQuotaResumeExhaustedOnThirdResume(t *testing.T) {
	database, wsURL := startQuotaRefreshServer(t, func(context.Context, string) (quota.Snapshot, error) {
		return fullSessionSnap(), nil
	})
	s := quotaSession(t, database, "exhausted", "fakeq")
	setAutoResume(t, database, true)
	if _, err := database.AddUserMessageWithAttachments(s.ID, "seed", "", nil); err != nil {
		t.Fatal(err)
	}
	armQuotaAttempts(t, database, s.ID, 3)
	pauseQuotaText(t, database, s.ID, "quota-fail again")
	if _, err := database.EnqueueMessage(s.ID, "later", ""); err != nil {
		t.Fatal(err)
	}
	if err := database.SetQueuePaused(s.ID, true); err != nil {
		t.Fatal(err)
	}
	owned, generation := claimQuotaResume(database, s.ID)
	if !owned || generation == 0 {
		t.Fatalf("claim owned=%v generation=%d", owned, generation)
	}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(s.ID), http.Header{"X-Miniapp-Room-Wake": []string{"test"}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(map[string]any{"type": "quota_resume", "generation": generation}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "exhausted", func() bool {
		state, _, attempts, err := database.GetQuotaResume(s.ID)
		paused, perr := database.QueuePaused(s.ID)
		sess, gerr := database.GetSession(s.ID)
		return err == nil && perr == nil && gerr == nil && paused && state == db.QuotaResumeExhausted && attempts == 3 && queueText(t, database, s.ID) == "quota-fail again,later" && strings.Contains(sess.LastErrorText, "disk full")
	})
}

func TestQuotaResumeFailureBelowLimitRearms(t *testing.T) {
	database, wsURL := startQuotaRefreshServer(t, func(context.Context, string) (quota.Snapshot, error) {
		return fullSessionSnap(), nil
	})
	s := quotaSession(t, database, "rearm", "fakeq")
	setAutoResume(t, database, true)
	armQuotaAttempts(t, database, s.ID, 1)
	pauseQuotaText(t, database, s.ID, "quota-fail again")
	owned, generation := claimQuotaResume(database, s.ID)
	if !owned || generation == 0 {
		t.Fatalf("claim owned=%v generation=%d", owned, generation)
	}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(s.ID), http.Header{"X-Miniapp-Room-Wake": []string{"test"}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(map[string]any{"type": "quota_resume", "generation": generation}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "rearmed waiting", func() bool {
		state, _, attempts, err := database.GetQuotaResume(s.ID)
		paused, perr := database.QueuePaused(s.ID)
		return err == nil && perr == nil && paused && state == db.QuotaResumeWaiting && attempts == 1 && queueText(t, database, s.ID) == "quota-fail again"
	})
}

func TestQuotaResumeSuccessResetsCount(t *testing.T) {
	database, wsURL := startTestServer(t)
	s := quotaSession(t, database, "recount", "fakeq")
	setAutoResume(t, database, true)
	armQuotaAttempts(t, database, s.ID, 2)
	pauseQuotaText(t, database, s.ID, "hello")
	conn := dialQuota(t, wsURL(s.ID))
	defer conn.Close()
	if err := conn.WriteJSON(map[string]string{"type": "queue_resume"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "cleared", func() bool {
		state, _, attempts, err := database.GetQuotaResume(s.ID)
		return err == nil && state == db.QuotaResumeNone && attempts == 0 && lastReply(database, s.ID) == "ok:hello"
	})
	ok, err := database.TransitionQuotaResume(s.ID, db.QuotaResumeNone, db.QuotaResumeWaiting, "2026-11-18T08:00:00Z", false)
	if err != nil || !ok {
		t.Fatalf("waiting ok=%v err=%v", ok, err)
	}
	ok, err = database.TransitionQuotaResume(s.ID, db.QuotaResumeWaiting, db.QuotaResumeResuming, "", false)
	if err != nil || !ok {
		t.Fatalf("resuming ok=%v err=%v", ok, err)
	}
	state, _, attempts, err := database.GetQuotaResume(s.ID)
	if err != nil || state != db.QuotaResumeResuming || attempts != 1 {
		t.Fatalf("state=%s attempts=%d err=%v", state, attempts, err)
	}
}

func TestQuotaResumeSwitchOffDuringRunStaysNone(t *testing.T) {
	database, wsURL := startTestServer(t)
	s := quotaSession(t, database, "switch-off", "fakeq")
	setAutoResume(t, database, true)
	armQuotaAttempts(t, database, s.ID, 1)
	pauseQuotaText(t, database, s.ID, "block-quota-off")
	conn := dialQuota(t, wsURL(s.ID))
	defer conn.Close()
	if err := conn.WriteJSON(map[string]string{"type": "queue_resume"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "running", func() bool { return taskIsActive(s.ID) })
	setAutoResume(t, database, false)
	fakeGate <- nil
	waitFor(t, "stays none", func() bool {
		state, _, attempts, err := database.GetQuotaResume(s.ID)
		return err == nil && !taskIsActive(s.ID) && state == db.QuotaResumeNone && attempts == 0 && lastReply(database, s.ID) == "ok:block-quota-off"
	})
}

func startQuotaRefreshServer(t *testing.T, refresh func(context.Context, string) (quota.Snapshot, error)) (*db.DB, func(string) string) {
	t.Helper()
	database := openQuotaDB(t)
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Get("/sessions/:id/ws", func(c *fiber.Ctx) error {
		if c.Get("X-Miniapp-Room-Wake") == "test" {
			c.Locals("room_wake", true)
		}
		return c.Next()
	}, fiberws.New(NewHandler(database, "", ShellOpts{}, nil, tg.NotifyConfig{}, WithQuotaResumeRefresh(refresh))))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = app.Listener(ln) }()
	t.Cleanup(func() { _ = app.Shutdown() })
	port := ln.Addr().(*net.TCPAddr).Port
	return database, func(id string) string {
		return fmt.Sprintf("ws://127.0.0.1:%d/sessions/%s/ws", port, id)
	}
}

func dialQuota(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

func TestQuotaResumePMGenerationABA(t *testing.T) {
	database := openQuotaDB(t)
	s := quotaSession(t, database, "generation-aba", "fakeq")
	step := func(from, to string) {
		t.Helper()
		ok, err := database.TransitionQuotaResume(s.ID, from, to, "2026-11-18T10:01:00Z", false)
		if err != nil || !ok {
			t.Fatalf("transition %s -> %s: %v %v", from, to, ok, err)
		}
	}
	step(db.QuotaResumeNone, db.QuotaResumeWaiting)
	step(db.QuotaResumeWaiting, db.QuotaResumeResuming)
	owned, generation := claimQuotaResume(database, s.ID)
	if !owned || generation == 0 {
		t.Fatalf("claim owned=%v generation=%d", owned, generation)
	}
	step(db.QuotaResumeResuming, db.QuotaResumeNone)
	step(db.QuotaResumeNone, db.QuotaResumeWaiting)
	step(db.QuotaResumeWaiting, db.QuotaResumeResuming)
	if err := releaseQuotaResumeRun(database, s.ID, owned, generation); err != nil {
		t.Fatal(err)
	}
	state, _, attempts, err := database.GetQuotaResume(s.ID)
	if err != nil || state != db.QuotaResumeResuming || attempts != 1 {
		t.Fatalf("old run cleared new generation: state=%s attempts=%d err=%v", state, attempts, err)
	}
}

func TestQuotaResumeLateRefreshDoesNotInsertOldMessage(t *testing.T) {
	now := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	database := openQuotaDB(t)
	s := quotaSession(t, database, "late-refresh", "fakeq")
	setAutoResume(t, database, true)
	msg, err := database.AddUserMessageWithAttachments(s.ID, "old-prompt", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	step := func(from, to string) error {
		ok, err := database.TransitionQuotaResume(s.ID, from, to, "2026-11-18T10:01:00Z", false)
		if err != nil || !ok {
			return fmt.Errorf("transition %s -> %s: %v %v", from, to, ok, err)
		}
		return nil
	}
	if err := step(db.QuotaResumeNone, db.QuotaResumeWaiting); err != nil {
		t.Fatal(err)
	}
	if err := step(db.QuotaResumeWaiting, db.QuotaResumeResuming); err != nil {
		t.Fatal(err)
	}
	owned, generation := claimQuotaResume(database, s.ID)
	if !owned || generation == 0 {
		t.Fatalf("claim owned=%v generation=%d", owned, generation)
	}
	ready := make(chan struct{})
	swapped := make(chan error, 1)
	go func() {
		<-ready
		var swapErr error
		for _, edge := range [][2]string{
			{db.QuotaResumeResuming, db.QuotaResumeNone},
			{db.QuotaResumeNone, db.QuotaResumeWaiting},
			{db.QuotaResumeWaiting, db.QuotaResumeResuming},
		} {
			if err := step(edge[0], edge[1]); err != nil {
				swapErr = err
				break
			}
		}
		swapped <- swapErr
	}()
	refresh := func(context.Context, string) (quota.Snapshot, error) {
		close(ready)
		if err := <-swapped; err != nil {
			return quota.Snapshot{}, err
		}
		return fullSessionSnap(), nil
	}
	if err := finishHeldQuotaResume(database, s.ID, "fakeq", msg.ID, true, false, false, refresh, now, generation, generation); err != nil {
		t.Fatal(err)
	}
	if queueText(t, database, s.ID) != "" {
		t.Fatalf("舊訊息被插入 %s", queueText(t, database, s.ID))
	}
	state, _, attempts, err := database.GetQuotaResume(s.ID)
	if err != nil || state != db.QuotaResumeResuming || attempts != 1 {
		t.Fatalf("state=%s attempts=%d err=%v", state, attempts, err)
	}
	if err := finishQuotaResume(database, s.ID, "fakeq", msg.ID, true, false, false, func(context.Context, string) (quota.Snapshot, error) {
		return fullSessionSnap(), nil
	}, now); err != nil {
		t.Fatal(err)
	}
	if queueText(t, database, s.ID) != "" {
		t.Fatalf("未持有的刷新插入了舊訊息 %s", queueText(t, database, s.ID))
	}
	if err := releaseQuotaResumeRun(database, s.ID, owned, generation); err != nil {
		t.Fatal(err)
	}
	state, _, attempts, err = database.GetQuotaResume(s.ID)
	if err != nil || state != db.QuotaResumeResuming || attempts != 1 {
		t.Fatalf("舊收尾清掉新世代 state=%s attempts=%d err=%v", state, attempts, err)
	}
}

func TestQuotaResumeDecisionGapMissesNewGeneration(t *testing.T) {
	database := openQuotaDB(t)
	s := quotaSession(t, database, "decision-gap", "fakeq")
	setAutoResume(t, database, true)
	msg, err := database.AddUserMessageWithAttachments(s.ID, "old-prompt", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	step := func(from, to string) error {
		ok, err := database.TransitionQuotaResume(s.ID, from, to, "2026-11-18T10:01:00Z", false)
		if err != nil || !ok {
			return fmt.Errorf("transition %s -> %s: %v %v", from, to, ok, err)
		}
		return nil
	}
	if err := step(db.QuotaResumeNone, db.QuotaResumeWaiting); err != nil {
		t.Fatal(err)
	}
	if err := step(db.QuotaResumeWaiting, db.QuotaResumeResuming); err != nil {
		t.Fatal(err)
	}
	owned, generation := claimQuotaResume(database, s.ID)
	if !owned || generation == 0 {
		t.Fatalf("claim owned=%v generation=%d", owned, generation)
	}
	_, _, attempts, err := database.GetQuotaResume(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	to := db.QuotaResumeWaiting
	if attempts >= 3 {
		to = db.QuotaResumeExhausted
	}
	decided := make(chan struct{})
	swapped := make(chan error, 1)
	go func() {
		<-decided
		var swapErr error
		for _, edge := range [][2]string{
			{db.QuotaResumeResuming, db.QuotaResumeNone},
			{db.QuotaResumeNone, db.QuotaResumeWaiting},
			{db.QuotaResumeWaiting, db.QuotaResumeResuming},
		} {
			if err := step(edge[0], edge[1]); err != nil {
				swapErr = err
				break
			}
		}
		swapped <- swapErr
	}()
	close(decided)
	if err := <-swapped; err != nil {
		t.Fatal(err)
	}
	ok, err := database.ArmQuotaResume(s.ID, msg.ID, db.QuotaResumeResuming, to, "2026-11-18T10:02:00Z", generation)
	if err != nil || ok {
		t.Fatalf("判定之後的舊世代不該武裝 ok=%v err=%v", ok, err)
	}
	if queueText(t, database, s.ID) != "" {
		t.Fatalf("舊訊息被插入 %s", queueText(t, database, s.ID))
	}
	state, _, attempts, err := database.GetQuotaResume(s.ID)
	if err != nil || state != db.QuotaResumeResuming || attempts != 1 {
		t.Fatalf("state=%s attempts=%d err=%v", state, attempts, err)
	}
	_, _, next, err := database.SnapshotQuotaResume(s.ID)
	if err != nil || next == generation {
		t.Fatalf("新世代=%d 舊世代=%d err=%v", next, generation, err)
	}
	if err := releaseQuotaResumeRun(database, s.ID, owned, generation); err != nil {
		t.Fatal(err)
	}
	state, _, attempts, err = database.GetQuotaResume(s.ID)
	if err != nil || state != db.QuotaResumeResuming || attempts != 1 {
		t.Fatalf("舊收尾清掉新世代 state=%s attempts=%d err=%v", state, attempts, err)
	}
}

func TestQuotaResumePMOffOnDuringRefresh(t *testing.T) {
	database := openQuotaDB(t)
	s := quotaSession(t, database, "off-on", "fakeq")
	setAutoResume(t, database, true)
	msg, err := database.AddUserMessageWithAttachments(s.ID, "old", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, observed, err := database.SnapshotQuotaResume(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	refresh := func(context.Context, string) (quota.Snapshot, error) {
		setAutoResume(t, database, false)
		setAutoResume(t, database, true)
		return fullSessionSnap(), nil
	}
	if err := finishHeldQuotaResume(database, s.ID, "fakeq", msg.ID, true, false, false, refresh, time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC), 0, observed); err != nil {
		t.Fatal(err)
	}
	state, _, _, err := database.GetQuotaResume(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state != db.QuotaResumeNone || queueText(t, database, s.ID) != "" {
		t.Fatalf("off/on resurrected old run: state=%s queue=%s", state, queueText(t, database, s.ID))
	}
}

func TestQuotaResumeHeldOffOnDuringRefresh(t *testing.T) {
	database := openQuotaDB(t)
	s := quotaSession(t, database, "held-off-on", "fakeq")
	setAutoResume(t, database, true)
	msg, err := database.AddUserMessageWithAttachments(s.ID, "old", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.EnqueueMessage(s.ID, "keep", ""); err != nil {
		t.Fatal(err)
	}
	if err := database.SetQueuePaused(s.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := database.TransitionQuotaResume(s.ID, db.QuotaResumeNone, db.QuotaResumeWaiting, "2026-11-18T10:01:00Z", false); err != nil {
		t.Fatal(err)
	}
	if _, err := database.TransitionQuotaResume(s.ID, db.QuotaResumeWaiting, db.QuotaResumeResuming, "", false); err != nil {
		t.Fatal(err)
	}
	owned, held := claimQuotaResume(database, s.ID)
	if !owned || held == 0 {
		t.Fatalf("owned=%v held=%d", owned, held)
	}
	refresh := func(context.Context, string) (quota.Snapshot, error) {
		setAutoResume(t, database, false)
		setAutoResume(t, database, true)
		return fullSessionSnap(), nil
	}
	if err := finishHeldQuotaResume(database, s.ID, "fakeq", msg.ID, true, false, false, refresh, time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC), held, held); err != nil {
		t.Fatal(err)
	}
	state, _, _, err := database.GetQuotaResume(s.ID)
	paused, perr := database.QueuePaused(s.ID)
	if err != nil || perr != nil || state != db.QuotaResumeNone || queueText(t, database, s.ID) != "keep" || !paused || len(userMessages(database, s.ID)) != 1 {
		t.Fatalf("state=%s queue=%s paused=%v users=%v err=%v %v", state, queueText(t, database, s.ID), paused, userMessages(database, s.ID), err, perr)
	}
}

// finishQuotaResume（測試用） 在失敗收尾判斷是否為 5 小時額度用盡。符合時把原使用者訊息放回佇列最前面並暫停。
// 這條沒有持有世代，只允許從 none 武裝。
func finishQuotaResume(database *db.DB, sessionID, agentType string, userMsgID int64, runFailed, cancelled, permDenied bool, refresh func(context.Context, string) (quota.Snapshot, error), now time.Time) error {
	return finishHeldQuotaResume(database, sessionID, agentType, userMsgID, runFailed, cancelled, permDenied, refresh, now, 0, 0)
}

// claimQuotaResume 在執行開始時一次讀出這是不是自動重跑，以及當時的世代。
// 世代在 waiting→resuming、取消與關閉開關時增加，不歸零、不重用。
func claimQuotaResume(database *db.DB, sessionID string) (owned bool, generation int64) {
	owned, generation, err := database.ClaimQuotaResume(sessionID)
	if err != nil {
		return false, 0
	}
	return owned, generation
}
