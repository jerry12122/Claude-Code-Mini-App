package ws

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/media"
)

type runNowTurn struct {
	mode                             string
	opts                             agent.RunOptions
	ready, finish, cancelled, exited chan struct{}
	once                             sync.Once
}

func (r *runNowTurn) release() { r.once.Do(func() { close(r.finish) }) }

type runNowRunner struct {
	plans   chan *runNowTurn
	active  atomic.Int32
	overlap atomic.Bool
}

func (*runNowRunner) Name() string { return "run-now-fake-codex" }
func (r *runNowRunner) Run(ctx context.Context, opts agent.RunOptions, cb agent.EventCallback) error {
	var p *runNowTurn
	select {
	case p = <-r.plans:
	case <-ctx.Done():
		return ctx.Err()
	}
	if r.active.Add(1) != 1 {
		r.overlap.Store(true)
	}
	defer func() { r.active.Add(-1); close(p.exited) }()
	p.opts = opts
	cb(agent.Event{Type: agent.EventStreamStart})
	cb(agent.Event{Type: agent.EventDelta, Text: "partial"})
	if p.mode == "linger" {
		cb(agent.Event{Type: agent.EventDone})
	}
	close(p.ready)
	select {
	case <-p.finish:
		if p.mode != "linger" {
			cb(agent.Event{Type: agent.EventDone})
		}
		return nil
	case <-ctx.Done():
		close(p.cancelled)
		if p.mode == "hold-cancel" {
			<-p.finish
		}
		if p.mode != "linger" {
			cb(agent.Event{Type: agent.EventDelta, Text: " tail"})
		}
		return ctx.Err()
	}
}

type runNowRig struct {
	database *db.DB
	s        *db.Session
	h        *wsConn
	d        *sessionDispatcher
	r        *runNowRunner
	turns    []*runNowTurn
	replies  chan serverMsg
}

func newRunNowRig(t *testing.T, sessionTypes ...string) *runNowRig {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/run-now.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	sessionType := agent.TypeCodex
	if len(sessionTypes) > 0 {
		sessionType = sessionTypes[0]
	}
	s, err := database.CreateSessionWithInstructions("run-now", "", t.TempDir(), "default", sessionType, nil, "agent", "", "native note", db.GlobalInstruction{Text: "global note", Mode: db.GlobalInstructionEvery})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAgentSessionID(s.ID, "provider-session"); err != nil {
		t.Fatal(err)
	}
	s.AgentSessionID = "provider-session"
	original, err := agent.NewRunner(agent.TypeCodex)
	if err != nil {
		t.Fatal(err)
	}
	r := &runNowRunner{plans: make(chan *runNowTurn, 16)}
	agent.Register(agent.TypeCodex, func() agent.Runner { return r })
	t.Cleanup(func() { agent.Register(agent.TypeCodex, func() agent.Runner { return original }) })
	x := &runNowRig{database: database, s: s, r: r, replies: make(chan serverMsg, 100)}
	x.h = &wsConn{database: database, sessionID: s.ID, sess: s, agentType: agent.TypeCodex, agentSessionID: s.AgentSessionID, author: "Alice"}
	x.d = &sessionDispatcher{database: database, sessionID: s.ID, actor: "owner", author: "Alice", start: x.h.startInput,
		reply: func(m serverMsg) bool { x.replies <- m; return true }, broadcast: x.h.broadcast, broadcastQueue: x.h.broadcastQueue}
	x.h.dispatcher = x.d
	x.h.drainIfIdle = x.d.Drain
	gate.mu.Lock()
	baseline := gate.inflight
	gate.mu.Unlock()
	t.Cleanup(func() {
		for _, p := range x.turns {
			p.release()
		}
		taskCancel(s.ID)
		waitFor(t, "run-now 回合與 defer 清理", func() bool {
			gate.mu.Lock()
			defer gate.mu.Unlock()
			_, pending := runNowFinishing.Load(s.ID)
			return gate.inflight == baseline && !pending
		})
		if r.overlap.Load() {
			t.Error("同一 provider session 的 runner 曾重疊")
		}
	})
	return x
}

