package server

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	mymcp "github.com/jerry12122/Claude-Code-Mini-App/internal/mcp"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/quota"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/tg"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/ws"
)

func TestRoomCloseoutResetAndDelete(t *testing.T) {
	t.Run("reset clears one session", func(t *testing.T) {
		database, runtime, _ := startCloseout(t, true)
		pm, _, worker, other := closeoutMembers(t, database)
		seedTrail(t, database, worker.ID, "keep", false)
		seedTrail(t, database, other.ID, "other", false)
		res := callCloseout(t, runtime, pm.ID, "reset_session", map[string]any{"session_id": worker.ID})
		if res.Status != http.StatusOK || res.IsError || !res.OK {
			t.Fatalf("reset status=%d error=%v ok=%v body=%s", res.Status, res.IsError, res.OK, res.Body)
		}
		if _, err := database.GetSession(worker.ID); err != nil {
			t.Fatal(err)
		}
		assertTrail(t, database, worker.ID, "keep", true, false)
		assertTrail(t, database, other.ID, "other", false, false)
	})

	t.Run("refuses pending denials and shell confirm with data intact", func(t *testing.T) {
		database, runtime, _ := startCloseout(t, true)
		pm, _, worker, _ := closeoutMembers(t, database)
		seedTrail(t, database, worker.ID, "waiting", true)
		res := callCloseout(t, runtime, pm.ID, "reset_session", map[string]any{"session_id": worker.ID})
		if res.Status != http.StatusOK || !res.IsError || res.OK {
			t.Fatalf("待授權／待確認仍重置 status=%d error=%v ok=%v body=%s", res.Status, res.IsError, res.OK, res.Body)
		}
		assertTrail(t, database, worker.ID, "waiting", false, true)
	})

	t.Run("refuses busy shared foreign and missing socket", func(t *testing.T) {
		database, runtime, _ := startCloseout(t, true)
		pm, roomID, worker, outsider := closeoutMembers(t, database)
		shared, err := database.CreateSession("shared", "", "", "default", "claude", nil, "agent")
		if err != nil {
			t.Fatal(err)
		}
		if err := database.UpdateRoom(outsider.RoomID, nil, &[]db.RoomMember{
			{SessionID: outsider.PMID, Role: db.RoomRolePM},
			{SessionID: outsider.ID, Role: db.RoomRoleWorker},
			{SessionID: shared.ID, Role: db.RoomRoleWorker},
		}, outsider.PMID); err != nil {
			t.Fatal(err)
		}
		members := []db.RoomMember{{SessionID: pm.ID, Role: db.RoomRolePM}, {SessionID: worker.ID, Role: db.RoomRoleWorker}, {SessionID: shared.ID, Role: db.RoomRoleWorker}}
		if err := database.UpdateRoom(roomID, nil, &members, pm.ID); err != nil {
			t.Fatal(err)
		}
		seedTrail(t, database, worker.ID, "busy", true)
		seedTrail(t, database, shared.ID, "shared", true)
		seedTrail(t, database, outsider.ID, "foreign", true)
		if err := database.UpdateSessionStatus(worker.ID, db.SessionStatusRunning); err != nil {
			t.Fatal(err)
		}
		busy := callCloseout(t, runtime, pm.ID, "reset_session", map[string]any{"session_id": worker.ID})
		sharedRes := callCloseout(t, runtime, pm.ID, "reset_session", map[string]any{"session_id": shared.ID})
		foreign := callCloseout(t, runtime, pm.ID, "reset_session", map[string]any{"session_id": outsider.ID})
		for _, res := range []closeoutResult{busy, sharedRes, foreign} {
			if res.Status != http.StatusOK || !res.IsError || res.OK {
				t.Fatalf("拒絕失敗 status=%d error=%v ok=%v body=%s", res.Status, res.IsError, res.OK, res.Body)
			}
		}
		assertTrail(t, database, worker.ID, "busy", false, true)
		assertTrail(t, database, shared.ID, "shared", false, true)
		assertTrail(t, database, outsider.ID, "foreign", false, true)

		denied := callCloseout(t, runtime, worker.ID, "reset_session", map[string]any{"session_id": worker.ID})
		if denied.Status != http.StatusForbidden {
			t.Fatalf("非 PM 得到 %d", denied.Status)
		}
	})

	t.Run("no socket is not ok", func(t *testing.T) {
		database, runtime, _ := startCloseout(t, false)
		pm, _, worker, _ := closeoutMembers(t, database)
		seedTrail(t, database, worker.ID, "offline", true)
		res := callCloseout(t, runtime, pm.ID, "reset_session", map[string]any{"session_id": worker.ID})
		if res.Status != http.StatusOK || !res.IsError || res.OK {
			t.Fatalf("沒連上仍成功 status=%d error=%v ok=%v body=%s", res.Status, res.IsError, res.OK, res.Body)
		}
		assertTrail(t, database, worker.ID, "offline", false, true)
	})

	t.Run("maintenance rejection is not ok", func(t *testing.T) {
		database, runtime, _ := startCloseout(t, true)
		pm, _, worker, _ := closeoutMembers(t, database)
		if err := database.UpdateAgentSessionID(worker.ID, "agent-reject"); err != nil {
			t.Fatal(err)
		}
		release, err := ws.TryBeginDesktopUpdate()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(release)
		res := callCloseout(t, runtime, pm.ID, "reset_session", map[string]any{"session_id": worker.ID})
		release()
		if res.Status != http.StatusOK || !res.IsError || res.OK {
			t.Fatalf("維護中仍成功 status=%d error=%v ok=%v body=%s", res.Status, res.IsError, res.OK, res.Body)
		}
		got, err := database.GetSession(worker.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.AgentSessionID != "agent-reject" {
			t.Fatalf("被拒後續接變成 %q", got.AgentSessionID)
		}
	})

	t.Run("delete keeps sessions and other room", func(t *testing.T) {
		database, runtime, _ := startCloseout(t, false)
		pm, roomID, worker, other := closeoutMembers(t, database)
		if err := database.SaveRoomMemo(roomID, pm.ID, "結案備忘"); err != nil {
			t.Fatal(err)
		}
		seedTrail(t, database, worker.ID, "stay", true)
		wrong := callCloseout(t, runtime, pm.ID, "room_delete", map[string]any{"room_id": other.RoomID})
		if wrong.Status != http.StatusOK || !wrong.IsError || wrong.OK {
			t.Fatalf("別間工作室被接受 status=%d error=%v ok=%v body=%s", wrong.Status, wrong.IsError, wrong.OK, wrong.Body)
		}
		if _, err := database.GetRoom(roomID); err != nil {
			t.Fatal(err)
		}
		denied := callCloseout(t, runtime, worker.ID, "room_delete", nil)
		if denied.Status != http.StatusForbidden {
			t.Fatalf("非 PM 刪除得到 %d", denied.Status)
		}
		res := callCloseout(t, runtime, pm.ID, "room_delete", nil)
		if res.Status != http.StatusOK || res.IsError || !res.OK {
			t.Fatalf("delete status=%d error=%v ok=%v body=%s", res.Status, res.IsError, res.OK, res.Body)
		}
		if _, err := database.GetRoom(roomID); err == nil {
			t.Fatal("工作室還在")
		}
		if _, err := database.GetRoom(other.RoomID); err != nil {
			t.Fatal(err)
		}
		if _, err := database.GetSession(pm.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := database.GetSession(worker.ID); err != nil {
			t.Fatal(err)
		}
		assertTrail(t, database, worker.ID, "stay", false, true)
	})
}

type closeoutPerson struct {
	ID     string
	RoomID string
	PMID   string
}

func closeoutMembers(t *testing.T, database *db.DB) (pm *db.Session, roomID string, worker *db.Session, other closeoutPerson) {
	t.Helper()
	var err error
	pm, err = database.CreateSession("PM", "", "", "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	worker, err = database.CreateSession("worker", "", "", "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	otherPM, err := database.CreateSession("other-pm", "", "", "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	otherWorker, err := database.CreateSession("other-worker", "", "", "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	room, err := database.CreateRoom("本間", []db.RoomMember{{SessionID: pm.ID, Role: db.RoomRolePM}, {SessionID: worker.ID, Role: db.RoomRoleWorker}})
	if err != nil {
		t.Fatal(err)
	}
	otherRoom, err := database.CreateRoom("別間", []db.RoomMember{{SessionID: otherPM.ID, Role: db.RoomRolePM}, {SessionID: otherWorker.ID, Role: db.RoomRoleWorker}})
	if err != nil {
		t.Fatal(err)
	}
	return pm, room.ID, worker, closeoutPerson{ID: otherWorker.ID, RoomID: otherRoom.ID, PMID: otherPM.ID}
}

func startCloseout(t *testing.T, withWS bool) (*db.DB, roomRuntime, *fiber.App) {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/closeout.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	runtime := newRoomRuntime(database, ln.Addr().(*net.TCPAddr).Port)
	runtime.registerRoutes(app, func(c *fiber.Ctx) error { return c.Next() }, quota.NewService())
	if withWS {
		app.Use("/sessions/:id/ws", func(c *fiber.Ctx) error {
			if websocket.IsWebSocketUpgrade(c) {
				return c.Next()
			}
			return fiber.ErrUpgradeRequired
		})
		app.Get("/sessions/:id/ws", runtime.wsAuth(func(c *fiber.Ctx) error { return c.Next() }), websocket.New(ws.NewHandler(database, "", ws.ShellOpts{}, quota.NewService(), tg.NotifyConfig{})))
	}
	go func() { _ = app.Listener(ln) }()
	t.Cleanup(func() { _ = app.Shutdown() })
	return database, runtime, app
}

type closeoutResult struct {
	Status  int
	IsError bool
	OK      bool
	Body    string
}

func callCloseout(t *testing.T, runtime roomRuntime, sessionID, name string, args map[string]any) closeoutResult {
	t.Helper()
	if args == nil {
		args = map[string]any{}
	}
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params":  map[string]any{"name": name, "arguments": args},
	})
	if err != nil {
		t.Fatal(err)
	}
	url, token := runtime.bridge(sessionID)
	if url == "" {
		url = "http://" + runtime.address + "/mcp/room"
		token = "invalid"
	}
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(string(payload)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set(mymcp.PermSessionHeader, sessionID)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	res, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if token == "invalid" {
		return closeoutResult{Status: res.StatusCode, Body: string(body)}
	}
	var parsed struct {
		Result struct {
			IsError           bool `json:"isError"`
			StructuredContent struct {
				OK bool `json:"ok"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	_ = json.Unmarshal(body, &parsed)
	return closeoutResult{Status: res.StatusCode, IsError: parsed.Result.IsError, OK: parsed.Result.StructuredContent.OK, Body: string(body)}
}

// seedTrail 寫入續接、訊息、佇列；pending 為真時再加上待授權與待 Shell 確認。
func seedTrail(t *testing.T, database *db.DB, id, marker string, pending bool) {
	t.Helper()
	if err := database.UpdateAgentSessionID(id, "agent-"+marker); err != nil {
		t.Fatal(err)
	}
	if err := database.AddMessage(id, "user", "msg-"+marker, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreatePendingMessage(id); err != nil {
		t.Fatal(err)
	}
	if _, err := database.EnqueueMessage(id, "queue-"+marker, ""); err != nil {
		t.Fatal(err)
	}
	if !pending {
		return
	}
	if err := database.UpdatePendingDenials(id, marker); err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateShellPending(id, marker); err != nil {
		t.Fatal(err)
	}
}

func assertTrail(t *testing.T, database *db.DB, id, marker string, cleared, pending bool) {
	t.Helper()
	sess, err := database.GetSession(id)
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := database.ListMessages(id)
	if err != nil {
		t.Fatal(err)
	}
	queued, err := database.CountQueuedMessages(id)
	if err != nil {
		t.Fatal(err)
	}
	if cleared {
		if sess.AgentSessionID != "" || len(msgs) != 0 || queued != 0 || sess.PendingDenials != "" || sess.ShellPending != "" {
			t.Fatalf("重置後仍有資料 agent=%q msgs=%d queue=%d denials=%q shell=%q", sess.AgentSessionID, len(msgs), queued, sess.PendingDenials, sess.ShellPending)
		}
		return
	}
	wantPending := ""
	if pending {
		wantPending = marker
	}
	if sess.AgentSessionID != "agent-"+marker || queued != 1 || sess.PendingDenials != wantPending || sess.ShellPending != wantPending || len(msgs) == 0 {
		t.Fatalf("不該動的會話變了 agent=%q msgs=%d queue=%d denials=%q shell=%q", sess.AgentSessionID, len(msgs), queued, sess.PendingDenials, sess.ShellPending)
	}
}
