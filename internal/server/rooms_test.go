package server

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	mymcp "github.com/jerry12122/Claude-Code-Mini-App/internal/mcp"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/quota"
)

// 走完整的 authMiddleware：未登入不得列出、建立、修改、刪除工作室，也不得讀時間軸。
func TestRoomsRequireAuth(t *testing.T) {
	base := startTestServer(t, "web:\n  password: correct-horse\n")
	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/rooms", ""},
		{"POST", "/rooms", `{"name":"x","members":[]}`},
		{"GET", "/rooms/x", ""},
		{"PUT", "/rooms/x", `{"name":"y"}`},
		{"DELETE", "/rooms/x", ""},
		{"GET", "/rooms/x/timeline", ""},
		{"GET", "/rooms/x/timeline?before=abc&limit=5", ""},
		{"GET", "/rooms/x/board", ""},
		{"PUT", "/rooms/x/board", `{"text":"memo","previous":""}`},
		{"POST", "/rooms/x/messages", `{"text":"hi","request_id":"test","pm_id":"x"}`},
		{"POST", "/mcp/room", "{}"},
	} {
		req, err := http.NewRequest(tc.method, base+tc.path, strings.NewReader(tc.body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, res.Body)
		res.Body.Close()
		if res.StatusCode != 401 {
			t.Errorf("未登入 %s %s 得到 %d，預期 401", tc.method, tc.path, res.StatusCode)
		}
	}
}

func TestRoomQuotaRequiresCurrentPM(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/rooms.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	pm, err := database.CreateSession("PM", "", "", "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	worker, err := database.CreateSession("worker", "", "", "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	members := []db.RoomMember{{SessionID: pm.ID, Role: db.RoomRolePM}, {SessionID: worker.ID, Role: db.RoomRoleWorker}}
	room, err := database.CreateRoom("工作室", members)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	runtime := newRoomRuntime(database, ln.Addr().(*net.TCPAddr).Port)
	runtime.registerRoutes(app, func(c *fiber.Ctx) error { return c.Next() }, quota.NewService())
	go func() { _ = app.Listener(ln) }()
	t.Cleanup(func() { _ = app.Shutdown() })
	url, token := runtime.bridge(pm.ID)
	client := &http.Client{Timeout: 5 * time.Second}
	call := func(sessionID string) *http.Response {
		t.Helper()
		body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_quota","arguments":{"provider":"claude"}}}`
		req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set(mymcp.PermSessionHeader, sessionID)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	res := call(pm.ID)
	var got struct {
		Result struct {
			IsError           bool `json:"isError"`
			StructuredContent struct {
				Quota map[string]quota.Payload `json:"quota"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	decodeErr := json.NewDecoder(res.Body).Decode(&got)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || decodeErr != nil || got.Result.IsError ||
		got.Result.StructuredContent.Quota["claude"].DisplayText != "—" {
		t.Fatalf("quota response status=%d err=%v result=%+v", res.StatusCode, decodeErr, got.Result)
	}
	assertForbidden := func(sessionID string) {
		t.Helper()
		res := call(sessionID)
		res.Body.Close()
		if res.StatusCode != http.StatusForbidden {
			t.Fatalf("quota request returned %d, want 403", res.StatusCode)
		}
	}
	assertForbidden(worker.ID)
	members[0].Role, members[1].Role = db.RoomRoleWorker, db.RoomRolePM
	if err := database.UpdateRoom(room.ID, nil, &members, pm.ID); err != nil {
		t.Fatal(err)
	}
	assertForbidden(pm.ID)
}
