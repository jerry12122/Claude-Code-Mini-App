package db

import (
	"fmt"
	"strings"
	"testing"
)

// 正式回報走真實流程：DispatchRoomJob → 執行者完成 → DeliverRoomReports → 佇列 → Promote → PM 答覆。
// 歸屬只靠 DeliverRoomReports 寫入的 source_session_id（執行者）與本室 room-report 作者，不解析本文。

type reportFlow struct {
	t  *testing.T
	d  *DB
	pm string
}

func (r *reportFlow) must(err error) {
	r.t.Helper()
	if err != nil {
		r.t.Fatal(err)
	}
}

func (r *reportFlow) runJob(roomID, pmID, worker, outcome string) int64 {
	r.t.Helper()
	jobID, err := r.d.DispatchRoomJob(roomID, pmID, worker, "做事")
	r.must(err)
	queued, err := r.d.ListQueuedMessages(worker)
	r.must(err)
	input, err := r.d.PromoteQueuedMessage(worker, queued[len(queued)-1])
	r.must(err)
	reply, err := r.d.CreatePendingMessage(worker)
	r.must(err)
	r.must(r.d.BindRoomJob(worker, input.ID, reply))
	r.must(r.d.AppendMessageContent(reply, "結果 "+outcome))
	r.must(r.d.FinalizeMessage(reply))
	r.must(r.d.FinishRoomJob(worker, reply, outcome))
	return jobID
}

// answer 在 PM session 補一則 PM 的答覆。
func (r *reportFlow) answer(pmID, text string) int64 {
	r.t.Helper()
	id, err := r.d.CreatePendingMessage(pmID)
	r.must(err)
	r.must(r.d.AppendMessageContent(id, text))
	r.must(r.d.FinalizeMessage(id))
	return id
}

// promoteReports 把 PM 佇列中的回報逐則升為訊息並各補一則答覆，回傳 job id → (回報訊息, 答覆訊息)。
func (r *reportFlow) promoteReports(pmID string) map[int64][2]int64 {
	r.t.Helper()
	queued, err := r.d.ListQueuedMessages(pmID)
	r.must(err)
	out := map[int64][2]int64{}
	for _, q := range queued {
		var job int64
		if _, err := fmt.Sscanf(q.Content, "派工 #%d", &job); err != nil {
			r.t.Fatalf("not a report: %q", q.Content)
		}
		m, err := r.d.PromoteQueuedMessage(pmID, q)
		r.must(err)
		out[job] = [2]int64{m.ID, r.answer(pmID, fmt.Sprintf("PM 答 #%d", job))}
	}
	return out
}

func timelineByID(t *testing.T, d *DB, roomID, member string) map[int64]RoomTimelineItem {
	t.Helper()
	page, err := d.RoomTimelineMember(roomID, member, nil, RoomTimelineMaxLimit)
	if err != nil {
		t.Fatal(err)
	}
	out := map[int64]RoomTimelineItem{}
	for _, it := range page.Items {
		out[it.MessageID] = it
	}
	return out
}

func requireIn(t *testing.T, scope string, got map[int64]RoomTimelineItem, ids ...int64) {
	t.Helper()
	for _, id := range ids {
		if _, ok := got[id]; !ok {
			t.Fatalf("%s 缺少訊息 %d", scope, id)
		}
	}
}

func requireOut(t *testing.T, scope string, got map[int64]RoomTimelineItem, ids ...int64) {
	t.Helper()
	for _, id := range ids {
		if _, ok := got[id]; ok {
			t.Fatalf("%s 不該含訊息 %d", scope, id)
		}
	}
}

