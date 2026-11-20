package db

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func guardRoom(t *testing.T) (*DB, string, string, string) {
	t.Helper()
	d := roomTestDB(t)
	pm := roomSession(t, d, "PM", "claude")
	worker := roomSession(t, d, "worker", "claude")
	r, err := d.CreateRoom("room", []RoomMember{{SessionID: pm.ID, Role: RoomRolePM}, {SessionID: worker.ID, Role: RoomRoleWorker}})
	requireRoomTestOK(t, err)
	return d, r.ID, pm.ID, worker.ID
}

func TestRoomAgentMessageRechecksScopeInEnqueueTransaction(t *testing.T) {
	for _, change := range []string{"source_deleted", "membership_removed", "pm_handoff"} {
		t.Run(change, func(t *testing.T) {
			d, r, pm, worker := guardRoom(t)
			job, err := d.DispatchRoomJob(r, pm, worker, "work")
			requireRoomTestOK(t, err)
			reply := bindQueuedRoomTestJob(t, d, worker)
			requireRoomTestOK(t, d.FinishRoomJob(worker, reply, "done"))
			if _, err := d.EnqueueAgentMessageCancelQuota(pm, "completion", "", worker, &AgentMessage{Intent: "completion", JobID: job}); err == nil {
				t.Fatal("完工可借一般 agent queue 重複入列")
			}
			message := AgentMessage{Intent: "question", JobID: job}
			requireRoomTestOK(t, d.CheckRoomAgentMessage(pm, worker, message))
			tx, err := d.Begin()
			requireRoomTestOK(t, err)
			defer tx.Rollback()
			switch change {
			case "source_deleted":
				_, err = tx.Exec("DELETE FROM sessions WHERE id=?", worker)
			case "membership_removed":
				_, err = tx.Exec("DELETE FROM room_members WHERE room_id=? AND session_id=?", r, worker)
			case "pm_handoff":
				_, err = tx.Exec("UPDATE room_members SET role='worker' WHERE room_id=? AND session_id=?", r, pm)
				requireRoomTestOK(t, err)
				_, err = tx.Exec("UPDATE room_members SET role='pm' WHERE room_id=? AND session_id=?", r, worker)
			}
			requireRoomTestOK(t, err)
			done := make(chan error, 1)
			go func() { _, err := d.EnqueueAgentMessageCancelQuota(pm, "question", "", worker, &message); done <- err }()
			deadline := time.Now().Add(5 * time.Second)
			for d.Stats().InUse < 2 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if d.Stats().InUse < 2 {
				t.Fatal("未進入等待寫鎖的競爭路徑")
			}
			requireRoomTestOK(t, tx.Commit())
			if err := <-done; err == nil {
				t.Fatal("來源／PM 變更後仍按預檢舊範圍入列")
			}
			q, err := d.ListQueuedMessages(pm)
			requireRoomTestOK(t, err)
			if len(q) != 0 {
				t.Fatal("拒絕後仍有 runnable")
			}
		})
	}
}

func TestWorkflowMemoRequiresExpectedVersion(t *testing.T) {
	d := roomTestDB(t)
	pm := roomSession(t, d, "PM", "claude")
	room, err := d.CreateRoom("room", []RoomMember{{SessionID: pm.ID, Role: RoomRolePM}})
	requireRoomTestOK(t, err)
	if err := d.SaveRoomMemo(room.ID, pm.ID, "覆寫", ""); err == nil {
		t.Fatal("缺少 expected_version 仍可覆寫")
	}
}

func TestWorkflowVersionMemoDispatchAndHandoff(t *testing.T) {
	d, r, pm, worker := guardRoom(t)
	board, err := d.RoomBoard(r)
	requireRoomTestOK(t, err)
	if board.Version == "" || board.Room.ID != r {
		t.Fatal("缺少權威版／同快照成員")
	}
	requireRoomTestOK(t, d.SaveRoomMemo(r, pm, "first", board.Version))
	if err := d.SaveRoomMemo(r, pm, "lost", board.Version); err == nil || !strings.Contains(err.Error(), "過期") {
		t.Fatalf("舊備忘覆寫：%v", err)
	}
	for _, expected := range []string{"", board.Version} {
		if _, err := d.DispatchRoomJob(r, pm, worker, "bad", expected); err == nil {
			t.Fatal("缺／舊版仍派工")
		}
	}
	current, err := d.RoomBoard(r)
	requireRoomTestOK(t, err)
	id, err := d.DispatchRoomJob(r, pm, worker, "current", current.Version)
	requireRoomTestOK(t, err)
	if id == 0 {
		t.Fatal("新版沒派工")
	}
	current, err = d.RoomBoard(r)
	requireRoomTestOK(t, err)
	members := []RoomMember{{SessionID: worker, Role: RoomRolePM}, {SessionID: pm, Role: RoomRoleWorker}}
	// 先終止派工，讓合法交接可進行；scope 後舊 PM 仍須由交易拒絕。
	_, err = d.Exec("UPDATE room_jobs SET outcome='interrupted' WHERE id=?", id)
	requireRoomTestOK(t, err)
	requireRoomTestOK(t, d.ClearQueuedMessages(worker))
	requireRoomTestOK(t, d.UpdateRoom(r, nil, &members, pm))
	if err := d.SaveRoomMemo(r, pm, "old PM", current.Version); err != ErrRoomPMChanged {
		t.Fatalf("舊 PM 未重驗：%v", err)
	}
}

