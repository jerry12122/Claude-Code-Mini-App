package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCloseoutToolsStayOnRoomServer(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/closeout-tools.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()
	names := func(handler http.Handler) map[string]bool {
		t.Helper()
		server := httptest.NewServer(handler)
		t.Cleanup(server.Close)
		client, err := gomcp.NewClient(&gomcp.Implementation{Name: "closeout-tools", Version: "1"}, nil).Connect(ctx, &gomcp.StreamableClientTransport{Endpoint: server.URL}, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = client.Close() })
		got := map[string]bool{}
		var cursor string
		for {
			list, err := client.ListTools(ctx, &gomcp.ListToolsParams{Cursor: cursor})
			if err != nil {
				t.Fatal(err)
			}
			for _, tool := range list.Tools {
				got[tool.Name] = true
			}
			if list.NextCursor == "" {
				break
			}
			cursor = list.NextCursor
		}
		return got
	}
	global := names(NewHTTPHandler(database, nil, NewRegistry(nil, nil), 0))
	room := names(NewRoomHTTPHandler(database, nil, func(context.Context, string, string, string) error {
		t.Fatal("列出工具不該重置")
		return nil
	}))
	for _, name := range []string{"room_delete", "reset_session", "room_room_delete", "room_reset_session"} {
		if global[name] {
			t.Errorf("全域 MCP 不該提供 %s", name)
		}
	}
	if !room["room_delete"] || !room["reset_session"] {
		t.Fatalf("工作室 MCP 工具 = %v", room)
	}
	if room["delete_session"] {
		t.Fatal("工作室 MCP 不該提供全域 delete_session")
	}
}

// scope 回來之後、刪除取得寫鎖之前，PM 真的交接給別人：舊 PM 的 room_delete 必須失敗，工作室原封不動。
func TestRoomDeleteRefusesPMHandedOffAfterScope(t *testing.T) {
	setup := func(t *testing.T, handoff bool) (*db.DB, string, string, string, string, *gomcp.CallToolResult) {
		t.Helper()
		database, err := db.Open(t.TempDir() + "/delete-race.db")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = database.Close() })
		mk := func(name string) string {
			s, err := database.CreateSession(name, "", "", "default", "claude", nil, "agent")
			if err != nil {
				t.Fatal(err)
			}
			return s.ID
		}
		oldPM, newPM, worker := mk("old-pm"), mk("new-pm"), mk("worker")
		room, err := database.CreateRoom("本間", []db.RoomMember{{SessionID: oldPM, Role: db.RoomRolePM}, {SessionID: newPM, Role: db.RoomRoleWorker}, {SessionID: worker, Role: db.RoomRoleWorker}})
		if err != nil {
			t.Fatal(err)
		}
		if err := database.SaveRoomMemo(room.ID, oldPM, "memo-keep"); err != nil {
			t.Fatal(err)
		}
		if _, err := database.DispatchRoomJob(room.ID, oldPM, worker, "job-keep"); err != nil {
			t.Fatal(err)
		}
		server := gomcp.NewServer(&gomcp.Implementation{Name: "delete-race", Version: "1"}, nil)
		scope := func(_ *gomcp.CallToolRequest, _ string) (string, string, error) {
			// 與正式 scope 一樣先通過檢查，再發生一次真正的 DB 交接。
			gotRoom, err := database.RoomForPM(oldPM)
			if err != nil {
				return "", "", err
			}
			if handoff {
				members := []db.RoomMember{{SessionID: newPM, Role: db.RoomRolePM}, {SessionID: oldPM, Role: db.RoomRoleWorker}, {SessionID: worker, Role: db.RoomRoleWorker}}
				if err := database.UpdateRoom(room.ID, nil, &members, oldPM); err != nil {
					return "", "", err // 不可在 handler goroutine 呼叫 t.Fatal，會讓呼叫端卡住
				}
			}
			return gotRoom, oldPM, nil
		}
		registerCloseoutTools(server, database, "", scope, nil)
		ct, st := gomcp.NewInMemoryTransports()
		ctx := context.Background()
		ss, err := server.Connect(ctx, st, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ss.Close() })
		cs, err := gomcp.NewClient(&gomcp.Implementation{Name: "c", Version: "1"}, nil).Connect(ctx, ct, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cs.Close() })
		board, err := database.RoomBoard(room.ID)
		if err != nil {
			t.Fatal(err)
		}
		res, err := cs.CallTool(ctx, &gomcp.CallToolParams{Name: "room_delete", Arguments: map[string]any{"expected_version": board.Version}})
		if err != nil {
			t.Fatal(err)
		}
		return database, room.ID, oldPM, newPM, worker, res
	}
	count := func(t *testing.T, database *db.DB, q string, args ...any) int {
		t.Helper()
		var n int
		if err := database.QueryRow(q, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	t.Run("交接後舊 PM 失敗且資料不變", func(t *testing.T) {
		database, roomID, oldPM, newPM, worker, res := setup(t, true)
		if !res.IsError {
			t.Fatalf("舊 PM 刪掉了新 PM 的工作室：%+v", res)
		}
		if got, err := database.RoomPM(roomID); err != nil || got != newPM {
			t.Fatalf("現任 PM = %q err=%v，應為 %q", got, err, newPM)
		}
		if n := count(t, database, `SELECT COUNT(*) FROM room_memos WHERE room_id = ? AND text = 'memo-keep'`, roomID); n != 1 {
			t.Fatal("備忘被刪")
		}
		if n := count(t, database, `SELECT COUNT(*) FROM room_jobs WHERE room_id = ?`, roomID); n != 1 {
			t.Fatal("派工被刪")
		}
		if n, err := database.CountQueuedMessages(worker); err != nil || n == 0 {
			t.Fatalf("佇列被刪 n=%d err=%v", n, err)
		}
		for _, id := range []string{oldPM, newPM, worker} {
			if _, err := database.GetSession(id); err != nil {
				t.Fatalf("session %s 被刪：%v", id, err)
			}
		}
	})

	t.Run("同一 PM 缺使用者批准仍不刪除", func(t *testing.T) {
		database, roomID, oldPM, newPM, worker, res := setup(t, false)
		if !res.IsError {
			t.Fatalf("缺批准仍刪除：%+v", res)
		}
		if _, err := database.GetRoom(roomID); err != nil {
			t.Fatal("工作室被刪除")
		}
		if n := count(t, database, `SELECT COUNT(*) FROM room_memos WHERE room_id = ?`, roomID) + count(t, database, `SELECT COUNT(*) FROM room_jobs WHERE room_id = ?`, roomID); n != 2 {
			t.Fatal("備忘或派工被刪")
		}
		for _, id := range []string{oldPM, newPM, worker} {
			if _, err := database.GetSession(id); err != nil {
				t.Fatalf("session %s 被刪：%v", id, err)
			}
		}
	})
}
