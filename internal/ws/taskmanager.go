package ws

import (
	"context"
	"sync"
)

// taskEntry 代表某 session 上正在執行的 runner（與 WS 連線解耦）
type taskEntry struct {
	cancel context.CancelFunc
	msgID  int64
}

var taskManager struct {
	mu    sync.Mutex
	tasks map[string]*taskEntry
}

func init() {
	taskManager.tasks = make(map[string]*taskEntry)
}

// taskStart 登記新任務；若已有任務則先 cancel 舊的
func taskStart(sessionID string, cancel context.CancelFunc, msgID int64) {
	taskManager.mu.Lock()
	if old, ok := taskManager.tasks[sessionID]; ok && old.cancel != nil {
		old.cancel()
	}
	taskManager.tasks[sessionID] = &taskEntry{cancel: cancel, msgID: msgID}
	taskManager.mu.Unlock()
}

// taskEnd 任務 goroutine 結束時清除登記（與 taskCancel 擇一或併用）
func taskEnd(sessionID string) {
	taskManager.mu.Lock()
	delete(taskManager.tasks, sessionID)
	taskManager.mu.Unlock()
}

// taskCancel 中斷或重置時取消任務並移除登記
func taskCancel(sessionID string) {
	taskManager.mu.Lock()
	e, ok := taskManager.tasks[sessionID]
	if !ok {
		taskManager.mu.Unlock()
		return
	}
	delete(taskManager.tasks, sessionID)
	taskManager.mu.Unlock()
	if e != nil && e.cancel != nil {
		e.cancel()
	}
}
