package db

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestQueueEditPreservesQueueReceiptAndSession(t *testing.T) {
	d := roomTestDB(t)
	s := roomSession(t, d, "私人", "claude")
	for _, id := range []string{"second", "first"} {
		requireRoomTestOK(t, d.AddAttachment(&Attachment{ID: id, SessionID: s.ID, Name: id + ".txt", StorageName: id, MimeType: "text/plain", Size: 3}))
	}
	ids := []string{"second", "first"}
	_, err := d.AcceptInput(s.ID, "owner", "edit-request", "原文", "", ids)
	requireRoomTestOK(t, err)
	_, err = d.EnqueueMessage(s.ID, "後一則", "")
	requireRoomTestOK(t, err)
	requireRoomTestOK(t, d.SetQueuePaused(s.ID, true))
	_, err = d.Exec(`UPDATE sessions SET quota_resume_state = 'waiting', quota_resume_generation = 7, quota_resume_attempts = 2, quota_resume_at = '2099-01-01T00:00:00Z' WHERE id = ?`, s.ID)
	requireRoomTestOK(t, err)
	before, err := d.ListQueuedMessages(s.ID)
	requireRoomTestOK(t, err)
	sessionBefore, err := d.GetSession(s.ID)
	requireRoomTestOK(t, err)
	var digestBefore string
	requireRoomTestOK(t, d.QueryRow(`SELECT digest FROM input_receipts WHERE session_id = ?`, s.ID).Scan(&digestBefore))
	requireRoomTestOK(t, d.EditQueuedMessage(context.Background(), s.ID, before[0].ID, "原文", "  新文\n完整保留  "))
	after, err := d.ListQueuedMessages(s.ID)
	requireRoomTestOK(t, err)
	want := append([]QueuedMessage(nil), before...)
	want[0].Content = "  新文\n完整保留  "
	if !reflect.DeepEqual(after, want) {
		t.Fatalf("除了 content 不可改 FIFO／附件／來源／時間：got=%+v want=%+v", after, want)
	}
	sessionAfter, err := d.GetSession(s.ID)
	requireRoomTestOK(t, err)
	if !reflect.DeepEqual(sessionAfter, sessionBefore) {
		t.Fatal("編輯不可改 paused／quota 自動復原或 session 狀態")
	}
	var digestAfter, requestID string
	requireRoomTestOK(t, d.QueryRow(`SELECT digest FROM input_receipts WHERE session_id = ?`, s.ID).Scan(&digestAfter))
	requireRoomTestOK(t, d.QueryRow(`SELECT input_request_id FROM queued_messages WHERE id = ?`, before[0].ID).Scan(&requestID))
	if digestAfter != digestBefore || requestID != "edit-request" {
		t.Fatal("編輯不可改 receipt 或 request id")
	}
	duplicate, err := d.AcceptInput(s.ID, "owner", "edit-request", "原文", "", ids)
	requireRoomTestOK(t, err)
	if !duplicate {
		t.Fatal("原 payload 延遲重送應 duplicate")
	}
	retried, err := d.ListQueuedMessages(s.ID)
	requireRoomTestOK(t, err)
	if !reflect.DeepEqual(retried, after) {
		t.Fatal("原重送覆寫新文或新增佇列")
	}
	_, err = d.AcceptInput(s.ID, "owner", "edit-request", after[0].Content, "", ids)
	if !errors.Is(err, ErrInputConflict) {
		t.Fatalf("原 request id 配新 payload 應衝突：%v", err)
	}
	// 同內容 no-op 不改其他欄位；另一編輯者用舊 previous 必須衝突。
	requireRoomTestOK(t, d.EditQueuedMessage(context.Background(), s.ID, after[0].ID, after[0].Content, after[0].Content))
	if err := d.EditQueuedMessage(context.Background(), s.ID, after[0].ID, "原文", "過期編輯"); !errors.Is(err, ErrQueueEditConflict) {
		t.Fatalf("CAS：%v", err)
	}
	if _, err := d.PromoteQueuedMessage(s.ID, before[0]); err == nil {
		t.Fatal("舊 prompt snapshot 不可提升並遺失編輯")
	}
	m, err := d.PromoteQueuedMessage(s.ID, after[0])
	requireRoomTestOK(t, err)
	if m.Content != after[0].Content || len(m.Attachments) != 2 {
		t.Fatal("提升遺失新文／附件")
	}
	if err := d.EditQueuedMessage(context.Background(), s.ID, after[0].ID, after[0].Content, "太晚"); !errors.Is(err, ErrQueueEditConflict) {
		t.Fatalf("已提升必須拒絕：%v", err)
	}
	duplicate, err = d.AcceptInput(s.ID, "owner", "edit-request", "原文", "", ids)
	requireRoomTestOK(t, err)
	if !duplicate {
		t.Fatal("提升後原重送應仍 duplicate")
	}
	remaining, err := d.ListQueuedMessages(s.ID)
	requireRoomTestOK(t, err)
	if len(remaining) != 1 || remaining[0].ID != before[1].ID {
		t.Fatal("延遲重送不應重新排入")
	}
}

