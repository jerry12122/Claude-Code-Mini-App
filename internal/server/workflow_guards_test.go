package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/fasthttp/websocket"
)

// 走真 user WS 的既有問答面板，不用 approved=true 模擬批准。
func callApprovedCloseout(t *testing.T, r roomRuntime, pmID, name string, args map[string]any) closeoutResult {
	t.Helper()
	return callAnsweredCloseout(t, r, pmID, name, args, "批准這次操作", nil)
}

func TestWorkflowMCPTokenCannotBorrowUserDeleteOrAnswer(t *testing.T) {
	base := startTestServer(t, "no_auth: true\nmcp_token: workflow-test-agent\n")
	request := func(method, path, body, token string) (int, map[string]any) {
		t.Helper()
		req, err := http.NewRequest(method, base+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var data map[string]any
		_ = json.NewDecoder(res.Body).Decode(&data)
		return res.StatusCode, data
	}
	status, session := request("POST", "/sessions", `{"name":"PM","agent_type":"claude","input_mode":"agent"}`, "")
	if status != 201 {
		t.Fatalf("create=%d %v", status, session)
	}
	pm := session["id"].(string)
	status, room := request("POST", "/rooms", fmt.Sprintf(`{"name":"room","members":[{"session_id":%q,"role":"pm"}]}`, pm), "")
	if status != 201 {
		t.Fatalf("room=%d %v", status, room)
	}
	roomID := room["id"].(string)
	for _, path := range []string{"/sessions/" + pm, "/rooms/" + roomID} {
		status, _ := request("DELETE", path, "", "workflow-test-agent")
		if status != 403 {
			t.Fatalf("agent 借用 DELETE %s：%d", path, status)
		}
	}
	conn, _, err := websocket.DefaultDialer.Dial(strings.Replace(base, "http://", "ws://", 1)+"/sessions/"+pm+"/ws", http.Header{"Authorization": {"Bearer workflow-test-agent"}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(4 * time.Second))
	if err := conn.WriteJSON(map[string]any{"type": "question_answer", "request_id": "forged", "answers": []any{map[string]any{"selected": []string{"批准這次操作"}}}}); err != nil {
		t.Fatal(err)
	}
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		var event struct{ Type, Content string }
		_ = json.Unmarshal(data, &event)
		if event.Type == "question_error" {
			if !strings.Contains(event.Content, "MCP 不可代答") {
				t.Fatalf("未標記 agent：%s", event.Content)
			}
			break
		}
	}
	conn.Close()
	// 原 UI 擁有者路徑仍可完成刪除（UI 保有原本確認／復原介面）。
	for _, path := range []string{"/rooms/" + roomID, "/sessions/" + pm} {
		status, body := request("DELETE", path, "", "")
		if status != 204 {
			t.Fatalf("UI delete %s=%d %v", path, status, body)
		}
	}
}

func callAnsweredCloseout(t *testing.T, r roomRuntime, pmID, name string, args map[string]any, choice string, beforeAnswer func()) closeoutResult {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial("ws://"+r.address+"/sessions/"+pmID+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetReadDeadline(time.Now().Add(8 * time.Second)); err != nil {
		t.Fatal(err)
	}
	// 先等 sync，確定訂閱已建立，工具才可開始廣播。
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		var event struct{ Type string }
		_ = json.Unmarshal(data, &event)
		if event.Type == "sync" {
			break
		}
	}
	answered := make(chan error, 1)
	go func() {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				answered <- err
				return
			}
			var event struct {
				Type     string
				Question struct {
					RequestID string `json:"request_id"`
				}
			}
			if json.Unmarshal(data, &event) != nil || event.Type != "question_request" {
				continue
			}
			if beforeAnswer != nil {
				beforeAnswer()
			}
			answered <- conn.WriteJSON(map[string]any{"type": "question_answer", "request_id": event.Question.RequestID, "answers": []any{map[string]any{"selected": []string{choice}}}})
			return
		}
	}()
	if args == nil {
		args = map[string]any{}
	}
	roomID, err := r.db.RoomForPM(pmID)
	if err != nil {
		t.Fatal(err)
	}
	board, err := r.db.RoomBoard(roomID)
	if err != nil {
		t.Fatal(err)
	}
	args["expected_version"] = board.Version
	result := callCloseout(t, r, pmID, name, args)
	conn.Close()
	select {
	case err := <-answered:
		if err != nil && !result.IsError {
			t.Fatal(fmt.Errorf("使用者確認失敗：%w", err))
		}
	case <-time.After(time.Second):
		t.Fatal("確認連線未結束")
	}
	return result
}

func TestWorkflowCloseoutUserApproval(t *testing.T) {
	for _, choice := range []string{"取消", "批准這次操作"} {
		t.Run(choice, func(t *testing.T) {
			d, r, _ := startCloseout(t, true)
			pm, room, worker, _ := closeoutMembers(t, d)
			if err := d.AddMessage(worker.ID, "user", "keep", ""); err != nil {
				t.Fatal(err)
			}
			result := callAnsweredCloseout(t, r, pm.ID, "reset_session", map[string]any{"session_id": worker.ID}, choice, nil)
			messages, err := d.ListMessages(worker.ID)
			if err != nil {
				t.Fatal(err)
			}
			if choice == "取消" {
				if !result.IsError || len(messages) != 1 {
					t.Fatalf("取消改資料：%+v %v", result, messages)
				}
			} else {
				if result.IsError || !result.OK || len(messages) != 0 {
					t.Fatalf("批准未成功：%+v %v room=%s", result, messages, room)
				}
			}
		})
	}
	t.Run("批准前目標改變", func(t *testing.T) {
		d, r, _ := startCloseout(t, true)
		pm, _, worker, _ := closeoutMembers(t, d)
		changed := make(chan error, 1)
		result := callAnsweredCloseout(t, r, pm.ID, "reset_session", map[string]any{"session_id": worker.ID}, "批准這次操作", func() { changed <- d.AddMessage(worker.ID, "user", "new delivery", "") })
		if err := <-changed; err != nil {
			t.Fatal(err)
		}
		messages, err := d.ListMessages(worker.ID)
		if err != nil || !result.IsError || len(messages) != 1 {
			t.Fatalf("舊批准清掉新版資料：%+v %v %v", result, messages, err)
		}
	})
}
