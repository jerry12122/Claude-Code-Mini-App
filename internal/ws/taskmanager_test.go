package ws

import (
	"context"
	"testing"
	"time"
)

// 重現「權限被拒後使用者允許重跑」的競態：A 被 taskCancel 取代成 B 後，
// A 的 goroutine 才姍姍來遲執行 defer taskEnd。修正前 taskEnd 只認 sessionID，
// 會把剛登記的 B 誤刪，導致 taskIsActive 提早回報 false（B 明明還在跑）。
func TestTaskEnd_DoesNotDeleteNewerTask(t *testing.T) {
	const sid = "s1"
	_, cancelA := context.WithCancel(context.Background())
	taskStart(sid, cancelA, 1) // A: msgID=1

	// 模擬 allow_once：taskCancel(A) 後立刻 taskStart(B)。
	taskCancel(sid)
	_, cancelB := context.WithCancel(context.Background())
	taskStart(sid, cancelB, 2) // B: msgID=2

	// A 的 goroutine 這時才跑到 defer taskEnd，帶的是自己的舊 msgID。
	taskEnd(sid, 1)

	if !taskIsActive(sid) {
		t.Fatal("B 應仍視為進行中，taskEnd(舊 msgID) 不該刪掉 B 的登記")
	}

	taskEnd(sid, 2)
	if taskIsActive(sid) {
		t.Fatal("B 用自己的 msgID 收尾後應清除登記")
	}
}

func TestTaskTimingPreservesStartAndClearsOnEnd(t *testing.T) {
	const sid = "timing-test"
	t.Cleanup(func() { taskCancel(sid) })
	before := time.Now().UnixMilli()
	taskStart(sid, nil, 1)
	initial := (serverMsg{Type: "status", Value: StateThinking}).withTaskTiming(sid)
	if initial.RunStartedAt < before || initial.ServerTime < initial.RunStartedAt {
		t.Fatalf("invalid task timing: %+v", initial)
	}
	for _, state := range []string{StateStreaming, StateAwaitingConfirm, StateAwaitingInput} {
		msg := (serverMsg{Type: "sync", Value: state}).withTaskTiming(sid)
		if msg.RunStartedAt != initial.RunStartedAt || msg.ServerTime < initial.ServerTime {
			t.Fatalf("%s reset task timing: %+v", state, msg)
		}
	}
	if msg := (serverMsg{Type: "status", Value: StateIdle}).withTaskTiming(sid); msg.RunStartedAt != 0 || msg.ServerTime != 0 {
		t.Fatalf("idle should omit timing: %+v", msg)
	}
	taskStart(sid, nil, 2)
	next := (serverMsg{Type: "status", Value: StateThinking}).withTaskTiming(sid)
	taskEnd(sid, 1)
	if msg := (serverMsg{Type: "sync", Value: StateThinking}).withTaskTiming(sid); msg.RunStartedAt != next.RunStartedAt {
		t.Fatalf("old task cleanup removed new timing: %+v", msg)
	}
	taskEnd(sid, 2)
	if msg := (serverMsg{Type: "sync", Value: StateThinking}).withTaskTiming(sid); msg.RunStartedAt != 0 || msg.ServerTime != 0 {
		t.Fatalf("ended task should omit timing: %+v", msg)
	}
}

func TestTaskInterruptForRunNow_WaitsForDone(t *testing.T) {
	const sid = "run-now-done"
	t.Cleanup(func() { interjectedTasks.Delete(int64(101)) })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { taskCancel(sid) })
	taskStart(sid, cancel, 101)
	taskManager.mu.Lock()
	entry := taskManager.tasks[sid]
	taskManager.mu.Unlock()
	if entry.done == nil {
		t.Fatal("登記任務時應建立 done 通道")
	}
	done, ok := taskInterruptForRunNow(sid)
	if !ok || done != entry.done {
		t.Fatal("打斷應回傳舊任務的 done 通道")
	}
	if taskIsActive(sid) || ctx.Err() != context.Canceled {
		t.Fatal("打斷應移除登記並取消舊任務")
	}
	waited := make(chan struct{})
	go func() {
		<-done
		close(waited)
	}()
	select {
	case <-waited:
		t.Fatal("舊回合尚未收尾，等待不應解除")
	case <-time.After(20 * time.Millisecond):
	}
	close(entry.done)
	select {
	case <-waited:
	case <-time.After(time.Second):
		t.Fatal("舊回合關閉 done 後應解除等待")
	}
	if done, ok := taskInterruptForRunNow(sid); ok || done != nil {
		t.Fatal("沒有任務時應回傳 nil、false")
	}
}

