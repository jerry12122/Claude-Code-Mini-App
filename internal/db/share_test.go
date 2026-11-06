package db

import (
	"errors"
	"testing"
	"time"
)

func newShareTestDB(t *testing.T) (*DB, *Session) {
	t.Helper()
	database, err := Open(t.TempDir() + "/share.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	s, err := database.CreateSession("共享", "", t.TempDir(), "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	return database, s
}

func TestClearEndedSharesPreservesActiveAndChat(t *testing.T) {
	database, session := newShareTestDB(t)
	makeShare := func() *Share {
		s, err := database.CreateShare(session.ID, ShareRoleViewer, ShareModeLive, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := database.TryJoin(s.Token, s.PIN, "guest"); err != nil {
			t.Fatal(err)
		}
		return s
	}
	active, revoked, expired := makeShare(), makeShare(), makeShare()
	if err := database.RevokeShare(revoked.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE shares SET expires_at = ? WHERE id = ?`, time.Now().UTC().Add(-time.Hour).Format(shareTimeLayout), expired.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE shares SET failed_attempts = ? WHERE id = ?`, MaxPINAttempts, active.ID); err != nil {
		t.Fatal(err)
	}
	message, err := database.AddUserMessageWithAttachments(session.ID, "keep chat", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := database.ClearEndedShares(active.ID); n != 0 || err != nil {
		t.Fatalf("active: n=%d err=%v", n, err)
	}
	if n, err := database.ClearEndedShares(revoked.ID); n != 1 || err != nil {
		t.Fatalf("single: n=%d err=%v", n, err)
	}
	if n, err := database.ClearEndedShares(revoked.ID); n != 0 || err != nil {
		t.Fatalf("repeat: n=%d err=%v", n, err)
	}
	if n, err := database.ClearEndedShares(0); n != 1 || err != nil {
		t.Fatalf("all: n=%d err=%v", n, err)
	}
	if _, err := database.GetShareByToken(revoked.Token); !errors.Is(err, ErrShareNotFound) {
		t.Fatalf("revoked link still exists: %v", err)
	}
	if _, err := database.GetShareByToken(expired.Token); !errors.Is(err, ErrShareNotFound) {
		t.Fatalf("expired link still exists: %v", err)
	}
	var guests int
	if err := database.QueryRow(`SELECT COUNT(*) FROM share_guests`).Scan(&guests); err != nil || guests != 1 {
		t.Fatalf("guests=%d err=%v", guests, err)
	}
	if _, err := database.GetShare(active.ID); err != nil {
		t.Fatalf("active removed: %v", err)
	}
	if _, err := database.GetSession(session.ID); err != nil {
		t.Fatalf("session removed: %v", err)
	}
	var content string
	if err := database.QueryRow(`SELECT content FROM messages WHERE id = ?`, message.ID).Scan(&content); err != nil || content != "keep chat" {
		t.Fatalf("message=%q err=%v", content, err)
	}
}

func TestCreateShare_EditorSnapshotRejected(t *testing.T) {
	database, s := newShareTestDB(t)
	if _, err := database.CreateShare(s.ID, ShareRoleEditor, ShareModeSnapshot, time.Hour); !errors.Is(err, ErrInvalidShare) {
		t.Fatalf("editor+snapshot 應被拒絕，got %v", err)
	}
	if _, err := database.CreateShare(s.ID, "admin", ShareModeLive, time.Hour); !errors.Is(err, ErrInvalidShare) {
		t.Fatalf("未知角色應被拒絕，got %v", err)
	}
	if _, err := database.CreateShare(s.ID, ShareRoleViewer, ShareModeLive, 0); !errors.Is(err, ErrInvalidShare) {
		t.Fatalf("ttl<=0 應被拒絕，got %v", err)
	}
	if _, err := database.CreateShare("not-exist", ShareRoleViewer, ShareModeLive, time.Hour); err == nil {
		t.Fatal("session 不存在應失敗")
	}
	sh, err := database.CreateShare(s.ID, ShareRoleEditor, ShareModeLive, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(sh.PIN) != 6 || sh.Token == "" || sh.SnapshotMsgID != nil {
		t.Fatalf("share=%+v", sh)
	}
}

func TestTryJoin_PINLockAfterFiveFailures(t *testing.T) {
	database, s := newShareTestDB(t)
	sh, _ := database.CreateShare(s.ID, ShareRoleViewer, ShareModeLive, time.Hour)
	wrong := "000000"
	if sh.PIN == wrong {
		wrong = "111111"
	}
	for i := 1; i <= MaxPINAttempts; i++ {
		_, err := database.TryJoin(sh.Token, wrong, "小明")
		var pe *PINError
		if !errors.As(err, &pe) || pe.Remaining != MaxPINAttempts-i {
			t.Fatalf("第 %d 次錯誤 err=%v", i, err)
		}
	}
	// 鎖定後即使 PIN 正確也不可加入
	if _, err := database.TryJoin(sh.Token, sh.PIN, "小明"); !errors.Is(err, ErrShareLocked) {
		t.Fatalf("鎖定後應拒絕正確 PIN，got %v", err)
	}
	got, _ := database.GetShare(sh.ID)
	if !got.Locked() {
		t.Fatalf("failed_attempts=%d", got.FailedAttempts)
	}
}

func TestTryJoin_SuccessResetsFailures(t *testing.T) {
	database, s := newShareTestDB(t)
	sh, _ := database.CreateShare(s.ID, ShareRoleViewer, ShareModeLive, time.Hour)
	wrong := "000000"
	if sh.PIN == wrong {
		wrong = "111111"
	}
	for i := 0; i < MaxPINAttempts-1; i++ {
		_, _ = database.TryJoin(sh.Token, wrong, "x")
	}
	if _, err := database.TryJoin(sh.Token, sh.PIN, "x"); err != nil {
		t.Fatal(err)
	}
	got, _ := database.GetShare(sh.ID)
	if got.FailedAttempts != 0 {
		t.Fatalf("成功加入後應歸零，got %d", got.FailedAttempts)
	}
}

func TestTryJoin_NicknameDedupe(t *testing.T) {
	database, s := newShareTestDB(t)
	sh, _ := database.CreateShare(s.ID, ShareRoleEditor, ShareModeLive, time.Hour)
	want := []string{"小明", "小明2", "小明3"}
	for _, w := range want {
		ga, err := database.TryJoin(sh.Token, sh.PIN, "  小明 ")
		if err != nil {
			t.Fatal(err)
		}
		if ga.Guest.Nickname != w {
			t.Fatalf("nickname=%q want %q", ga.Guest.Nickname, w)
		}
	}
	// 大小寫視為重複；保留名稱「擁有者」也要加後綴；方括號會被濾掉
	ga, _ := database.TryJoin(sh.Token, sh.PIN, "擁有者")
	if ga.Guest.Nickname != "擁有者2" {
		t.Fatalf("保留名稱應加後綴，got %q", ga.Guest.Nickname)
	}
	a, _ := database.TryJoin(sh.Token, sh.PIN, "Bob")
	b, _ := database.TryJoin(sh.Token, sh.PIN, "bob")
	if a.Guest.Nickname != "Bob" || b.Guest.Nickname != "bob2" {
		t.Fatalf("a=%q b=%q", a.Guest.Nickname, b.Guest.Nickname)
	}
	c, _ := database.TryJoin(sh.Token, sh.PIN, "[Eve]")
	if c.Guest.Nickname != "Eve" {
		t.Fatalf("c=%q", c.Guest.Nickname)
	}
	if _, err := database.TryJoin(sh.Token, sh.PIN, " [] "); !errors.Is(err, ErrInvalidNickname) {
		t.Fatalf("空暱稱應拒絕，got %v", err)
	}
	// 不同分享的暱稱各自獨立
	sh2, _ := database.CreateShare(s.ID, ShareRoleViewer, ShareModeLive, time.Hour)
	d, _ := database.TryJoin(sh2.Token, sh2.PIN, "小明")
	if d.Guest.Nickname != "小明" {
		t.Fatalf("d=%q", d.Guest.Nickname)
	}
}

func TestShare_ExpiredAndRevokedTokenInactive(t *testing.T) {
	database, s := newShareTestDB(t)
	sh, _ := database.CreateShare(s.ID, ShareRoleViewer, ShareModeLive, time.Hour)
	ga, err := database.TryJoin(sh.Token, sh.PIN, "a")
	if err != nil {
		t.Fatal(err)
	}
	cur, err := database.ResolveGuestToken(ga.Guest.Token)
	if err != nil || !cur.Share.Active(time.Now()) {
		t.Fatalf("新分享應有效 err=%v", err)
	}

	// 到期
	past := time.Now().UTC().Add(-time.Minute).Format(shareTimeLayout)
	if _, err := database.Exec(`UPDATE shares SET expires_at = ? WHERE id = ?`, past, sh.ID); err != nil {
		t.Fatal(err)
	}
	cur, _ = database.ResolveGuestToken(ga.Guest.Token)
	if cur.Share.Active(time.Now()) || cur.Share.Status(time.Now()) != "expired" {
		t.Fatalf("到期後應失效 status=%s", cur.Share.Status(time.Now()))
	}
	if _, err := database.TryJoin(sh.Token, sh.PIN, "b"); !errors.Is(err, ErrShareEnded) {
		t.Fatalf("到期後不可加入，got %v", err)
	}

	// 撤銷
	sh2, _ := database.CreateShare(s.ID, ShareRoleViewer, ShareModeLive, time.Hour)
	ga2, _ := database.TryJoin(sh2.Token, sh2.PIN, "c")
	if err := database.RevokeShare(sh2.ID); err != nil {
		t.Fatal(err)
	}
	cur, _ = database.ResolveGuestToken(ga2.Guest.Token)
	if cur.Share.Active(time.Now()) || cur.Share.Status(time.Now()) != "revoked" {
		t.Fatal("撤銷後應失效")
	}
	if _, err := database.TryJoin(sh2.Token, sh2.PIN, "d"); !errors.Is(err, ErrShareEnded) {
		t.Fatalf("撤銷後不可加入，got %v", err)
	}
	if err := database.RevokeShare(99999); !errors.Is(err, ErrShareNotFound) {
		t.Fatalf("got %v", err)
	}
	if _, err := database.ResolveGuestToken("nope"); !errors.Is(err, ErrShareNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestRevokeActiveShares_AndDeleteSession(t *testing.T) {
	database, s := newShareTestDB(t)
	a, _ := database.CreateShare(s.ID, ShareRoleViewer, ShareModeLive, time.Hour)
	b, _ := database.CreateShare(s.ID, ShareRoleViewer, ShareModeLive, time.Hour)
	c, _ := database.CreateShare(s.ID, ShareRoleViewer, ShareModeLive, time.Hour)
	_ = database.RevokeShare(c.ID) // 已結束者不應再被計入
	ids, err := database.RevokeActiveShares("")
	if err != nil || len(ids) != 2 {
		t.Fatalf("ids=%v err=%v", ids, err)
	}
	for _, id := range []int64{a.ID, b.ID} {
		if got, _ := database.GetShare(id); !got.Revoked {
			t.Fatalf("share %d 應已撤銷", id)
		}
	}

	d, _ := database.CreateShare(s.ID, ShareRoleViewer, ShareModeLive, time.Hour)
	if err := database.DeleteSession(s.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := database.GetShare(d.ID); !got.Revoked {
		t.Fatal("刪除 session 後分享應撤銷")
	}
}

func TestSnapshot_MaxIDAndAttachments(t *testing.T) {
	database, s := newShareTestDB(t)
	for _, name := range []string{"f1", "f2"} {
		if err := database.AddAttachment(&Attachment{ID: name, SessionID: s.ID, Name: name, StorageName: name, MimeType: "text/plain", Size: 1}); err != nil {
			t.Fatal(err)
		}
	}
	m1, _ := database.AddUserMessageWithAttachments(s.ID, "one", "", []string{"f1"})
	_ = database.AddMessage(s.ID, "claude", "answer", "")
	snap, err := database.CreateShare(s.ID, ShareRoleViewer, ShareModeSnapshot, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if snap.SnapshotMsgID == nil || *snap.SnapshotMsgID < m1.ID+1 {
		t.Fatalf("snapshot_msg_id=%v", snap.SnapshotMsgID)
	}
	m3, _ := database.AddUserMessageWithAttachments(s.ID, "later", "小明", []string{"f2"})

	msgs, err := database.ListMessagesQuery(MessageQuery{SessionID: s.ID, IncludeResult: true, MaxID: snap.SnapshotMsgID})
	if err != nil || len(msgs) != 2 {
		t.Fatalf("snapshot 應只有 2 則，got %d err=%v", len(msgs), err)
	}
	for _, m := range msgs {
		if m.ID >= m3.ID {
			t.Fatalf("不應讀到 snapshot 之後的訊息 id=%d", m.ID)
		}
	}
	all, _ := database.ListMessages(s.ID)
	if len(all) != 3 || all[2].Author != "小明" {
		t.Fatalf("author 應被保存: %+v", all)
	}

	if ok, _ := database.AttachmentInSnapshot(s.ID, "f1", *snap.SnapshotMsgID); !ok {
		t.Fatal("f1 屬於快照內訊息")
	}
	if ok, _ := database.AttachmentInSnapshot(s.ID, "f2", *snap.SnapshotMsgID); ok {
		t.Fatal("f2 屬於快照之後的訊息，不應可讀")
	}

	// 空 session 的快照：MaxID=0 一則都不給
	empty, _ := database.CreateSession("empty", "", t.TempDir(), "default", "claude", nil, "agent")
	_ = database.AddMessage(empty.ID, "user", "later", "")
	esnap, _ := database.CreateShare(empty.ID, ShareRoleViewer, ShareModeSnapshot, time.Hour)
	_ = database.AddMessage(empty.ID, "user", "after", "")
	got, _ := database.ListMessagesQuery(MessageQuery{SessionID: empty.ID, MaxID: esnap.SnapshotMsgID})
	if len(got) != 1 {
		t.Fatalf("got %d", len(got))
	}
}

func TestQueueAuthorRoundTrip(t *testing.T) {
	database, s := newShareTestDB(t)
	q, err := database.EnqueueMessage(s.ID, "hello", "小明")
	if err != nil || q.Author != "小明" {
		t.Fatalf("q=%+v err=%v", q, err)
	}
	list, _ := database.ListQueuedMessages(s.ID)
	if len(list) != 1 || list[0].Author != "小明" {
		t.Fatalf("list=%+v", list)
	}
	m, err := database.PromoteQueuedMessage(s.ID, list[0])
	if err != nil || m.Author != "小明" {
		t.Fatalf("m=%+v err=%v", m, err)
	}
}

func TestClaimPendingDenials_FirstWins(t *testing.T) {
	database, s := newShareTestDB(t)
	_ = database.UpdatePendingDenials(s.ID, `[{"tool_name":"Write"}]`)
	first, err := database.ClaimPendingDenials(s.ID)
	if err != nil || !first {
		t.Fatalf("first=%v err=%v", first, err)
	}
	if second, _ := database.ClaimPendingDenials(s.ID); second {
		t.Fatal("第二次認領不應成功")
	}
}
