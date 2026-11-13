package ws

import (
	"github.com/fasthttp/websocket"
	"testing"
)

func TestCompletedReplyRemainsUnread(t *testing.T) {
	for _, scenario := range []string{"connected", "disconnected", "read_during_run"} {
		t.Run(scenario, func(t *testing.T) {
			database, wsURL := startTestServer(t)
			s, err := database.CreateSession("unread", "", t.TempDir(), "default", "fakeq", nil, "agent")
			if err != nil {
				t.Fatal(err)
			}
			conn, _, err := websocket.DefaultDialer.Dial(wsURL(s.ID), nil)
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
			if err := conn.WriteJSON(map[string]string{"type": "input", "data": "block-unread"}); err != nil {
				t.Fatal(err)
			}
			waitFor(t, "running", func() bool { return taskIsActive(s.ID) })
			if scenario == "read_during_run" {
				if err := database.MarkSessionRead(s.ID); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "disconnected" {
				conn.Close()
			}
			fakeGate <- nil
			waitFor(t, "completed", func() bool { return !taskIsActive(s.ID) && lastReply(database, s.ID) == "ok:block-unread" })
			got, err := database.GetSession(s.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.UnreadCount != 1 {
				t.Fatalf("unseen completion must remain unread: got=%d want=1", got.UnreadCount)
			}
		})
	}
}