func TestWorkflowVersionConcurrentMemoCAS(t *testing.T) {
	d, r, pm, _ := guardRoom(t)
	b, err := d.RoomBoard(r)
	requireRoomTestOK(t, err)
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, text := range []string{"a", "b"} {
		go func(text string) { <-start; results <- d.SaveRoomMemo(r, pm, text, b.Version) }(text)
	}
	close(start)
	successes := 0
	for range 2 {
		if <-results == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("同版競寫成功 %d 次", successes)
	}
}

func TestWorkflowOperationApprovals(t *testing.T) {
	for _, action := range []string{"reset_session", "delete_session", "room_delete"} {
		t.Run(action, func(t *testing.T) {
			d, r, pm, worker := guardRoom(t)
			target := worker
			if action == "room_delete" {
				target = r
			}
			apply := func(id, act, tgt string) error {
				tx, err := d.Begin()
				if err != nil {
					return err
				}
				defer tx.Rollback()
				if err := consumeOperation(tx, id, act, r, pm, tgt); err != nil {
					return err
				}
				return tx.Commit()
			}
			for _, id := range []string{"", "forged"} {
				if err := apply(id, action, target); err == nil {
					t.Fatal("缺少／偽造批准可用")
				}
			}
			id, _, err := d.RequestOperationApproval(action, r, pm, target)
			requireRoomTestOK(t, err)
			if err := apply(id, action, target); err == nil {
				t.Fatal("未批准可用")
			}
			requireRoomTestOK(t, d.ApproveOperation(id))
			otherAction := "reset_session"
			otherTarget := worker
			if action == otherAction {
				otherAction = "delete_session"
			}
			if err := apply(id, otherAction, otherTarget); err == nil {
				t.Fatal("不同 action 共用批准")
			}
			if err := apply(id, action, "changed-target"); err == nil {
				t.Fatal("可改目標")
			}
			requireRoomTestOK(t, apply(id, action, target))
			if err := apply(id, action, target); err == nil {
				t.Fatal("批准可重播")
			}
			stale, _, err := d.RequestOperationApproval(action, r, pm, target)
			requireRoomTestOK(t, err)
			requireRoomTestOK(t, d.ApproveOperation(stale))
			// 狀態往返也必須讓舊批准失效。
			requireRoomTestOK(t, d.UpdateSessionStatus(worker, SessionStatusRunning))
			requireRoomTestOK(t, d.UpdateSessionStatus(worker, SessionStatusIdle))
			if err := apply(stale, action, target); err == nil {
				t.Fatal("狀態改回仍能用舊批准")
			}
			expired, _, err := d.RequestOperationApproval(action, r, pm, target)
			requireRoomTestOK(t, err)
			requireRoomTestOK(t, d.ApproveOperation(expired))
			_, err = d.Exec("UPDATE operation_approvals SET expires_at=datetime('now','-1 second') WHERE id=?", expired)
			requireRoomTestOK(t, err)
			if err := apply(expired, action, target); err == nil {
				t.Fatal("批准過期可用")
			}
		})
	}
}

