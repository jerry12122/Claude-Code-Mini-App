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
