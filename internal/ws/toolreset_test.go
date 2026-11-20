package ws

import (
	"database/sql"
	"fmt"
	"net"
	"testing"

	fiberws "github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/tg"
)

// startWakeServer 與 startTestServer 相同，但連線都帶 room_wake，等於工作室工具用的內部喚醒連線。
func startWakeServer(t *testing.T) (*db.DB, func(string) string) {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/ws.db")
	if err != nil {
		t.Fatal(err)
	}
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Get("/sessions/:id/ws", func(c *fiber.Ctx) error {
		c.Locals("room_wake", true)
		return c.Next()
	}, fiberws.New(NewHandler(database, "", ShellOpts{}, nil, tg.NotifyConfig{})))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = app.Listener(ln) }()
	t.Cleanup(func() { _ = app.Shutdown(); database.Close() })
	port := ln.Addr().(*net.TCPAddr).Port
	return database, func(id string) string { return fmt.Sprintf("ws://127.0.0.1:%d/sessions/%s/ws", port, id) }
}

type resetRig struct {
	database                         *db.DB
	url                              func(string) string
	room, otherRoom                  string
	pm, worker, otherPM, otherWorker string
	approval                         string
}

func newResetRig(t *testing.T) *resetRig {
	t.Helper()
	database, url := startWakeServer(t)
	mk := func(name string) string {
		s, err := database.CreateSession(name, "", "", "default", "claude", nil, "agent")
		if err != nil {
			t.Fatal(err)
		}
		return s.ID
	}
	r := &resetRig{database: database, url: url, pm: mk("pm"), worker: mk("worker"), otherPM: mk("other-pm"), otherWorker: mk("other-worker")}
	room, err := database.CreateRoom("本間", []db.RoomMember{{SessionID: r.pm, Role: db.RoomRolePM}, {SessionID: r.worker, Role: db.RoomRoleWorker}})
	if err != nil {
		t.Fatal(err)
	}
	other, err := database.CreateRoom("別間", []db.RoomMember{{SessionID: r.otherPM, Role: db.RoomRolePM}, {SessionID: r.otherWorker, Role: db.RoomRoleWorker}})
	if err != nil {
		t.Fatal(err)
	}
	r.room, r.otherRoom = room.ID, other.ID
	return r
}

