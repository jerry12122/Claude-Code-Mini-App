package server

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
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

func TestRoomQueueBatchHintHTTP(t *testing.T) {
	postBatch := func(t *testing.T, e *roomEditHTTP, requestID string, rows []db.QueuedMessage) (int, []byte) {
		t.Helper()
		items := make([]map[string]any, len(rows))
		for i, row := range rows {
			items[i] = map[string]any{"id": row.ID, "previous": row.Content}
		}
		return e.request("POST", "/rooms/"+e.room+"/messages/batch", map[string]any{"expected_pm_id": e.pm, "request_id": requestID, "items": items}, e.token)
	}
	getPage := func(t *testing.T, e *roomEditHTTP) (db.RoomQueuePage, []byte) {
		t.Helper()
		status, raw := e.request("GET", "/rooms/"+e.room+"/queue?expected_pm_id="+e.pm, nil, e.token)
		var page db.RoomQueuePage
		if err := json.Unmarshal(raw, &page); err != nil || status != 200 {
			t.Fatalf("GET queue %d %s %v", status, raw, err)
		}
		return page, raw
	}
	counts := func(t *testing.T, e *roomEditHTTP) (receipts, batches int) {
		t.Helper()
		if err := e.d.QueryRow(`SELECT count(*) FROM input_receipts WHERE session_id=?`, e.pm).Scan(&receipts); err != nil {
			t.Fatal(err)
		}
		if err := e.d.QueryRow(`SELECT count(*) FROM queue_batches WHERE session_id=?`, e.pm).Scan(&batches); err != nil {
			t.Fatal(err)
		}
		return receipts, batches
	}

	t.Run("contiguous posts", func(t *testing.T) {
		e := newRoomEditHTTP(t)
		if err := e.d.SetQueuePaused(e.pm, true); err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{"a", "b"} {
			if err := e.d.AcceptRoomInput(e.room, e.pm, id, id, nil); err != nil {
				t.Fatal(err)
			}
		}
		page, raw := getPage(t, e)
		if !page.BatchReady || len(page.Queue) != 2 {
			t.Fatalf("連續應可一起送：ready=%v %s", page.BatchReady, raw)
		}
		rows, err := e.d.ListQueuedMessages(e.pm)
		if err != nil {
			t.Fatal(err)
		}
		status, body := postBatch(t, e, "http-ready", rows)
		if status != 202 {
			t.Fatalf("連續 HTTP 應 202：%d %s", status, body)
		}
	})
	t.Run("hidden report does not post and does not leak", func(t *testing.T) {
		e := newRoomEditHTTP(t)
		if err := e.d.SetQueuePaused(e.pm, true); err != nil {
			t.Fatal(err)
		}
		if err := e.d.AcceptRoomInput(e.room, e.pm, "a", "甲", nil); err != nil {
			t.Fatal(err)
		}
		if _, err := e.d.EnqueueMessageFromCancelQuota(e.pm, "SECRET-REPORT", "room-report:"+e.room, e.worker); err != nil {
			t.Fatal(err)
		}
		if err := e.d.AcceptRoomInput(e.room, e.pm, "b", "乙", nil); err != nil {
			t.Fatal(err)
		}
		before, err := e.d.ListQueuedMessages(e.pm)
		if err != nil {
			t.Fatal(err)
		}
		receipts, batches := counts(t, e)
		page, raw := getPage(t, e)
		if page.BatchReady || len(page.Queue) != 2 {
			t.Fatalf("夾回報應不可一起送：ready=%v %s", page.BatchReady, raw)
		}
		for _, hidden := range []string{"SECRET-REPORT", "room-report", e.worker} {
			if strings.Contains(string(raw), hidden) {
				t.Fatalf("HTTP 快照洩漏 %q", hidden)
			}
		}
		status, body := postBatch(t, e, "http-hidden", []db.QueuedMessage{before[0], before[2]})
		if status != 403 || !strings.Contains(string(body), "forbidden") {
			t.Fatalf("伺服器再驗應拒絕：%d %s", status, body)
		}
		after, err := e.d.ListQueuedMessages(e.pm)
		if err != nil {
			t.Fatal(err)
		}
		r2, b2 := counts(t, e)
		if !reflect.DeepEqual(before, after) || receipts != r2 || batches != b2 {
			t.Fatal("403 後佇列、receipt 或 batch 被改寫")
		}
		editStatus, _ := e.request("PATCH", fmt.Sprintf("/rooms/%s/messages/%d", e.room, before[0].ID), map[string]string{"expected_pm_id": e.pm, "previous": before[0].Content, "content": "草稿仍在"}, e.token)
		if editStatus != 204 {
			t.Fatalf("拒絕後仍應可編輯草稿：%d", editStatus)
		}
	})
	t.Run("snapshot race keeps the server refusal", func(t *testing.T) {
		e := newRoomEditHTTP(t)
		if err := e.d.SetQueuePaused(e.pm, true); err != nil {
			t.Fatal(err)
		}
		if err := e.d.AcceptRoomInput(e.room, e.pm, "a", "甲", nil); err != nil {
			t.Fatal(err)
		}
		other, err := e.d.CreateSession("other", "", t.TempDir(), "default", "room-edit-fake", nil, "agent")
		if err != nil {
			t.Fatal(err)
		}
		gap, err := e.d.EnqueueMessage(other.ID, "GAP", "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.d.Exec(`DELETE FROM queued_messages WHERE id=?`, gap.ID); err != nil {
			t.Fatal(err)
		}
		if err := e.d.AcceptRoomInput(e.room, e.pm, "b", "乙", nil); err != nil {
			t.Fatal(err)
		}
		page, raw := getPage(t, e)
		if !page.BatchReady || len(page.Queue) != 2 || page.Queue[1].ID-page.Queue[0].ID <= 1 {
			t.Fatalf("別 session 的全域 ID 空隙不該擋：ready=%v %s", page.BatchReady, raw)
		}
		if _, err := e.d.Exec(`INSERT INTO queued_messages(id, session_id, content, author, source_session_id, attachment_ids) VALUES(?, ?, 'RACE-SECRET', ?, ?, '[]')`, gap.ID, e.pm, "room-report:"+e.room, e.worker); err != nil {
			t.Fatal(err)
		}
		status, body := postBatch(t, e, "http-race", []db.QueuedMessage{{ID: page.Queue[0].ID, Content: page.Queue[0].Content}, {ID: page.Queue[1].ID, Content: page.Queue[1].Content}})
		if status != 403 {
			t.Fatalf("快照後插入應被交易拒絕：%d %s", status, body)
		}
		var pending int
		if err := e.d.QueryRow(`SELECT count(*) FROM queue_batches WHERE session_id=? AND state='pending'`, e.pm).Scan(&pending); err != nil {
			t.Fatal(err)
		}
		if pending != 0 {
			t.Fatal("競爭失敗留下 pending batch")
		}
		again, _ := getPage(t, e)
		if again.BatchReady || strings.Contains(string(mustJSON(t, again)), "RACE-SECRET") {
			t.Fatal("競爭後重新讀取仍不可一起送，且不得洩漏")
		}
	})
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