func (x *runNowRig) plan(mode string) *runNowTurn {
	p := &runNowTurn{mode: mode, ready: make(chan struct{}), finish: make(chan struct{}), cancelled: make(chan struct{}), exited: make(chan struct{})}
	x.turns = append(x.turns, p)
	x.r.plans <- p
	return p
}

func readyRunNow(t *testing.T, p *runNowTurn) {
	t.Helper()
	select {
	case <-p.ready:
	case <-time.After(time.Second):
		t.Fatal("指定 fake runner 未啟動")
	}
}

func (x *runNowRig) send(text string, ids ...string) {
	x.d.Dispatch(clientMsg{Data: text, RequestID: "request-" + text, AttachmentIDs: ids})
}

func (x *runNowRig) queue(t *testing.T) []db.QueuedMessage {
	t.Helper()
	q, err := x.database.ListQueuedMessages(x.s.ID)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func (x *runNowRig) file(t *testing.T) string {
	t.Helper()
	old := media.WorkspaceDir
	media.WorkspaceDir = t.TempDir()
	t.Cleanup(func() { media.WorkspaceDir = old })
	p, err := media.SaveUpload(x.s.ID, "report.txt", strings.NewReader("report"))
	if err != nil {
		t.Fatal(err)
	}
	if err := x.database.AddAttachment(&db.Attachment{ID: "run-now-file", SessionID: x.s.ID, Name: "report.txt", StorageName: filepath.Base(p), MimeType: "text/plain", Size: 6}); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRunNowBasic(t *testing.T) {
	x := newRunNowRig(t)
	attachment := x.file(t)
	a := x.plan("block")
	x.send("A")
	readyRunNow(t, a)
	x.send("B")
	x.send("C", "run-now-file")
	q := x.queue(t)
	if len(q) != 2 {
		t.Fatalf("queue=%+v", q)
	}
	select {
	case <-a.cancelled:
		t.Fatal("普通送出不得打斷")
	default:
	}
	c, b := x.plan("block"), x.plan("block")
	x.d.RunNow(q[1].ID)
	readyRunNow(t, c)
	select {
	case <-a.exited:
	default:
		t.Fatal("新回合早於舊 runner 退出")
	}
	msgs, err := x.database.ListMessages(x.s.ID)
	if err != nil || len(msgs) != 4 || msgs[0].Content != "A" || msgs[1].Content != "partial tail" || msgs[1].Status != db.MessageStatusDone || msgs[2].Content != "C" || len(msgs[2].Attachments) != 1 {
		t.Fatalf("歷史順序/定稿/附件錯誤：%+v err=%v", msgs, err)
	}
	remaining := x.queue(t)
	if len(remaining) != 1 || remaining[0].ID != q[0].ID {
		t.Fatalf("B 原位置應保留：%+v", remaining)
	}
	if paused, err := x.database.QueuePaused(x.s.ID); err != nil || paused {
		t.Fatalf("不應暫停：%v %v", paused, err)
	}
	if c.opts.SessionID != "provider-session" || !strings.Contains(c.opts.Prompt, attachment) {
		t.Fatalf("provider session 或附件遺失：%+v", c.opts)
	}
	c.release()
	readyRunNow(t, b)
	b.release()
	waitFor(t, "剩餘 B 續跑", func() bool { return !taskIsActive(x.s.ID) && queueLen(x.database, x.s.ID) == 0 })
	if got := strings.Join(userMessages(x.database, x.s.ID), ","); got != "A,C,B" {
		t.Fatalf("使用者順序=%s", got)
	}
}

func TestRunNowTimeoutGuardsDispatchAndRetry(t *testing.T) {
	x := newRunNowRig(t)
	x.d.runNowWait = 20 * time.Millisecond
	a := x.plan("hold-cancel")
	x.send("A")
	readyRunNow(t, a)
	x.send("C")
	q := x.queue(t)
	c, d := x.plan("block"), x.plan("block")
	returned := make(chan struct{})
	go func() { x.d.RunNow(q[0].ID); close(returned) }()
	select {
	case <-returned:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("短等待上限到期後應回傳，不能無限等舊回合")
	}
	select {
	case <-c.ready:
		t.Fatal("逾時不得啟動 C")
	default:
	}
	select {
	case <-a.exited:
		t.Fatal("本例舊 runner 應仍在收尾")
	default:
	}
	if len(x.queue(t)) != 1 {
		t.Fatal("逾時不得取走 C")
	}
	x.send("D")
	second := *x.d
	second.RunNow(q[0].ID)
	select {
	case <-c.ready:
		t.Fatal("逾時後 Dispatch／再次 RunNow 不得重疊 resume")
	default:
	}
	if len(x.queue(t)) != 2 || len(userMessages(x.database, x.s.ID)) != 1 {
		t.Fatal("逾時後只能排隊，不得寫入新回合歷史")
	}
	errors := 0
	for len(x.replies) > 0 {
		m := <-x.replies
		if m.Type == "error" {
			errors++
			if !strings.Contains(m.Content, "仍在收尾") {
				t.Errorf("逾時訊息不得宣稱已回復：%q", m.Content)
			}
		}
	}
	if errors != 2 {
		t.Fatalf("兩次逾時應各回 error，got=%d", errors)
	}
	a.release()
	select {
	case <-a.exited:
	case <-time.After(time.Second):
		t.Fatal("舊 runner 未退出")
	}
	waitFor(t, "逾時保護自動清理", func() bool { _, ok := runNowFinishing.Load(x.s.ID); return !ok })
	x.d.RunNow(q[0].ID)
	readyRunNow(t, c)
	c.release()
	readyRunNow(t, d)
	d.release()
	waitFor(t, "timeout 後續正常完成", func() bool { return !taskIsActive(x.s.ID) && queueLen(x.database, x.s.ID) == 0 })
}

func TestRunNowIdlePausedAndLingering(t *testing.T) {
	for _, mode := range []string{"idle", "block", "linger"} {
		t.Run(mode, func(t *testing.T) {
			x := newRunNowRig(t)
			var a *runNowTurn
			if mode != "idle" {
				a = x.plan(mode)
				x.send("A")
				readyRunNow(t, a)
			}
			b, err := x.database.EnqueueMessage(x.s.ID, "B", "")
			if err != nil {
				t.Fatal(err)
			}
			c, err := x.database.EnqueueMessage(x.s.ID, "C", "")
			if err != nil {
				t.Fatal(err)
			}
			if err := x.database.SetQueuePaused(x.s.ID, true); err != nil {
				t.Fatal(err)
			}
			next := x.plan("block")
			x.d.RunNow(c.ID)
			readyRunNow(t, next)
			if a != nil {
				select {
				case <-a.exited:
				default:
					t.Fatal("lingering／舊回合應先退出")
				}
			}
			next.release()
			waitFor(t, "指定一筆完成", func() bool { return !taskIsActive(x.s.ID) })
			q := x.queue(t)
			paused, err := x.database.QueuePaused(x.s.ID)
			if err != nil || !paused || len(q) != 1 || q[0].ID != b.ID {
				t.Fatalf("原 paused 與 B 應保留：%v %+v %v", paused, q, err)
			}
		})
	}
}

func TestRunNowNaturalFinishAndTwoConnections(t *testing.T) {
	for _, natural := range []bool{false, true} {
		t.Run(map[bool]string{false: "two-connections", true: "natural-finish"}[natural], func(t *testing.T) {
			x := newRunNowRig(t)
			a := x.plan("block")
			x.send("A")
			readyRunNow(t, a)
			x.send("C")
			q := x.queue(t)
			c := x.plan("block")
			if natural {
				lock := dispatchLock(x.s.ID)
				lock.Lock()
				a.release()
				waitFor(t, "自然收尾解除登記", func() bool { return !taskIsActive(x.s.ID) })
				lock.Unlock()
			}
			second := *x.d
			var calls sync.WaitGroup
			calls.Add(2)
			go func() { defer calls.Done(); x.d.RunNow(q[0].ID) }()
			go func() { defer calls.Done(); second.RunNow(q[0].ID) }()
			returned := make(chan struct{})
			go func() { calls.Wait(); close(returned) }()
			select {
			case <-returned:
			case <-time.After(time.Second):
				t.Fatal("重試／自然完成交會不得互鎖")
			}
			readyRunNow(t, c)
			select {
			case <-c.cancelled:
				t.Fatal("第二連線不得取消已啟動的指定回合")
			default:
			}
			if got := strings.Join(userMessages(x.database, x.s.ID), ","); got != "A,C" {
				t.Fatalf("只能執行一次：%s", got)
			}
			c.release()
		})
	}
}

func TestRunNowRejectsWithoutSideEffects(t *testing.T) {
	for _, name := range []string{"type:kiro", "type:kiroacp", "type:antigravity", "type:gemini", "type:telegram", "type:unknown", "permission", "question", "denials", "shell-running", "shell-confirm-db", "shell-confirm-memory", "maintenance", "missing-id", "foreign-id", "missing-file"} {
		t.Run(name, func(t *testing.T) {
			typeName := agent.TypeCodex
			if strings.HasPrefix(name, "type:") {
				typeName = strings.TrimPrefix(name, "type:")
			}
			x := newRunNowRig(t, typeName)
			a := x.plan("block")
			x.send("A")
			readyRunNow(t, a)
			x.send("C")
			q := x.queue(t)
			id := q[0].ID
			waitFor(t, "穩定 partial 供拒絕副作用比對", func() bool {
				msgs, err := x.database.ListMessages(x.s.ID)
				return err == nil && len(msgs) == 2 && msgs[1].Content == "partial"
			})
			if err := x.database.SetQueuePaused(x.s.ID, true); err != nil {
				t.Fatal(err)
			}
			switch name {
			case "permission":
				e := &pendingPermEntry{ch: make(chan bool, 1)}
				permSet(x.s.ID, e)
				t.Cleanup(func() { permClear(x.s.ID, e) })
			case "question":
				pendingQuestions.Lock()
				pendingQuestions.m[x.s.ID] = &pendingQuestion{ctx: context.Background(), RequestID: "still-waiting"}
				pendingQuestions.Unlock()
				t.Cleanup(func() { pendingQuestions.Lock(); delete(pendingQuestions.m, x.s.ID); pendingQuestions.Unlock() })
			case "denials":
				if err := x.database.UpdatePendingDenials(x.s.ID, `[{"tool_name":"Write"}]`); err != nil {
					t.Fatal(err)
				}
			case "shell-running":
				_, cancel := context.WithCancel(context.Background())
				shellTaskStart(x.s.ID, cancel, 12345)
				t.Cleanup(func() { shellTaskCancel(x.s.ID) })
			case "shell-confirm-db":
				if err := x.database.UpdateShellPending(x.s.ID, `{"line":"ls"}`); err != nil {
					t.Fatal(err)
				}
			case "shell-confirm-memory":
				setShellPending(x.s.ID, &shellPendingInfo{Command: "ls"})
				t.Cleanup(func() { clearInMemoryShellApproval(x.s.ID) })
			case "maintenance":
				gate.mu.Lock()
				gate.maint = true
				gate.mu.Unlock()
				t.Cleanup(func() { gate.mu.Lock(); gate.maint = false; gate.mu.Unlock() })
			case "missing-id":
				id = -1
			case "foreign-id":
				s, err := x.database.CreateSession("other", "", "", "default", agent.TypeCodex, nil, "agent")
				if err != nil {
					t.Fatal(err)
				}
				other, err := x.database.EnqueueMessage(s.ID, "foreign", "")
				if err != nil {
					t.Fatal(err)
				}
				id = other.ID
			case "missing-file":
				x.file(t)
				bad, err := x.database.EnqueueMessage(x.s.ID, "unavailable file", "", "run-now-file")
				if err != nil {
					t.Fatal(err)
				}
				// 只移除本例暫存上傳檔，佇列/附件記錄仍保留。
				files, err := x.database.ResolveAttachments(x.s.ID, []string{"run-now-file"})
				if err != nil {
					t.Fatal(err)
				}
				p, err := media.ExistingUploadPath(x.s.ID, files[0].StorageName)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(p); err != nil {
					t.Fatal(err)
				}
				id = bad.ID
			}
			beforeQ := x.queue(t)
			beforeM, err := x.database.ListMessages(x.s.ID)
			if err != nil {
				t.Fatal(err)
			}
			beforeS, err := x.database.GetSession(x.s.ID)
			if err != nil {
				t.Fatal(err)
			}
			beforePerm, _ := permPending(x.s.ID)
			beforeQuestion := questionPending(x.s.ID)
			beforeShell := peekShellPending(x.s.ID)
			beforeShellTask := shellTaskActive(x.s.ID)
			for len(x.replies) > 0 {
				<-x.replies
			}
			x.plan("block")
			x.d.RunNow(id)
			select {
			case <-a.cancelled:
				t.Error("拒絕前提存在時不得取消舊任務")
			default:
			}
			afterM, err := x.database.ListMessages(x.s.ID)
			if err != nil {
				t.Fatal(err)
			}
			afterS, err := x.database.GetSession(x.s.ID)
			if err != nil {
				t.Fatal(err)
			}
			afterPerm, _ := permPending(x.s.ID)
			if !reflect.DeepEqual(beforeQ, x.queue(t)) || !reflect.DeepEqual(beforeM, afterM) || !reflect.DeepEqual(beforeS, afterS) || !taskIsActive(x.s.ID) {
				t.Error("拒絕不得改變 queue/history/session/paused/任務")
			}
			if !reflect.DeepEqual(beforePerm, afterPerm) || beforeQuestion != questionPending(x.s.ID) || beforeShell != peekShellPending(x.s.ID) || beforeShellTask != shellTaskActive(x.s.ID) {
				t.Error("拒絕不得清除授權、問題或 Shell 等待狀態")
			}
			if len(x.replies) != 1 {
				t.Fatalf("應回一則拒絕，got=%d", len(x.replies))
			}
			m := <-x.replies
			if m.Type != "error" || (name == "maintenance" && m.Code != "update_in_progress") {
				t.Fatalf("拒絕訊息=%+v", m)
			}
		})
	}
}

func TestRunNowRechecksAfterWait(t *testing.T) {
	for _, state := range []string{"permission", "question", "denials", "shell", "removed", "new-task"} {
		t.Run(state, func(t *testing.T) {
			x := newRunNowRig(t)
			a := x.plan("hold-cancel")
			x.send("A")
			readyRunNow(t, a)
			x.send("C")
			q := x.queue(t)
			next := x.plan("block")
			for len(x.replies) > 0 {
				<-x.replies
			}
			returned := make(chan struct{})
			go func() { x.d.RunNow(q[0].ID); close(returned) }()
			select {
			case <-a.cancelled:
			case <-time.After(time.Second):
				t.Fatal("應先開始取消舊回合")
			}
			switch state {
			case "denials":
				if err := x.database.UpdatePendingDenials(x.s.ID, `[{"tool_name":"Write"}]`); err != nil {
					t.Fatal(err)
				}
			case "permission":
				e := &pendingPermEntry{ch: make(chan bool, 1)}
				permSet(x.s.ID, e)
				t.Cleanup(func() { permClear(x.s.ID, e) })
			case "question":
				pendingQuestions.Lock()
				pendingQuestions.m[x.s.ID] = &pendingQuestion{RequestID: "late"}
				pendingQuestions.Unlock()
				t.Cleanup(func() { pendingQuestions.Lock(); delete(pendingQuestions.m, x.s.ID); pendingQuestions.Unlock() })
			case "shell":
				setShellPending(x.s.ID, &shellPendingInfo{Command: "late shell"})
				t.Cleanup(func() { clearInMemoryShellApproval(x.s.ID) })
			case "removed":
				if err := x.database.DeleteQueuedMessage(x.s.ID, q[0].ID); err != nil {
					t.Fatal(err)
				}
			case "new-task":
				taskStart(x.s.ID, nil, 900001)
			}
			a.release()
			select {
			case <-returned:
			case <-time.After(time.Second):
				t.Fatal("等待後重驗應能回傳")
			}
			select {
			case <-next.ready:
				t.Fatal("等待期間新增的拒絕狀態不得被忽略")
			default:
			}
			if got := strings.Join(userMessages(x.database, x.s.ID), ","); got != "A" {
				t.Fatalf("不得提升 C：%s", got)
			}
			if len(x.replies) != 1 || (<-x.replies).Type != "error" {
				t.Fatal("應回傳等待後的拒絕")
			}
			if state == "new-task" && !taskIsActive(x.s.ID) {
				t.Fatal("不得取消另一來源的新任務")
			}
		})
	}
}

func TestRunNowPromptOnlyAfterInterruption(t *testing.T) {
	for _, interrupt := range []bool{false, true} {
		t.Run(map[bool]string{false: "idle", true: "interrupted"}[interrupt], func(t *testing.T) {
			x := newRunNowRig(t)
			path := x.file(t)
			usePMSkill(t, "KEEP-PM-CORE")
			if _, err := x.database.CreateRoom("pm", []db.RoomMember{{SessionID: x.s.ID, Role: db.RoomRolePM}}); err != nil {
				t.Fatal(err)
			}
			if interrupt {
				old := x.plan("block")
				x.send("A")
				readyRunNow(t, old)
			}
			// 經既有 receipt 接受 C；空閒時先暫存為佇列，避免普通 Dispatch 立即啟動。
			if _, err := x.database.AcceptInputCancelQuota(x.s.ID, "owner", "request-C", "C", "Alice", []string{"run-now-file"}); err != nil {
				t.Fatal(err)
			}
			q := x.queue(t)
			next := x.plan("block")
			users := make(chan serverMsg, 10)
			unsub := hub.Subscribe(x.s.ID, func(m serverMsg) bool {
				if m.Type == "user_message" {
					users <- m
				}
				return true
			})
			defer unsub()
			x.d.RunNow(q[0].ID)
			readyRunNow(t, next)
			const notice = "上一輪回覆已被使用者中途打斷，以下為新指示。\n"
			if strings.HasPrefix(next.opts.Prompt, notice) != interrupt {
				t.Errorf("prompt 最前面只在真的中斷時加固定一行：%q", next.opts.Prompt)
			}
			for _, want := range []string{"KEEP-PM-CORE", "PM短規則", "global note", "[Alice]", path, "C"} {
				if !strings.Contains(next.opts.Prompt, want) {
					t.Errorf("prompt 缺少 %q：%q", want, next.opts.Prompt)
				}
			}
			if next.opts.SystemInstruction != "native note" {
				t.Errorf("原生指令=%q", next.opts.SystemInstruction)
			}
			select {
			case m := <-users:
				if m.Content != "C" || len(m.Attachments) != 1 {
					t.Errorf("user 廣播被污染：%+v", m)
				}
			default:
				t.Fatal("缺少 user 廣播")
			}
			x.send("C", "run-now-file")
			if len(x.queue(t)) != 0 {
				t.Fatal("promote 後相同 receipt 重試不得再入列")
			}
			var input clientMsg
			if err := json.Unmarshal([]byte(`{"type":"input","data":"D","request_id":"request-D","interrupted":true,"runNow":true,"run_now":true}`), &input); err != nil {
				t.Fatal(err)
			}
			later := x.plan("block")
			x.d.Dispatch(input)
			select {
			case <-next.cancelled:
				t.Fatal("一般 input 不得冒用內部旗標打斷")
			default:
			}
			next.release()
			readyRunNow(t, later)
			if strings.HasPrefix(later.opts.Prompt, notice) {
				t.Error("普通 input 不得冒用中斷前綴")
			}
			later.release()
			waitFor(t, "prompt 案例完成", func() bool { return !taskIsActive(x.s.ID) })
			want := "C,D"
			if interrupt {
				want = "A,C,D"
			}
			if got := strings.Join(userMessages(x.database, x.s.ID), ","); got != want {
				t.Errorf("歷史／receipt 原文=%q，預期 %q", got, want)
			}
		})
	}
}
