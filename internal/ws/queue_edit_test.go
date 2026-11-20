package ws

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

func TestRoomQueueEditBroadcastWithoutDrain(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/edit.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	pm, err := database.CreateSession("PM", "", t.TempDir(), "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	room, err := database.CreateRoom("room", []db.RoomMember{{SessionID: pm.ID, Role: db.RoomRolePM}})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AcceptRoomInput(room.ID, pm.ID, "request", "原文", nil); err != nil {
		t.Fatal(err)
	}
	q, err := database.ListQueuedMessages(pm.ID)
	if err != nil {
		t.Fatal(err)
	}
	// 未暫停的 queue 也不可因編輯而 Drain。
	before, err := database.GetSession(pm.ID)
	if err != nil {
		t.Fatal(err)
	}
	quotaState, _, _, err := database.GetQuotaResume(pm.ID)
	if err != nil {
		t.Fatal(err)
	}
	events := make(chan serverMsg, 4)
	unsub := hub.Subscribe(pm.ID, func(msg serverMsg) bool { events <- msg; return true })
	defer unsub()
	if err := EditRoomQueuedMessage(context.Background(), database, room.ID, pm.ID, q[0].ID, "原文", "新文"); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-events:
		if event.Type != "queue_update" || len(event.Queue) != 1 || event.Queue[0].Content != "新文" || event.QueuePaused || event.QuotaResumeState == nil || *event.QuotaResumeState != quotaState {
			t.Fatalf("queue_update=%+v", event)
		}
	default:
		t.Fatal("缺少訂閱者queue_update")
	}
	after, err := database.GetSession(pm.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("helper改變了session／quota狀態")
	}
	msgs, err := database.ListMessages(pm.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 || taskIsActive(pm.ID) {
		t.Fatal("編輯啟動／提升了輸入")
	}
	if err := EditRoomQueuedMessage(context.Background(), database, room.ID, pm.ID, q[0].ID, "原文", "衝突"); !errors.Is(err, db.ErrQueueEditConflict) {
		t.Fatal(err)
	}
	select {
	case <-events:
		t.Fatal("失敗不可廣播假成功")
	default:
	}
}

func TestRoomQueueEditSharesRunNowDispatchLock(t *testing.T) {
	x := newRunNowRig(t)
	room, err := x.database.CreateRoom("shared-lock", []db.RoomMember{{SessionID: x.s.ID, Role: db.RoomRolePM}})
	if err != nil {
		t.Fatal(err)
	}
	first := x.plan("hold-cancel")
	x.send("active")
	readyRunNow(t, first)
	if err := x.database.AcceptRoomInput(room.ID, x.s.ID, "room-request", "排隊原文", nil); err != nil {
		t.Fatal(err)
	}
	q := x.queue(t)[0]
	next := x.plan("normal")
	runDone := make(chan struct{})
	go func() { x.d.RunNow(q.ID); close(runDone) }()
	select {
	case <-first.cancelled:
	case <-time.After(time.Second):
		t.Fatal("RunNow未進入持鎖等待舊回合")
	}
	editStarted := make(chan struct{})
	editDone := make(chan error, 1)
	go func() {
		close(editStarted)
		editDone <- EditRoomQueuedMessage(context.Background(), x.database, room.ID, x.s.ID, q.ID, "排隊原文", "不應超車")
	}()
	<-editStarted
	// RunNow 已由 cancelled 事件確認持鎖、並被明確的 finish gate 阻擋；不是等待猜測 runner 進度。
	select {
	case err := <-editDone:
		t.Fatalf("編輯穿過RunNow的dispatchLock：%v", err)
	case <-time.After(40 * time.Millisecond):
	}
	first.release()
	select {
	case <-runDone:
	case <-time.After(time.Second):
		t.Fatal("RunNow未收尾")
	}
	readyRunNow(t, next)
	select {
	case err := <-editDone:
		if !errors.Is(err, db.ErrQueueEditConflict) {
			t.Fatalf("RunNow先提升後編輯應衝突：%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("編輯未在鎖放開後返回")
	}
	next.release()
}
