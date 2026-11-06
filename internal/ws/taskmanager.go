package ws

import (
	"context"
	"sync"
	"time"
)

type taskEntry struct {
	cancel    context.CancelFunc
	msgID     int64
	startedAt int64 // Unix 毫秒；記錄任務啟動，與瀏覽器連線無關。
	pid       int   // 子進程 PID，Run() 啟動後由 taskSetPid 補上
	softSent  bool  // 是否已送過優雅停止信號（第一次按停止）
}

var taskManager struct {
	mu    sync.Mutex
	tasks map[string]*taskEntry
}

func init() {
	taskManager.tasks = make(map[string]*taskEntry)
	pendingPerms.m = make(map[string]*pendingPermEntry)
}

// dispatchLocks 序列化「判斷忙碌→排隊或啟動」與「佇列取出→啟動」。
// runAgent 在 taskCancel 與 taskStart 之間有 DB I/O 空窗；不加鎖時新 input 可能在空窗判定成閒置而插隊。
// 以 session 為 key（同一 session 可能有多條 WS 連線，各自持有 closure）。
// ponytail: 刪除 session 後 mutex 不回收；每個 session 一個，量小可忽略，真的累積再於 DeleteSession 時清掉。
var dispatchLocks sync.Map // sessionID → *sync.Mutex

func dispatchLock(sessionID string) *sync.Mutex {
	m, _ := dispatchLocks.LoadOrStore(sessionID, &sync.Mutex{})
	return m.(*sync.Mutex)
}

// pendingPerms：kiroacp 互動式授權的等待中請求，以 session 為 key。
// 不能放在單一 WS 連線的 closure：發起任務的分頁斷線／重連、別的分頁或 MCP loopback 連線送來的
// allow_once/deny_once 會找不到通道，runner 就永遠卡在等授權。
type pendingPermEntry struct {
	ch    chan bool // true=allow, false=deny
	tools any       // 重連時重送 permission_request 用
}

var pendingPerms struct {
	mu sync.Mutex
	m  map[string]*pendingPermEntry
}

func permSet(sessionID string, e *pendingPermEntry) {
	pendingPerms.mu.Lock()
	pendingPerms.m[sessionID] = e
	pendingPerms.mu.Unlock()
}

// permClear 只刪自己登記的那筆，避免舊請求收尾時刪掉新請求。
func permClear(sessionID string, e *pendingPermEntry) {
	pendingPerms.mu.Lock()
	if pendingPerms.m[sessionID] == e {
		delete(pendingPerms.m, sessionID)
	}
	pendingPerms.mu.Unlock()
}

func permPending(sessionID string) (tools any, ok bool) {
	pendingPerms.mu.Lock()
	defer pendingPerms.mu.Unlock()
	e, ok := pendingPerms.m[sessionID]
	if !ok {
		return nil, false
	}
	return e.tools, true
}

// permResolve 把決定交給等待中的 runner；回傳 false 表示沒有等待中的請求。
func permResolve(sessionID string, allow bool) bool {
	pendingPerms.mu.Lock()
	e, ok := pendingPerms.m[sessionID]
	pendingPerms.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case e.ch <- allow:
	default: // 已有決定在途（重複點擊），忽略
	}
	return true
}

func taskStart(sessionID string, cancel context.CancelFunc, msgID int64) {
	taskManager.mu.Lock()
	if old, ok := taskManager.tasks[sessionID]; ok && old.cancel != nil {
		old.cancel()
	}
	taskManager.tasks[sessionID] = &taskEntry{cancel: cancel, msgID: msgID, startedAt: time.Now().UnixMilli()}
	taskManager.mu.Unlock()
}

func (msg serverMsg) withTaskTiming(sessionID string) serverMsg {
	switch msg.Value {
	case StateThinking, StateStreaming, StateAwaitingConfirm, StateAwaitingInput:
		taskManager.mu.Lock()
		if task := taskManager.tasks[sessionID]; task != nil {
			msg.RunStartedAt = task.startedAt
			msg.ServerTime = time.Now().UnixMilli()
		}
		taskManager.mu.Unlock()
	}
	return msg
}

// taskSetPid 記錄任務對應子進程的 PID，供 taskInterrupt 第一階段送優雅停止信號用。
// 僅在目前登記的任務仍是呼叫者自己（msgID 相同）時才寫入，避免舊任務污染新任務。
func taskSetPid(sessionID string, msgID int64, pid int) {
	taskManager.mu.Lock()
	if e, ok := taskManager.tasks[sessionID]; ok && e.msgID == msgID {
		e.pid = pid
	}
	taskManager.mu.Unlock()
}

// taskEnd 只在目前登記的任務仍是呼叫者自己（msgID 相同）時才刪除，
// 避免舊任務（例如權限被拒後已被 taskCancel 取代）的延遲收尾把新任務的登記誤刪。
func taskEnd(sessionID string, msgID int64) {
	taskManager.mu.Lock()
	if e, ok := taskManager.tasks[sessionID]; ok && e.msgID == msgID {
		delete(taskManager.tasks, sessionID)
	}
	taskManager.mu.Unlock()
}

func taskIsActive(sessionID string) bool {
	taskManager.mu.Lock()
	defer taskManager.mu.Unlock()
	_, ok := taskManager.tasks[sessionID]
	return ok
}

// taskCancel 立即強制終止任務（cancel context → cmd.Cancel 觸發 KillTree）。
// 供 reset_context、rerun 等需要立即清乾淨、不留優雅緩衝的路徑使用。
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

// taskInterrupt 按一次「停止」就強制終止（taskCancel → KillTree）。
// 曾改成兩段式（先送 SendInterrupt 優雅信號、第二次按才強制），但優雅信號實務上常被子進程忽略，
// 使用者得連按兩次且第一下無回饋，故退回單段；SendInterrupt／pid／softSent 保留未用，日後要恢復優雅停止可直接接回。
//
// 回傳 true 表示有動作被執行（任務仍在跑），false 表示目前沒有任務可停。
func taskInterrupt(sessionID string) bool {
	if !taskIsActive(sessionID) {
		return false
	}
	taskCancel(sessionID)
	return true
}
