package ws

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

// codeUpdateInProgress 是維護中拒絕新工作時回給前端的 code（前端據此顯示「更新中，稍後再試」）。
const codeUpdateInProgress = "update_in_progress"

// ErrUpdateBusy 表示桌面程式仍有工作（執行中、待授權、待確認、排隊中…），不能重啟更新。
var ErrUpdateBusy = errors.New("ws: 桌面程式仍有進行中的工作，無法更新")

var errUpdateInProgress = errors.New("ws: 桌面程式正在更新")

// BusyError 是 ErrUpdateBusy 的包裝，附上簡短原因分類，讓前端可以顯示「為何忙碌」。
// errors.Is(err, ErrUpdateBusy) 仍為 true，既有比較不受影響。
//
// Reason 取值：running_task（執行中或仍在收尾）、shell_task（終端機指令執行中）、
// pending_confirm（等待授權／確認）、queued（佇列中）、maintenance（另一個更新程序進行中）。
type BusyError struct {
	Reason  string
	Details string
}

func (e *BusyError) Error() string {
	return ErrUpdateBusy.Error() + "（" + e.Reason + "）" + e.Details
}

func (e *BusyError) Unwrap() error { return ErrUpdateBusy }

// BusyReason 回傳 err 的原因分類；非 BusyError 回傳空字串。
func BusyReason(err error) string {
	var be *BusyError
	if errors.As(err, &be) {
		return be.Reason
	}
	return ""
}

// admissionGate 是「工作閘門」。
//
//   - inflight：已登記、尚未真正返回的工作數。登記從「第一個取消或副作用」之前開始，
//     到 goroutine 真的結束（包含 defer）為止；因此 taskCancel／shellTaskCancel 先刪 map 時，
//     清理尚未完成的工作仍算忙碌。
//   - maint：維護中。成立後所有新工作在寫入 DB 或啟動子程序之前就被拒絕（update_in_progress）。
//
// 鎖順序：gate.mu 永遠最外層；持有 gate.mu 時只會取 taskManager／shellTaskManager／shellPendingMu／
// pendingPerms／pendingQuestions 與 DB 的讀取，不會回頭呼叫任何會取得 gate.mu 的函式。
// 所有 release 都在呼叫端已釋放其他鎖之後才執行。
type admissionGate struct {
	mu       sync.Mutex
	db       *db.DB
	maint    bool
	inflight int
}

var gate admissionGate

// setGateDB 由 NewHandler 呼叫：TryBeginDesktopUpdate 需要讀取全站 DB 的工作摘要。
func setGateDB(database *db.DB) {
	gate.mu.Lock()
	gate.db = database
	gate.mu.Unlock()
}

// admitWork 是新工作的入口。維護中回傳 errUpdateInProgress（呼叫端不得有任何寫入）；
// 否則登記一筆 inflight，回傳的釋放函式必須在工作真正結束後呼叫，且只有第一次有效。
func admitWork() (func(), error) {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.maint {
		return nil, errUpdateInProgress
	}
	return gate.enterLocked(), nil
}

// holdWork 為 goroutine 接手既有 token：呼叫端必須仍持有 admitWork 取得的 token。
// 因為 token 已阻止維護成立，這裡不會因維護而失敗。
func holdWork() func() {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	return gate.enterLocked()
}

func (g *admissionGate) enterLocked() func() {
	g.inflight++
	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			g.inflight--
			g.mu.Unlock()
		})
	}
}

// withWorkAdmission 在閘門內執行 fn；維護中則透過 reply 回覆 update_in_progress 並不執行 fn。
func withWorkAdmission(reply func(serverMsg) bool, requestID string, fn func()) {
	release, err := admitWork()
	if err != nil {
		reply(updateInProgressMsg(requestID))
		return
	}
	defer release()
	fn()
}

// updateInProgressMsg 產生維護中的拒絕訊息；帶 request_id 的輸入回 input_rejected，其餘回 error。
func updateInProgressMsg(requestID string) serverMsg {
	const content = "桌面程式正在更新，請稍後再送出"
	if requestID != "" {
		return serverMsg{Type: "input_rejected", RequestID: requestID, Code: codeUpdateInProgress, Content: content}
	}
	return serverMsg{Type: "error", Code: codeUpdateInProgress, Content: content}
}

