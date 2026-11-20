package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fasthttp/websocket"
	fiberws "github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/tg"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/ws"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGlobalDeleteRequiresUserApproval(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/guards.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	s, err := database.CreateSession("keep", "", "", "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	d := deps{db: database}
	if _, _, err := d.deleteSession(context.Background(), nil, deleteSessionIn{SessionID: s.ID}); err == nil {
		t.Fatal("缺少使用者批准仍刪除")
	}
	if _, err := database.GetSession(s.ID); err != nil {
		t.Fatal("拒絕後資料消失", err)
	}
}

func TestWorkflowMCPBoardVersions(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/versions.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	pm, _ := database.CreateSession("PM", "", "", "default", "claude", nil, "agent")
	worker, _ := database.CreateSession("worker", "", "", "default", "claude", nil, "agent")
	room, err := database.CreateRoom("room", []db.RoomMember{{SessionID: pm.ID, Role: db.RoomRolePM}, {SessionID: worker.ID, Role: db.RoomRoleWorker}})
	if err != nil {
		t.Fatal(err)
	}
	server := gomcp.NewServer(&gomcp.Implementation{Name: "guards", Version: "1"}, nil)
	registerRoomTools(server, database, "", func(*gomcp.CallToolRequest, string) (string, string, error) { return room.ID, pm.ID, nil })
	ct, st := gomcp.NewInMemoryTransports()
	ss, err := server.Connect(t.Context(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	client, err := gomcp.NewClient(&gomcp.Implementation{Name: "c", Version: "1"}, nil).Connect(t.Context(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	call := func(name string, args map[string]any) *gomcp.CallToolResult {
		t.Helper()
		res, err := client.CallTool(t.Context(), &gomcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	version := func() string {
		t.Helper()
		res := call("get_board", map[string]any{})
		var board struct{ Version string }
		if err := json.Unmarshal([]byte(res.Content[0].(*gomcp.TextContent).Text), &board); err != nil || board.Version == "" {
			t.Fatal("board 沒有權威版本", err)
		}
		return board.Version
	}
	old := version()
	for _, expected := range []any{nil, ""} {
		args := map[string]any{"text": "lost"}
		if expected != nil {
			args["expected_version"] = expected
		}
		if !call("save_memo", args).IsError {
			t.Fatal("缺版覆寫成功")
		}
	}
	if call("save_memo", map[string]any{"text": "keep", "expected_version": old}).IsError {
		t.Fatal("新版備忘失敗")
	}
	if !call("save_memo", map[string]any{"text": "lost", "expected_version": old}).IsError {
		t.Fatal("舊版覆寫成功")
	}
	for _, expected := range []any{nil, "", old} {
		args := map[string]any{"session_id": worker.ID, "title": "測試工作", "text": "job"}
		if expected != nil {
			args["expected_version"] = expected
		}
		if !call("dispatch", args).IsError {
			t.Fatal("缺版／舊版派工成功")
		}
	}
	for _, title := range []any{nil, "", "two\nlines", strings.Repeat("甲", 121)} {
		args := map[string]any{"session_id": worker.ID, "text": "job", "expected_version": version()}
		if title != nil {
			args["title"] = title
		}
		if !call("dispatch", args).IsError {
			t.Fatal("缺少／無效標題仍派工")
		}
	}
	if call("dispatch", map[string]any{"session_id": worker.ID, "title": "測試工作", "text": "job", "expected_version": version()}).IsError {
		t.Fatal("新版派工失敗")
	}
}

func guardSocketServer(t *testing.T) (*db.DB, *deps, string) {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/socket.db")
	if err != nil {
		t.Fatal(err)
	}
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Get("/sessions/:id/ws", func(c *fiber.Ctx) error {
		if c.Get("X-Test-Agent") == "yes" {
			c.Locals("mcp_agent", true)
		}
		return c.Next()
	}, fiberws.New(ws.NewHandler(database, "", ws.ShellOpts{}, nil, tg.NotifyConfig{})))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = app.Listener(ln) }()
	base := fmt.Sprintf("ws://%s/sessions/", ln.Addr())
	d := &deps{db: database, maxHops: DefaultMaxHops, reg: NewRegistry(func(id string) string { return base + id + "/ws" }, http.Header{"X-Test-Agent": {"yes"}})}
	t.Cleanup(func() {
		d.reg.mu.Lock()
		for _, st := range d.reg.sessions {
			_ = st.conn.Close()
		}
		d.reg.mu.Unlock()
		_ = app.Shutdown()
		_ = database.Close()
	})
	return database, d, base
}

func TestWorkflowManualCompletionAndAutomaticReportShareOneEntry(t *testing.T) {
	database, d, _ := guardSocketServer(t)
	pm, _ := database.CreateSession("PM", "", t.TempDir(), "default", "fakemcp", nil, "agent")
	a, _ := database.CreateSession("a", "", "", "default", "claude", nil, "agent")
	b, _ := database.CreateSession("b", "", "", "default", "claude", nil, "agent")
	driver, _ := database.CreateSession("外部詢問者", "", "", "default", "fakemcp", nil, "agent")
	room, err := database.CreateRoom("room", []db.RoomMember{{SessionID: pm.ID, Role: db.RoomRolePM}, {SessionID: a.ID, Role: db.RoomRoleWorker}, {SessionID: b.ID, Role: db.RoomRoleWorker}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.sendMessage(t.Context(), nil, sendMessageIn{SessionID: pm.ID, FromSessionID: driver.ID, Intent: "question", Text: "block Q:PM"}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "PM running", func() bool { s, _ := database.GetSession(pm.ID); return s.Status == db.SessionStatusRunning })
	defer func() {
		e2eGate <- struct{}{}
		waitUntil(t, "PM finished", func() bool { s, _ := database.GetSession(pm.ID); return s.Status == db.SessionStatusIdle })
	}()
	jobIDs := map[string]int64{}
	for _, worker := range []string{a.ID, b.ID} {
		job, err := database.DispatchRoomJob(room.ID, pm.ID, worker, "job")
		if err != nil {
			t.Fatal(err)
		}
		jobIDs[worker] = job
		queue, _ := database.ListQueuedMessages(worker)
		input, err := database.PromoteQueuedMessage(worker, queue[0])
		if err != nil {
			t.Fatal(err)
		}
		reply, _ := database.CreatePendingMessage(worker)
		if err := database.BindRoomJob(worker, input.ID, reply); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		errs := make(chan error, 12)
		for i := range 6 {
			wg.Go(func() {
				in := sendMessageIn{SessionID: pm.ID, FromSessionID: worker, Text: "completion", Intent: "completion", JobID: job}
				if i%2 == 0 {
					in.Intent = ""
					in.JobID = 0
					in.CompletionJobID = job
				}
				_, _, err := d.sendMessage(t.Context(), nil, in)
				errs <- err
			})
			wg.Go(func() {
				if err := database.FinishRoomJob(worker, reply, "done"); err != nil {
					errs <- err
					return
				}
				errs <- database.DeliverRoomReports()
			})
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	queue, err := database.ListQueuedMessages(pm.ID)
	if err != nil || len(queue) != 2 {
		t.Fatalf("完成重複進 runnable queue：%d %v", len(queue), err)
	}
	for _, in := range []sendMessageIn{
		{FromSessionID: a.ID, Text: "completed"},
		{FromSessionID: a.ID, Intent: "question", Text: "真問題但漏 job"},
		{Intent: "question", JobID: jobIDs[a.ID], Text: "漏 source"},
		{Text: "漏所有標識"},
		{FromSessionID: a.ID, Intent: "question", JobID: jobIDs[b.ID], Text: "錯 job"},
		{FromSessionID: a.ID, Intent: "completion", JobID: jobIDs[b.ID], Text: "冒用 source"},
		{FromSessionID: "forged-source", Intent: "completion", JobID: jobIDs[a.ID], Text: "來源不存在"},
		{FromSessionID: a.ID, Intent: "question", CompletionJobID: jobIDs[a.ID], Text: "不可把問題當完工吞掉"},
	} {
		in.SessionID = pm.ID
		if _, _, err := d.sendMessage(t.Context(), nil, in); err == nil {
			t.Fatalf("缺／錯標識未拒絕：%+v", in)
		}
	}
	queue, err = database.ListQueuedMessages(pm.ID)
	if err != nil || len(queue) != 2 {
		t.Fatalf("被拒訊息進入 PM queue：%d %v", len(queue), err)
	}
	if _, _, err := d.sendMessage(t.Context(), nil, sendMessageIn{SessionID: pm.ID, FromSessionID: a.ID, Intent: "question", JobID: jobIDs[a.ID], Text: "工作完成了嗎？"}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "一般詢問保留", func() bool { q, _ := database.ListQueuedMessages(pm.ID); return len(q) == 3 })
	if _, _, err := d.sendMessage(t.Context(), nil, sendMessageIn{SessionID: pm.ID, FromSessionID: a.ID, CompletionJobID: 999999}); err == nil {
		t.Fatal("錯誤 job 不能被吞成成功")
	}
	// 測試結束前清佇列，避免 PM 解除阻塞後把驗證資料跑掉。
	if err := database.ClearQueuedMessages(pm.ID); err != nil {
		t.Fatal(err)
	}
}

func TestWorkflowAskSessionToPMRemainsExplicitQuestion(t *testing.T) {
	database, d, _ := guardSocketServer(t)
	pm, err := database.CreateSession("PM", "", t.TempDir(), "default", "fakemcp", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	worker, err := database.CreateSession("worker", "", "", "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.CreateRoom("room", []db.RoomMember{{SessionID: pm.ID, Role: db.RoomRolePM}, {SessionID: worker.ID, Role: db.RoomRoleWorker}})
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"", worker.ID} {
		_, out, err := d.askSession(t.Context(), nil, askSessionIn{SessionID: pm.ID, FromSessionID: source, Text: "Q:完成後的新詢問", TimeoutSec: 5})
		if err != nil || out.TimedOut || out.Text != "reply-to:Q:完成後的新詢問" {
			t.Fatalf("ask_session 不能正常詢問：%+v %v", out, err)
		}
	}
}

func TestWorkflowGlobalDeleteRealUserApproval(t *testing.T) {
	database, d, base := guardSocketServer(t)
	pm, _ := database.CreateSession("PM", "", "", "bypassPermissions", "claude", nil, "agent")
	worker, _ := database.CreateSession("keep", "", "", "default", "claude", nil, "agent")
	room, err := database.CreateRoom("room", []db.RoomMember{{SessionID: pm.ID, Role: db.RoomRolePM}, {SessionID: worker.ID, Role: db.RoomRoleWorker}})
	if err != nil {
		t.Fatal(err)
	}
	user, _, err := websocket.DefaultDialer.Dial(base+pm.ID+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer user.Close()
	_ = user.SetReadDeadline(time.Now().Add(8 * time.Second))
	for {
		_, data, err := user.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		var e struct{ Type string }
		_ = json.Unmarshal(data, &e)
		if e.Type == "sync" {
			break
		}
	}
	// 真 MCP schema；模型自行傳 confirmed 或 approval_id 仍不能核准。
	srv := httptest.NewServer(NewHTTPHandler(database, nil, d.reg, 0))
	defer srv.Close()
	client, err := gomcp.NewClient(&gomcp.Implementation{Name: "user-approval", Version: "1"}, nil).Connect(t.Context(), &gomcp.StreamableClientTransport{Endpoint: srv.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	forged, err := client.CallTool(t.Context(), &gomcp.CallToolParams{Name: "delete_session", Arguments: map[string]any{"session_id": worker.ID, "room_id": room.ID, "pm_session_id": pm.ID, "confirmed": true, "approval_id": "forged"}})
	if err != nil {
		t.Fatal(err)
	}
	if !forged.IsError {
		t.Fatal("模型偽造批准欄位被接受")
	}
	result := make(chan *gomcp.CallToolResult, 1)
	failure := make(chan error, 1)
	board, err := database.RoomBoard(room.ID)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		res, err := client.CallTool(t.Context(), &gomcp.CallToolParams{Name: "delete_session", Arguments: map[string]any{"session_id": worker.ID, "room_id": room.ID, "pm_session_id": pm.ID, "expected_version": board.Version}})
		if err != nil {
			failure <- err
			return
		}
		result <- res
	}()
	for {
		_, data, err := user.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		var e struct {
			Type     string
			Question struct {
				RequestID string `json:"request_id"`
			}
		}
		_ = json.Unmarshal(data, &e)
		if e.Type != "question_request" {
			continue
		}
		if _, err := database.GetSession(worker.ID); err != nil {
			t.Fatal("使用者批准前已刪除")
		}
		if err := user.WriteJSON(map[string]any{"type": "question_answer", "request_id": e.Question.RequestID, "answers": []any{map[string]any{"selected": []string{"批准這次操作"}}}}); err != nil {
			t.Fatal(err)
		}
		break
	}
	select {
	case err := <-failure:
		t.Fatal(err)
	case res := <-result:
		if res.IsError {
			t.Fatalf("真正批准失敗：%+v", res)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("刪除未完成")
	}
	if _, err := database.GetSession(worker.ID); err == nil {
		t.Fatal("目標仍存在")
	}
}
