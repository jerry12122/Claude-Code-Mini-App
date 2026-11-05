package db

import (
	"errors"
	"testing"
)

func TestInputAcceptanceSurvivesRetryRestartAndQueueFailure(t *testing.T) {
	path := t.TempDir() + "/receipt.db"
	database, s := newFailureTestSession(t, path)
	defer func() { database.Close() }()
	accept := func(id, text string) bool {
		t.Helper()
		duplicate, err := database.AcceptInput(s.ID, "owner", id, text, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		return duplicate
	}
	if accept("one", "original") || !accept("one", "original") {
		t.Fatal("retry was not deduplicated")
	}
	if _, err := database.AcceptInput(s.ID, "owner", "one", "changed", "", nil); !errors.Is(err, ErrInputConflict) {
		t.Fatalf("conflict: %v", err)
	}
	if _, err := database.Exec(`CREATE TRIGGER reject_queue BEFORE INSERT ON queued_messages BEGIN SELECT RAISE(ABORT, 'queue unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.AcceptInput(s.ID, "owner", "failed", "keep", "", nil); err == nil {
		t.Fatal("expected queue failure")
	}
	if yes, err := database.InputAccepted(s.ID, "owner", "failed", "keep", "", nil); err != nil || yes {
		t.Fatalf("receipt survived rollback: %v %v", yes, err)
	}
	if _, err := database.Exec(`DROP TRIGGER reject_queue`); err != nil {
		t.Fatal(err)
	}
	q, err := database.ListQueuedMessages(s.ID)
	if err != nil || len(q) != 1 {
		t.Fatalf("queue=%v, err=%v", q, err)
	}
	if _, err := database.PromoteQueuedMessage(s.ID, q[0]); err != nil {
		t.Fatal(err)
	}
	if err := database.ClearMessages(s.ID); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	database, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if !accept("one", "original") {
		t.Fatal("reopened/history-cleared receipt was lost")
	}
	if n, err := database.CountQueuedMessages(s.ID); err != nil || n != 0 {
		t.Fatalf("retry queued again: %d %v", n, err)
	}
	if duplicate, err := database.AcceptInput(s.ID, "share:1:guest", "one", "original", "", nil); err != nil || duplicate {
		t.Fatalf("actor scope: %v %v", duplicate, err)
	}
	if err := database.DeleteSession(s.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := database.QueryRow(`SELECT COUNT(*) FROM input_receipts WHERE session_id = ?`, s.ID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("receipt cleanup: %d %v", n, err)
	}
}

func TestRestartMarksPartialReplyInterruptedAndPausesQueue(t *testing.T) {
	path := t.TempDir() + "/restart.db"
	database, s := newFailureTestSession(t, path)
	defer func() { database.Close() }()
	if _, err := database.EnqueueMessage(s.ID, "next", ""); err != nil {
		t.Fatal(err)
	}
	id, err := database.CreatePendingMessage(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AppendMessageContent(id, "partial"); err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateSessionStatus(s.ID, SessionStatusRunning); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	database, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.ResetRunningSessions(); err != nil {
		t.Fatal(err)
	}
	if paused, err := database.QueuePaused(s.ID); err != nil || !paused {
		t.Fatalf("queue not paused: %v %v", paused, err)
	}
	rows, err := database.ListMessages(s.ID)
	if err != nil || len(rows) != 1 || rows[0].Content != "partial" || rows[0].Status != MessageStatusInterrupted {
		t.Fatalf("history=%+v err=%v", rows, err)
	}
	s, err = database.GetSession(s.ID)
	if err != nil || s.Status != SessionStatusIdle || s.LastErrorText == "" {
		t.Fatalf("restart=%+v err=%v", s, err)
	}
	if err := database.SetSessionLastError(s.ID, "original failure"); err != nil {
		t.Fatal(err)
	}
	if err := database.ResetPendingMessages(); err != nil {
		t.Fatal(err)
	}
	s, err = database.GetSession(s.ID)
	if err != nil || s.LastErrorText != "original failure" {
		t.Fatalf("lost existing failure: %+v %v", s, err)
	}
}
