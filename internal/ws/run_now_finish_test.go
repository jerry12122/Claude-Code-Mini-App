package ws

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/tg"
)

type finishRunner struct {
	mode    string
	started chan struct{}
	finish  chan struct{}
	exited  chan struct{}
}

func (*finishRunner) Name() string { return "finish-test" }
func (r *finishRunner) Run(ctx context.Context, _ agent.RunOptions, cb agent.EventCallback) error {
	defer close(r.exited)
	cb(agent.Event{Type: agent.EventStreamStart})
	cb(agent.Event{Type: agent.EventDelta, Text: "partial"})
	if r.mode == "linger" {
		cb(agent.Event{Type: agent.EventDone})
	}
	close(r.started)
	if r.mode == "success" || r.mode == "finish-race" || r.mode == "failure" {
		<-r.finish
		if r.mode == "failure" {
			return errors.New("finish-test failure")
		}
		cb(agent.Event{Type: agent.EventDone})
		return nil
	}
	<-ctx.Done()
	<-r.finish
	if r.mode == "linger" {
		return ctx.Err()
	}
	cb(agent.Event{Type: agent.EventDelta, Text: " tail"})
	cb(agent.Event{Type: agent.EventDone, ResultText: "salvaged"})
	if r.mode == "cancel-exit0" {
		return nil
	}
	return ctx.Err()
}

type finishTransport func(*http.Request) (*http.Response, error)

