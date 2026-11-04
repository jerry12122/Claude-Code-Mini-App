package ws

import (
	"errors"
	"strings"
	"testing"

	"github.com/fasthttp/websocket"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

// failureTestConn 建一個 fakeq session 並連上 WS，回傳送 input 的 helper。
func failureTestConn(t *testing.T) (*db.DB, *db.Session, func(string), func(map[string]string)) {
	t.Helper()
	database, wsURL := startTestServer(t)
	s, err := database.CreateSession("f", "", t.TempDir(), "default", "fakeq", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(s.ID), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	go func() { // 丟棄 server 推播，避免寫入端卡住
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	input := func(text string) {
		if err := conn.WriteJSON(map[string]string{"type": "input", "data": text}); err != nil {
			t.Fatal(err)
		}
	}
	raw := func(m map[string]string) {
		if err := conn.WriteJSON(m); err != nil {
			t.Fatal(err)
		}
	}
	return database, s, input, raw
}

func lastError(database *db.DB, id string) (string, string) {
	s, err := database.GetSession(id)
	if err != nil {
		return "", ""
	}
	return s.LastErrorText, s.LastErrorAt
}

func TestFailureRecord_FailedRunStoresErrorAndSuccessClearsIt(t *testing.T) {
	database, s, input, _ := failureTestConn(t)

	input("block-1")
	waitFor(t, "block-1 running", func() bool { return taskIsActive(s.ID) })
	fakeGate <- errors.New("You've hit your usage limit, try again at 4:42 PM")
	waitFor(t, "failure stored", func() bool {
		text, at := lastError(database, s.ID)
		return !taskIsActive(s.ID) && strings.Contains(text, "usage limit") && at != ""
	})

	input("hello")
	waitFor(t, "success clears failure", func() bool {
		text, at := lastError(database, s.ID)
		return !taskIsActive(s.ID) && lastReply(database, s.ID) == "ok:hello" && text == "" && at == ""
	})
}

func TestFailureRecord_CancelNeitherWritesNorClears(t *testing.T) {
	database, s, input, raw := failureTestConn(t)

	if err := database.SetSessionLastError(s.ID, "old failure"); err != nil {
		t.Fatal(err)
	}
	before, beforeAt := lastError(database, s.ID)

	input("block-cancel")
	waitFor(t, "block-cancel running", func() bool { return taskIsActive(s.ID) })
	raw(map[string]string{"type": "interrupt"})
	waitFor(t, "cancelled", func() bool { return !taskIsActive(s.ID) })

	after, afterAt := lastError(database, s.ID)
	if after != before || afterAt != beforeAt {
		t.Fatalf("取消不應改動失敗紀錄: before=(%q,%q) after=(%q,%q)", before, beforeAt, after, afterAt)
	}
}

// queue_resume 在任務執行中只解除暫停、不打斷目前任務；任務結束後才依序取出下一則（resume_queue 的忙碌情境）。
func TestQueueResume_BusySessionClearsPauseWithoutInterrupt(t *testing.T) {
	database, s, input, raw := failureTestConn(t)

	input("block-busy")
	waitFor(t, "block-busy running", func() bool { return taskIsActive(s.ID) })
	input("after-resume")
	waitFor(t, "queued", func() bool { return queueLen(database, s.ID) == 1 })
	if err := database.SetQueuePaused(s.ID, true); err != nil {
		t.Fatal(err)
	}

	raw(map[string]string{"type": "queue_resume"})
	waitFor(t, "pause cleared", func() bool {
		p, _ := database.QueuePaused(s.ID)
		return !p
	})
	if !taskIsActive(s.ID) {
		t.Fatal("resume 不應打斷執行中的任務")
	}
	if n := queueLen(database, s.ID); n != 1 {
		t.Fatalf("任務未結束前不應取出佇列，queue=%d", n)
	}

	fakeGate <- nil
	waitFor(t, "queued message ran after task ended", func() bool {
		return queueLen(database, s.ID) == 0 && !taskIsActive(s.ID) && lastReply(database, s.ID) == "ok:after-resume"
	})
}
