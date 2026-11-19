package ws

import (
	"context"
	"testing"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/quota"
)

func TestQuotaResumeCancelKeepsQueuePaused(t *testing.T) {
	database, wsURL := startTestServer(t)
	s := quotaSession(t, database, "banner", "fakeq")
	if _, err := database.EnqueueMessage(s.ID, "A", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := database.EnqueueMessage(s.ID, "B", ""); err != nil {
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
	if _, err := database.TransitionQuotaResume(s.ID, db.QuotaResumeResuming, db.QuotaResumeWaiting, "2026-11-18T10:01:00Z", false); err != nil {
		t.Fatal(err)
	}
	_, _, before, err := database.SnapshotQuotaResume(s.ID)
	if err != nil || before == 0 {
		t.Fatalf("generation=%d err=%v", before, err)
	}
	owner := dialClient(t, wsURL(s.ID))
	other := dialClient(t, wsURL(s.ID))
	owner.await(t, "sync", func(m map[string]any) bool {
		return m["type"] == "sync" && m["quota_resume_state"] == db.QuotaResumeWaiting && m["quota_resume_at"] == "2026-11-18T10:01:00Z"
	})
	other.await(t, "other sync", isType("sync"))
	owner.send(t, map[string]any{"type": "quota_resume_cancel"})
	updated := other.await(t, "queue update", func(m map[string]any) bool {
		return m["type"] == "queue_update" && m["quota_resume_state"] == db.QuotaResumeNone && m["quota_resume_at"] == ""
	})
	queue, _ := updated["queue"].([]any)
	if len(queue) != 2 || updated["queue_paused"] != true {
		t.Fatalf("queue update=%v", updated)
	}
	state, at, attempts, err := database.GetQuotaResume(s.ID)
	paused, perr := database.QueuePaused(s.ID)
	_, _, generation, gerr := database.SnapshotQuotaResume(s.ID)
	if err != nil || perr != nil || gerr != nil || state != db.QuotaResumeNone || at != "" || attempts != 0 || !paused || queueText(t, database, s.ID) != "A,B" || generation != before+1 || taskIsActive(s.ID) {
		t.Fatalf("state=%s at=%s attempts=%d paused=%v queue=%s generation=%d active=%v err=%v %v %v", state, at, attempts, paused, queueText(t, database, s.ID), generation, taskIsActive(s.ID), err, perr, gerr)
	}
}

func TestQuotaResumeCancelExhaustedAndGuests(t *testing.T) {
	database, dial := startShareServer(t)
	s := quotaSession(t, database, "guest-cancel", "fakeq")
	if _, err := database.EnqueueMessage(s.ID, "A", ""); err != nil {
		t.Fatal(err)
	}
	if err := database.SetQueuePaused(s.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := database.TransitionQuotaResume(s.ID, db.QuotaResumeNone, db.QuotaResumeExhausted, "2026-11-18T10:01:00Z", false); err != nil {
		t.Fatal(err)
	}
	_, editorAccess := newShare(t, database, s.ID, db.ShareRoleEditor, db.ShareModeLive, "編輯", time.Hour)
	editor := dialClient(t, dial(s.ID, editorAccess.Guest.Token))
	editor.await(t, "editor sync", isType("sync"))
	editor.send(t, map[string]any{"type": "quota_resume_cancel"})
	editor.barrier(t)
	state, _, _, err := database.GetQuotaResume(s.ID)
	if err != nil || state != db.QuotaResumeExhausted || queueText(t, database, s.ID) != "A" {
		t.Fatalf("editor changed state=%s queue=%s err=%v", state, queueText(t, database, s.ID), err)
	}
	_, viewerAccess := newShare(t, database, s.ID, db.ShareRoleViewer, db.ShareModeLive, "觀眾", time.Hour)
	viewer := dialClient(t, dial(s.ID, viewerAccess.Guest.Token))
	viewer.await(t, "viewer sync", isType("sync"))
	viewer.send(t, map[string]any{"type": "quota_resume_cancel"})
	viewer.barrier(t)
	state, _, _, err = database.GetQuotaResume(s.ID)
	if err != nil || state != db.QuotaResumeExhausted || queueText(t, database, s.ID) != "A" {
		t.Fatalf("viewer changed state=%s queue=%s err=%v", state, queueText(t, database, s.ID), err)
	}
	owner := dialClient(t, dial(s.ID, ""))
	owner.await(t, "owner sync", func(m map[string]any) bool {
		return m["type"] == "sync" && m["quota_resume_state"] == db.QuotaResumeExhausted
	})
	owner.send(t, map[string]any{"type": "quota_resume_cancel"})
	owner.barrier(t)
	state, at, attempts, err := database.GetQuotaResume(s.ID)
	paused, _ := database.QueuePaused(s.ID)
	if err != nil || state != db.QuotaResumeNone || at != "" || attempts != 0 || !paused || queueText(t, database, s.ID) != "A" {
		t.Fatalf("state=%s at=%s attempts=%d paused=%v queue=%s err=%v", state, at, attempts, paused, queueText(t, database, s.ID), err)
	}
}

func TestQuotaResumeManualContinueStartsOnce(t *testing.T) {
	database, wsURL := startTestServer(t)
	s := quotaSession(t, database, "manual", "fakeq")
	pauseQuotaText(t, database, s.ID, "A")
	if _, err := database.TransitionQuotaResume(s.ID, db.QuotaResumeNone, db.QuotaResumeWaiting, "2026-11-18T10:01:00Z", false); err != nil {
		t.Fatal(err)
	}
	conn := dialClient(t, wsURL(s.ID))
	conn.send(t, map[string]any{"type": "queue_resume"})
	waitFor(t, "ran once", func() bool {
		state, _, attempts, err := database.GetQuotaResume(s.ID)
		return err == nil && state == db.QuotaResumeNone && attempts == 0 && lastReply(database, s.ID) == "ok:A" && len(userMessages(database, s.ID)) == 1
	})
}

func TestQuotaResumeDeleteAndClearCancelSchedule(t *testing.T) {
	database, wsURL := startTestServer(t)
	s := quotaSession(t, database, "delete", "fakeq")
	first, err := database.EnqueueMessage(s.ID, "A", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.EnqueueMessage(s.ID, "B", ""); err != nil {
		t.Fatal(err)
	}
	if err := database.SetQueuePaused(s.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := database.TransitionQuotaResume(s.ID, db.QuotaResumeNone, db.QuotaResumeWaiting, "2026-11-18T10:01:00Z", false); err != nil {
		t.Fatal(err)
	}
	conn := dialClient(t, wsURL(s.ID))
	other := dialClient(t, wsURL(s.ID))
	conn.await(t, "owner sync", isType("sync"))
	other.await(t, "other sync", isType("sync"))
	conn.send(t, map[string]any{"type": "queue_remove", "id": first.ID})
	other.await(t, "delete update", func(m map[string]any) bool {
		queue, _ := m["queue"].([]any)
		return m["type"] == "queue_update" && m["quota_resume_state"] == db.QuotaResumeNone && m["quota_resume_at"] == "" && len(queue) == 1 && m["queue_paused"] == true
	})
	state, _, attempts, err := database.GetQuotaResume(s.ID)
	paused, _ := database.QueuePaused(s.ID)
	if err != nil || state != db.QuotaResumeNone || attempts != 0 || !paused || queueText(t, database, s.ID) != "B" {
		t.Fatalf("delete state=%s attempts=%d paused=%v queue=%s err=%v", state, attempts, paused, queueText(t, database, s.ID), err)
	}
	if _, err := database.TransitionQuotaResume(s.ID, db.QuotaResumeNone, db.QuotaResumeWaiting, "2026-11-18T11:00:00Z", false); err != nil {
		t.Fatal(err)
	}
	conn.send(t, map[string]any{"type": "reset_context"})
	conn.await(t, "reset", isType("reset"))
	state, _, _, err = database.GetQuotaResume(s.ID)
	if err != nil || state != db.QuotaResumeNone || queueText(t, database, s.ID) != "" {
		t.Fatalf("clear state=%s queue=%s err=%v", state, queueText(t, database, s.ID), err)
	}
}

func TestQuotaResumeDirectInputCancelsOnlyWhenAccepted(t *testing.T) {
	database, wsURL := startTestServer(t)
	s := quotaSession(t, database, "input", "fakeq")
	if _, err := database.EnqueueMessage(s.ID, "A", ""); err != nil {
		t.Fatal(err)
	}
	if err := database.SetQueuePaused(s.ID, true); err != nil {
		t.Fatal(err)
	}
	arm := func() {
		t.Helper()
		state, _, _, err := database.GetQuotaResume(s.ID)
		if err != nil {
			t.Fatal(err)
		}
		if state != db.QuotaResumeNone {
			if _, err := database.TransitionQuotaResume(s.ID, state, db.QuotaResumeNone, "", false); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := database.TransitionQuotaResume(s.ID, db.QuotaResumeNone, db.QuotaResumeWaiting, "2026-11-18T10:01:00Z", false); err != nil {
			t.Fatal(err)
		}
	}
	arm()
	conn := dialClient(t, wsURL(s.ID))
	conn.send(t, map[string]any{"type": "input", "data": "   "})
	conn.barrier(t)
	state, _, _, err := database.GetQuotaResume(s.ID)
	if err != nil || state != db.QuotaResumeWaiting || queueText(t, database, s.ID) != "A" {
		t.Fatalf("empty state=%s queue=%s err=%v", state, queueText(t, database, s.ID), err)
	}
	conn.send(t, map[string]any{"type": "input", "data": "x", "attachment_ids": []string{"missing"}})
	conn.barrier(t)
	state, _, _, err = database.GetQuotaResume(s.ID)
	if err != nil || state != db.QuotaResumeWaiting {
		t.Fatalf("bad attachment state=%s err=%v", state, err)
	}
	conn.send(t, map[string]any{"type": "input", "data": "hello", "request_id": "same"})
	conn.barrier(t)
	state, _, _, err = database.GetQuotaResume(s.ID)
	if err != nil || state != db.QuotaResumeNone || queueText(t, database, s.ID) != "A,hello" {
		t.Fatalf("accepted state=%s queue=%s err=%v", state, queueText(t, database, s.ID), err)
	}
	arm()
	conn.send(t, map[string]any{"type": "input", "data": "hello", "request_id": "same"})
	conn.barrier(t)
	state, _, _, err = database.GetQuotaResume(s.ID)
	if err != nil || state != db.QuotaResumeWaiting || queueText(t, database, s.ID) != "A,hello" {
		t.Fatalf("retry state=%s queue=%s err=%v", state, queueText(t, database, s.ID), err)
	}
}

func TestQuotaResumeCancelLeavesRunningRunner(t *testing.T) {
	database, wsURL := startTestServer(t)
	s := quotaSession(t, database, "running", "fakeq")
	conn := dialClient(t, wsURL(s.ID))
	conn.send(t, map[string]any{"type": "input", "data": "block-keep"})
	waitFor(t, "running", func() bool { return taskIsActive(s.ID) })
	if _, err := database.TransitionQuotaResume(s.ID, db.QuotaResumeNone, db.QuotaResumeWaiting, "2026-11-18T10:01:00Z", false); err != nil {
		t.Fatal(err)
	}
	conn.send(t, map[string]any{"type": "quota_resume_cancel"})
	conn.barrier(t)
	state, _, _, err := database.GetQuotaResume(s.ID)
	if err != nil || state != db.QuotaResumeNone || !taskIsActive(s.ID) {
		t.Fatalf("state=%s active=%v err=%v", state, taskIsActive(s.ID), err)
	}
	fakeGate <- nil
	waitFor(t, "finished", func() bool { return !taskIsActive(s.ID) && lastReply(database, s.ID) == "ok:block-keep" })
}

func TestQuotaResumeSyncNoneClearsAt(t *testing.T) {
	database, wsURL := startTestServer(t)
	s := quotaSession(t, database, "none-sync", "fakeq")
	conn := dialClient(t, wsURL(s.ID))
	syncMsg := conn.await(t, "sync", isType("sync"))
	if syncMsg["quota_resume_state"] != db.QuotaResumeNone || syncMsg["quota_resume_at"] != "" {
		t.Fatalf("sync=%v", syncMsg)
	}
	if _, ok := syncMsg["generation"]; ok {
		t.Fatal("sync 不該帶出世代")
	}
}

func injectCancelFailure(t *testing.T, database *db.DB) {
	t.Helper()
	_, err := database.Exec(`CREATE TRIGGER fail_quota_cancel BEFORE UPDATE OF quota_resume_state ON sessions WHEN OLD.quota_resume_state='waiting' AND NEW.quota_resume_state='none' BEGIN SELECT RAISE(ABORT,'injected cancel failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
}

func dropCancelFailure(t *testing.T, database *db.DB) {
	t.Helper()
	if _, err := database.Exec(`DROP TRIGGER fail_quota_cancel`); err != nil {
		t.Fatal(err)
	}
}

func TestQuotaResumePMAcceptCancelFailureAtomic(t *testing.T) {
	database, wsURL := startTestServer(t)
	s := quotaSession(t, database, "accept-cancel-failure", "fakeq")
	if _, err := database.EnqueueMessage(s.ID, "A", ""); err != nil {
		t.Fatal(err)
	}
	if err := database.SetQueuePaused(s.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := database.TransitionQuotaResume(s.ID, db.QuotaResumeNone, db.QuotaResumeWaiting, "2026-11-18T10:01:00Z", false); err != nil {
		t.Fatal(err)
	}
	injectCancelFailure(t, database)
	conn := dialClient(t, wsURL(s.ID))
	conn.send(t, map[string]any{"type": "input", "data": "hello", "request_id": "cancel-fail"})
	conn.barrier(t)
	var receipts int
	if err := database.QueryRow(`SELECT COUNT(*) FROM input_receipts WHERE session_id=?`, s.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	state, _, _, err := database.GetQuotaResume(s.ID)
	if err != nil || receipts != 0 || queueText(t, database, s.ID) != "A" || state != db.QuotaResumeWaiting || taskIsActive(s.ID) {
		t.Fatalf("cancel failed but input persisted: receipts=%d queue=%s state=%s active=%v err=%v", receipts, queueText(t, database, s.ID), state, taskIsActive(s.ID), err)
	}
	dropCancelFailure(t, database)
	conn.send(t, map[string]any{"type": "input", "data": "hello", "request_id": "cancel-fail"})
	conn.barrier(t)
	if err := database.QueryRow(`SELECT COUNT(*) FROM input_receipts WHERE session_id=?`, s.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	state, _, attempts, err := database.GetQuotaResume(s.ID)
	paused, _ := database.QueuePaused(s.ID)
	if err != nil || receipts != 1 || queueText(t, database, s.ID) != "A,hello" || state != db.QuotaResumeNone || attempts != 0 || !paused {
		t.Fatalf("retry state=%s attempts=%d paused=%v queue=%s receipts=%d err=%v", state, attempts, paused, queueText(t, database, s.ID), receipts, err)
	}
	if _, err := database.TransitionQuotaResume(s.ID, db.QuotaResumeNone, db.QuotaResumeWaiting, "2026-11-18T10:02:00Z", false); err != nil {
		t.Fatal(err)
	}
	conn.send(t, map[string]any{"type": "input", "data": "hello", "request_id": "cancel-fail"})
	conn.barrier(t)
	state, _, _, err = database.GetQuotaResume(s.ID)
	if err := database.QueryRow(`SELECT COUNT(*) FROM input_receipts WHERE session_id=?`, s.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if err != nil || receipts != 1 || state != db.QuotaResumeWaiting || queueText(t, database, s.ID) != "A,hello" {
		t.Fatalf("duplicate state=%s queue=%s receipts=%d err=%v", state, queueText(t, database, s.ID), receipts, err)
	}
}

func TestQuotaResumeLegacyCancelRollsBackBeforeStart(t *testing.T) {
	database, wsURL := startTestServer(t)
	s := quotaSession(t, database, "legacy-cancel", "fakeq")
	if _, err := database.TransitionQuotaResume(s.ID, db.QuotaResumeNone, db.QuotaResumeWaiting, "2026-11-18T10:01:00Z", false); err != nil {
		t.Fatal(err)
	}
	injectCancelFailure(t, database)
	conn := dialClient(t, wsURL(s.ID))
	conn.await(t, "sync", isType("sync"))
	conn.send(t, map[string]any{"type": "input", "data": "run-me"})
	conn.barrier(t)
	state, _, _, err := database.GetQuotaResume(s.ID)
	if err != nil || state != db.QuotaResumeWaiting || queueText(t, database, s.ID) != "" || len(userMessages(database, s.ID)) != 0 || taskIsActive(s.ID) {
		t.Fatalf("legacy persisted state=%s queue=%s users=%v active=%v err=%v", state, queueText(t, database, s.ID), userMessages(database, s.ID), taskIsActive(s.ID), err)
	}
	dropCancelFailure(t, database)
	conn.send(t, map[string]any{"type": "input", "data": "run-me"})
	waitFor(t, "legacy started", func() bool {
		return !taskIsActive(s.ID) && lastReply(database, s.ID) == "ok:run-me"
	})
	state, _, attempts, err := database.GetQuotaResume(s.ID)
	if err != nil || state != db.QuotaResumeNone || attempts != 0 || len(userMessages(database, s.ID)) != 1 {
		t.Fatalf("after drop state=%s attempts=%d users=%v err=%v", state, attempts, userMessages(database, s.ID), err)
	}
}

func TestQuotaResumeDeleteCancelRollsBack(t *testing.T) {
	database, wsURL := startTestServer(t)
	s := quotaSession(t, database, "delete-cancel", "fakeq")
	a, err := database.EnqueueMessage(s.ID, "A", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.EnqueueMessage(s.ID, "B", ""); err != nil {
		t.Fatal(err)
	}
	if err := database.SetQueuePaused(s.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := database.TransitionQuotaResume(s.ID, db.QuotaResumeNone, db.QuotaResumeWaiting, "2026-11-18T10:01:00Z", false); err != nil {
		t.Fatal(err)
	}
	owner := dialClient(t, wsURL(s.ID))
	other := dialClient(t, wsURL(s.ID))
	owner.await(t, "sync", isType("sync"))
	other.await(t, "other sync", isType("sync"))
	injectCancelFailure(t, database)
	owner.send(t, map[string]any{"type": "queue_remove", "id": a.ID})
	owner.barrier(t)
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		if other.count(isType("queue_update")) != 0 {
			t.Fatal("取消失敗卻送出佇列廣播")
		}
		time.Sleep(20 * time.Millisecond)
	}
	state, _, _, err := database.GetQuotaResume(s.ID)
	if err != nil || state != db.QuotaResumeWaiting || queueText(t, database, s.ID) != "A,B" {
		t.Fatalf("delete rolled forward state=%s queue=%s err=%v", state, queueText(t, database, s.ID), err)
	}
	dropCancelFailure(t, database)
	owner.send(t, map[string]any{"type": "queue_remove", "id": a.ID})
	updated := other.await(t, "delete update", func(m map[string]any) bool {
		return m["type"] == "queue_update" && m["quota_resume_state"] == db.QuotaResumeNone && m["quota_resume_at"] == ""
	})
	queue, _ := updated["queue"].([]any)
	if len(queue) != 1 || updated["queue_paused"] != true || queueText(t, database, s.ID) != "B" {
		t.Fatalf("update=%v queue=%s", updated, queueText(t, database, s.ID))
	}
}

func TestQuotaResumeManualFinishMissesCancelAndNewInput(t *testing.T) {
	now := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	database := openQuotaDB(t)
	s := quotaSession(t, database, "late-finish", "fakeq")
	setAutoResume(t, database, true)
	msg, err := database.AddUserMessageWithAttachments(s.ID, "old", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, observed, err := database.SnapshotQuotaResume(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	run := func(during func()) (string, string) {
		t.Helper()
		entered := make(chan struct{})
		release := make(chan struct{})
		refresh := func(context.Context, string) (quota.Snapshot, error) {
			close(entered)
			<-release
			return fullSessionSnap(), nil
		}
		errCh := make(chan error, 1)
		go func() {
			errCh <- finishHeldQuotaResume(database, s.ID, "fakeq", msg.ID, true, false, false, refresh, now, 0, observed)
		}()
		<-entered
		during()
		close(release)
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
		state, _, _, err := database.GetQuotaResume(s.ID)
		if err != nil {
			t.Fatal(err)
		}
		return state, queueText(t, database, s.ID)
	}
	state, queue := run(func() {
		if err := database.CancelQuotaResume(s.ID); err != nil {
			t.Fatal(err)
		}
	})
	if state != db.QuotaResumeNone || queue != "" {
		t.Fatalf("cancel then finish state=%s queue=%s", state, queue)
	}
	_, _, observed, err = database.SnapshotQuotaResume(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	state, queue = run(func() {
		if _, err := database.AcceptInputCancelQuota(s.ID, "owner", "new-one", "hello", "", nil); err != nil {
			t.Fatal(err)
		}
	})
	if state != db.QuotaResumeNone || queue != "hello" {
		t.Fatalf("new input then finish state=%s queue=%s", state, queue)
	}
}
