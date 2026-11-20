package ws

import (
	"context"
	"testing"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

func TestWorkflowPrivateConnectionsCannotApprove(t *testing.T) {
	for _, local := range []string{"mcp_agent", "room_wake"} {
		t.Run(local, func(t *testing.T) {
			database, url := startFlaggedServer(t, local)
			pm, err := database.CreateSession("PM", "", "", "bypassPermissions", "claude", nil, "agent")
			if err != nil {
				t.Fatal(err)
			}
			worker, err := database.CreateSession("worker", "", "", "default", "claude", nil, "agent")
			if err != nil {
				t.Fatal(err)
			}
			room, err := database.CreateRoom("room", []db.RoomMember{{SessionID: pm.ID, Role: db.RoomRolePM}, {SessionID: worker.ID, Role: db.RoomRoleWorker}})
			if err != nil {
				t.Fatal(err)
			}
			c := dialClient(t, url(pm.ID))
			c.await(t, "sync", isType("sync"))
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := RequestOperationApproval(ctx, database, "reset_session", room.ID, pm.ID, worker.ID)
				done <- err
			}()
			question := c.await(t, "確認", isType("question_request"))
			q := question["question"].(map[string]any)
			rid := q["request_id"].(string)
			c.send(t, map[string]any{"type": "question_answer", "request_id": rid, "answers": []any{map[string]any{"selected": []string{"批准這次操作"}}}})
			c.await(t, "拒絕 MCP 代答", isType("question_error"))
			var approved int
			if err := database.QueryRow("SELECT COALESCE(SUM(approved),0) FROM operation_approvals").Scan(&approved); err != nil || approved != 0 {
				t.Fatalf("agent 批准成功：%d %v", approved, err)
			}
			cancel()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("取消仍批准")
				}
			case <-time.After(time.Second):
				t.Fatal("取消未結束")
			}
		})
	}
}

func TestWorkflowQuestionCannotApproveByFreeText(t *testing.T) {
	input := []byte(`{"questions":[{"question":"clear?","header":"確認","options":[{"label":"取消"},{"label":"批准這次操作"}]}]}`)
	q, err := newQuestion(input)
	if err != nil {
		t.Fatal(err)
	}
	q.operation = true
	id := t.Name()
	pendingQuestions.Lock()
	pendingQuestions.m[id] = q
	pendingQuestions.Unlock()
	defer func() { pendingQuestions.Lock(); delete(pendingQuestions.m, id); pendingQuestions.Unlock() }()
	if err := questionResolve(id, q.RequestID, []questionAnswer{{Text: "批准這次操作"}}); err == nil {
		t.Fatal("自由文字不應批准")
	}
	if err := questionResolve(id, "replayed", []questionAnswer{{Selected: []string{"批准這次操作"}}}); err == nil {
		t.Fatal("錯誤識別可批准")
	}
	if err := questionResolve(id, q.RequestID, []questionAnswer{{Selected: []string{"批准這次操作"}}}); err != nil {
		t.Fatal(err)
	}
	if err := questionResolve(id, q.RequestID, []questionAnswer{{Selected: []string{"批准這次操作"}}}); err == nil {
		t.Fatal("同題可重播")
	}
}

func TestWorkflowApprovedRoomDeleteRefusesLingeringTask(t *testing.T) {
	r := newResetRig(t)
	id, _, err := r.database.RequestOperationApproval("room_delete", r.room, r.pm, r.room)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.database.ApproveOperation(id); err != nil {
		t.Fatal(err)
	}
	cancelled := false
	taskStart(r.worker, func() { cancelled = true }, -1)
	defer taskEnd(r.worker, -1)
	if err := DeleteRoomApproved(r.database, r.room, r.pm, id); err == nil || cancelled {
		t.Fatalf("仍在執行的成員被刪／中斷：%v %v", err, cancelled)
	}
	if _, err := r.database.GetRoom(r.room); err != nil {
		t.Fatal("工作室被刪除", err)
	}
}
