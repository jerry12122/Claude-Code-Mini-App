package ws

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

func TestQueueBatchRealWSWaitsAndRunsOnce(t *testing.T) {
	t.Setenv("MINIAPP_TEST_LOOPBACK", "1")
	x, dial := newRunNowWSRig(t)
	attachmentPath := x.file(t)
	owner := dialClient(t, dial(x.s.ID, ""))
	owner.barrier(t)
	a := x.plan("block")
	owner.send(t, map[string]any{"type": "input", "data": "A", "request_id": "A"})
	readyRunNow(t, a)
	for _, text := range []string{"  B\n", "C  "} {
		owner.send(t, map[string]any{"type": "input", "data": text, "request_id": text, "attachment_ids": []string{"run-now-file"}})
	}
	owner.barrier(t)
	q := x.queue(t)
	if len(q) != 2 {
		t.Fatalf("q=%+v", q)
	}
	items := []map[string]any{{"id": q[0].ID, "previous": q[0].Content}, {"id": q[1].ID, "previous": q[1].Content}}
	owner.send(t, map[string]any{"type": "queue_batch", "request_id": "batch-one", "items": items})
	owner.barrier(t)
	if owner.count(isType("queue_batch_success")) != 1 {
		t.Fatalf("真WS未確認一起送：queue=%+v", x.queue(t))
	}
	select {
	case <-a.cancelled:
		t.Fatal("一起送不得打斷 A")
	default:
	}
	owner.send(t, map[string]any{"type": "queue_batch", "request_id": "batch-one", "items": items})
	owner.barrier(t)
	owner.send(t, map[string]any{"type": "input", "data": "later", "request_id": "later"})
	owner.barrier(t)
	batch, later := x.plan("block"), x.plan("block")
	a.release()
	readyRunNow(t, batch)
	if !strings.Contains(batch.opts.Prompt, "  B\n\n\nC  ") {
		t.Fatalf("批次文字／FIFO／空白未保留：%q", batch.opts.Prompt)
	}
	if strings.Count(batch.opts.Prompt, attachmentPath) != 1 {
		t.Fatalf("附件聯集應只出現一次：%q", batch.opts.Prompt)
	}
	history, err := x.database.ListMessages(x.s.ID)
	if err != nil || len(history) < 4 || len(history[2].Attachments) != 1 || len(history[3].Attachments) != 1 {
		t.Fatalf("原各筆附件關聯未保留：%+v %v", history, err)
	}
	if got := strings.Join(userMessages(x.database, x.s.ID), "|"); got != "A|  B\n|C  " {
		t.Fatalf("原歷史應逐筆保留：%q", got)
	}
	if q := x.queue(t); len(q) != 1 || q[0].Content != "later" {
		t.Fatalf("新進訊息不得入舊批：%+v", q)
	}
	for _, text := range []string{"  B\n", "C  "} {
		owner.send(t, map[string]any{"type": "input", "data": text, "request_id": text, "attachment_ids": []string{"run-now-file"}})
	}
	owner.barrier(t)
	if q := x.queue(t); len(q) != 1 {
		t.Fatal("receipt 重送又入列")
	}
	owner.send(t, map[string]any{"type": "queue_batch", "request_id": "batch-one", "items": items})
	owner.barrier(t)
	if owner.count(isType("queue_batch_success")) != 3 {
		t.Fatal("完成後重試應確認原批次")
	}
	batch.release()
	readyRunNow(t, later)
	later.release()
	waitFor(t, "batch 完成", func() bool { return !taskIsActive(x.s.ID) && queueLen(x.database, x.s.ID) == 0 })
	waitFor(t, "batch defer 完整完成", func() bool { gate.mu.Lock(); defer gate.mu.Unlock(); return gate.inflight == 0 })
	if _, err := x.database.AcceptInput(x.s.ID, "owner", "unrelated", "new idle input", "", nil); err != nil {
		t.Fatal(err)
	}
	owner.send(t, map[string]any{"type": "queue_batch", "request_id": "batch-one", "items": items})
	owner.barrier(t)
	if taskIsActive(x.s.ID) || len(x.queue(t)) != 1 {
		t.Fatal("完成的操作重試不得喚醒新輸入")
	}
}