// TryBeginDesktopUpdate 在單一同步邊界內檢查所有忙碌來源，全部為空才取得維護權。
//
// 忙碌來源（任一即回傳 ErrUpdateBusy）：
//   - 登記中的工作（inflight）：agent 執行、shell 執行、授權／問題等待中的 goroutine，
//     以及 taskCancel／shellTaskCancel 已刪除 map 但清理尚未返回者。
//   - 記憶體：taskManager.tasks、shellTaskManager.tasks、shellPending、pendingPerms、pendingQuestions。
//   - DB（全站）：queued_messages、pending_denials、shell_pending、status 為 running／awaiting_input／awaiting_shell_confirm。
//
// DB 查詢失敗回傳一般錯誤（不是 ErrUpdateBusy），讓呼叫端區分「忙碌」與「檢查失敗」。
// 成功時回傳釋放函式；在維護中，所有新工作都會被 admitWork 拒絕。
func TryBeginDesktopUpdate() (func(), error) {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.maint {
		return nil, &BusyError{Reason: "maintenance"}
	}
	// 先看記憶體來源（能分出 shell_task／pending_confirm），再看 inflight（剩下的都算仍在執行或收尾）。
	if reason, sid := inMemoryBusyReason(); reason != "" {
		return nil, updateBusyError(reason, sid)
	}
	if gate.inflight > 0 {
		return nil, updateBusyError("running_task", "")
	}
	if gate.db == nil {
		return nil, errors.New("ws: 工作閘門尚未綁定資料庫")
	}
	report, err := gate.db.PendingWork()
	if err != nil {
		return nil, fmt.Errorf("ws: 檢查待辦工作失敗: %w", err)
	}
	if report.Busy() {
		return nil, updateBusyError(dbBusyReason(report), "")
	}
	gate.maint = true
	var once sync.Once
	return func() {
		once.Do(func() {
			gate.mu.Lock()
			gate.maint = false
			gate.mu.Unlock()
		})
	}, nil
}

// dbBusyReason 把 DB 的忙碌摘要對應到原因分類（呼叫端已確認 report.Busy()）。
func dbBusyReason(r db.WorkReport) string {
	switch {
	case r.QueuedMessages > 0:
		return "queued"
	case r.PendingDenials > 0 || r.ShellPending > 0:
		return "pending_confirm"
	default:
		return "running_task"
	}
}

// inMemoryBusyReason 檢查記憶體的忙碌來源，回傳原因分類與會話 ID；全部空閒回傳空字串。
// 呼叫端持有 gate.mu；這裡逐一取各自的鎖，不巢狀持有。
func inMemoryBusyReason() (string, string) {
	taskManager.mu.Lock()
	var sid string
	for id := range taskManager.tasks {
		sid = id
		break
	}
	taskManager.mu.Unlock()
	if sid != "" {
		return "running_task", sid
	}
	shellTaskManager.mu.Lock()
	for id := range shellTaskManager.tasks {
		sid = id
		break
	}
	shellTaskManager.mu.Unlock()
	if sid != "" {
		return "shell_task", sid
	}
	shellPendingMu.Lock()
	for id := range shellPending {
		sid = id
		break
	}
	shellPendingMu.Unlock()
	if sid != "" {
		return "pending_confirm", sid
	}
	pendingPerms.mu.Lock()
	for id := range pendingPerms.m {
		sid = id
		break
	}
	pendingPerms.mu.Unlock()
	if sid != "" {
		return "pending_confirm", sid
	}
	pendingQuestions.Lock()
	for id := range pendingQuestions.m {
		sid = id
		break
	}
	pendingQuestions.Unlock()
	if sid != "" {
		return "pending_confirm", sid
	}
	return "", ""
}

// Called only after refusing maintenance; diagnostic failure must never admit it.
func updateBusyError(reason, sid string) *BusyError {
	e := &BusyError{Reason: reason}
	var details []string
	if sid != "" {
		name := sid
		if gate.db != nil {
			if s, err := gate.db.GetSession(sid); err == nil {
				name = s.Name + " [" + sid + "]"
			}
		}
		details = append(details, name+"："+map[string]string{"running_task": "任務仍在執行或收尾", "shell_task": "終端機指令執行中", "pending_confirm": "等待授權或回答"}[reason])
	}
	if gate.db != nil {
		if rows, err := gate.db.PendingWorkDetails(); err == nil {
			details = append(details, rows...)
		} else {
			details = append(details, "無法讀取會話明細，請稍後重試")
		}
	}
	if len(details) == 0 {
		details = append(details, "工作仍在收尾，請稍後重試")
	}
	e.Details = "\n" + strings.Join(details, "\n")
	return e
}