func TestRoomTimelineMemberFormalReportsFollowSource(t *testing.T) {
	d := roomTestDB(t)
	pm := roomSession(t, d, "PM", "claude")
	newPM := roomSession(t, d, "接任 PM", "claude")
	a := roomSession(t, d, "甲", "codex")
	b := roomSession(t, d, "乙", "codex")
	c := roomSession(t, d, "丙", "codex")
	members := []RoomMember{{SessionID: pm.ID, Role: RoomRolePM}, {SessionID: newPM.ID, Role: RoomRoleWorker},
		{SessionID: a.ID, Role: RoomRoleWorker}, {SessionID: b.ID, Role: RoomRoleWorker}, {SessionID: c.ID, Role: RoomRoleWorker}}
	room, err := d.CreateRoom("回報", members)
	if err != nil {
		t.Fatal(err)
	}
	r := &reportFlow{t: t, d: d, pm: pm.ID}
	userAuthor := RoomUserAuthorPrefix + room.ID

	// 舊資料：沒有來源的回報與其答覆，只能留在總覽
	legacyReport, err := d.Exec(`INSERT INTO messages(session_id, role, content, status, author) VALUES (?, 'user', ?, 'done', ?)`,
		pm.ID, "派工 #999｜甲｜已完成\n\n舊回報", "room-report:"+room.ID)
	r.must(err)
	legacyID, _ := legacyReport.LastInsertId()
	legacyAnswer := r.answer(pm.ID, "PM 答舊回報")
	// 使用者→PM 的對話不受影響
	userIn, err := d.AddUserMessageWithAttachments(pm.ID, "你好 PM", userAuthor, nil)
	r.must(err)
	userOut := r.answer(pm.ID, "PM 答你")

	// 別間的回報進了同一個 PM session：作者是別間，不能因來源是甲就被納入
	foreign, err := d.Exec(`INSERT INTO messages(session_id, role, content, status, author, source_session_id) VALUES (?, 'user', ?, 'done', 'room-report:other-room', ?)`,
		pm.ID, "派工 #5｜甲｜已完成", a.ID)
	r.must(err)
	foreignID, _ := foreign.LastInsertId()

	jobA1 := r.runJob(room.ID, pm.ID, a.ID, "done")
	jobB := r.runJob(room.ID, pm.ID, b.ID, "failed")
	jobC := r.runJob(room.ID, pm.ID, c.ID, "interrupted")
	r.must(d.DeliverRoomReports())
	r.must(d.DeliverRoomReports()) // 只送一次
	queued, err := d.ListQueuedMessages(pm.ID)
	r.must(err)
	if len(queued) != 3 {
		t.Fatalf("回報應各送一次，got %d", len(queued))
	}
	want := map[int64]string{jobA1: a.ID, jobB: b.ID, jobC: c.ID}
	for _, q := range queued {
		var job int64
		fmt.Sscanf(q.Content, "派工 #%d", &job)
		if q.Author != "room-report:"+room.ID || q.SourceSessionID != want[job] || !strings.HasPrefix(q.Content, "派工 #") {
			t.Fatalf("回報來源／作者／內容不對：%+v", q)
		}
	}
	first := r.promoteReports(pm.ID)

	// PM 交接給新 PM 後，甲再完成一次工作：回報送給現任 PM，來源仍是甲
	r.must(d.UpdateRoom(room.ID, nil, &[]RoomMember{{SessionID: newPM.ID, Role: RoomRolePM}, {SessionID: pm.ID, Role: RoomRoleWorker},
		{SessionID: a.ID, Role: RoomRoleWorker}, {SessionID: b.ID, Role: RoomRoleWorker}, {SessionID: c.ID, Role: RoomRoleWorker}}, pm.ID))
	jobA2 := r.runJob(room.ID, newPM.ID, a.ID, "done")
	r.must(d.DeliverRoomReports())
	second := r.promoteReports(newPM.ID)
	r.must(d.DeliverRoomReports())
	if left, _ := d.ListQueuedMessages(newPM.ID); len(left) != 0 {
		t.Fatalf("重複送達：%+v", left)
	}

	all := map[int64]RoomTimelineItem{}
	for k, v := range timelineByID(t, d, room.ID, "") {
		all[k] = v
	}
	requireIn(t, "總覽", all, legacyID, legacyAnswer, first[jobA1][0], second[jobA2][0])
	if _, ok := all[foreignID]; ok {
		t.Fatal("別間作者的回報不該出現在這個工作室")
	}

	check := func(name, member string, mine map[int64][2]int64, others ...map[int64][2]int64) map[int64]RoomTimelineItem {
		got := timelineByID(t, d, room.ID, member)
		for _, pair := range mine {
			requireIn(t, name, got, pair[0], pair[1])
		}
		for _, set := range others {
			for _, pair := range set {
				requireOut(t, name, got, pair[0], pair[1])
			}
		}
		requireOut(t, name, got, legacyID, legacyAnswer, foreignID)
		return got
	}
	pick := func(m map[int64][2]int64, jobs ...int64) map[int64][2]int64 {
		out := map[int64][2]int64{}
		for _, j := range jobs {
			out[j] = m[j]
		}
		return out
	}
	merged := map[int64][2]int64{}
	for k, v := range first {
		merged[k] = v
	}
	for k, v := range second {
		merged[k] = v
	}
	gotA := check("甲", a.ID, pick(merged, jobA1, jobA2), pick(merged, jobB, jobC))
	gotB := check("乙", b.ID, pick(merged, jobB), pick(merged, jobA1, jobA2, jobC))
	check("丙", c.ID, pick(merged, jobC), pick(merged, jobA1, jobA2, jobB))

	// 回報維持 report 分類，寄件人是執行者名稱（來源是結構化資料，不是正文）
	for job, name := range map[int64]string{jobA1: "甲", jobA2: "甲"} {
		it := gotA[merged[job][0]]
		if it.SourceKind != "report" || it.Sender != name || it.Mine || it.Author != "room-report:"+room.ID {
			t.Fatalf("回報顯示被改掉：%+v", it)
		}
		if ans := gotA[merged[job][1]]; ans.SourceKind != "assistant" || ans.Recipient != "工作室" {
			t.Fatalf("PM 答覆顯示被改掉：%+v", ans)
		}
	}
	if it := gotB[merged[jobB][0]]; it.SourceKind != "report" || it.Sender != "乙" {
		t.Fatalf("failed 回報顯示：%+v", it)
	}

	// 現任 PM：只有你↔PM，不含任何回報與其答覆
	pmView := timelineByID(t, d, room.ID, newPM.ID)
	requireOut(t, "現任 PM", pmView, legacyID, legacyAnswer, foreignID)
	for _, pair := range merged {
		requireOut(t, "現任 PM", pmView, pair[0], pair[1])
	}
	// 卸任後的舊 PM 現在是 worker：自己 session 的內容照舊，使用者對話仍在
	oldView := timelineByID(t, d, room.ID, pm.ID)
	requireIn(t, "舊 PM(worker)", oldView, userIn.ID, userOut)
}

