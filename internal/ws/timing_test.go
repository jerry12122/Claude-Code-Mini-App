package ws

import (
	"testing"
	"time"

	"github.com/fasthttp/websocket"
)

func TestExecutionTimingSurvivesWebSocketReconnect(t *testing.T) {
	database, url := startTestServer(t)
	s, err := database.CreateSession("timer", "", "", "default", "fakeq", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { taskCancel(s.ID) })
	readUntil := func(conn *websocket.Conn, kind, value string) serverMsg {
		t.Helper()
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		for {
			var msg serverMsg
			if err := conn.ReadJSON(&msg); err != nil {
				t.Fatal(err)
			}
			if msg.Type == kind && (value == "" || msg.Value == value) {
				return msg
			}
		}
	}
	first, _, err := websocket.DefaultDialer.Dial(url(s.ID), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	readUntil(first, "sync", "")
	if err := first.WriteJSON(clientMsg{Type: "input", Data: "block timer"}); err != nil {
		t.Fatal(err)
	}
	started := readUntil(first, "status", StateThinking)
	if started.RunStartedAt == 0 || started.ServerTime < started.RunStartedAt {
		t.Fatalf("missing start: %+v", started)
	}
	first.Close()
	second, _, err := websocket.DefaultDialer.Dial(url(s.ID), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	synced := readUntil(second, "sync", StateThinking)
	if synced.RunStartedAt != started.RunStartedAt || synced.ServerTime < started.ServerTime {
		t.Fatalf("reconnect changed start: start=%+v sync=%+v", started, synced)
	}
	select {
	case fakeGate <- nil:
	case <-time.After(3 * time.Second):
		t.Fatal("runner did not start")
	}
	ended := readUntil(second, "status", StateIdle)
	if ended.RunStartedAt != 0 || ended.ServerTime != 0 {
		t.Fatalf("ended task kept timing: %+v", ended)
	}
}
