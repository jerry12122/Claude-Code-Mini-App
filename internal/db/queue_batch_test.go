package db

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func batchItems(q []QueuedMessage) []QueueBatchItem {
	items := make([]QueueBatchItem, len(q))
	for i := range q {
		text := q[i].Content
		items[i] = QueueBatchItem{ID: q[i].ID, Previous: &text}
	}
	return items
}

func TestQueueBatchAttachmentsRollbackCancelAndRestart(t *testing.T) {
	path := t.TempDir() + "/restart.db"
	d, s := newFailureTestSession(t, path)
	defer func() { d.Close() }()
	for _, id := range []string{"shared", "second"} {
		requireRoomTestOK(t, d.AddAttachment(&Attachment{ID: id, SessionID: s.ID, Name: id, StorageName: id}))
	}
	for i, ids := range [][]string{{"shared"}, {"shared", "second"}} {
		text := []string{"  first\n", "second  "}[i]
		if _, err := d.AcceptInput(s.ID, "owner", text, text, "", ids); err != nil {
			t.Fatal(err)
		}
	}
	q, _ := d.ListQueuedMessages(s.ID)
	id, err := d.ScheduleQueueBatch(context.Background(), "", s.ID, "attach", batchItems(q))
	if err != nil {
		t.Fatal(err)
	}
	requireRoomTestOK(t, d.Close())
	d, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	q, _ = d.ListQueuedMessages(s.ID)
	text, ids, err := d.QueueBatchInput(s.ID, q[0])
	if err != nil || text != "  first\n\n\nsecond  " || !reflect.DeepEqual(ids, []string{"shared", "second"}) {
		t.Fatalf("snapshot=%q %v %v", text, ids, err)
	}
	if _, err := d.Exec(`CREATE TRIGGER reject_second_batch BEFORE INSERT ON messages WHEN NEW.content='second  ' BEGIN SELECT RAISE(ABORT,'batch failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.PromoteQueuedBatch(context.Background(), s.ID, q[0]); err == nil {
		t.Fatal("應全批回滾")
	}
	msgs, _ := d.ListMessages(s.ID)
	after, _ := d.ListQueuedMessages(s.ID)
	if len(msgs) != 0 || !reflect.DeepEqual(after, q) {
		t.Fatal("提升失敗留下部分 history 或刪除部分 rows")
	}
	if _, err := d.Exec(`DROP TRIGGER reject_second_batch`); err != nil {
		t.Fatal(err)
	}
	requireRoomTestOK(t, d.CancelQueueBatch(context.Background(), "", s.ID, id))
	after, _ = d.ListQueuedMessages(s.ID)
	if len(after) != 2 || after[0].BatchID != "" {
		t.Fatal("取消應只解組")
	}
	requireRoomTestOK(t, d.EditQueuedMessage(context.Background(), s.ID, after[0].ID, after[0].Content, "edited"))
	if yes, err := d.InputAccepted(s.ID, "owner", "  first\n", "  first\n", "", []string{"shared"}); err != nil || !yes {
		t.Fatal("編輯不可改原 receipt")
	}
	after, _ = d.ListQueuedMessages(s.ID)
	next, err := d.ScheduleQueueBatch(context.Background(), "", s.ID, "new-op", batchItems(after))
	if err != nil || next == id {
		t.Fatal(err)
	}
	requireRoomTestOK(t, d.ClearQueuedMessages(s.ID))
	var pending int
	if err := d.QueryRow(`SELECT count(*) FROM queue_batches WHERE session_id=? AND state='pending'`, s.ID).Scan(&pending); err != nil || pending != 0 {
		t.Fatal("清空留下 pending metadata")
	}
	requireRoomTestOK(t, d.DeleteSession(s.ID))
	var metadata int
	if err := d.QueryRow(`SELECT count(*) FROM queue_batches WHERE session_id=?`, s.ID).Scan(&metadata); err != nil || metadata != 0 {
		t.Fatalf("session 刪除應清除批次操作 metadata：%d %v", metadata, err)
	}
}

func TestQueueBatchRoomScopeHandoffAndInvalidRequests(t *testing.T) {
	d := roomTestDB(t)
	old := roomSession(t, d, "old", "claude")
	next := roomSession(t, d, "next", "codex")
	room, err := d.CreateRoom("batch", []RoomMember{{SessionID: old.ID, Role: RoomRolePM}, {SessionID: next.ID, Role: RoomRoleWorker}})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"one", "two"} {
		requireRoomTestOK(t, d.AcceptRoomInput(room.ID, old.ID, id, id, nil))
	}
	q, _ := d.ListQueuedMessages(old.ID)
	if _, err := d.ScheduleQueueBatch(context.Background(), "", old.ID, "private", batchItems(q)); !errors.Is(err, ErrQueueBatchForbidden) {
		t.Fatalf("Room 不得混入私人：%v", err)
	}
	if _, err := d.ScheduleQueueBatch(context.Background(), room.ID, next.ID, "wrong-pm", batchItems(q)); !errors.Is(err, ErrRoomPMChanged) {
		t.Fatal(err)
	}
	id, err := d.ScheduleQueueBatch(context.Background(), room.ID, old.ID, "room-op", batchItems(q))
	if err != nil {
		t.Fatal(err)
	}
	members := []RoomMember{{SessionID: old.ID, Role: RoomRoleWorker}, {SessionID: next.ID, Role: RoomRolePM}}
	if err := d.UpdateRoom(room.ID, nil, &members, old.ID); !errors.Is(err, ErrRoomHandoffBusy) {
		t.Fatalf("pending batch 交接=%v", err)
	}
	requireRoomTestOK(t, d.CancelQueueBatch(context.Background(), room.ID, old.ID, id))
	requireRoomTestOK(t, d.UpdateRoom(room.ID, nil, &members, old.ID))
	if _, err := d.ScheduleQueueBatch(context.Background(), room.ID, old.ID, "stale", batchItems(q)); !errors.Is(err, ErrRoomPMChanged) {
		t.Fatal(err)
	}
	if err := d.AcceptRoomInput(room.ID, next.ID, "two", "two", nil); err != nil {
		t.Fatal("交接應保留第二筆 receipt", err)
	}
	q, _ = d.ListQueuedMessages(next.ID)
	if len(q) != 2 {
		t.Fatalf("handoff=%+v", q)
	}
	valid := batchItems(q)
	for _, items := range [][]QueueBatchItem{nil, valid[:1], {valid[1], valid[0]}, {{ID: valid[0].ID, Previous: nil}, valid[1]}} {
		if _, err := d.ScheduleQueueBatch(context.Background(), room.ID, next.ID, "invalid", items); !errors.Is(err, ErrQueueBatchInvalid) {
			t.Fatalf("invalid=%v", err)
		}
	}
	bad := strings.Repeat("x", 129)
	if _, err := d.ScheduleQueueBatch(context.Background(), room.ID, next.ID, bad, valid); !errors.Is(err, ErrQueueBatchInvalid) {
		t.Fatal(err)
	}
}

func TestQueueBatchPreservesEveryReceiptAndHistory(t *testing.T) {
	d, s := newFailureTestSession(t, t.TempDir()+"/batch.db")
	defer d.Close()
	for _, id := range []string{"one", "two"} {
		if _, err := d.AcceptInput(s.ID, "owner", id, "  "+id+"\n", "", nil); err != nil {
			t.Fatal(err)
		}
	}
	q, err := d.ListQueuedMessages(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	requireRoomTestOK(t, d.SetQueuePaused(s.ID, true))
	before, err := d.GetSession(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	id, err := d.ScheduleQueueBatch(context.Background(), "", s.ID, "operation", batchItems(q))
	if err != nil || id == "" {
		t.Fatalf("schedule=%q %v", id, err)
	}
	if again, err := d.ScheduleQueueBatch(context.Background(), "", s.ID, "operation", batchItems(q)); err != nil || again != id {
		t.Fatalf("replay=%q %v", again, err)
	}
	if _, err := d.AcceptInput(s.ID, "owner", "three", "later", "", nil); err != nil {
		t.Fatal(err)
	}
	group, err := d.ListQueuedMessages(s.ID)
	if err != nil || group[0].BatchID != id || group[1].BatchID != id || group[2].BatchID != "" {
		t.Fatalf("group=%+v %v", group, err)
	}
	if err := d.EditQueuedMessage(context.Background(), s.ID, q[0].ID, q[0].Content, "changed"); !errors.Is(err, ErrQueueEditConflict) {
		t.Fatalf("group edit=%v", err)
	}
	if err := d.DeleteQueuedMessage(s.ID, q[1].ID); !errors.Is(err, ErrQueueBatchConflict) {
		t.Fatalf("group delete=%v", err)
	}
	msgs, err := d.PromoteQueuedBatch(context.Background(), s.ID, group[0])
	if err != nil || len(msgs) != 2 {
		t.Fatalf("promote=%+v %v", msgs, err)
	}
	for i, id := range []string{"one", "two"} {
		if msgs[i].Content != q[i].Content {
			t.Fatal("原文被改寫")
		}
		var request string
		if err := d.QueryRow("SELECT input_request_id FROM messages WHERE id=?", msgs[i].ID).Scan(&request); err != nil || request != id {
			t.Fatalf("request=%q %v", request, err)
		}
		if yes, err := d.InputAccepted(s.ID, "owner", id, q[i].Content, "", nil); err != nil || !yes {
			t.Fatalf("receipt=%v %v", yes, err)
		}
	}
	left, err := d.ListQueuedMessages(s.ID)
	if err != nil || len(left) != 1 || left[0].Content != "later" {
		t.Fatalf("new input absorbed=%+v %v", left, err)
	}
	if again, err := d.ScheduleQueueBatch(context.Background(), "", s.ID, "operation", batchItems(q)); err != nil || again != id {
		t.Fatalf("completed replay=%q %v", again, err)
	}
	after, err := d.GetSession(s.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("schedule/promote 改變 paused/quota/session")
	}
}

func TestQueueBatchCASAndSourceAreAtomic(t *testing.T) {
	d, s := newFailureTestSession(t, t.TempDir()+"/conflict.db")
	defer d.Close()
	for _, text := range []string{"one", "two"} {
		if _, err := d.EnqueueMessage(s.ID, text, ""); err != nil {
			t.Fatal(err)
		}
	}
	q, _ := d.ListQueuedMessages(s.ID)
	items := batchItems(q)
	wrong := "stale"
	items[1].Previous = &wrong
	if _, err := d.ScheduleQueueBatch(context.Background(), "", s.ID, "op", items); !errors.Is(err, ErrQueueBatchConflict) {
		t.Fatal(err)
	}
	if rows, _ := d.ListQueuedMessages(s.ID); !reflect.DeepEqual(rows, q) {
		t.Fatal("CAS 部分修改")
	}
	if _, err := d.Exec("UPDATE queued_messages SET author='room-report:other' WHERE id=?", q[1].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ScheduleQueueBatch(context.Background(), "", s.ID, "op", batchItems(q)); !errors.Is(err, ErrQueueBatchForbidden) {
		t.Fatal(err)
	}
	var n int
	d.QueryRow("SELECT count(*) FROM queue_batches").Scan(&n)
	if n != 0 {
		t.Fatal("失敗留下 batch receipt")
	}
}
