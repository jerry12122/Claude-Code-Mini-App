package server

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

func TestRoomQueueBatchHTTPAndOptimisticIDs(t *testing.T) {
	e := newRoomEditHTTP(t)
	for _, id := range []string{"first", "second"} {
		if err := e.d.AcceptRoomInput(e.room, e.pm, id, "  "+id+"\n", nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.d.SetQueuePaused(e.pm, true); err != nil {
		t.Fatal(err)
	}
	q, err := e.d.ListQueuedMessages(e.pm)
	if err != nil {
		t.Fatal(err)
	}
	items := []map[string]any{{"id": q[0].ID, "previous": q[0].Content}, {"id": q[1].ID, "previous": q[1].Content}}
	body := map[string]any{"expected_pm_id": e.pm, "request_id": "room-batch", "items": items}
	status, raw := e.request("POST", "/rooms/"+e.room+"/messages/batch", body, e.token)
	var accepted struct {
		BatchID string `json:"batch_id"`
	}
	json.Unmarshal(raw, &accepted)
	if status != 202 || accepted.BatchID == "" {
		t.Fatalf("Room HTTP 未接受批次：%d %s", status, raw)
	}
	queuedStatus, queuedRaw := e.request("GET", "/rooms/"+e.room+"/timeline", nil, e.token)
	var queued struct {
		Messages []struct {
			BatchID string `json:"batch_id"`
			Queued  bool   `json:"queued"`
		} `json:"messages"`
	}
	json.Unmarshal(queuedRaw, &queued)
	if queuedStatus != 200 || len(queued.Messages) != 2 || queued.Messages[0].BatchID != accepted.BatchID || !queued.Messages[0].Queued || queued.Messages[1].BatchID != accepted.BatchID {
		t.Fatalf("Room 重連無法辨識已安排的批次：%d %s", queuedStatus, queuedRaw)
	}
	for _, id := range []string{"first", "second"} {
		if err := e.d.AcceptRoomInput(e.room, e.pm, id, "  "+id+"\n", nil); err != nil {
			t.Fatal(err)
		}
	}
	q, err = e.d.ListQueuedMessages(e.pm)
	if err != nil || len(q) != 2 {
		t.Fatalf("receipt retry=%+v %v", q, err)
	}
	if _, err := e.d.PromoteQueuedBatch(context.Background(), e.pm, q[0]); err != nil {
		t.Fatal(err)
	}
	status, raw = e.request("GET", "/rooms/"+e.room+"/timeline", nil, e.token)
	var timeline struct {
		Items []struct {
			InputRequestID string `json:"input_request_id"`
			Queued         bool   `json:"queued"`
			Content        string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &timeline); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, m := range timeline.Items {
		if m.InputRequestID != "" {
			if m.Queued {
				t.Fatal("原樂觀輸入未收斂")
			}
			seen[m.InputRequestID] = true
		}
	}
	if status != 200 || !seen["first"] || !seen["second"] {
		t.Fatalf("timeline 原 request ids 遺失：%d %s", status, raw)
	}
	status, raw = e.request("POST", "/rooms/"+e.room+"/messages/batch", body, e.token)
	var replay struct {
		BatchID string `json:"batch_id"`
	}
	json.Unmarshal(raw, &replay)
	if status != 202 || replay.BatchID != accepted.BatchID {
		t.Fatalf("HTTP 重試不冪等：%d %s", status, raw)
	}
	other, err := e.d.CreateSession("其他", "", "", "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.d.EnqueueMessage(other.ID, "private", "")
	if err != nil {
		t.Fatal(err)
	}
	private, _ := e.d.ListQueuedMessages(other.ID)
	body["request_id"] = "cross"
	body["items"] = []db.QueueBatchItem{{ID: private[0].ID, Previous: &private[0].Content}, {ID: private[0].ID + 1, Previous: &private[0].Content}}
	status, _ = e.request("POST", "/rooms/"+e.room+"/messages/batch", body, e.token)
	if status != 409 {
		t.Fatalf("cross scope=%d", status)
	}
	status, _ = e.request("POST", "/rooms/"+e.room+"/messages/batch", body, "")
	if status != 401 {
		t.Fatalf("unauthorized=%d", status)
	}
	// 第二個批次只安排在 paused queue；取消不改文字、順序或 paused。
	for _, id := range []string{"third", "fourth"} {
		if err := e.d.AcceptRoomInput(e.room, e.pm, id, id, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.d.SetQueuePaused(e.pm, true); err != nil {
		t.Fatal(err)
	}
	q, err = e.d.ListQueuedMessages(e.pm)
	if err != nil {
		t.Fatal(err)
	}
	body = map[string]any{"expected_pm_id": e.pm, "request_id": "cancel-op", "items": []map[string]any{{"id": q[0].ID, "previous": q[0].Content}, {"id": q[1].ID, "previous": q[1].Content}}}
	status, raw = e.request("POST", "/rooms/"+e.room+"/messages/batch", body, e.token)
	json.Unmarshal(raw, &accepted)
	if status != 202 {
		t.Fatalf("arrange cancel=%d %s", status, raw)
	}
	status, _ = e.request("DELETE", "/rooms/"+e.room+"/messages/batch/"+accepted.BatchID, map[string]string{"expected_pm_id": e.pm}, e.token)
	if status != 204 {
		t.Fatalf("cancel=%d", status)
	}
	left, err := e.d.ListQueuedMessages(e.pm)
	if err != nil || len(left) != 2 || left[0].BatchID != "" || left[0].Content != q[0].Content {
		t.Fatalf("cancel changed queue: %+v %v", left, err)
	}
}

func TestRoomQueueReadHTTPScopedFIFO(t *testing.T) {
	e := newRoomEditHTTP(t)
	file := &db.Attachment{ID: "room-queue-file", SessionID: e.pm, Name: "keep.txt", StorageName: "stored", MimeType: "text/plain", Size: 4}
	if err := e.d.AddAttachment(file); err != nil {
		t.Fatal(err)
	}
	if err := e.d.AcceptRoomInput(e.room, e.pm, "queue-first", "  first\n", []string{file.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.d.EnqueueMessage(e.pm, "PRIVATE DO NOT EXPOSE", ""); err != nil {
		t.Fatal(err)
	}
	if err := e.d.AcceptRoomInput(e.room, e.pm, "queue-second", "second  ", nil); err != nil {
		t.Fatal(err)
	}
	for _, author := range []string{db.RoomUserAuthorPrefix + "other-room", "room-job:1", "room-report:" + e.room} {
		if _, err := e.d.EnqueueMessage(e.pm, "INTERNAL DO NOT EXPOSE", author); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.d.EnqueueMessageFromCancelQuota(e.pm, "SOURCE DO NOT EXPOSE", db.RoomUserAuthorPrefix+e.room, e.worker); err != nil {
		t.Fatal(err)
	}
	if err := e.d.SetQueuePaused(e.pm, true); err != nil {
		t.Fatal(err)
	}
	beforeQ, err := e.d.ListQueuedMessages(e.pm)
	if err != nil {
		t.Fatal(err)
	}
	beforeS, err := e.d.GetSession(e.pm)
	if err != nil {
		t.Fatal(err)
	}
	path := "/rooms/" + e.room + "/queue?expected_pm_id=" + e.pm
	status, raw := e.request("GET", path+"&limit=1&member="+e.worker, nil, e.token)
	var page struct {
		PMID  string `json:"pm_id"`
		Queue []struct {
			ID          int64           `json:"id"`
			Content     string          `json:"content"`
			RequestID   string          `json:"input_request_id"`
			Attachments []db.Attachment `json:"attachments"`
		} `json:"queue"`
		Paused  bool  `json:"queue_paused"`
		HasMore bool  `json:"has_more"`
		Next    int64 `json:"next_after_id"`
	}
	json.Unmarshal(raw, &page)
	if status != 200 || page.PMID != e.pm || !page.Paused || !page.HasMore || len(page.Queue) != 1 || page.Queue[0].Content != "  first\n" || page.Queue[0].RequestID != "queue-first" || len(page.Queue[0].Attachments) != 1 || page.Queue[0].ID <= 0 || page.Next != page.Queue[0].ID {
		t.Fatalf("獨立FIFO端點缺失或資料不符：%d %s", status, raw)
	}
	status, raw = e.request("GET", path+"&limit=1&after_id="+fmt.Sprint(page.Next), nil, e.token)
	json.Unmarshal(raw, &page)
	if status != 200 || page.HasMore || page.Next != 0 || len(page.Queue) != 1 || page.Queue[0].Content != "second  " || page.Queue[0].RequestID != "queue-second" {
		t.Fatalf("第二頁或scope錯誤：%d %s", status, raw)
	}
	for _, query := range []string{"", "?expected_pm_id=" + e.pm + "&limit=0", "?expected_pm_id=" + e.pm + "&limit=101", "?expected_pm_id=" + e.pm + "&after_id=-1"} {
		status, _ = e.request("GET", "/rooms/"+e.room+"/queue"+query, nil, e.token)
		if status != 400 {
			t.Fatalf("invalid query %q=%d", query, status)
		}
	}
	status, _ = e.request("GET", "/rooms/"+e.room+"/queue?expected_pm_id="+e.worker, nil, e.token)
	if status != 409 {
		t.Fatalf("stale PM=%d", status)
	}
	status, _ = e.request("GET", path, nil, "")
	if status != 401 {
		t.Fatalf("unauthenticated=%d", status)
	}
	afterQ, err := e.d.ListQueuedMessages(e.pm)
	if err != nil {
		t.Fatal(err)
	}
	afterS, err := e.d.GetSession(e.pm)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(beforeQ, afterQ) || !reflect.DeepEqual(beforeS, afterS) {
		t.Fatal("讀取不可改queue/paused/quota/session")
	}
	members := []db.RoomMember{{SessionID: e.pm, Role: db.RoomRoleWorker}, {SessionID: e.worker, Role: db.RoomRolePM}}
	if err := e.d.UpdateRoom(e.room, nil, &members, e.pm); err != nil {
		t.Fatal(err)
	}
	status, raw = e.request("GET", path, nil, e.token)
	if status != 409 {
		t.Fatalf("交接後舊 expectedPM 未拒絕：%d %s", status, raw)
	}
	status, raw = e.request("GET", "/rooms/"+e.room+"/queue?expected_pm_id="+e.worker, nil, e.token)
	json.Unmarshal(raw, &page)
	if status != 200 || page.PMID != e.worker || len(page.Queue) != 2 || page.Queue[0].RequestID != "queue-first" || page.Queue[1].RequestID != "queue-second" || len(page.Queue[0].Attachments) != 1 {
		t.Fatalf("新 PM FIFO／附件授權錯誤：%d %s", status, raw)
	}
}
