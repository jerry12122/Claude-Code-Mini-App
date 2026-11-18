package db

import (
	"errors"
	"strings"
	"testing"
)

func TestRoomWorkflowStoresParallelJobsAndReportsOnce(t *testing.T) {
	d := roomTestDB(t)
	pm := roomSession(t, d, "PM", "claude")
	a := roomSession(t, d, "A", "claude")
	room, err := d.CreateRoom("工作室", []RoomMember{{SessionID: pm.ID, Role: RoomRolePM}, {SessionID: a.ID, Role: RoomRoleWorker}})
	if err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	b, err := d.CreateRoomWorker(room.ID, pm.ID, "B", "codex")
	must(err)
	worker, err := d.GetSession(b)
	must(err)
	if worker.WorkDir != pm.WorkDir || worker.PermissionMode != "default" {
		t.Fatalf("worker did not inherit the PM workspace with default permissions: %+v", worker)
	}
	if err := d.AcceptRoomInput(room.ID, a.ID, "message1", "release", nil); !errors.Is(err, ErrRoomPM) {
		t.Fatalf("message sent to a non-PM: %v", err)
	}
	must(d.AcceptRoomInput(room.ID, pm.ID, "message1", "release", nil))
	must(d.AcceptRoomInput(room.ID, pm.ID, "message1", "release", nil))
	if _, err := d.DispatchRoomJob(room.ID, b, a.ID, "out of scope"); !errors.Is(err, ErrRoomWorker) {
		t.Fatalf("non-PM dispatch error = %v", err)
	}
	must(d.SaveRoomMemo(room.ID, pm.ID, "- [ ] 兩項工作"))
	first, err := d.DispatchRoomJob(room.ID, pm.ID, a.ID, "job A")
	must(err)
	_, err = d.DispatchRoomJob(room.ID, pm.ID, b, "job B")
	must(err)
	if _, err = d.DispatchRoomJob(room.ID, pm.ID, a.ID, "duplicate"); !errors.Is(err, ErrRoomWorker) {
		t.Fatalf("duplicate error = %v", err)
	}
	queue, err := d.ListQueuedMessages(a.ID)
	must(err)
	started, err := d.PromoteQueuedMessage(a.ID, queue[0])
	must(err)
	assistant, err := d.CreatePendingMessage(a.ID)
	must(err)
	must(d.BindRoomJob(a.ID, started.ID, assistant))
	must(d.UpdateSessionStatus(a.ID, SessionStatusAwaitingConfirm))
	must(d.DeliverRoomReports())
	board, err := d.RoomBoard(room.ID)
	must(err)
	if board.Memo != "- [ ] 兩項工作" || len(board.Jobs) != 2 || board.Jobs[0].Status != "queued" ||
		board.Jobs[1].ID != first || board.Jobs[1].Status != "blocked" {
		t.Fatalf("unexpected workflow board: %+v", board)
	}

	must(d.AppendMessageContent(assistant, "worker output"))
	must(d.FinalizeMessage(assistant))
	must(d.FinishRoomJob(a.ID, assistant, "done"))
	must(d.DeliverRoomReports())
	must(d.DeliverRoomReports())
	reports, err := d.ListQueuedMessages(pm.ID)
	must(err)
	if len(reports) != 2 || reports[0].Content != "release" || RoomMessageAuthor(reports[1].Author) != "執行者回報" {
		t.Fatalf("expected one user message and one report only for PM: %+v", reports)
	}
	queued, err := d.ListQueuedMessages(b)
	must(err)
	if len(queued) != 1 || queued[0].Content != "job B" {
		t.Fatalf("unexpected messages sent to worker B: %+v", queued)
	}
}