func TestQueueEditScopes(t *testing.T) {
	d, roomID, pm, worker := roomWithWorker(t)
	other := roomSession(t, d, "別人", "claude")
	otherRoom, err := d.CreateRoom("另一間", []RoomMember{{SessionID: other.ID, Role: RoomRolePM}})
	requireRoomTestOK(t, err)
	ctx := context.Background()
	for _, tc := range []struct{ author, source string }{
		{RoomUserAuthorPrefix + roomID, ""}, {"room-job:1", ""}, {"room-report:" + roomID, ""},
		{"room-handoff:" + roomID, ""}, {"訪客", ""}, {"", worker}, {RoomUserAuthorPrefix + roomID, worker},
	} {
		t.Run(tc.author+tc.source, func(t *testing.T) {
			q, err := d.enqueueMessage(pm, "原", tc.author, tc.source, nil)
			requireRoomTestOK(t, err)
			if err := d.EditQueuedMessage(ctx, pm, q.ID, "原", "新"); !errors.Is(err, ErrQueueEditForbidden) {
				t.Fatalf("私人不可跨來源，author=%q source=%q：%v", tc.author, tc.source, err)
			}
			if err := d.EditQueuedMessage(ctx, other.ID, q.ID, "原", "新"); !errors.Is(err, ErrQueueEditConflict) {
				t.Fatalf("跨 session：%v", err)
			}
			if tc.author != RoomUserAuthorPrefix+roomID || tc.source != "" {
				if err := d.EditRoomQueuedMessage(ctx, roomID, pm, q.ID, "原", "新"); !errors.Is(err, ErrQueueEditForbidden) {
					t.Fatalf("room 不可編 job/report/agent：%v", err)
				}
			}
			requireRoomTestOK(t, d.DeleteQueuedMessage(pm, q.ID))
		})
	}
	q, err := d.EnqueueMessage(pm, "私人", "")
	requireRoomTestOK(t, err)
	if err := d.EditRoomQueuedMessage(ctx, roomID, pm, q.ID, "私人", "新"); !errors.Is(err, ErrQueueEditForbidden) {
		t.Fatalf("room 不可編私人：%v", err)
	}
	requireRoomTestOK(t, d.AcceptRoomInput(roomID, pm, "room-input", "群聊", nil))
	queue, err := d.ListQueuedMessages(pm)
	requireRoomTestOK(t, err)
	roomQ := queue[len(queue)-1]
	if err := d.EditRoomQueuedMessage(ctx, otherRoom.ID, other.ID, roomQ.ID, "群聊", "跨間"); !errors.Is(err, ErrQueueEditConflict) {
		t.Fatalf("跨 room：%v", err)
	}
	if err := d.EditRoomQueuedMessage(ctx, roomID, other.ID, roomQ.ID, "群聊", "過期 PM"); !errors.Is(err, ErrRoomPMChanged) {
		t.Fatalf("錯誤 PM：%v", err)
	}
	requireRoomTestOK(t, d.DeleteQueuedMessage(pm, q.ID))
	if err := d.EditQueuedMessage(ctx, pm, q.ID, "私人", "已刪"); !errors.Is(err, ErrQueueEditConflict) {
		t.Fatalf("已刪除：%v", err)
	}
	if err := d.DeleteRoom(otherRoom.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.EditRoomQueuedMessage(ctx, otherRoom.ID, other.ID, roomQ.ID, "群聊", "房間不存在"); !errors.Is(err, ErrRoomPM) {
		t.Fatalf("已刪 room：%v", err)
	}
}

func TestQueueEditRoomHandoffAndReceipts(t *testing.T) {
	d, roomID, oldPM, nextPM := roomWithWorker(t)
	ctx := context.Background()
	ids := []string{"room-file"}
	requireRoomTestOK(t, d.AddAttachment(&Attachment{ID: ids[0], SessionID: oldPM, Name: "保留.txt", StorageName: "stored", MimeType: "text/plain", Size: 8}))
	requireRoomTestOK(t, d.AcceptRoomInput(roomID, oldPM, "room-edit-id", "原群聊", ids))
	before, err := d.ListQueuedMessages(oldPM)
	requireRoomTestOK(t, err)
	q := before[0]
	requireRoomTestOK(t, d.SetQueuePaused(oldPM, true))
	var digest string
	requireRoomTestOK(t, d.QueryRow(`SELECT digest FROM input_receipts WHERE session_id = ?`, oldPM).Scan(&digest))
	requireRoomTestOK(t, d.EditRoomQueuedMessage(ctx, roomID, oldPM, q.ID, "原群聊", "已編輯"))
	after, err := d.ListQueuedMessages(oldPM)
	requireRoomTestOK(t, err)
	q.Content = "已編輯"
	if !reflect.DeepEqual(after, []QueuedMessage{q}) {
		t.Fatal("room 編輯不可改其他 queue 欄位")
	}
	paused, err := d.QueuePaused(oldPM)
	requireRoomTestOK(t, err)
	if !paused {
		t.Fatal("room 編輯解除暫停")
	}
	members := []RoomMember{{SessionID: oldPM, Role: RoomRoleWorker}, {SessionID: nextPM, Role: RoomRolePM}}
	requireRoomTestOK(t, d.UpdateRoom(roomID, nil, &members, oldPM))
	if err := d.EditRoomQueuedMessage(ctx, roomID, oldPM, q.ID, "已編輯", "舊 PM 寫入"); !errors.Is(err, ErrRoomPMChanged) {
		t.Fatalf("交接後舊 PM：%v", err)
	}
	requireRoomTestOK(t, d.SetQueuePaused(nextPM, true))
	requireRoomTestOK(t, d.EditRoomQueuedMessage(ctx, roomID, nextPM, q.ID, "已編輯", "接任後編輯"))
	// 移交的原附件透過 attachment_access 仍有效，空白內容也可保留原附件。
	requireRoomTestOK(t, d.EditRoomQueuedMessage(ctx, roomID, nextPM, q.ID, "接任後編輯", ""))
	var copied string
	requireRoomTestOK(t, d.QueryRow(`SELECT digest FROM input_receipts WHERE session_id = ? AND request_id = 'room-edit-id'`, nextPM).Scan(&copied))
	if copied != digest {
		t.Fatal("交接 receipt digest 被編輯改寫")
	}
	requireRoomTestOK(t, d.AcceptRoomInput(roomID, nextPM, "room-edit-id", "原群聊", ids))
	if err := d.AcceptRoomInput(roomID, nextPM, "room-edit-id", "", ids); !errors.Is(err, ErrInputConflict) {
		t.Fatalf("交接後新 payload 不可重用 request：%v", err)
	}
	next, err := d.ListQueuedMessages(nextPM)
	requireRoomTestOK(t, err)
	if len(next) != 1 || next[0].ID != q.ID || next[0].Content != "" || len(next[0].Attachments) != 1 || next[0].Attachments[0].Name != "保留.txt" {
		t.Fatalf("交接／重送後 queue=%+v", next)
	}
	_, err = d.PromoteQueuedMessage(nextPM, next[0])
	requireRoomTestOK(t, err)
	if err := d.EditRoomQueuedMessage(ctx, roomID, nextPM, q.ID, "", "已提升"); !errors.Is(err, ErrQueueEditConflict) {
		t.Fatalf("room 已提升：%v", err)
	}
	requireRoomTestOK(t, d.AcceptRoomInput(roomID, nextPM, "room-edit-id", "原群聊", ids))
	n, err := d.CountQueuedMessages(nextPM)
	requireRoomTestOK(t, err)
	if n != 0 {
		t.Fatal("交接且提升後原重送不應新增輸入")
	}
}

func TestQueueEditValidationAndRollback(t *testing.T) {
	d := roomTestDB(t)
	s := roomSession(t, d, "驗證", "claude")
	q, err := d.EnqueueMessage(s.ID, "原", "")
	requireRoomTestOK(t, err)
	ctx := context.Background()
	for _, content := range []string{"", " \n\t", "含\x00字", string([]byte{0xff}), strings.Repeat("x", (16<<20)+1)} {
		if err := d.EditQueuedMessage(ctx, s.ID, q.ID, "原", content); !errors.Is(err, ErrQueueEditInvalid) {
			t.Fatalf("無效文字 len=%d：%v", len(content), err)
		}
	}
	long := strings.Repeat("合法長文\n", 50000)
	requireRoomTestOK(t, d.EditQueuedMessage(ctx, s.ID, q.ID, "原", long))
	// 更新錯誤必須回滾，不能假成功或遺失原佇列。
	_, err = d.Exec(`CREATE TRIGGER reject_queue_edit BEFORE UPDATE OF content ON queued_messages BEGIN SELECT RAISE(ABORT, 'test edit unavailable'); END`)
	requireRoomTestOK(t, err)
	if err := d.EditQueuedMessage(ctx, s.ID, q.ID, long, "失敗"); err == nil {
		t.Fatal("更新失敗卻回成功")
	}
	_, err = d.Exec(`DROP TRIGGER reject_queue_edit`)
	requireRoomTestOK(t, err)
	got, err := d.ListQueuedMessages(s.ID)
	requireRoomTestOK(t, err)
	if got[0].Content != long {
		t.Fatal("失敗沒有回滾")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := d.EditQueuedMessage(cancelled, s.ID, q.ID, long, "取消"); !errors.Is(err, context.Canceled) {
		t.Fatalf("context=%v", err)
	}
	for _, tc := range []struct{ name, raw string }{
		{"遺失", `["missing"]`}, {"外來", `["foreign"]`}, {"重複", `["local","local"]`}, {"損壞", `not-json`}, {"null", `null`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			owner := s.ID
			if tc.name == "外來" {
				owner = roomSession(t, d, "other", "claude").ID
			}
			if tc.name == "外來" || tc.name == "重複" {
				id := "local"
				if tc.name == "外來" {
					id = "foreign"
				}
				requireRoomTestOK(t, d.AddAttachment(&Attachment{ID: id, SessionID: owner, Name: id, StorageName: id, MimeType: "text/plain"}))
			}
			_, err := d.Exec(`UPDATE queued_messages SET attachment_ids=? WHERE id=?`, tc.raw, q.ID)
			requireRoomTestOK(t, err)
			for _, content := range []string{"", "即使有文字也必須核對附件"} {
				if err := d.EditQueuedMessage(ctx, s.ID, q.ID, long, content); !errors.Is(err, ErrInvalidAttachment) {
					t.Fatalf("無效附件不應允許編輯：%v", err)
				}
			}
		})
	}
}

func TestQueueEditConcurrentEditorsAndPromote(t *testing.T) {
	d := roomTestDB(t)
	s := roomSession(t, d, "競爭", "claude")
	q, err := d.EnqueueMessage(s.ID, "原", "")
	requireRoomTestOK(t, err)
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, text := range []string{"甲", "乙"} {
		wg.Add(1)
		go func(text string) {
			defer wg.Done()
			<-start
			results <- d.EditQueuedMessage(context.Background(), s.ID, q.ID, "原", text)
		}(text)
	}
	close(start)
	wg.Wait()
	close(results)
	ok, conflicts := 0, 0
	for err := range results {
		if err == nil {
			ok++
		} else if errors.Is(err, ErrQueueEditConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if ok != 1 || conflicts != 1 {
		t.Fatalf("編輯勝者=%d conflict=%d", ok, conflicts)
	}
	// 不靠 sleep：兩種交易順序都可重現；舊 snapshot 不可提升，新 snapshot 才可。
	if _, err := d.PromoteQueuedMessage(s.ID, *q); err == nil {
		t.Fatal("過期提升不應成功")
	}
	list, err := d.ListQueuedMessages(s.ID)
	requireRoomTestOK(t, err)
	_, err = d.PromoteQueuedMessage(s.ID, list[0])
	requireRoomTestOK(t, err)
	if err := d.EditQueuedMessage(context.Background(), s.ID, q.ID, list[0].Content, "太晚"); !errors.Is(err, ErrQueueEditConflict) {
		t.Fatalf("提升後編輯：%v", err)
	}
	// 同時啟動 editor 與持有舊 snapshot 的 Promote，只允許其中一方提交。
	q, err = d.EnqueueMessage(s.ID, "同時", "")
	requireRoomTestOK(t, err)
	gate := make(chan struct{})
	editResult, promoteResult := make(chan error, 1), make(chan error, 1)
	go func() {
		<-gate
		editResult <- d.EditQueuedMessage(context.Background(), s.ID, q.ID, "同時", "勝出新文")
	}()
	go func() {
		<-gate
		_, err := d.PromoteQueuedMessage(s.ID, *q)
		promoteResult <- err
	}()
	close(gate)
	editErr, promoteErr := <-editResult, <-promoteResult
	if editErr == nil {
		if promoteErr == nil {
			t.Fatal("editor 與過期 Promote 不可同時成功")
		}
		remaining, err := d.ListQueuedMessages(s.ID)
		requireRoomTestOK(t, err)
		if len(remaining) != 1 || remaining[0].Content != "勝出新文" {
			t.Fatal("勝出的編輯未保留在 queue")
		}
	} else {
		if !errors.Is(editErr, ErrQueueEditConflict) || promoteErr != nil {
			t.Fatalf("不合法的競爭結果：edit=%v promote=%v", editErr, promoteErr)
		}
		remaining, err := d.ListQueuedMessages(s.ID)
		requireRoomTestOK(t, err)
		messages, err := d.ListMessages(s.ID)
		requireRoomTestOK(t, err)
		if len(remaining) != 0 || messages[len(messages)-1].Content != "同時" {
			t.Fatal("勝出的提升遺失原輸入")
		}
	}
}
