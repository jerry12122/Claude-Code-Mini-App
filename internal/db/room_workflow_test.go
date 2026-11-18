package db

import (
	"errors"
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
