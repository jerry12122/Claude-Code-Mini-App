package db

import (
	"errors"
	"fmt"
	"testing"
)

func roomTestDB(t *testing.T) *DB {
	t.Helper()
	database, err := Open(t.TempDir() + "/room.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func roomSession(t *testing.T, database *DB, name, agentType string) *Session {
	t.Helper()
	s, err := database.CreateSession(name, "", t.TempDir(), "default", agentType, nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// roomMsg 直接寫入一則訊息（指定時間、角色與狀態），回傳訊息 id。
func roomMsg(t *testing.T, database *DB, sessionID, role, content, status, createdAt string) int64 {
	t.Helper()
	res, err := database.Exec(
		`INSERT INTO messages (session_id, role, content, status, created_at) VALUES (?, ?, ?, ?, ?)`,
		sessionID, role, content, status, createdAt,
	)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func countRows(t *testing.T, database *DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := database.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRoomCreateKeepsMemberOrderAndRole(t *testing.T) {
	database := roomTestDB(t)
	a := roomSession(t, database, "PM", "claude")
	b := roomSession(t, database, "W1", "codex")
	c := roomSession(t, database, "W2", "cursor")

	room, err := database.CreateRoom("工作室", []RoomMember{
		{SessionID: b.ID, Role: RoomRoleWorker},
		{SessionID: a.ID, Role: RoomRolePM},
		{SessionID: c.ID, Role: RoomRoleWorker},
	})
	if err != nil {
		t.Fatal(err)
	}
	if room.ID == "" || room.Name != "工作室" || room.CreatedAt == "" {
		t.Fatalf("room 欄位不完整: %+v", room)
	}
	got, err := database.GetRoom(room.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct{ id, role, name, agent string }{
		{b.ID, "worker", "W1", "codex"}, {a.ID, "pm", "PM", "claude"}, {c.ID, "worker", "W2", "cursor"},
	}
	if len(got.Members) != len(want) {
		t.Fatalf("members=%d，預期 %d", len(got.Members), len(want))
	}
	for i, w := range want {
		m := got.Members[i]
		if m.SessionID != w.id || m.Role != w.role || m.SessionName != w.name || m.AgentType != w.agent || m.Position != i {
			t.Errorf("members[%d]=%+v，預期 %+v position=%d", i, m, w, i)
		}
	}
}

func TestRoomRejectsBadRoleAtDBLevel(t *testing.T) {
	database := roomTestDB(t)
	a := roomSession(t, database, "A", "claude")
	if _, err := database.CreateRoom("r", []RoomMember{{SessionID: a.ID, Role: "boss"}}); err == nil {
		t.Fatal("role=boss 應失敗（CHECK 限制 pm／worker）")
	}
	if n := countRows(t, database, `SELECT COUNT(*) FROM rooms`); n != 0 {
		t.Errorf("失敗的建立不應留下 rooms，got %d", n)
	}
}

func TestRoomCreateMissingSessionFailsWithoutResidue(t *testing.T) {
	database := roomTestDB(t)
	a := roomSession(t, database, "A", "claude")
	_, err := database.CreateRoom("r", []RoomMember{
		{SessionID: a.ID, Role: RoomRolePM}, {SessionID: "ghost", Role: RoomRoleWorker},
	})
	if !errors.Is(err, ErrRoomSessionMissing) {
		t.Fatalf("err=%v，預期 ErrRoomSessionMissing", err)
	}
	if n := countRows(t, database, `SELECT COUNT(*) FROM rooms`) + countRows(t, database, `SELECT COUNT(*) FROM room_members`); n != 0 {
		t.Errorf("失敗的建立應整筆回滾，殘留 %d 列", n)
	}
}

func TestRoomUpdateRenameAndReplaceMembers(t *testing.T) {
	database := roomTestDB(t)
	a := roomSession(t, database, "A", "claude")
	b := roomSession(t, database, "B", "claude")
	c := roomSession(t, database, "C", "claude")
	room, err := database.CreateRoom("old", []RoomMember{{SessionID: a.ID, Role: RoomRolePM}, {SessionID: b.ID, Role: RoomRoleWorker}})
	if err != nil {
		t.Fatal(err)
	}

	newName := "new"
	members := []RoomMember{{SessionID: b.ID, Role: RoomRolePM}, {SessionID: c.ID, Role: RoomRoleWorker}}
	if err := database.UpdateRoom(room.ID, &newName, &members); err != nil {
		t.Fatal(err)
	}
	got, _ := database.GetRoom(room.ID)
	if got.Name != "new" || len(got.Members) != 2 || got.Members[0].SessionID != b.ID || got.Members[0].Role != "pm" || got.Members[1].SessionID != c.ID {
		t.Fatalf("更新結果不符: %+v", got)
	}

	// 只改名不動成員
	only := "again"
	if err := database.UpdateRoom(room.ID, &only, nil); err != nil {
		t.Fatal(err)
	}
	got, _ = database.GetRoom(room.ID)
	if got.Name != "again" || len(got.Members) != 2 {
		t.Fatalf("只改名不該動成員: %+v", got)
	}

	// 成員含不存在的 session：整筆回滾，原狀不變
	bad := []RoomMember{{SessionID: "ghost", Role: RoomRoleWorker}}
	if err := database.UpdateRoom(room.ID, nil, &bad); !errors.Is(err, ErrRoomSessionMissing) {
		t.Fatalf("err=%v，預期 ErrRoomSessionMissing", err)
	}
	got, _ = database.GetRoom(room.ID)
	if len(got.Members) != 2 {
		t.Errorf("失敗的更新應回滾，成員數=%d", len(got.Members))
	}

	if err := database.UpdateRoom("nope", &newName, nil); !errors.Is(err, ErrRoomNotFound) {
		t.Errorf("不存在的 room err=%v，預期 ErrRoomNotFound", err)
	}
}

func TestRoomListCountsMembers(t *testing.T) {
	database := roomTestDB(t)
	a := roomSession(t, database, "A", "claude")
	if _, err := database.CreateRoom("r1", []RoomMember{{SessionID: a.ID, Role: RoomRolePM}}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateRoom("r2", nil); err != nil {
		t.Fatal(err)
	}
	list, err := database.ListRooms()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("rooms=%d，預期 2", len(list))
	}
	counts := map[string]int{}
	for _, r := range list {
		counts[r.Name] = r.MemberCount
	}
	if counts["r1"] != 1 || counts["r2"] != 0 {
		t.Errorf("member_count=%v", counts)
	}
}

func TestRoomDeleteKeepsSessionsAndMessages(t *testing.T) {
	database := roomTestDB(t)
	a := roomSession(t, database, "A", "claude")
	roomMsg(t, database, a.ID, "user", "hi", "done", "2026-11-18 10:00:00")
	room, _ := database.CreateRoom("r", []RoomMember{{SessionID: a.ID, Role: RoomRolePM}})

	if err := database.DeleteRoom(room.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.GetSession(a.ID); err != nil {
		t.Errorf("刪除工作室不應刪 session: %v", err)
	}
	if n := countRows(t, database, `SELECT COUNT(*) FROM messages WHERE session_id = ?`, a.ID); n != 1 {
		t.Errorf("刪除工作室不應刪訊息，剩 %d", n)
	}
	if n := countRows(t, database, `SELECT COUNT(*) FROM room_members`); n != 0 {
		t.Errorf("成員列應隨工作室移除，剩 %d", n)
	}
	if err := database.DeleteRoom(room.ID); !errors.Is(err, ErrRoomNotFound) {
		t.Errorf("重複刪除 err=%v，預期 ErrRoomNotFound", err)
	}
}

func TestDeleteSessionRemovesMembershipAndTimelineSurvives(t *testing.T) {
	database := roomTestDB(t)
	a := roomSession(t, database, "A", "claude")
	b := roomSession(t, database, "B", "codex")
	roomMsg(t, database, a.ID, "user", "from-a", "done", "2026-11-18 10:00:00")
	roomMsg(t, database, b.ID, "claude", "from-b", "done", "2026-11-18 10:00:01")
	room, _ := database.CreateRoom("r", []RoomMember{{SessionID: a.ID, Role: RoomRolePM}, {SessionID: b.ID, Role: RoomRoleWorker}})

	if err := database.DeleteSession(b.ID); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, database, `SELECT COUNT(*) FROM room_members WHERE session_id = ?`, b.ID); n != 0 {
		t.Errorf("session 刪除後成員身分應移除，剩 %d", n)
	}
	got, err := database.GetRoom(room.ID)
	if err != nil || len(got.Members) != 1 || got.Members[0].SessionID != a.ID {
		t.Fatalf("成員清單不符: %+v err=%v", got, err)
	}
	page, err := database.RoomTimeline(room.ID, nil, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Content != "from-a" {
		t.Errorf("時間軸應只剩 A 的訊息: %+v", page.Items)
	}
}

// 即使成員列殘留（繞過 DeleteSession 的刪除），查詢也不得出錯或回出幽靈成員。
func TestRoomQueriesTolerateOrphanMemberRows(t *testing.T) {
	database := roomTestDB(t)
	a := roomSession(t, database, "A", "claude")
	b := roomSession(t, database, "B", "claude")
	roomMsg(t, database, a.ID, "user", "a", "done", "2026-11-18 10:00:00")
	roomMsg(t, database, b.ID, "user", "b", "done", "2026-11-18 10:00:01")
	room, _ := database.CreateRoom("r", []RoomMember{{SessionID: a.ID, Role: RoomRolePM}, {SessionID: b.ID, Role: RoomRoleWorker}})
	if _, err := database.Exec(`DELETE FROM sessions WHERE id = ?`, b.ID); err != nil {
		t.Fatal(err)
	}
	got, err := database.GetRoom(room.ID)
	if err != nil || len(got.Members) != 1 {
		t.Fatalf("孤兒成員應被略過: %+v err=%v", got, err)
	}
	page, err := database.RoomTimeline(room.ID, nil, 50)
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("孤兒成員的訊息不應出現: %+v err=%v", page, err)
	}
	list, _ := database.ListRooms()
	if len(list) != 1 || list[0].MemberCount != 1 {
		t.Errorf("member_count 應只算存在的 session: %+v", list)
	}
}

func TestRoomTimelineNotFound(t *testing.T) {
	database := roomTestDB(t)
	if _, err := database.RoomTimeline("nope", nil, 10); !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("err=%v，預期 ErrRoomNotFound", err)
	}
}

func TestRoomTimelineMergeOrderIsStable(t *testing.T) {
	database := roomTestDB(t)
	a := roomSession(t, database, "A", "claude")
	b := roomSession(t, database, "B", "codex")
	other := roomSession(t, database, "不在群組", "claude")
	// 同一秒的訊息交錯：以 id 決勝；最終新到舊
	id1 := roomMsg(t, database, a.ID, "user", "1", "done", "2026-11-18 10:00:00")
	id2 := roomMsg(t, database, b.ID, "claude", "2", "done", "2026-11-18 10:00:00")
	id3 := roomMsg(t, database, a.ID, "claude", "3", "done", "2026-11-18 10:00:00")
	id4 := roomMsg(t, database, b.ID, "system", "4", "done", "2026-11-18 10:00:05")
	roomMsg(t, database, other.ID, "user", "不該出現", "done", "2026-11-18 10:00:03")
	room, _ := database.CreateRoom("r", []RoomMember{{SessionID: a.ID, Role: RoomRolePM}, {SessionID: b.ID, Role: RoomRoleWorker}})

	for i := 0; i < 3; i++ { // 重複查詢，順序必須一致
		page, err := database.RoomTimeline(room.ID, nil, 50)
		if err != nil {
			t.Fatal(err)
		}
		var ids []int64
		for _, it := range page.Items {
			ids = append(ids, it.MessageID)
		}
		want := []int64{id4, id3, id2, id1}
		if fmt.Sprint(ids) != fmt.Sprint(want) {
			t.Fatalf("順序=%v，預期 %v", ids, want)
		}
		if page.HasMore {
			t.Error("全部都在一頁內，has_more 應為 false")
		}
	}

	page, _ := database.RoomTimeline(room.ID, nil, 50)
	top := page.Items[0] // id4：B 的 system
	if top.SessionID != b.ID || top.SessionName != "B" || top.MemberRole != "worker" || top.AgentType != "codex" || top.Role != "system" || top.Content != "4" || top.CreatedAt != "2026-11-18 10:00:05" {
		t.Errorf("欄位不符: %+v", top)
	}
}

func TestRoomTimelineFiltersNonTextMessages(t *testing.T) {
	database := roomTestDB(t)
	a := roomSession(t, database, "A", "claude")
	roomMsg(t, database, a.ID, "user", "u", "done", "2026-11-18 10:00:00")
	roomMsg(t, database, a.ID, RoleShell, "shell out", "done", "2026-11-18 10:00:01")
	roomMsg(t, database, a.ID, "claude", "   ", "done", "2026-11-18 10:00:02")     // 只有工具呼叫的空回合
	roomMsg(t, database, a.ID, "claude", "", "interrupted", "2026-11-18 10:00:03") // 中斷且沒內容
	roomMsg(t, database, a.ID, "tool", "tool detail", "done", "2026-11-18 10:00:04")
	roomMsg(t, database, a.ID, "claude", "a", "done", "2026-11-18 10:00:05")
	room, _ := database.CreateRoom("r", []RoomMember{{SessionID: a.ID, Role: RoomRolePM}})

	page, err := database.RoomTimeline(room.ID, nil, 50)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, it := range page.Items {
		got = append(got, it.Content)
	}
	if fmt.Sprint(got) != "[a u]" {
		t.Errorf("只該有使用者／助理／系統的文字訊息，got %v", got)
	}
}

func TestRoomTimelinePaginationNoGapNoDup(t *testing.T) {
	database := roomTestDB(t)
	sessions := []*Session{
		roomSession(t, database, "A", "claude"),
		roomSession(t, database, "B", "codex"),
		roomSession(t, database, "C", "cursor"),
	}
	var want []int64 // 依 (時間, id) 新到舊
	// 3 個 session 交錯、時間每 3 則才前進一秒，製造大量同秒平手
	for i := 0; i < 40; i++ {
		s := sessions[(i*7)%3]
		ts := fmt.Sprintf("2026-11-18 10:00:%02d", i/3)
		id := roomMsg(t, database, s.ID, "user", fmt.Sprintf("m%d", i), "done", ts)
		want = append([]int64{id}, want...)
	}
	room, _ := database.CreateRoom("r", []RoomMember{
		{SessionID: sessions[0].ID, Role: RoomRolePM}, {SessionID: sessions[1].ID, Role: RoomRoleWorker}, {SessionID: sessions[2].ID, Role: RoomRoleWorker},
	})

	for _, size := range []int{1, 3, 7, 40, 100} {
		var got []int64
		var cur *RoomCursor
		pages := 0
		for {
			page, err := database.RoomTimeline(room.ID, cur, size)
			if err != nil {
				t.Fatal(err)
			}
			pages++
			for _, it := range page.Items {
				got = append(got, it.MessageID)
			}
			if !page.HasMore {
				break
			}
			if page.Next == nil {
				t.Fatal("has_more=true 但沒有下一頁游標")
			}
			cur = page.Next
			if pages > 100 {
				t.Fatal("分頁沒有終止")
			}
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("limit=%d 分頁結果與完整排序不同:\n got=%v\nwant=%v", size, got, want)
		}
	}
}

// 分頁過程中有新訊息進來：往更早翻頁不得漏或重（新訊息只出現在最新頁）。
func TestRoomTimelinePaginationStableWhileNewMessagesArrive(t *testing.T) {
	database := roomTestDB(t)
	a := roomSession(t, database, "A", "claude")
	b := roomSession(t, database, "B", "claude")
	for i := 0; i < 10; i++ {
		s := a
		if i%2 == 1 {
			s = b
		}
		roomMsg(t, database, s.ID, "user", fmt.Sprintf("m%d", i), "done", fmt.Sprintf("2026-11-18 10:00:%02d", i))
	}
	room, _ := database.CreateRoom("r", []RoomMember{{SessionID: a.ID, Role: RoomRolePM}, {SessionID: b.ID, Role: RoomRoleWorker}})

	first, _ := database.RoomTimeline(room.ID, nil, 4)
	roomMsg(t, database, a.ID, "user", "late", "done", "2026-11-18 10:30:00")
	second, err := database.RoomTimeline(room.ID, first.Next, 4)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int64]bool{}
	for _, it := range append(first.Items, second.Items...) {
		if seen[it.MessageID] {
			t.Fatalf("訊息 %d 重複", it.MessageID)
		}
		seen[it.MessageID] = true
		if it.Content == "late" {
			t.Fatal("翻舊頁不該出現翻頁後才進來的新訊息")
		}
	}
	if len(second.Items) != 4 || second.Items[0].Content != "m5" {
		t.Errorf("第二頁應接續 m5…m2，got %+v", second.Items)
	}
}

func TestRoomTimelinePendingKeepsOriginalID(t *testing.T) {
	database := roomTestDB(t)
	a := roomSession(t, database, "A", "claude")
	roomMsg(t, database, a.ID, "user", "q", "done", "2020-01-01 00:00:00") // 比 pending 的「現在」更早
	room, _ := database.CreateRoom("r", []RoomMember{{SessionID: a.ID, Role: RoomRolePM}})

	pid, err := database.CreatePendingMessage(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	page, _ := database.RoomTimeline(room.ID, nil, 50)
	if len(page.Items) != 2 || page.Items[0].MessageID != pid || page.Items[0].Status != MessageStatusPending {
		t.Fatalf("pending 訊息（即使內容為空）應以原 id 出現: %+v", page.Items)
	}

	if err := database.AppendMessageContent(pid, "partial"); err != nil {
		t.Fatal(err)
	}
	page, _ = database.RoomTimeline(room.ID, nil, 50)
	if len(page.Items) != 2 || page.Items[0].MessageID != pid || page.Items[0].Content != "partial" || page.Items[0].Status != MessageStatusPending {
		t.Fatalf("串流中內容應就地更新、id 不變: %+v", page.Items)
	}

	if err := database.FinalizeMessage(pid); err != nil {
		t.Fatal(err)
	}
	page, _ = database.RoomTimeline(room.ID, nil, 50)
	if len(page.Items) != 2 || page.Items[0].MessageID != pid || page.Items[0].Status != MessageStatusDone {
		t.Fatalf("完成後仍是同一個 id，不得重複插入: %+v", page.Items)
	}
}
