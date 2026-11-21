package db

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestQueueDeliveryPersistsAndRejectsIllegal(t *testing.T) {
	path := t.TempDir() + "/delivery.db"
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := d.GetGeneralSettings()
	if err != nil || got.QueueDelivery != QueueDeliveryEach {
		t.Fatalf("缺設定應為逐筆: %+v %v", got, err)
	}
	saved, err := d.PutGeneralSettings([]byte(`{"vscodeNoAdmin":true,"quotaGuardEnabled":true,"quotaGuardPercent":80,"queueDelivery":"batch"}`))
	if err != nil || saved.QueueDelivery != QueueDeliveryBatch || !saved.VscodeNoAdmin {
		t.Fatalf("應存一起送且保留其他欄: %+v %v", saved, err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	d, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	got, err = d.GetGeneralSettings()
	if err != nil || got.QueueDelivery != QueueDeliveryBatch || !got.VscodeNoAdmin {
		t.Fatalf("重開應保留: %+v %v", got, err)
	}
	if _, err := d.PutGeneralSettings([]byte(`{"vscodeNoAdmin":true,"quotaGuardEnabled":true,"quotaGuardPercent":80,"queueDelivery":"all"}`)); !errors.Is(err, ErrInvalidQueueDelivery) {
		t.Fatalf("非法 enum 應拒絕: %v", err)
	}
	got, _ = d.GetGeneralSettings()
	if got.QueueDelivery != QueueDeliveryBatch {
		t.Fatalf("拒絕後不可改值: %+v", got)
	}
	if _, err := d.PutGeneralSettings([]byte(`{"vscodeNoAdmin":false,"quotaGuardEnabled":true,"quotaGuardPercent":80}`)); err != nil {
		t.Fatal(err)
	}
	got, _ = d.GetGeneralSettings()
	if got.QueueDelivery != QueueDeliveryBatch || got.VscodeNoAdmin {
		t.Fatalf("缺欄不可把已存的一起送清掉: %+v", got)
	}
}

func TestIdleDeliverySnapshotsPrefixOnly(t *testing.T) {
	d, s := newFailureTestSession(t, t.TempDir()+"/idle.db")
	defer d.Close()
	if _, err := d.PutGeneralSettings([]byte(`{"quotaGuardEnabled":true,"quotaGuardPercent":80,"queueDelivery":"batch"}`)); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"訊息1", "訊息2"} {
		if _, err := d.EnqueueMessage(s.ID, text, ""); err != nil {
			t.Fatal(err)
		}
	}
	q, _ := d.ListQueuedMessages(s.ID)
	head, err := d.ApplyIdleDelivery(context.Background(), s.ID)
	if err != nil || head.BatchID == "" || head.ID != q[0].ID {
		t.Fatalf("應只快照當下可送的前綴: %+v %v", head, err)
	}
	if _, err := d.EnqueueMessage(s.ID, "晚到", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := d.EnqueueMessage(s.ID, "更晚", ""); err != nil {
		t.Fatal(err)
	}
	after, _ := d.ListQueuedMessages(s.ID)
	if len(after) != 4 || after[2].Content != "晚到" || after[2].BatchID != "" || after[3].BatchID != "" || after[0].BatchID != head.BatchID || after[1].BatchID != head.BatchID {
		t.Fatalf("晚到與快照外的第三筆不可入舊批: %+v", after)
	}
	solo, err := d.CreateSession("solo", "", "/tmp/s", "default", "codex", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.EnqueueMessage(solo.ID, "只有一筆", ""); err != nil {
		t.Fatal(err)
	}
	head, err = d.ApplyIdleDelivery(context.Background(), solo.ID)
	if err != nil || head.BatchID != "" || head.Content != "只有一筆" {
		t.Fatalf("單筆應立即送出、不等第二筆: %+v %v", head, err)
	}
}

func TestIdleDeliverySingleSourceAndOverLimit(t *testing.T) {
	d, pm := newFailureTestSession(t, t.TempDir()+"/bound.db")
	defer d.Close()
	other, err := d.CreateSession("other", "", "/tmp/o", "default", "codex", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.PutGeneralSettings([]byte(`{"quotaGuardEnabled":true,"quotaGuardPercent":80,"queueDelivery":"batch"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := d.EnqueueMessage(pm.ID, "私人", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := d.enqueueMessage(pm.ID, "別的來源", "", other.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := d.EnqueueMessage(pm.ID, "工作回報", "room-job:9"); err != nil {
		t.Fatal(err)
	}
	head, err := d.ApplyIdleDelivery(context.Background(), pm.ID)
	if err != nil || head.BatchID != "" || head.Content != "私人" {
		t.Fatalf("不同來源不可黏成一批，單筆應原樣送出: %+v %v", head, err)
	}
	if err := d.ClearQueuedMessages(pm.ID); err != nil {
		t.Fatal(err)
	}
	room, err := d.CreateRoom("這間", []RoomMember{{SessionID: pm.ID, Role: RoomRolePM}})
	if err != nil {
		t.Fatal(err)
	}
	author := RoomUserAuthorPrefix + room.ID
	if _, err := d.EnqueueMessage(pm.ID, "室1", author); err != nil {
		t.Fatal(err)
	}
	if _, err := d.EnqueueMessage(pm.ID, "室2", author); err != nil {
		t.Fatal(err)
	}
	if _, err := d.EnqueueMessage(pm.ID, "別間", RoomUserAuthorPrefix+"other"); err != nil {
		t.Fatal(err)
	}
	head, err = d.ApplyIdleDelivery(context.Background(), pm.ID)
	if err != nil || head.BatchID == "" {
		t.Fatal(err)
	}
	rows, _ := d.ListQueuedMessages(pm.ID)
	if rows[0].BatchID == "" || rows[1].BatchID != rows[0].BatchID || rows[2].BatchID != "" {
		t.Fatalf("不同 room 不可併入: %+v", rows)
	}
	if err := d.ClearQueuedMessages(pm.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.UpdateRoom(room.ID, nil, &[]RoomMember{{SessionID: other.ID, Role: RoomRolePM}}, pm.ID); err != nil {
		t.Fatal(err)
	}
	fresh, err := d.CreateSession("fresh", "", "/tmp/n", "default", "codex", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.EnqueueMessage(fresh.ID, "a", author); err != nil {
		t.Fatal(err)
	}
	if _, err := d.EnqueueMessage(fresh.ID, "b", author); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ApplyIdleDelivery(context.Background(), fresh.ID); !errors.Is(err, ErrRoomPMChanged) && !errors.Is(err, ErrQueueBatchConflict) {
		t.Fatalf("交接後不可用舊 PM 送出: %v", err)
	}
	left, _ := d.ListQueuedMessages(fresh.ID)
	if len(left) != 2 || left[0].BatchID != "" || left[1].BatchID != "" {
		t.Fatalf("交接失敗不可消耗: %+v", left)
	}
	if err := d.ClearQueuedMessages(pm.ID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 101; i++ {
		if _, err := d.EnqueueMessage(pm.ID, "x", ""); err != nil {
			t.Fatal(err)
		}
	}
	before, _ := d.ListQueuedMessages(pm.ID)
	if _, err := d.ApplyIdleDelivery(context.Background(), pm.ID); !errors.Is(err, ErrQueueBatchInvalid) {
		t.Fatalf("超過 100 應拒絕而不是截斷: %v", err)
	}
	after, _ := d.ListQueuedMessages(pm.ID)
	if len(after) != len(before) || after[0].BatchID != "" {
		t.Fatal("超限不可消耗")
	}
}

func TestIdleDeliveryEachKeepsFIFOAndCancel(t *testing.T) {
	d, s := newFailureTestSession(t, t.TempDir()+"/each.db")
	defer d.Close()
	for _, text := range []string{"先", "後"} {
		if _, err := d.EnqueueMessage(s.ID, text, ""); err != nil {
			t.Fatal(err)
		}
	}
	head, err := d.ApplyIdleDelivery(context.Background(), s.ID)
	if err != nil || head.Content != "先" || head.BatchID != "" {
		t.Fatalf("逐筆應維持 FIFO: %+v %v", head, err)
	}
	rows, _ := d.ListQueuedMessages(s.ID)
	if rows[0].BatchID != "" || rows[1].BatchID != "" {
		t.Fatal("逐筆不可寫批次")
	}
	if _, err := d.PutGeneralSettings([]byte(`{"queueDelivery":"batch"}`)); err != nil {
		t.Fatal(err)
	}
	head, err = d.ApplyIdleDelivery(context.Background(), s.ID)
	if err != nil || head.BatchID == "" {
		t.Fatal(err)
	}
	if _, err := d.PutGeneralSettings([]byte(`{"queueDelivery":"each"}`)); err != nil {
		t.Fatal(err)
	}
	again, err := d.ApplyIdleDelivery(context.Background(), s.ID)
	if err != nil || again.BatchID != head.BatchID {
		t.Fatalf("切回逐筆不可拆開既有快照: %+v %v", again, err)
	}
	if err := d.CancelQueueBatch(context.Background(), "", s.ID, head.BatchID); err != nil {
		t.Fatal(err)
	}
	left, _ := d.ListQueuedMessages(s.ID)
	if len(left) != 2 || left[0].Content != "先" || left[0].BatchID != "" || left[1].BatchID != "" {
		t.Fatalf("取消應留下原文且解除批次: %+v", left)
	}
	huge := strings.Repeat("x", (8<<20)+1)
	solo, err := d.CreateSession("size", "", "/tmp/size", "default", "codex", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.PutGeneralSettings([]byte(`{"queueDelivery":"batch"}`)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := d.EnqueueMessage(solo.ID, huge, ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.ApplyIdleDelivery(context.Background(), solo.ID); !errors.Is(err, ErrQueueBatchInvalid) {
		t.Fatalf("超過 16MiB 應拒絕: %v", err)
	}
	kept, _ := d.ListQueuedMessages(solo.ID)
	if len(kept) != 2 || kept[0].BatchID != "" || kept[1].Content != huge {
		t.Fatal("超限不可截斷或消耗")
	}
}