func TestQueueBatchPrivateSourcesStayPrivate(t *testing.T) {
	database, dial := startShareServer(t)
	s, err := database.CreateSession("batch role", "", "", "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	_, ga := newShare(t, database, s.ID, db.ShareRoleEditor, db.ShareModeLive, "editor", time.Hour)
	editor := dialClient(t, dial(s.ID, ga.Guest.Token))
	editor.barrier(t)
	for _, text := range []string{"owner one", "owner two"} {
		if _, err := database.EnqueueMessage(s.ID, text, ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := database.SetQueuePaused(s.ID, true); err != nil {
		t.Fatal(err)
	}
	q, err := database.ListQueuedMessages(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	items := []map[string]any{{"id": q[0].ID, "previous": q[0].Content}, {"id": q[1].ID, "previous": q[1].Content}}
	editor.send(t, map[string]any{"type": "queue_batch", "request_id": "guest-batch", "items": items})
	editor.barrier(t)
	if editor.count(isType("queue_batch_error")) != 1 {
		t.Fatal("editor 不得安排私人 owner 批次")
	}
	if event := editor.first(isType("queue_batch_error")); event["code"] != "forbidden" {
		t.Fatalf("角色應先拒絕而非格式錯誤：%v", event)
	}
	after, err := database.ListQueuedMessages(s.ID)
	if err != nil || after[0].BatchID != "" || after[1].BatchID != "" {
		t.Fatal("guest 分組了 owner queue")
	}
	owner := dialClient(t, dial(s.ID, ""))
	owner.barrier(t)
	owner.send(t, map[string]any{"type": "queue_batch", "request_id": "malformed", "items": []any{map[string]any{"id": "wrong", "previous": "text"}}})
	owner.barrier(t)
	if owner.count(isType("queue_batch_error")) != 1 {
		t.Fatal("JSON 型別錯誤不能靜默丟棄")
	}
	_, viewerToken := newShare(t, database, s.ID, db.ShareRoleViewer, db.ShareModeLive, "viewer", time.Hour)
	viewer := dialClient(t, dial(s.ID, viewerToken.Guest.Token))
	viewer.barrier(t)
	viewer.send(t, map[string]any{"type": "queue_batch", "request_id": "viewer", "items": []any{}})
	viewer.barrier(t)
	if viewer.count(isType("error")) != 1 {
		t.Fatal("viewer 必須保持唯讀")
	}
}

func TestQueueBatchDrainGuardsAndRunNowEditRace(t *testing.T) {
	for _, guard := range []string{"paused", "permission", "question", "shell", "quota", "maintenance"} {
		t.Run(guard, func(t *testing.T) {
			x := newRunNowRig(t)
			x.d.author = ""
			x.h.author = ""
			for _, text := range []string{"one", "two"} {
				if _, err := x.database.AcceptInput(x.s.ID, "owner", text, text, "", nil); err != nil {
					t.Fatal(err)
				}
			}
			q := x.queue(t)
			items := []db.QueueBatchItem{{ID: q[0].ID, Previous: &q[0].Content}, {ID: q[1].ID, Previous: &q[1].Content}}
			id, err := ChangeQueueBatch(context.Background(), x.database, "", x.s.ID, "op", items, "")
			if err != nil {
				t.Fatal(err)
			}
			switch guard {
			case "paused":
				if err := x.database.SetQueuePaused(x.s.ID, true); err != nil {
					t.Fatal(err)
				}
			case "permission":
				e := &pendingPermEntry{ch: make(chan bool, 1)}
				permSet(x.s.ID, e)
				t.Cleanup(func() { permClear(x.s.ID, e) })
			case "question":
				pendingQuestions.Lock()
				pendingQuestions.m[x.s.ID] = &pendingQuestion{}
				pendingQuestions.Unlock()
				t.Cleanup(func() { pendingQuestions.Lock(); delete(pendingQuestions.m, x.s.ID); pendingQuestions.Unlock() })
			case "shell":
				setShellPending(x.s.ID, &shellPendingInfo{Command: "wait"})
				t.Cleanup(func() { clearInMemoryShellApproval(x.s.ID) })
			case "quota":
				x.d.quotaGuard = func() string { return "quota unavailable" }
			case "maintenance":
				gate.mu.Lock()
				gate.maint = true
				gate.mu.Unlock()
				t.Cleanup(func() { gate.mu.Lock(); gate.maint = false; gate.mu.Unlock() })
			}
			next := x.plan("block")
			x.d.Drain()
			select {
			case <-next.ready:
				t.Fatal("批次繞過守門")
			default:
			}
			if len(x.queue(t)) != 2 || len(userMessages(x.database, x.s.ID)) != 0 {
				t.Fatal("守門時不可提升批次")
			}
			if pending, err := x.database.QueueBatchPending(x.s.ID, id); err != nil || !pending {
				t.Fatal("守門不應消耗批次")
			}
		})
	}
	for _, operation := range []string{"edit", "run-now", "remove", "drain"} {
		t.Run(operation, func(t *testing.T) {
			x := newRunNowRig(t)
			x.d.author = ""
			x.h.author = ""
			for _, text := range []string{"one", "two"} {
				if _, err := x.database.AcceptInput(x.s.ID, "owner", text, text, "", nil); err != nil {
					t.Fatal(err)
				}
			}
			q := x.queue(t)
			items := []db.QueueBatchItem{{ID: q[0].ID, Previous: &q[0].Content}, {ID: q[1].ID, Previous: &q[1].Content}}
			id, err := ChangeQueueBatch(context.Background(), x.database, "", x.s.ID, "op", items, "")
			if err != nil {
				t.Fatal(err)
			}
			if err := x.database.SetQueuePaused(x.s.ID, true); err != nil {
				t.Fatal(err)
			}
			lock := dispatchLock(x.s.ID)
			lock.Lock()
			started, done := make(chan struct{}), make(chan struct{})
			var operationErr error
			go func() {
				close(started)
				defer close(done)
				switch operation {
				case "edit":
					operationErr = EditPrivateQueuedMessage(context.Background(), x.database, x.s.ID, "edit", q[0].ID, q[0].Content, "changed")
				case "run-now":
					x.d.RunNow(q[0].ID)
				case "remove":
					operationErr = RemoveQueuedMessage(x.database, x.s.ID, q[0].ID)
				case "drain":
					x.d.Drain()
				}
			}()
			<-started
			select {
			case <-done:
				lock.Unlock()
				t.Fatal("未共用 dispatchLock")
			case <-time.After(20 * time.Millisecond):
			}
			lock.Unlock()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("操作未返回")
			}
			if operation == "edit" && !errors.Is(operationErr, db.ErrQueueEditConflict) {
				t.Fatal(operationErr)
			}
			if operation == "remove" && !errors.Is(operationErr, db.ErrQueueBatchConflict) {
				t.Fatal(operationErr)
			}
			if q := x.queue(t); len(q) != 2 || q[0].BatchID != id {
				t.Fatalf("競爭修改了半批：%+v", q)
			}
			if operation == "run-now" {
				select {
				case reply := <-x.replies:
					if reply.Type != "error" {
						t.Fatal(reply)
					}
				default:
					t.Fatal("RunNow 必須拒絕批次成員")
				}
			}
			// 同批並行 Drain：只有一輪 runner；兩筆歷史與 batch receipt 同時提交。
			if err := x.database.SetQueuePaused(x.s.ID, false); err != nil {
				t.Fatal(err)
			}
			next := x.plan("block")
			var wg sync.WaitGroup
			wg.Add(2)
			for i := 0; i < 2; i++ {
				go func() { defer wg.Done(); x.d.Drain() }()
			}
			wg.Wait()
			readyRunNow(t, next)
			if len(userMessages(x.database, x.s.ID)) != 2 || len(x.queue(t)) != 0 {
				t.Fatal("並行 Drain 應只提交一批")
			}
			next.release()
		})
	}
}

func TestQueueBatchRoomHandoffSharesDispatchLock(t *testing.T) {
	x := newRunNowRig(t)
	next, err := x.database.CreateSession("next", "", "", "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	room, err := x.database.CreateRoom("batch lock", []db.RoomMember{{SessionID: x.s.ID, Role: db.RoomRolePM}, {SessionID: next.ID, Role: db.RoomRoleWorker}})
	if err != nil {
		t.Fatal(err)
	}
	members := []db.RoomMember{{SessionID: x.s.ID, Role: db.RoomRoleWorker}, {SessionID: next.ID, Role: db.RoomRolePM}}
	lock := dispatchLock(x.s.ID)
	lock.Lock()
	done := make(chan error, 1)
	go func() { done <- UpdateRoomMembers(context.Background(), x.database, room.ID, nil, &members, x.s.ID) }()
	select {
	case err := <-done:
		lock.Unlock()
		t.Fatalf("交接沒有等待出列鎖：%v", err)
	case <-time.After(20 * time.Millisecond):
	}
	lock.Unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("交接未完成")
	}
}