func TestWorkflowApprovedCleanupRechecksAndRollsBack(t *testing.T) {
	for _, action := range []string{"reset_session", "delete_session", "room_delete"} {
		for _, state := range []string{"idle", "queued", "busy", "shared", "handoff", "sql_failure"} {
			t.Run(action+"/"+state, func(t *testing.T) {
				d, r, pm, worker := guardRoom(t)
				requireRoomTestOK(t, d.AddMessage(worker, "user", "keep", ""))
				target := worker
				if action == "room_delete" {
					target = r
				}
				if state == "queued" {
					_, err := d.EnqueueMessage(worker, "keep queued", "")
					requireRoomTestOK(t, err)
				}
				if state == "busy" {
					requireRoomTestOK(t, d.UpdateSessionStatus(worker, SessionStatusRunning))
				}
				if state == "shared" {
					other := roomSession(t, d, "other PM", "claude")
					_, err := d.CreateRoom("shared", []RoomMember{{SessionID: other.ID, Role: RoomRolePM}, {SessionID: worker, Role: RoomRoleWorker}})
					requireRoomTestOK(t, err)
				}
				id, _, err := d.RequestOperationApproval(action, r, pm, target)
				if state == "shared" && action != "room_delete" {
					if err == nil {
						t.Fatal("共用會話可要求清理")
					}
					return
				}
				requireRoomTestOK(t, err)
				requireRoomTestOK(t, d.ApproveOperation(id))
				if state == "handoff" {
					members := []RoomMember{{SessionID: worker, Role: RoomRolePM}, {SessionID: pm, Role: RoomRoleWorker}}
					requireRoomTestOK(t, d.UpdateRoom(r, nil, &members, pm))
				}
				if state == "sql_failure" {
					_, err := d.Exec(`CREATE TRIGGER fail_cleanup BEFORE DELETE ON messages BEGIN SELECT RAISE(ABORT,'test rollback'); END`)
					if action == "room_delete" {
						_, err = d.Exec(`CREATE TRIGGER fail_room_cleanup BEFORE DELETE ON room_memos BEGIN SELECT RAISE(ABORT,'test rollback'); END`)
						requireRoomTestOK(t, d.SaveRoomMemo(r, pm, "memo"))
						id, _, err = d.RequestOperationApproval(action, r, pm, target)
						requireRoomTestOK(t, err)
						requireRoomTestOK(t, d.ApproveOperation(id))
					}
					requireRoomTestOK(t, err)
				}
				before, e := d.ListMessages(worker)
				requireRoomTestOK(t, e)
				switch action {
				case "room_delete":
					err = d.DeleteRoomAsPM(r, pm, id)
				case "reset_session":
					err = d.ResetRoomMemberSession(worker, r, pm, id)
				default:
					err = d.DeleteSessionApproved(worker, r, pm, id)
				}
				success := state == "idle" || (state == "shared" && action == "room_delete")
				if success {
					requireRoomTestOK(t, err)
					return
				}
				if err == nil {
					t.Fatal("危險狀態仍清理")
				}
				messages, e := d.ListMessages(worker)
				requireRoomTestOK(t, e)
				if len(messages) != len(before) || messages[0].Content != "keep" {
					t.Fatal("拒絕改資料")
				}
				var consumed int
				requireRoomTestOK(t, d.QueryRow("SELECT consumed FROM operation_approvals WHERE id=?", id).Scan(&consumed))
				if consumed != 0 {
					t.Fatal("失敗沒有回滾批准消耗")
				}
			})
		}
	}
}

func TestWorkflowParallelCompletionRetryKeepsJobsDistinct(t *testing.T) {
	d, r, pm, worker := guardRoom(t)
	other, err := d.CreateRoomWorker(r, pm, "other", "claude")
	requireRoomTestOK(t, err)
	replies := []int64{}
	for _, w := range []string{worker, other} {
		job, err := d.DispatchRoomJob(r, pm, w, "job")
		requireRoomTestOK(t, err)
		queue, err := d.ListQueuedMessages(w)
		requireRoomTestOK(t, err)
		input, err := d.PromoteQueuedMessage(w, queue[0])
		requireRoomTestOK(t, err)
		reply, err := d.CreatePendingMessage(w)
		requireRoomTestOK(t, err)
		replies = append(replies, reply)
		requireRoomTestOK(t, d.BindRoomJob(w, input.ID, reply))
		requireRoomTestOK(t, d.RoomCompletionNotification(job, w, pm))
	}
	var wg sync.WaitGroup
	errs := make(chan error, 24)
	for i := range 12 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w := worker
			if i%2 == 1 {
				w = other
			}
			errs <- d.FinishRoomJob(w, replies[i%2], "done")
			errs <- d.DeliverRoomReports()
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		requireRoomTestOK(t, err)
	}
	q, err := d.ListQueuedMessages(pm)
	requireRoomTestOK(t, err)
	if len(q) != 2 {
		t.Fatalf("每 job 應一筆，收到 %d", len(q))
	}
	// 同 worker 的新版交付另派 job，舊 reply 的 retry 不能吞掉它。
	_, err = d.DispatchRoomJob(r, pm, worker, "new delivery")
	requireRoomTestOK(t, err)
	q, err = d.ListQueuedMessages(worker)
	requireRoomTestOK(t, err)
	input, err := d.PromoteQueuedMessage(worker, q[0])
	requireRoomTestOK(t, err)
	reply, err := d.CreatePendingMessage(worker)
	requireRoomTestOK(t, err)
	requireRoomTestOK(t, d.BindRoomJob(worker, input.ID, reply))
	requireRoomTestOK(t, d.FinishRoomJob(worker, replies[0], "failed"))
	requireRoomTestOK(t, d.FinishRoomJob(worker, reply, "done"))
	requireRoomTestOK(t, d.DeliverRoomReports())
	q, err = d.ListQueuedMessages(pm)
	requireRoomTestOK(t, err)
	if len(q) != 3 {
		t.Fatal("新版交付被吞")
	}
}