// 明確 room-user 的輸入不會因為字首像信封而被 PM 篩選排除；只有空作者、無來源、且完整符合信封格式的舊資料才排除。
func TestRoomTimelineMemberPMKeepsExplicitRoomUserEnvelopeLikeText(t *testing.T) {
	d := roomTestDB(t)
	pm := roomSession(t, d, "PM", "claude")
	w := roomSession(t, d, "甲", "codex")
	room, err := d.CreateRoom("字首", []RoomMember{{SessionID: pm.ID, Role: RoomRolePM}, {SessionID: w.ID, Role: RoomRoleWorker}})
	if err != nil {
		t.Fatal(err)
	}
	r := &reportFlow{t: t, d: d, pm: pm.ID}
	userAuthor := RoomUserAuthorPrefix + room.ID
	add := func(content, author string) (int64, int64) {
		m, err := d.AddUserMessageWithAttachments(pm.ID, content, author, nil)
		r.must(err)
		return m.ID, r.answer(pm.ID, fmt.Sprintf("答 %d", m.ID))
	}
	plainIn, plainOut := add("來自 Mini-App session 的訊息，是我自己打的", userAuthor)
	quotedIn, quotedOut := add(consultFixture(w.ID, pm.ID, "使用者貼上的舊信封"), userAuthor)
	// 空作者（私人 owner）打的普通字首，不是完整信封：仍是使用者輸入
	ownerIn, ownerOut := add("來自 Mini-App session 開頭但不是信封", "")
	// 空作者、無來源、完整信封：來源不明的舊信封，只在總覽
	legacyIn, legacyOut := add(consultFixture(w.ID, pm.ID, "不明來源"), "")

	got := timelineByID(t, d, room.ID, pm.ID)
	requireIn(t, "PM", got, plainIn, plainOut, quotedIn, quotedOut, ownerIn, ownerOut)
	requireOut(t, "PM", got, legacyIn, legacyOut)
	requireIn(t, "總覽", timelineByID(t, d, room.ID, ""), legacyIn, legacyOut, plainIn, quotedIn)
	if it := got[quotedIn]; !it.Mine || it.SourceKind != "user" {
		t.Fatalf("明確 room-user 的信封文字顯示被改：%+v", it)
	}
}