func TestRoomHandoffPreservesMemoAttachmentsAndReports(t *testing.T) {
	d := roomTestDB(t)
	oldPM := roomSession(t, d, "舊 PM", "claude")
	newPM := roomSession(t, d, "新 PM", "codex")
	worker := roomSession(t, d, "執行者", "cursor")
	outside := roomSession(t, d, "外部", "claude")
	members := []RoomMember{{oldPM.ID, RoomRolePM, 0}, {newPM.ID, RoomRoleWorker, 0}, {worker.ID, RoomRoleWorker, 0}}
	room, err := d.CreateRoom("交接", members)
	if err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.CreateRoom("兩個 PM", []RoomMember{{oldPM.ID, RoomRolePM, 0}, {newPM.ID, RoomRolePM, 0}}); !errors.Is(err, ErrRoomPM) {
		t.Fatalf("expected PM conflict, got %v", err)
	}
	must(d.EditRoomMemo(room.ID, "", "手動待辦"))
	if err := d.EditRoomMemo(room.ID, "", "覆蓋"); !errors.Is(err, ErrRoomMemoChanged) {
		t.Fatalf("expected memo conflict, got %v", err)
	}
	if err := d.RenameRoomSession(room.ID, oldPM.ID, outside.ID, "改名"); !errors.Is(err, ErrRoomSessionMissing) {
		t.Fatalf("renaming an outside session = %v", err)
	}
	must(d.RenameRoomSession(room.ID, oldPM.ID, worker.ID, "前端"))
	file := &Attachment{ID: "file1", SessionID: oldPM.ID, Name: "memo.txt", StorageName: "internal.txt", MimeType: "text/plain", Size: 4}
	must(d.AddAttachment(file))
	must(d.AcceptRoomInput(room.ID, oldPM.ID, "message", "", []string{file.ID}))
	members[0].Role = RoomRoleWorker
	members[1].Role = RoomRolePM
	if err := d.UpdateRoom(room.ID, nil, &members, oldPM.ID); !errors.Is(err, ErrRoomHandoffBusy) {
		t.Fatalf("expected pending attachment to block handoff, got %v", err)
	}
	q, err := d.ListQueuedMessages(oldPM.ID)
	must(err)
	_, err = d.PromoteQueuedMessage(oldPM.ID, q[0])
	must(err)
	must(d.AddMessage(oldPM.ID, "claude", "附件已收到", ""))
	_, err = d.DispatchRoomJob(room.ID, oldPM.ID, worker.ID, "檢查介面")
	must(err)
	q, err = d.ListQueuedMessages(worker.ID)
	must(err)
	assignment, err := d.PromoteQueuedMessage(worker.ID, q[0])
	must(err)
	replyID, err := d.CreatePendingMessage(worker.ID)
	must(err)
	must(d.BindRoomJob(worker.ID, assignment.ID, replyID))
	must(d.UpdateRoom(room.ID, nil, &members, oldPM.ID))

	must(d.AppendMessageContent(replyID, "已驗證"))
	must(d.FinalizeMessage(replyID))
	must(d.FinishRoomJob(worker.ID, replyID, "done"))
	must(d.DeliverRoomReports())
	pmID, err := d.RoomPM(room.ID)
	must(err)
	board, err := d.RoomBoard(room.ID)
	must(err)
	q, err = d.ListQueuedMessages(newPM.ID)
	must(err)
	if pmID != newPM.ID || board.Memo != "手動待辦" || len(q) != 1 {
		t.Fatalf("handoff lost room state or worker report: pm=%q board=%+v queue=%+v", pmID, board, q)
	}
	_, err = d.PromoteQueuedMessage(newPM.ID, q[0])
	must(err)
	page, err := d.RoomTimeline(room.ID, nil, 20)
	must(err)
	if len(page.Items) != 5 {
		t.Fatalf("unexpected timeline: %+v", page.Items)
	}
	report, request, oldReply, attachment := page.Items[0], page.Items[2], page.Items[3], page.Items[4]
	if report.Sender != "前端" || report.Recipient != "新 PM" || request.Sender != "舊 PM" ||
		request.Recipient != "前端" || oldReply.Recipient != "工作室" || !attachment.Mine || len(attachment.Attachments) != 1 {
		t.Fatalf("wrong routes or missing attachment: %+v", page.Items)
	}
}

func TestRoomJobRepliesStayInAssignedRoom(t *testing.T) {
	d, roomID, pm, worker := roomWithWorker(t)
	otherPM := roomSession(t, d, "另一位 PM", "claude")
	other, err := d.CreateRoom("另一間", []RoomMember{{SessionID: otherPM.ID, Role: RoomRolePM}, {SessionID: worker, Role: RoomRoleWorker}})
	requireRoomTestOK(t, err)

	_, err = d.DispatchRoomJob(roomID, pm, worker, "這間的派工")
	requireRoomTestOK(t, err)
	reply := bindQueuedRoomTestJob(t, d, worker)
	requireRoomTestOK(t, d.AppendMessageContent(reply, "部分結果"))
	requireRoomTestOK(t, d.FinalizeMessage(reply))
	retry, err := d.CreatePendingMessage(worker)
	requireRoomTestOK(t, err)
	requireRoomTestOK(t, d.BindRoomJob(worker, 0, retry))
	requireRoomTestOK(t, d.AppendMessageContent(retry, "完整結果"))
	requireRoomTestOK(t, d.AddMessage(worker, "user", "一般會話輸入", ""))
	requireRoomTestOK(t, d.AddMessage(worker, "claude", "一般會話回覆", ""))

	original, err := d.RoomTimeline(roomID, nil, 20)
	requireRoomTestOK(t, err)
	if len(original.Items) != 5 {
		t.Fatalf("assigned room timeline = %+v", original.Items)
	}
	assertOtherRoom := func() {
		t.Helper()
		page, err := d.RoomTimeline(other.ID, nil, 20)
		requireRoomTestOK(t, err)
		if len(page.Items) != 2 || page.Items[0].Content != "一般會話回覆" || page.Items[1].Content != "一般會話輸入" {
			t.Fatalf("other room timeline = %+v", page.Items)
		}
	}
	assertOtherRoom()
	requireRoomTestOK(t, d.DeleteRoom(roomID))
	assertOtherRoom()
}

