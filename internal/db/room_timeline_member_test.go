package db

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

// 成員篩選只認結構化關係：session_id、source_session_id、room_jobs 綁定的 author 與前一則 user 輸入。
// 名稱、PM 標籤、信封文字都不能拿來歸屬。

type filterRoom struct {
	d                      *DB
	room                   *Room
	pm, workerA, workerB   *Session
	outsider               *Session
	n                      int
	jobA, jobB, jobOtherRm int64
}

func (f *filterRoom) at() string {
	f.n++
	return time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC).Add(time.Duration(f.n) * time.Second).Format("2006-01-02 15:04:05")
}

func (f *filterRoom) msg(t *testing.T, sid, role, content, author, source string) int64 {
	t.Helper()
	res, err := f.d.Exec(`INSERT INTO messages (session_id, role, content, status, created_at, author, source_session_id)
		VALUES (?, ?, ?, 'done', ?, ?, ?)`, sid, role, content, f.at(), author, source)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func newFilterRoom(t *testing.T) *filterRoom {
	t.Helper()
	d := roomTestDB(t)
	f := &filterRoom{d: d}
	f.pm = roomSession(t, d, "PM", "claude")
	// 兩個執行者刻意同名：篩選不能靠名稱分辨
	f.workerA = roomSession(t, d, "同名", "codex")
	f.workerB = roomSession(t, d, "同名", "codex")
	f.outsider = roomSession(t, d, "不在室內", "claude")
	room, err := d.CreateRoom("篩選", []RoomMember{
		{SessionID: f.pm.ID, Role: RoomRolePM}, {SessionID: f.workerA.ID, Role: RoomRoleWorker}, {SessionID: f.workerB.ID, Role: RoomRoleWorker}})
	if err != nil {
		t.Fatal(err)
	}
	f.room = room
	other := roomSession(t, d, "別間 PM", "claude")
	otherRoom, err := d.CreateRoom("別間", []RoomMember{{SessionID: other.ID, Role: RoomRolePM}})
	if err != nil {
		t.Fatal(err)
	}
	job := func(roomID, pm, worker, outcome string) int64 {
		res, err := d.Exec(`INSERT INTO room_jobs(room_id, pm_id, worker_id, outcome) VALUES (?, ?, ?, ?)`, roomID, pm, worker, outcome)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	f.jobA = job(room.ID, f.pm.ID, f.workerA.ID, "")
	f.jobB = job(room.ID, f.pm.ID, f.workerB.ID, "")
	f.jobOtherRm = job(otherRoom.ID, other.ID, f.workerA.ID, "done")
	return f
}

func (f *filterRoom) ids(t *testing.T, member string, before *RoomCursor, limit int) ([]int64, *RoomTimelinePage) {
	t.Helper()
	page, err := f.d.RoomTimelineMember(f.room.ID, member, before, limit)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]int64, 0, len(page.Items))
	for _, it := range page.Items {
		out = append(out, it.MessageID)
	}
	return out, page
}

func sameSet(got []int64, want ...int64) bool {
	if len(got) != len(want) {
		return false
	}
	seen := map[int64]bool{}
	for _, id := range got {
		seen[id] = true
	}
	for _, id := range want {
		if !seen[id] {
			return false
		}
	}
	return true
}

