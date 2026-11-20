package db

import (
	"context"
	"encoding/json"
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

func TestRoomQueueVisibleBatchHint(t *testing.T) {
	ctx := context.Background()
	type env struct {
		d              *DB
		room, pm, work string
		other, otherRm string
	}
	newEnv := func(t *testing.T) env {
		t.Helper()
		d, pm := newFailureTestSession(t, t.TempDir()+"/hint.db")
		t.Cleanup(func() { d.Close() })
		work, err := d.CreateSession("worker", "", t.TempDir(), "default", "codex", nil, "agent")
		if err != nil {
			t.Fatal(err)
		}
		other, err := d.CreateSession("other", "", t.TempDir(), "default", "codex", nil, "agent")
		if err != nil {
			t.Fatal(err)
		}
		room, err := d.CreateRoom("hint", []RoomMember{{SessionID: pm.ID, Role: RoomRolePM}, {SessionID: work.ID, Role: RoomRoleWorker}})
		if err != nil {
			t.Fatal(err)
		}
		otherRoom, err := d.CreateRoom("other", []RoomMember{{SessionID: other.ID, Role: RoomRolePM}})
		if err != nil {
			t.Fatal(err)
		}
		return env{d, room.ID, pm.ID, work.ID, other.ID, otherRoom.ID}
	}
	accept := func(t *testing.T, e env, id, text string) {
		t.Helper()
		if err := e.d.AcceptRoomInput(e.room, e.pm, id, text, nil); err != nil {
			t.Fatal(err)
		}
	}
	ledger := func(t *testing.T, e env) (rows []QueuedMessage, receipts, batches int) {
		t.Helper()
		var err error
		if rows, err = e.d.ListQueuedMessages(e.pm); err != nil {
			t.Fatal(err)
		}
		if err = e.d.QueryRow(`SELECT count(*) FROM input_receipts WHERE session_id=?`, e.pm).Scan(&receipts); err != nil {
			t.Fatal(err)
		}
		if err = e.d.QueryRow(`SELECT count(*) FROM queue_batches WHERE session_id=?`, e.pm).Scan(&batches); err != nil {
			t.Fatal(err)
		}
		return rows, receipts, batches
	}
	pageOf := func(t *testing.T, e env, limit int, after int64) RoomQueuePage {
		t.Helper()
		page, err := e.d.RoomQueue(ctx, e.room, e.pm, limit, after)
		if err != nil {
			t.Fatal(err)
		}
		return page
	}
	raw := func(t *testing.T, page RoomQueuePage) string {
		t.Helper()
		b, err := json.Marshal(page)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	t.Run("contiguous", func(t *testing.T) {
		e := newEnv(t)
		accept(t, e, "a", "甲")
		accept(t, e, "b", "乙")
		page := pageOf(t, e, 100, 0)
		if !page.BatchReady || len(page.Queue) != 2 || page.Queue[0].Content != "甲" || page.Queue[1].Content != "乙" {
			t.Fatalf("連續 Room user 應可一起送：ready=%v %+v", page.BatchReady, page.Queue)
		}
		if _, err := e.d.ScheduleQueueBatch(ctx, e.room, e.pm, "ok", batchItems(mustQueued(t, e.d, e.pm)[:2])); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("report between blocks without leaking or writing", func(t *testing.T) {
		e := newEnv(t)
		accept(t, e, "a", "甲")
		if _, err := e.d.EnqueueMessageFromCancelQuota(e.pm, "SECRET-REPORT", "room-report:"+e.room, e.work); err != nil {
			t.Fatal(err)
		}
		accept(t, e, "b", "乙")
		before, receipts, batches := ledger(t, e)
		page := pageOf(t, e, 100, 0)
		body := raw(t, page)
		if page.BatchReady || len(page.Queue) != 2 {
			t.Fatalf("夾回報不可一起送：ready=%v n=%d", page.BatchReady, len(page.Queue))
		}
		for _, hidden := range []string{"SECRET-REPORT", "room-report", e.work} {
			if strings.Contains(body, hidden) {
				t.Fatalf("快照洩漏隱藏來源 %q：%s", hidden, body)
			}
		}
		after, r2, b2 := ledger(t, e)
		if !reflect.DeepEqual(before, after) || receipts != r2 || batches != b2 {
			t.Fatal("讀取快照改了佇列、receipt 或 batch")
		}
		items := batchItems([]QueuedMessage{before[0], before[2]})
		if _, err := e.d.ScheduleQueueBatch(ctx, e.room, e.pm, "no", items); !errors.Is(err, ErrQueueBatchForbidden) {
			t.Fatalf("夾回報仍應被來源守門拒絕：%v", err)
		}
		after, r2, b2 = ledger(t, e)
		if !reflect.DeepEqual(before, after) || receipts != r2 || batches != b2 {
			t.Fatal("拒絕一起送改了佇列、receipt 或 batch")
		}
	})
	t.Run("report at ends still allows the room pair", func(t *testing.T) {
		e := newEnv(t)
		if _, err := e.d.EnqueueMessage(e.pm, "SECRET-FRONT", "room-report:"+e.room); err != nil {
			t.Fatal(err)
		}
		accept(t, e, "a", "甲")
		accept(t, e, "b", "乙")
		page := pageOf(t, e, 100, 0)
		if !page.BatchReady || strings.Contains(raw(t, page), "SECRET-FRONT") {
			t.Fatalf("回報在最前仍應可一起送：ready=%v %s", page.BatchReady, raw(t, page))
		}
		e = newEnv(t)
		accept(t, e, "a", "甲")
		accept(t, e, "b", "乙")
		if _, err := e.d.EnqueueMessage(e.pm, "SECRET-END", "room-report:"+e.room); err != nil {
			t.Fatal(err)
		}
		page = pageOf(t, e, 100, 0)
		if !page.BatchReady || strings.Contains(raw(t, page), "SECRET-END") {
			t.Fatalf("回報在最後仍應可一起送：ready=%v %s", page.BatchReady, raw(t, page))
		}
	})
	t.Run("other session id gap does not block", func(t *testing.T) {
		e := newEnv(t)
		accept(t, e, "a", "甲")
		if _, err := e.d.EnqueueMessage(e.other, "OTHER-SESSION-GAP", ""); err != nil {
			t.Fatal(err)
		}
		accept(t, e, "b", "乙")
		page := pageOf(t, e, 100, 0)
		if !page.BatchReady || len(page.Queue) != 2 || page.Queue[1].ID-page.Queue[0].ID <= 1 || strings.Contains(raw(t, page), "OTHER-SESSION-GAP") {
			t.Fatalf("別的 session 佔用全域 ID 不該擋：ready=%v ids=%v %s", page.BatchReady, []int64{page.Queue[0].ID, page.Queue[1].ID}, raw(t, page))
		}
	})
	t.Run("short prefix is not batchable", func(t *testing.T) {
		e := newEnv(t)
		if page := pageOf(t, e, 100, 0); page.BatchReady || len(page.Queue) != 0 {
			t.Fatalf("空佇列：ready=%v n=%d", page.BatchReady, len(page.Queue))
		}
		accept(t, e, "a", "甲")
		if page := pageOf(t, e, 100, 0); page.BatchReady || len(page.Queue) != 1 {
			t.Fatalf("一筆：ready=%v n=%d", page.BatchReady, len(page.Queue))
		}
	})
	t.Run("first page decision is independent of the next page", func(t *testing.T) {
		e := newEnv(t)
		accept(t, e, "v0", "v0")
		if _, err := e.d.EnqueueMessage(e.pm, "SECRET-PAGE", "room-report:"+e.room); err != nil {
			t.Fatal(err)
		}
		for i := 1; i <= 101; i++ {
			accept(t, e, "v"+strings.Repeat("x", i), "v"+strings.Repeat("y", i))
		}
		first := pageOf(t, e, 100, 0)
		if first.BatchReady || !first.HasMore || len(first.Queue) != 100 || strings.Contains(raw(t, first), "SECRET-PAGE") {
			t.Fatalf("第一頁含夾層不可一起送：ready=%v more=%v n=%d", first.BatchReady, first.HasMore, len(first.Queue))
		}
		second := pageOf(t, e, 100, first.Next)
		if !second.BatchReady || second.HasMore || len(second.Queue) != 2 {
			t.Fatalf("後頁自己連續，不得回寫第一頁決策：ready=%v more=%v n=%d", second.BatchReady, second.HasMore, len(second.Queue))
		}
	})
	t.Run("cross room and private stay rejected", func(t *testing.T) {
		e := newEnv(t)
		if err := e.d.AcceptRoomInput(e.otherRm, e.other, "x", "外甲", nil); err != nil {
			t.Fatal(err)
		}
		if err := e.d.AcceptRoomInput(e.otherRm, e.other, "y", "外乙", nil); err != nil {
			t.Fatal(err)
		}
		foreign, err := e.d.ListQueuedMessages(e.other)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.d.ScheduleQueueBatch(ctx, e.room, e.pm, "cross", batchItems(foreign)); !errors.Is(err, ErrQueueBatchForbidden) && !errors.Is(err, ErrQueueBatchConflict) && !errors.Is(err, ErrRoomPMChanged) {
			t.Fatalf("跨 room 不該成功：%v", err)
		}
		if _, err := e.d.EnqueueMessage(e.pm, "私一", ""); err != nil {
			t.Fatal(err)
		}
		if _, err := e.d.EnqueueMessage(e.pm, "私二", ""); err != nil {
			t.Fatal(err)
		}
		page := pageOf(t, e, 100, 0)
		if page.BatchReady || len(page.Queue) != 0 || strings.Contains(raw(t, page), "私一") {
			t.Fatalf("私人訊息不該出現在 Room 快照：ready=%v %s", page.BatchReady, raw(t, page))
		}
		private, err := e.d.ListQueuedMessages(e.pm)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.d.ScheduleQueueBatch(ctx, "", e.pm, "private-ok", batchItems(private)); err != nil {
			t.Fatal(err)
		}
		if _, err := e.d.ScheduleQueueBatch(ctx, e.room, e.pm, "private-room", batchItems(private)); err == nil {
			t.Fatal("私人來源不該用 Room 範圍一起送")
		}
	})
	t.Run("snapshot race keeps the refusal and the draft", func(t *testing.T) {
		e := newEnv(t)
		accept(t, e, "a", "甲")
		gap, err := e.d.EnqueueMessage(e.other, "OTHER-GAP", "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.d.Exec(`DELETE FROM queued_messages WHERE id=?`, gap.ID); err != nil {
			t.Fatal(err)
		}
		accept(t, e, "b", "乙")
		page := pageOf(t, e, 100, 0)
		if !page.BatchReady || len(page.Queue) != 2 || page.Queue[1].ID-page.Queue[0].ID <= 1 {
			t.Fatalf("快照當下應可一起送：ready=%v %+v", page.BatchReady, page.Queue)
		}
		if _, err := e.d.Exec(`INSERT INTO queued_messages(id, session_id, content, author, source_session_id, attachment_ids) VALUES(?, ?, 'RACE-SECRET', ?, ?, '[]')`, gap.ID, e.pm, "room-report:"+e.room, e.work); err != nil {
			t.Fatal(err)
		}
		before, receipts, batches := ledger(t, e)
		if _, err := e.d.ScheduleQueueBatch(ctx, e.room, e.pm, "race", batchItems([]QueuedMessage{{ID: page.Queue[0].ID, Content: page.Queue[0].Content}, {ID: page.Queue[1].ID, Content: page.Queue[1].Content}})); !errors.Is(err, ErrQueueBatchForbidden) {
			t.Fatalf("快照後插入應被交易拒絕：%v", err)
		}
		after, r2, b2 := ledger(t, e)
		if !reflect.DeepEqual(before, after) || receipts != r2 || batches != b2 {
			t.Fatal("競爭拒絕改了佇列、receipt 或 batch")
		}
		if err := e.d.EditRoomQueuedMessage(ctx, e.room, e.pm, page.Queue[0].ID, "甲", "草稿仍在"); err != nil {
			t.Fatal(err)
		}
		again := pageOf(t, e, 100, 0)
		if again.BatchReady || strings.Contains(raw(t, again), "RACE-SECRET") || again.Queue[0].Content != "草稿仍在" {
			t.Fatalf("競爭後仍不可一起送，草稿要留著：ready=%v %s", again.BatchReady, raw(t, again))
		}
	})
}

func mustQueued(t *testing.T, d *DB, sessionID string) []QueuedMessage {
	t.Helper()
	rows, err := d.ListQueuedMessages(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}