func TestRoomMonitorRecoversOrphanedJobAndReportsOnce(t *testing.T) {
	d, roomID, pm, worker := roomWithWorker(t)
	first, err := d.DispatchRoomJob(roomID, pm, worker, "等授權的派工")
	requireRoomTestOK(t, err)
	_, err = d.Exec("UPDATE room_jobs SET created_at = datetime('now', '-1 hour') WHERE id = ?", first)
	requireRoomTestOK(t, err)
	queued, err := d.ListQueuedMessages(worker)
	requireRoomTestOK(t, err)
	input, err := d.PromoteQueuedMessage(worker, queued[0])
	requireRoomTestOK(t, err)
	requireRoomTestOK(t, d.DeliverRoomReports())
	reply, err := d.CreatePendingMessage(worker)
	requireRoomTestOK(t, err)
	requireRoomTestOK(t, d.BindRoomJob(worker, input.ID, reply))
	requireRoomTestOK(t, d.UpdateSessionStatus(worker, SessionStatusAwaitingConfirm))
	requireRoomTestOK(t, d.DeliverRoomReports())
	board, err := d.RoomBoard(roomID)
	requireRoomTestOK(t, err)
	if board.Jobs[0].Status != "blocked" {
		t.Fatalf("live approval should stay open: %+v", board)
	}

	// 模擬舊版重設只清會話的資料，留下未結束的派工。
	requireRoomTestOK(t, d.ClearMessages(worker))
	requireRoomTestOK(t, d.UpdateSessionStatus(worker, SessionStatusIdle))
	requireRoomTestOK(t, d.DeliverRoomReports())
	requireRoomTestOK(t, d.DeliverRoomReports())
	second, err := d.DispatchRoomJob(roomID, pm, worker, "下一個派工")
	requireRoomTestOK(t, err)
	requireRoomTestOK(t, d.FinishRoomJob(worker, reply, "done"))
	board, err = d.RoomBoard(roomID)
	requireRoomTestOK(t, err)
	if len(board.Jobs) != 2 || board.Jobs[0].ID != second || board.Jobs[0].Status != "queued" ||
		board.Jobs[1].ID != first || board.Jobs[1].Status != "interrupted" {
		t.Fatalf("unexpected recovery board: %+v", board)
	}
	queue, err := d.ListQueuedMessages(pm)
	requireRoomTestOK(t, err)
	if len(queue) != 1 || !strings.Contains(queue[0].Content, "已中斷") {
		t.Fatalf("PM report queue = %+v", queue)
	}
}

func roomWithWorker(t *testing.T) (*DB, string, string, string) {
	t.Helper()
	d := roomTestDB(t)
	pm := roomSession(t, d, "PM", "claude")
	worker := roomSession(t, d, "執行者", "claude")
	room, err := d.CreateRoom("工作室", []RoomMember{{SessionID: pm.ID, Role: RoomRolePM}, {SessionID: worker.ID, Role: RoomRoleWorker}})
	requireRoomTestOK(t, err)
	return d, room.ID, pm.ID, worker.ID
}

func bindQueuedRoomTestJob(t *testing.T, d *DB, worker string) int64 {
	t.Helper()
	queue, err := d.ListQueuedMessages(worker)
	requireRoomTestOK(t, err)
	input, err := d.PromoteQueuedMessage(worker, queue[0])
	requireRoomTestOK(t, err)
	reply, err := d.CreatePendingMessage(worker)
	requireRoomTestOK(t, err)
	requireRoomTestOK(t, d.BindRoomJob(worker, input.ID, reply))
	return reply
}

func requireRoomTestOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