func (r *resetRig) seed(t *testing.T, id, marker string) {
	t.Helper()
	d := r.database
	for _, err := range []error{
		d.UpdateAgentSessionID(id, "agent-"+marker),
		d.AddMessage(id, "user", "msg-"+marker, ""),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.CreatePendingMessage(id); err != nil {
		t.Fatal(err)
	}
}

type resetSnap struct {
	agent, denials, shell, status string
	msgs, queued                  int
}

func (r *resetRig) snap(t *testing.T, id string) resetSnap {
	t.Helper()
	s, err := r.database.GetSession(id)
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := r.database.ListMessages(id)
	if err != nil {
		t.Fatal(err)
	}
	q, err := r.database.CountQueuedMessages(id)
	if err != nil {
		t.Fatal(err)
	}
	return resetSnap{s.AgentSessionID, s.PendingDenials, s.ShellPending, s.Status, len(msgs), q}
}

func isResetResult(m map[string]any) bool { return m["type"] == "reset" || m["type"] == "error" }

func (r *resetRig) sendReset(t *testing.T, c *wsClient) {
	t.Helper()
	if r.approval == "" {
		r.approve(t)
	}
	c.send(t, map[string]any{"type": "reset_context", "require_idle": true, "room_id": r.room, "pm_session_id": r.pm, "operation_approval": r.approval})
}

// 此 rig 測交易／dispatch 鎖；真正使用者問答另由 operation_approval_test 覆蓋。
func (r *resetRig) approve(t *testing.T) {
	t.Helper()
	id, _, err := r.database.RequestOperationApproval("reset_session", r.room, r.pm, r.worker)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.database.ApproveOperation(id); err != nil {
		t.Fatal(err)
	}
	r.approval = id
}

func TestToolResetIdleClearsOnlyThatSession(t *testing.T) {
	r := newResetRig(t)
	r.seed(t, r.worker, "w")
	r.seed(t, r.otherWorker, "o")
	otherBefore := r.snap(t, r.otherWorker)
	c := dialClient(t, r.url(r.worker))
	r.sendReset(t, c)
	if got := c.await(t, "reset 結果", isResetResult); got["type"] != "reset" {
		t.Fatalf("閒置會話被拒：%v", got)
	}
	if got := r.snap(t, r.worker); got != (resetSnap{status: db.SessionStatusIdle}) {
		t.Fatalf("重置後仍有資料：%+v", got)
	}
	if got := r.snap(t, r.otherWorker); got != otherBefore {
		t.Fatalf("別間會話被動到：%+v → %+v", otherBefore, got)
	}
}

func TestToolResetRefusesPendingAndBusyWithDataIntact(t *testing.T) {
	cases := map[string]func(*testing.T, *resetRig){
		"待授權": func(t *testing.T, r *resetRig) {
			if err := r.database.UpdatePendingDenials(r.worker, "denied"); err != nil {
				t.Fatal(err)
			}
		},
		"待 Shell 確認": func(t *testing.T, r *resetRig) {
			if err := r.database.UpdateShellPending(r.worker, "shell"); err != nil {
				t.Fatal(err)
			}
		},
		"執行中": func(t *testing.T, r *resetRig) {
			if err := r.database.UpdateSessionStatus(r.worker, db.SessionStatusRunning); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, mark := range cases {
		t.Run(name, func(t *testing.T) {
			r := newResetRig(t)
			r.seed(t, r.worker, "w")
			mark(t, r)
			before := r.snap(t, r.worker)
			c := dialClient(t, r.url(r.worker))
			r.sendReset(t, c)
			if got := c.await(t, "reset 結果", isResetResult); got["type"] != "error" {
				t.Fatalf("應被拒絕：%v", got)
			}
			if got := r.snap(t, r.worker); got != before {
				t.Fatalf("被拒後資料變了：%+v → %+v", before, got)
			}
		})
	}
}

// 任務在鎖內才登記（與正式啟動順序相同）：重置排在鎖後，必須看到它、不取消它。
func TestToolResetRefusesTaskStartedWhileWaitingOnDispatchLock(t *testing.T) {
	r := newResetRig(t)
	r.seed(t, r.worker, "w")
	before := r.snap(t, r.worker)
	c := dialClient(t, r.url(r.worker))
	dl := dispatchLock(r.worker)
	dl.Lock()
	r.sendReset(t, c)
	cancelled := false
	taskStart(r.worker, func() { cancelled = true }, -1)
	t.Cleanup(func() { taskEnd(r.worker, -1) })
	dl.Unlock()
	got := c.await(t, "reset 結果", isResetResult)
	if got["type"] != "error" || cancelled {
		t.Fatalf("鎖內啟動的任務被重置 result=%v cancelled=%v", got, cancelled)
	}
	if after := r.snap(t, r.worker); after != before {
		t.Fatalf("資料變了：%+v → %+v", before, after)
	}
}

// 競爭寫入者先拿到寫鎖並改了範圍；重置的交易只能排在後面，commit 後必須看到新範圍。
func TestToolResetScopeCheckAndClearShareOneTransaction(t *testing.T) {
	joinOther := func(r *resetRig, tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO room_members(room_id, session_id, role, position) VALUES(?, ?, 'worker', 9)`, r.otherRoom, r.worker)
		return err
	}
	swapPM := func(r *resetRig, tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE room_members SET role='worker' WHERE room_id=? AND session_id=?`, r.room, r.pm); err != nil {
			return err
		}
		_, err := tx.Exec(`UPDATE room_members SET role='pm' WHERE room_id=? AND session_id=?`, r.room, r.worker)
		return err
	}
	run := func(t *testing.T, mutate func(*resetRig, *sql.Tx) error, commit bool) (map[string]any, resetSnap, resetSnap) {
		r := newResetRig(t)
		r.seed(t, r.worker, "w")
		before := r.snap(t, r.worker)
		c := dialClient(t, r.url(r.worker))
		waitFor(t, "連線就緒", func() bool { return c.count(func(map[string]any) bool { return true }) > 0 })
		r.approve(t)
		tx, err := r.database.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if err := mutate(r, tx); err != nil {
			t.Fatal(err)
		}
		r.sendReset(t, c)
		waitFor(t, "重置的交易在等寫鎖", func() bool { return r.database.Stats().InUse >= 2 })
		if c.count(isResetResult) != 0 {
			t.Fatal("競爭寫入還沒結束，重置就已經回應")
		}
		if commit {
			err = tx.Commit()
		} else {
			err = tx.Rollback()
		}
		if err != nil {
			t.Fatal(err)
		}
		got := c.await(t, "reset 結果", isResetResult)
		return got, before, r.snap(t, r.worker)
	}
	for name, mutate := range map[string]func(*resetRig, *sql.Tx) error{"成為共用": joinOther, "PM 換人": swapPM} {
		t.Run(name, func(t *testing.T) {
			got, before, after := run(t, mutate, true)
			if got["type"] != "error" || after != before {
				t.Fatalf("範圍已變仍重置 result=%v %+v → %+v", got, before, after)
			}
		})
	}
	t.Run("競爭寫入放棄就照常重置", func(t *testing.T) {
		got, _, after := run(t, joinOther, false)
		if got["type"] != "reset" || after != (resetSnap{status: db.SessionStatusIdle}) {
			t.Fatalf("result=%v after=%+v", got, after)
		}
	})
}
