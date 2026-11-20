package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/fasthttp/websocket"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/ws"
)

type closeoutLingeringRunner struct {
	entered, release, finished chan struct{}
}

func (*closeoutLingeringRunner) Name() string { return "fakeq" }

func (r *closeoutLingeringRunner) Run(ctx context.Context, _ agent.RunOptions, cb agent.EventCallback) error {
	cb(agent.Event{Type: agent.EventDelta, Text: "finished result, process still alive"})
	cb(agent.Event{Type: agent.EventDone})
	close(r.entered)
	select {
	case <-ctx.Done():
	case <-r.release:
	}
	close(r.finished)
	return nil
}

func TestPMCloseoutRejectsLiveLingeringTask(t *testing.T) {
	runner := &closeoutLingeringRunner{entered: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{})}
	useE2ERunner(t, runner)
	database, runtime, _ := startCloseout(t, true)
	pm, roomID, worker := closeoutLingeringWorker(t, database)
	conn := dialCloseout(t, runtime, worker.ID)
	discardCloseout(conn)
	stopCloseoutRunnerAtCleanup(t, database, runner, worker.ID)
	if err := database.AddMessage(worker.ID, "user", "keep-lingering", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := database.EnqueueMessage(worker.ID, "queue-lingering", ""); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(map[string]string{"type": "input", "data": "test"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runner.entered:
	case <-time.After(4 * time.Second):
		t.Fatal("fake runner did not start")
	}
	before, err := database.GetSession(worker.ID)
	if err != nil {
		t.Fatal(err)
	}
	if before.Status != db.SessionStatusIdle {
		t.Fatalf("precondition: lingering status=%s", before.Status)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	approval, _, err := database.RequestOperationApproval("reset_session", roomID, pm.ID, worker.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.ApproveOperation(approval); err != nil {
		t.Fatal(err)
	}
	ctx = db.WithOperationApproval(ctx, approval)
	if err := runtime.resetMemberSession(ctx, worker.ID, roomID, pm.ID); err == nil {
		t.Fatal("reset_session reported success and cancelled a still-live lingering task; expected busy rejection")
	}
	select {
	case <-runner.finished:
		t.Fatal("tool reset cancelled the lingering runner")
	default:
	}
	msgs, err := database.ListMessages(worker.ID)
	if err != nil {
		t.Fatal(err)
	}
	kept := false
	for _, m := range msgs {
		if m.Content == "keep-lingering" {
			kept = true
		}
	}
	queued, err := database.CountQueuedMessages(worker.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !kept || queued != 1 {
		t.Fatalf("lingering reset cleared data msgs=%d queue=%d", len(msgs), queued)
	}
}

func TestUIResetStillCancelsLingeringTask(t *testing.T) {
	runner := &closeoutLingeringRunner{entered: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{})}
	useE2ERunner(t, runner)
	database, runtime, _ := startCloseout(t, true)
	pm, roomID, worker := closeoutLingeringWorker(t, database)
	conn := dialCloseout(t, runtime, worker.ID)
	stopCloseoutRunnerAtCleanup(t, database, runner, worker.ID)
	if err := conn.WriteJSON(map[string]string{"type": "input", "data": "test"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runner.entered:
	case <-time.After(4 * time.Second):
		t.Fatal("fake runner did not start")
	}
	if err := conn.WriteJSON(map[string]any{
		"type": "reset_context", "require_idle": true, "room_id": roomID, "pm_session_id": pm.ID,
	}); err != nil {
		t.Fatal(err)
	}
	// 等到 reset 廣播才算重置收完，之後清理才不會與仍在寫 DB 的重置搶。
	_ = conn.SetReadDeadline(time.Now().Add(4 * time.Second))
	for {
		_, payload, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("UI reset_context 沒有完成：%v", err)
		}
		var event struct{ Type string }
		if json.Unmarshal(payload, &event) == nil && event.Type == "reset" {
			break
		}
	}
	select {
	case <-runner.finished:
	case <-time.After(4 * time.Second):
		t.Fatal("UI reset_context did not cancel the lingering task")
	}
}

// stopCloseoutRunnerAtCleanup 在 DB 與服務關閉之前收掉自己的 runner：先清佇列（免得收尾又續跑），
// 放行 runner，等它退出，再等 ws 端所有收尾寫入完成（維護權拿得到代表沒有登記中的工作）。
// 必須在 startCloseout 之後呼叫，LIFO 才會讓它先於 DB／服務的關閉。
func stopCloseoutRunnerAtCleanup(t *testing.T, database *db.DB, runner *closeoutLingeringRunner, sessionID string) {
	t.Helper()
	t.Cleanup(func() {
		_ = database.ClearQueuedMessages(sessionID)
		close(runner.release)
		select {
		case <-runner.entered:
			select {
			case <-runner.finished:
			case <-time.After(5 * time.Second):
				t.Error("fake runner 沒有退出")
			}
		default:
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			release, err := ws.TryBeginDesktopUpdate()
			if err == nil {
				release()
				return
			}
			if time.Now().After(deadline) {
				t.Errorf("ws 收尾沒有結束：%v", err)
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	})
}

func closeoutLingeringWorker(t *testing.T, database *db.DB) (*db.Session, string, *db.Session) {
	t.Helper()
	pm, roomID, _, _ := closeoutMembers(t, database)
	worker, err := database.CreateSession("test-lingering", "", t.TempDir(), "default", "fakeq", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateRoom(roomID, nil, &[]db.RoomMember{
		{SessionID: pm.ID, Role: db.RoomRolePM},
		{SessionID: worker.ID, Role: db.RoomRoleWorker},
	}, pm.ID); err != nil {
		t.Fatal(err)
	}
	return pm, roomID, worker
}

func dialCloseout(t *testing.T, runtime roomRuntime, sessionID string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial("ws://"+runtime.address+"/sessions/"+sessionID+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func discardCloseout(conn *websocket.Conn) {
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
}