func (f finishTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRunNowFinish(t *testing.T) {
	for _, tc := range []struct {
		name, mode  string
		interjected bool
	}{
		{"interjected-cancel", "cancel", true},
		{"interjected-exit0", "cancel-exit0", true},
		{"interjected-lingering", "linger", true},
		{"interjected-natural-finish-race", "finish-race", true},
		{"ordinary-stop", "cancel", false},
		{"ordinary-failure", "failure", false},
		{"natural-success", "success", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database, err := db.Open(t.TempDir() + "/finish.db")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { database.Close() })
			r := &finishRunner{mode: tc.mode, started: make(chan struct{}), finish: make(chan struct{}), exited: make(chan struct{})}
			agent.Register("finish-test", func() agent.Runner { return r })
			s, err := database.CreateSession(tc.name, "", "", "default", "finish-test", nil, "agent")
			if err != nil {
				t.Fatal(err)
			}
			alerts := make(chan string, 8)
			transport := http.DefaultTransport
			http.DefaultTransport = finishTransport(func(req *http.Request) (*http.Response, error) {
				body, _ := io.ReadAll(req.Body)
				form, _ := url.ParseQuery(string(body))
				alerts <- form.Get("text")
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":true}`)), Header: make(http.Header)}, nil
			})
			t.Cleanup(func() { http.DefaultTransport = transport })
			var idleBroadcast atomic.Bool
			bodyEnded := make(chan struct{})
			resumeBody := make(chan struct{})
			var resumeOnce sync.Once
			resume := func() { resumeOnce.Do(func() { close(resumeBody) }) }
			unsub := hub.Subscribe(s.ID, func(m serverMsg) bool {
				if m.Type == "status" && m.Value == StateIdle {
					idleBroadcast.Store(true)
				}
				if tc.mode == "finish-race" && m.Type == "lingering" && m.Value == "off" {
					close(bodyEnded)
					<-resumeBody
				}
				return true
			})
			t.Cleanup(unsub)
			gate.mu.Lock()
			initialInflight := gate.inflight
			gate.mu.Unlock()
			lock := dispatchLock(s.ID)
			locked := false
			t.Cleanup(func() {
				if locked {
					lock.Unlock()
				}
				resume()
				taskCancel(s.ID)
				select {
				case <-r.finish:
				default:
					close(r.finish)
				}
				waitFor(t, "所有收尾 defer 結束", func() bool {
					gate.mu.Lock()
					defer gate.mu.Unlock()
					return gate.inflight == initialInflight
				})
			})
			var starts atomic.Int32
			d := &sessionDispatcher{database: database, sessionID: s.ID, broadcast: func(serverMsg) {}, broadcastQueue: func() {},
				start: func(_ clientMsg, q *db.QueuedMessage) error {
					starts.Add(1)
					_, err := database.PromoteQueuedMessage(s.ID, *q)
					return err
				}}
			drainEntered, drainReturned := make(chan struct{}), make(chan struct{})
			h := &wsConn{database: database, sessionID: s.ID, sess: s, agentType: "finish-test", botToken: "test-only", tgUserID: 123,
				notifyCfg: tg.NotifyConfig{OnCancel: true}, drainIfIdle: func() {
					close(drainEntered)
					d.Drain()
					close(drainReturned)
				}}
			h.runAgent("old turn", nil, 0, nil)
			select {
			case <-r.started:
			case <-time.After(time.Second):
				t.Fatal("fake runner 未啟動")
			}
			taskManager.mu.Lock()
			entry := taskManager.tasks[s.ID]
			taskManager.mu.Unlock()
			if entry == nil || entry.done == nil {
				t.Fatal("舊任務應有 done")
			}
			t.Cleanup(func() { interjectedTasks.Delete(entry.msgID) })
			if _, err := database.EnqueueMessage(s.ID, "next turn", ""); err != nil {
				t.Fatal(err)
			}
			lock.Lock()
			locked = true
			done := (<-chan struct{})(entry.done)
			select {
			case <-done:
				t.Fatal("runner 尚未返回，包含 lingering 狀態，都不能提早關閉 done")
			default:
			}
			if tc.mode == "finish-race" {
				close(r.finish)
				select {
				case <-bodyEnded:
				case <-time.After(time.Second):
					t.Fatal("自然完成尚未到收尾交會點")
				}
			}
			if tc.interjected {
				var ok bool
				done, ok = taskInterruptForRunNow(s.ID)
				if !ok || done != entry.done {
					t.Fatal("打斷必須保留舊任務 done")
				}
			} else if tc.mode == "cancel" {
				if !taskInterrupt(s.ID) {
					t.Fatal("一般停止應中斷舊任務")
				}
			} else {
				close(r.finish)
			}
			resume()
			if tc.mode == "cancel" || tc.mode == "cancel-exit0" || tc.mode == "linger" {
				select {
				case <-done:
					t.Error("取消時 runner 仍被退出閘門擋住，不得提早關 done")
				default:
				}
				close(r.finish)
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("持 dispatchLock 等待 done 不得卡在 Drain 或漏關通知")
			}
			select {
			case <-r.exited:
			default:
				t.Error("done 不得早於 runner 退出")
			}
			msgs, err := database.ListMessages(s.ID)
			if err != nil || len(msgs) != 1 || msgs[0].Status != db.MessageStatusDone {
				t.Fatalf("done 前應完成舊訊息定稿：%+v，err=%v", msgs, err)
			}
			want := "partial tail"
			if tc.mode == "success" || tc.mode == "finish-race" || tc.mode == "linger" || tc.mode == "failure" {
				want = "partial"
			}
			if msgs[0].Content != want {
				t.Errorf("取消後 partial 應完整保存：%q，預期 %q", msgs[0].Content, want)
			}
			current, err := database.GetSession(s.ID)
			if err != nil || current.Status != db.SessionStatusIdle || !idleBroadcast.Load() {
				t.Fatalf("done 前應寫回並廣播 idle：%+v，err=%v", current, err)
			}
			if tc.mode == "success" {
				select {
				case <-drainEntered:
				case <-time.After(time.Second):
					t.Fatal("自然成功應進入 Drain")
				}
				select {
				case <-drainReturned:
					t.Fatal("dispatchLock 仍持有，Drain 不得已返回")
				default:
				}
				if lateDone, ok := taskInterruptForRunNow(s.ID); ok || lateDone != nil {
					t.Error("自然收尾後不得再誤標已完成的回合")
				}
			}
			lock.Unlock()
			locked = false
			waitFor(t, "回合完整退出", func() bool {
				gate.mu.Lock()
				defer gate.mu.Unlock()
				return gate.inflight == initialInflight
			})
			paused, err := database.QueuePaused(s.ID)
			if err != nil || paused != (!tc.interjected && tc.mode != "success") {
				t.Errorf("佇列暫停=%v，err=%v", paused, err)
			}
			wantQueue := 1
			if tc.mode == "success" {
				wantQueue = 0
			}
			if got := queueLen(database, s.ID); got != wantQueue {
				t.Errorf("佇列數=%d，預期 %d", got, wantQueue)
			}
			if got := starts.Load(); got != int32(1-wantQueue) {
				t.Errorf("下一回合啟動次數=%d，預期 %d", got, 1-wantQueue)
			}
			if tc.interjected {
				if _, ok := interjectedTasks.LoadAndDelete(entry.msgID); ok {
					t.Error("runAgent 應已按舊 msgID 消耗標記")
				}
				select {
				case <-drainEntered:
					t.Error("interjected 不應自行 Drain")
				default:
				}
			}
			cancelNotice := false
			timer := time.NewTimer(100 * time.Millisecond)
			defer timer.Stop()
		loop:
			for {
				select {
				case text := <-alerts:
					cancelNotice = cancelNotice || strings.Contains(text, "任務已中斷")
				case <-timer.C:
					break loop
				}
			}
			if cancelNotice != (!tc.interjected && tc.mode == "cancel") {
				t.Errorf("取消通知=%v，interjected=%v", cancelNotice, tc.interjected)
			}
		})
	}
}