func TestTaskInterruptForRunNow_CancelOutsideLockAndPreservesNewTask(t *testing.T) {
	const sid = "run-now-reentrant-cancel"
	t.Cleanup(func() { interjectedTasks.Delete(int64(201)) })
	t.Cleanup(func() { taskCancel(sid) })
	taskStart(sid, func() {
		if taskIsActive(sid) {
			t.Error("取消回呼執行前應先移除舊登記")
		}
		taskStart(sid, nil, 202)
	}, 201)
	taskManager.mu.Lock()
	entry := taskManager.tasks[sid]
	taskManager.mu.Unlock()
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		done, ok := taskInterruptForRunNow(sid)
		if !ok || done == nil || done != entry.done {
			t.Error("取消回呼登記新任務後，仍應回傳舊任務的 done")
		}
	}()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("取消回呼重取 taskManager 鎖時不得互鎖")
	}
	taskEnd(sid, 201)
	if !taskIsActive(sid) {
		t.Fatal("打斷與舊任務收尾不得移除取消回呼登記的新任務")
	}
	taskManager.mu.Lock()
	newEntry := taskManager.tasks[sid]
	taskManager.mu.Unlock()
	if newEntry.msgID != 202 || newEntry.done == entry.done {
		t.Fatal("新任務應有自己的身份與完成通道")
	}
}

func TestTaskInterruptForRunNow_WithoutCancel(t *testing.T) {
	const sid = "run-now-nil-cancel"
	t.Cleanup(func() { interjectedTasks.Delete(int64(301)) })
	t.Cleanup(func() { taskCancel(sid) })
	taskStart(sid, nil, 301)
	done, ok := taskInterruptForRunNow(sid)
	if !ok || done == nil || taskIsActive(sid) {
		t.Fatal("沒有取消函式的登記仍應回傳 done 並移除")
	}
}

func TestTaskInterruptForRunNow_InterjectedOncePerMessage(t *testing.T) {
	const sid = "run-now-interjected-messages"
	t.Cleanup(func() {
		taskCancel(sid)
		interjectedTasks.Delete(int64(401))
		interjectedTasks.Delete(int64(402))
	})
	for _, msgID := range []int64{401, 402} {
		taskStart(sid, nil, msgID)
		if _, ok := taskInterruptForRunNow(sid); !ok {
			t.Fatal("已登記的任務應可打斷")
		}
	}
	for _, msgID := range []int64{401, 402} {
		if _, ok := interjectedTasks.LoadAndDelete(msgID); !ok {
			t.Errorf("msgID=%d 應各有自己的 interjected 標記", msgID)
		}
		if _, ok := interjectedTasks.LoadAndDelete(msgID); ok {
			t.Errorf("msgID=%d 的標記只能消耗一次", msgID)
		}
	}
}

func TestTaskInterruptForRunNow_MarksBeforeCancel(t *testing.T) {
	const sid = "run-now-mark-before-cancel"
	t.Cleanup(func() {
		taskCancel(sid)
		interjectedTasks.Delete(int64(411))
		interjectedTasks.Delete(int64(412))
	})
	cancelled := false
	taskStart(sid, func() {
		cancelled = true
		if _, ok := interjectedTasks.LoadAndDelete(int64(411)); !ok {
			t.Error("取消回呼執行時，舊 msgID 的標記應已可消耗")
		}
		taskStart(sid, nil, 412)
	}, 411)
	if _, ok := taskInterruptForRunNow(sid); !ok || !cancelled {
		t.Fatal("打斷應呼叫舊任務的取消函式")
	}
	for _, msgID := range []int64{411, 412} {
		if _, ok := interjectedTasks.LoadAndDelete(msgID); ok {
			t.Errorf("不得重建已消耗的舊標記或誤標新回合：msgID=%d", msgID)
		}
	}
	if !taskIsActive(sid) {
		t.Fatal("取消回呼登記的新回合應仍在")
	}
}

func TestTaskInterruptForRunNow_MissingTaskDoesNotMark(t *testing.T) {
	const sid = "run-now-missing-task"
	const msgID int64 = 421
	t.Cleanup(func() { interjectedTasks.Delete(msgID) })
	interjectedTasks.Store(msgID, struct{}{})
	before := 0
	interjectedTasks.Range(func(_, _ any) bool { before++; return true })
	if done, ok := taskInterruptForRunNow(sid); ok || done != nil {
		t.Fatal("沒有任務時應回傳 nil、false")
	}
	after := 0
	interjectedTasks.Range(func(_, _ any) bool { after++; return true })
	if after != before {
		t.Errorf("沒有任務不得增減標記數：原先 %d，之後 %d", before, after)
	}
	if _, ok := interjectedTasks.LoadAndDelete(msgID); !ok {
		t.Fatal("沒有任務時不得清除其他回合的標記")
	}
}

func TestSupportsRunNow(t *testing.T) {
	for _, tc := range []struct {
		agentType string
		want      bool
	}{
		{"claude", true},
		{"codex", true},
		{"cursor", true},
		{"kiro", false},
		{"kiroacp", false},
		{"antigravity", false},
		{"gemini", false},
		{"telegram", false},
		{"unknown", false},
		{"", false},
		{"Claude", false},
		{" claude ", false},
	} {
		t.Run(tc.agentType, func(t *testing.T) {
			if got := supportsRunNow(tc.agentType); got != tc.want {
				t.Errorf("supportsRunNow(%q) = %v，預期 %v", tc.agentType, got, tc.want)
			}
		})
	}
}
