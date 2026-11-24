package ws

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

// 沿用分享認證、真 WS listener 與已驗的 fake；不直接呼叫 handleMsg 或 RunNow。
func newRunNowWSRig(t *testing.T) (*runNowRig, func(string, string) string) {
	t.Helper()
	database, dial := startShareServer(t)
	s, err := database.CreateSession("run-now-ws", "", t.TempDir(), "default", agent.TypeCodex, nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAgentSessionID(s.ID, "provider-session"); err != nil {
		t.Fatal(err)
	}
	original, err := agent.NewRunner(agent.TypeCodex)
	if err != nil {
		t.Fatal(err)
	}
	r := newDiagnosticRunNowRunner(t)
	agent.Register(agent.TypeCodex, func() agent.Runner { return r })
	t.Cleanup(func() { agent.Register(agent.TypeCodex, func() agent.Runner { return original }) })
	x := &runNowRig{database: database, s: s, r: r}
	gate.mu.Lock()
	baseline := gate.inflight
	gate.mu.Unlock()
	t.Cleanup(func() {
		for _, p := range x.turns {
			p.release()
		}
		taskCancel(s.ID)
		waitFor(t, "WS runner 完整清理", func() bool {
			gate.mu.Lock()
			defer gate.mu.Unlock()
			_, pending := runNowFinishing.Load(s.ID)
			return gate.inflight == baseline && !pending
		})
		if r.overlap.Load() {
			t.Error("WS 觸發同 session runner 重疊")
		}
	})
	return x, dial
}

func runNowUser(text string) func(map[string]any) bool {
	return func(m map[string]any) bool { return m["type"] == "user_message" && m["content"] == text }
}

func TestRunNowWSOwnerAndEditor(t *testing.T) {
	for _, who := range []string{"owner", "editor"} {
		t.Run(who, func(t *testing.T) {
			x, dial := newRunNowWSRig(t)
			owner := dialClient(t, dial(x.s.ID, ""))
			_, ga := newShare(t, x.database, x.s.ID, db.ShareRoleEditor, db.ShareModeLive, "編輯者", time.Hour)
			editor := dialClient(t, dial(x.s.ID, ga.Guest.Token))
			owner.barrier(t)
			editor.barrier(t)
			a := x.plan("block")
			owner.send(t, map[string]any{"type": "input", "data": "A"})
			readyRunNow(t, a)
			owner.send(t, map[string]any{"type": "input", "data": "B"})
			owner.send(t, map[string]any{"type": "input", "data": "C"})
			owner.barrier(t)
			q := x.queue(t)
			if len(q) != 2 {
				t.Fatalf("queue=%+v", q)
			}
			select {
			case <-a.cancelled:
				t.Fatal("普通 input 必須只排隊")
			default:
			}
			c := x.plan("block")
			actor, other := owner, editor
			if who == "editor" {
				actor, other = editor, owner
			}
			actor.send(t, map[string]any{"type": "queue_run_now", "id": q[1].ID})
			actor.barrier(t)
			if actor.count(runNowUser("C")) != 1 {
				cancelled := false
				select {
				case <-a.cancelled:
					cancelled = true
				default:
				}
				t.Fatalf("真 WS queue_run_now 未產生 C 的 user_message：queue=%d，A cancelled=%v", len(x.queue(t)), cancelled)
			}
			readyRunNow(t, c)
			select {
			case <-a.exited:
			default:
				t.Fatal("C 不得早於 A 退出")
			}
			msgs, err := x.database.ListMessages(x.s.ID)
			if err != nil || len(msgs) != 4 || msgs[0].Content != "A" || msgs[1].Content != "partial tail" || msgs[1].Status != db.MessageStatusDone || msgs[2].Content != "C" {
				t.Fatalf("歷史順序／定稿錯誤：%+v，err=%v", msgs, err)
			}
			for _, client := range []*wsClient{owner, editor} {
				client.await(t, "C 的 user_message", runNowUser("C"))
				client.await(t, "僅剩 B 的 queue_update", func(m map[string]any) bool {
					queue, ok := m["queue"].([]any)
					if m["type"] != "queue_update" || !ok || len(queue) != 1 || m["queue_paused"] == true {
						return false
					}
					item, ok := queue[0].(map[string]any)
					return ok && item["id"] == float64(q[0].ID) && item["content"] == "B"
				})
			}
			remaining := x.queue(t)
			if len(remaining) != 1 || remaining[0].ID != q[0].ID {
				t.Fatalf("B 必須保留原位：%+v", remaining)
			}
			if paused, err := x.database.QueuePaused(x.s.ID); err != nil || paused {
				t.Fatalf("paused 必須維持 false：%v %v", paused, err)
			}
			if c.opts.SessionID != "provider-session" || !strings.HasPrefix(c.opts.Prompt, "上一輪回覆已被使用者中途打斷，以下為新指示。\n") {
				t.Fatalf("runner 續接／前綴錯誤：%+v", c.opts)
			}
			// 另一條真 WS 重送同 id：已提升的 C 不可再次啟動，也不可被誤取消。
			before := other.count(isType("error"))
			other.send(t, map[string]any{"type": "queue_run_now", "id": q[1].ID})
			other.barrier(t)
			if other.count(isType("error")) != before+1 {
				t.Fatal("重送已取走 id 應回 error")
			}
			select {
			case <-c.cancelled:
				t.Fatal("另一連線重送不得取消 C")
			default:
			}
			if got := strings.Join(userMessages(x.database, x.s.ID), ","); got != "A,C" {
				t.Fatalf("C 只能提升一次：%s", got)
			}
		})
	}
}

func TestRunNowWSViewerRemainsReadOnly(t *testing.T) {
	x, dial := newRunNowWSRig(t)
	owner := dialClient(t, dial(x.s.ID, ""))
	_, ga := newShare(t, x.database, x.s.ID, db.ShareRoleViewer, db.ShareModeLive, "觀眾", time.Hour)
	viewer := dialClient(t, dial(x.s.ID, ga.Guest.Token))
	owner.barrier(t)
	viewer.barrier(t)
	a := x.plan("block")
	owner.send(t, map[string]any{"type": "input", "data": "A"})
	readyRunNow(t, a)
	owner.send(t, map[string]any{"type": "input", "data": "B"})
	owner.send(t, map[string]any{"type": "input", "data": "C"})
	owner.barrier(t)
	waitFor(t, "viewer 案例穩定 partial", func() bool {
		m, err := x.database.ListMessages(x.s.ID)
		return err == nil && len(m) == 2 && m[1].Content == "partial"
	})
	q := x.queue(t)
	if len(q) != 2 {
		t.Fatalf("queue=%+v", q)
	}
	beforeM, err := x.database.ListMessages(x.s.ID)
	if err != nil {
		t.Fatal(err)
	}
	beforeS, err := x.database.GetSession(x.s.ID)
	if err != nil {
		t.Fatal(err)
	}
	beforeErrors := viewer.count(isType("error"))
	viewer.send(t, map[string]any{"type": "queue_run_now", "id": q[1].ID})
	viewer.barrier(t)
	if viewer.count(isType("error")) != beforeErrors+1 || viewer.first(func(m map[string]any) bool {
		content, _ := m["content"].(string)
		return m["type"] == "error" && strings.Contains(content, "唯讀")
	}) == nil {
		t.Fatal("viewer 必須由 readLoop 回唯讀錯誤")
	}
	afterM, err := x.database.ListMessages(x.s.ID)
	if err != nil {
		t.Fatal(err)
	}
	afterS, err := x.database.GetSession(x.s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(q, x.queue(t)) || !reflect.DeepEqual(beforeM, afterM) || !reflect.DeepEqual(beforeS, afterS) || !taskIsActive(x.s.ID) {
		t.Error("viewer 指令不得改 queue/history/session/paused/任務")
	}
	select {
	case <-a.cancelled:
		t.Error("viewer 不得取消 A")
	default:
	}
	if owner.count(runNowUser("C")) != 0 || viewer.count(runNowUser("C")) != 0 {
		t.Error("viewer 指令不得提升 C 或廣播新 user_message")
	}
}

func TestRunNowWSRejectsInvalidAndForeignIDs(t *testing.T) {
	x, dial := newRunNowWSRig(t)
	owner := dialClient(t, dial(x.s.ID, ""))
	_, ga := newShare(t, x.database, x.s.ID, db.ShareRoleEditor, db.ShareModeLive, "編輯者", time.Hour)
	editor := dialClient(t, dial(x.s.ID, ga.Guest.Token))
	owner.barrier(t)
	editor.barrier(t)
	a := x.plan("block")
	owner.send(t, map[string]any{"type": "input", "data": "A"})
	readyRunNow(t, a)
	owner.send(t, map[string]any{"type": "input", "data": "C"})
	owner.barrier(t)
	waitFor(t, "id 拒絕案例穩定 partial", func() bool {
		m, err := x.database.ListMessages(x.s.ID)
		return err == nil && len(m) == 2 && m[1].Content == "partial"
	})
	other, err := x.database.CreateSession("foreign", "", "", "default", agent.TypeCodex, nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := x.database.EnqueueMessage(other.ID, "foreign C", "")
	if err != nil {
		t.Fatal(err)
	}
	q := x.queue(t)
	beforeM, err := x.database.ListMessages(x.s.ID)
	if err != nil {
		t.Fatal(err)
	}
	beforeS, err := x.database.GetSession(x.s.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		id   int64
	}{{"zero", 0}, {"negative", -1}, {"foreign-session", foreign.ID}} {
		t.Run(tc.name, func(t *testing.T) {
			before := editor.count(isType("error"))
			editor.send(t, map[string]any{"type": "queue_run_now", "id": tc.id})
			editor.barrier(t)
			if editor.count(isType("error")) != before+1 {
				t.Error("非法／跨 session id 必須回 error，不能吞成成功")
			}
			afterM, err := x.database.ListMessages(x.s.ID)
			if err != nil {
				t.Fatal(err)
			}
			afterS, err := x.database.GetSession(x.s.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(q, x.queue(t)) || !reflect.DeepEqual(beforeM, afterM) || !reflect.DeepEqual(beforeS, afterS) || !taskIsActive(x.s.ID) {
				t.Error("id 拒絕不得改自己的 queue/history/session/paused/任務")
			}
			select {
			case <-a.cancelled:
				t.Error("錯誤 id 不得取消 A")
			default:
			}
			foreignQueue, err := x.database.ListQueuedMessages(other.ID)
			if err != nil || len(foreignQueue) != 1 || foreignQueue[0].ID != foreign.ID {
				t.Fatalf("其他 session 佇列被改變：%+v %v", foreignQueue, err)
			}
			if owner.count(runNowUser("C")) != 0 || editor.count(runNowUser("C")) != 0 {
				t.Error("拒絕不能廣播 user_message")
			}
		})
	}
}