func TestTaskFinishPreservesNewerTask(t *testing.T) {
	const sid = "finish-identity"
	t.Cleanup(func() {
		taskCancel(sid)
		interjectedTasks.Delete(int64(601))
		interjectedTasks.Delete(int64(602))
	})
	oldDone := taskStartWithDone(sid, nil, 601)
	got, ok := taskInterruptForRunNow(sid)
	if !ok || got != oldDone {
		t.Fatal("登記時取得的 done 應穩定屬於舊任務")
	}
	newDone := taskStartWithDone(sid, nil, 602)
	if !taskFinish(sid, 601) || taskFinish(sid, 601) {
		t.Fatal("收尾只能消耗一次舊 msgID 的標記")
	}
	if !taskIsActive(sid) || oldDone == newDone {
		t.Fatal("舊回合收尾不得移除新回合或共用 done")
	}
	if taskFinish(sid, 602) || taskIsActive(sid) {
		t.Fatal("一般自然收尾應移除自身登記且沒有 interjected 標記")
	}
	if done, ok := taskInterruptForRunNow(sid); ok || done != nil {
		t.Fatal("已完成回合不得新增晚到標記")
	}
}

func TestRunNowFinishRunnerSetupErrorClosesDone(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/setup.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	s, err := database.CreateSession("unknown-runner", "", "", "default", "unknown-finish-test", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	var entry *taskEntry
	unsub := hub.Subscribe(s.ID, func(m serverMsg) bool {
		if m.Type == "status" && m.Value == StateThinking {
			taskManager.mu.Lock()
			entry = taskManager.tasks[s.ID]
			taskManager.mu.Unlock()
		}
		return true
	})
	defer unsub()
	h := &wsConn{database: database, sessionID: s.ID, sess: s, agentType: s.AgentType}
	h.runAgent("setup failure", nil, 0, nil)
	if entry == nil {
		t.Fatal("應捕獲任務登記")
	}
	select {
	case <-entry.done:
	default:
		t.Fatal("runner 建立失敗也必須關閉 done")
	}
	current, err := database.GetSession(s.ID)
	msgs, msgErr := database.ListMessages(s.ID)
	if err != nil || msgErr != nil || current.Status != db.SessionStatusIdle || len(msgs) != 1 || msgs[0].Status != db.MessageStatusDone || taskIsActive(s.ID) {
		t.Fatalf("建立失敗應定稿並回到 idle：session=%+v messages=%+v err=%v/%v", current, msgs, err, msgErr)
	}
}