func TestRoomTimelineMemberFilterScopesByStructuredSource(t *testing.T) {
	f := newFilterRoom(t)
	user := RoomUserAuthorPrefix + f.room.ID
	report := "room-report:" + f.room.ID

	p1 := f.msg(t, f.pm.ID, "user", "給 PM（室內輸入）", user, "")
	p2 := f.msg(t, f.pm.ID, "claude", "PM 答 1", "", "")
	p3 := f.msg(t, f.pm.ID, "user", "給 PM（私人 owner 輸入）", "", "")
	p4 := f.msg(t, f.pm.ID, "claude", "PM 答私人", "", "")
	// 正式回報：job id 只在本文。篩選不得解析本文，所以只出現在總覽。
	p5 := f.msg(t, f.pm.ID, "user", fmt.Sprintf("派工 #%d｜同名｜已完成\n\n結果", f.jobA), report, "")
	p6 := f.msg(t, f.pm.ID, "claude", "PM 答回報", "", "")
	// 跨 session 的結構化來源：workerA 諮詢 PM
	p7 := f.msg(t, f.pm.ID, "user", consultFixture(f.workerA.ID, f.pm.ID, "A 的問題"), "", f.workerA.ID)
	p8 := f.msg(t, f.pm.ID, "claude", "PM 答 A", "", "")
	// 來源不明的舊信封（沒有 source_session_id）：只在總覽
	p9 := f.msg(t, f.pm.ID, "user", consultFixture(f.workerB.ID, f.pm.ID, "不明來源"), "", "")
	p10 := f.msg(t, f.pm.ID, "claude", "PM 答不明", "", "")
	// 附件-only 的室內輸入
	res, err := f.d.Exec(`INSERT INTO messages (session_id, role, content, status, created_at, author, attachment_ids) VALUES (?, 'user', '', 'done', ?, ?, '["att-1"]')`,
		f.pm.ID, f.at(), user)
	if err != nil {
		t.Fatal(err)
	}
	p11, _ := res.LastInsertId()

	a1 := f.msg(t, f.workerA.ID, "user", "派工 A", fmt.Sprintf("room-job:%d", f.jobA), "")
	a2 := f.msg(t, f.workerA.ID, "claude", "A 回覆", "", "")
	a3 := f.msg(t, f.workerA.ID, "user", "使用者直接對 A", "", "")
	a4 := f.msg(t, f.workerA.ID, "claude", "A 答使用者", "", "")
	// 別間的派工進了同一個 session：既有規則就不顯示，篩選更不能撿回來
	f.msg(t, f.workerA.ID, "user", "別間派工", fmt.Sprintf("room-job:%d", f.jobOtherRm), "")
	f.msg(t, f.workerA.ID, "claude", "別間回覆", "", "")
	b1 := f.msg(t, f.workerB.ID, "user", "派工 B", fmt.Sprintf("room-job:%d", f.jobB), "")
	b2 := f.msg(t, f.workerB.ID, "claude", "B 回覆", "", "")
	// 非成員的 session 不屬於這間
	f.msg(t, f.outsider.ID, "user", "外人", "", "")

	all, _ := f.ids(t, "", nil, 100)
	if len(all) != 17 {
		t.Fatalf("overview must keep everything (the filter is opt-in): got %d", len(all))
	}

	pm, _ := f.ids(t, f.pm.ID, nil, 100)
	if !sameSet(pm, p1, p2, p3, p4, p11) {
		t.Fatalf("PM filter = %v, want user→PM inputs and their answers only (p1 p2 p3 p4 p11)", pm)
	}
	for _, leaked := range []int64{p5, p6, p7, p8, p9, p10, a1, a2, b1, b2} {
		for _, id := range pm {
			if id == leaked {
				t.Fatalf("PM filter leaked %d", leaked)
			}
		}
	}

	a, _ := f.ids(t, f.workerA.ID, nil, 100)
	if !sameSet(a, a1, a2, a3, a4, p7, p8) {
		t.Fatalf("worker A filter = %v, want session + trusted cross-session source (a1-a4 p7 p8)", a)
	}
	b, _ := f.ids(t, f.workerB.ID, nil, 100)
	if !sameSet(b, b1, b2) {
		t.Fatalf("worker B (same name as A) = %v, want b1 b2 only", b)
	}

	if _, err := f.d.RoomTimelineMember(f.room.ID, f.outsider.ID, nil, 10); !errors.Is(err, ErrRoomMemberNotFound) {
		t.Fatalf("non-member = %v", err)
	}
	if _, err := f.d.RoomTimelineMember(f.room.ID, "no-such-session", nil, 10); !errors.Is(err, ErrRoomMemberNotFound) {
		t.Fatalf("missing session = %v", err)
	}
	if _, err := f.d.RoomTimelineMember("nope", f.pm.ID, nil, 10); !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("missing room = %v", err)
	}
	// 別間的成員不能經由這間的查詢讀到
	var otherPM string
	if err := f.d.QueryRow(`SELECT pm_id FROM room_jobs WHERE id = ?`, f.jobOtherRm).Scan(&otherPM); err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.RoomTimelineMember(f.room.ID, otherPM, nil, 10); !errors.Is(err, ErrRoomMemberNotFound) {
		t.Fatalf("other room member = %v", err)
	}
}

func TestRoomTimelineMemberFilterPagesOverFullHistory(t *testing.T) {
	f := newFilterRoom(t)
	var wantA []int64
	for i := 0; i < 30; i++ {
		wantA = append(wantA, f.msg(t, f.workerA.ID, "user", fmt.Sprintf("A 舊 %d", i), "", ""))
	}
	for i := 0; i < 120; i++ {
		f.msg(t, f.workerB.ID, "user", fmt.Sprintf("B 新 %d", i), "", "")
	}
	// A 的訊息全部藏在最新 50 則之外；總覽第一頁看不到任何一則
	first, _ := f.ids(t, "", nil, 50)
	for _, id := range first {
		for _, a := range wantA {
			if id == a {
				t.Fatal("test setup: A must be outside the newest 50")
			}
		}
	}
	var got []int64
	var cur *RoomCursor
	pages := 0
	for {
		ids, page := f.ids(t, f.workerA.ID, cur, 12)
		pages++
		if len(ids) == 0 && page.HasMore {
			t.Fatal("empty page that claims more")
		}
		got = append(got, ids...)
		if !page.HasMore {
			break
		}
		cur = page.Next
		if pages > 10 {
			t.Fatal("pagination does not terminate")
		}
	}
	if pages != 3 || len(got) != 30 {
		t.Fatalf("pages=%d got=%d, want 3 pages / 30 messages (filter applies before LIMIT)", pages, len(got))
	}
	seen := map[int64]bool{}
	for i, id := range got {
		if seen[id] {
			t.Fatalf("duplicate %d", id)
		}
		seen[id] = true
		if i > 0 && id >= got[i-1] {
			t.Fatalf("order not newest→oldest at %d", i)
		}
	}
	if !sameSet(got, wantA...) {
		t.Fatal("pages must cover exactly A's history")
	}
}

func TestRoomTimelineMemberFilterQueuedProjection(t *testing.T) {
	f := newFilterRoom(t)
	user := RoomUserAuthorPrefix + f.room.ID
	if _, err := f.d.EnqueueMessage(f.pm.ID, "排隊給 PM", user); err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.EnqueueMessage(f.workerA.ID, "排隊給 A", user); err != nil {
		t.Fatal(err)
	}
	queuedTexts := func(member string, before *RoomCursor) []string {
		_, page := f.ids(t, member, before, 20)
		var out []string
		for _, it := range page.Items {
			if it.Queued {
				out = append(out, it.Content)
			}
		}
		return out
	}
	if got := queuedTexts("", nil); len(got) != 2 {
		t.Fatalf("overview queued = %v", got)
	}
	if got := queuedTexts(f.pm.ID, nil); len(got) != 1 || got[0] != "排隊給 PM" {
		t.Fatalf("PM queued = %v", got)
	}
	if got := queuedTexts(f.workerA.ID, nil); len(got) != 1 || got[0] != "排隊給 A" {
		t.Fatalf("A queued = %v", got)
	}
	if got := queuedTexts(f.workerB.ID, nil); len(got) != 0 {
		t.Fatalf("B queued = %v", got)
	}
	// 往更早翻頁不帶佇列投影（與總覽一致）
	if got := queuedTexts(f.pm.ID, &RoomCursor{CreatedAt: "2999-01-01 00:00:00", ID: 1 << 40}); len(got) != 0 {
		t.Fatalf("older page queued = %v", got)
	}
}

func TestRoomTimelineMemberFilterFollowsRoleSwapAndRemoval(t *testing.T) {
	f := newFilterRoom(t)
	user := RoomUserAuthorPrefix + f.room.ID
	pmIn := f.msg(t, f.pm.ID, "user", "給舊 PM", user, "")
	f.msg(t, f.workerA.ID, "user", "派工 A", fmt.Sprintf("room-job:%d", f.jobA), "")
	direct := f.msg(t, f.workerA.ID, "user", "使用者對 A", "", "")
	if ids, _ := f.ids(t, f.pm.ID, nil, 20); !sameSet(ids, pmIn) {
		t.Fatalf("PM before swap = %v", ids)
	}
	// 有未結案派工的 worker 不能當 PM
	if _, err := f.d.Exec(`UPDATE room_jobs SET outcome = 'done' WHERE id = ?`, f.jobA); err != nil {
		t.Fatal(err)
	}
	members := []RoomMember{{SessionID: f.pm.ID, Role: RoomRoleWorker}, {SessionID: f.workerA.ID, Role: RoomRolePM}, {SessionID: f.workerB.ID, Role: RoomRoleWorker}}
	if err := f.d.UpdateRoom(f.room.ID, nil, &members, f.pm.ID); err != nil {
		t.Fatal(err)
	}
	// 舊 PM 現在是 worker：它 session 內本室的訊息都算它的；新 PM 只剩 user→PM
	if ids, _ := f.ids(t, f.pm.ID, nil, 20); !sameSet(ids, pmIn) {
		t.Fatalf("old PM as worker = %v", ids)
	}
	if ids, _ := f.ids(t, f.workerA.ID, nil, 20); !sameSet(ids, direct) {
		t.Fatalf("new PM = %v, want only the user's direct input (job input is not user→PM)", ids)
	}
	members = []RoomMember{{SessionID: f.workerA.ID, Role: RoomRolePM}, {SessionID: f.workerB.ID, Role: RoomRoleWorker}}
	if err := f.d.UpdateRoom(f.room.ID, nil, &members, f.workerA.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.RoomTimelineMember(f.room.ID, f.pm.ID, nil, 10); !errors.Is(err, ErrRoomMemberNotFound) {
		t.Fatalf("removed member = %v", err)
	}
}
