package desktopupdate

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/updater"
)

// State 是協調器對外的生命週期狀態。
type State string

const (
	StateIdle        State = "idle"
	StateChecking    State = "checking"
	StateUpToDate    State = "up-to-date"
	StateAvailable   State = "available"
	StateDownloading State = "downloading"
	StateVerifying   State = "verifying"
	StateReady       State = "ready"
	StateError       State = "error"
	// StateRestarting 表示已取得工作閘門並呼叫 Restart；成功後程序即將結束，不再接受任何更新操作。
	StateRestarting State = "restarting"
)

// Status 是可查詢的快照（進度、錯誤、ready 狀態）；欄位名稱即送往前端的 JSON 欄位。
type Status struct {
	State            State     `json:"state"`
	CurrentVersion   string    `json:"currentVersion"`
	CurrentSHA       string    `json:"currentSha"`
	AvailableVersion string    `json:"availableVersion"`
	ReleaseNotes     string    `json:"releaseNotes"`
	DownloadedBytes  int64     `json:"downloadedBytes"`
	TotalBytes       int64     `json:"totalBytes"`
	ErrorCode        ErrorCode `json:"errorCode"`
	ErrorMessage     string    `json:"errorMessage"`
	// BusyReason 只在 ErrorCode 為 update_busy 時有值（running_task／shell_task／pending_confirm／queued／…）。
	BusyReason string `json:"busyReason,omitempty"`
}

var (
	// ErrBusy 表示已有另一個更新操作進行中，或已有 ready 更新；不會發出任何網路請求。
	ErrBusy = errors.New("desktopupdate: update operation busy")
	// ErrNothingToDownload 表示目前沒有可下載的已確認候選版本。
	ErrNothingToDownload = errors.New("desktopupdate: no available update to download")
	// ErrNotReadyToRestart 表示目前不是 ready 狀態，沒有可套用的已下載更新；不會取得閘門、不會啟動 helper。
	ErrNotReadyToRestart = errors.New("desktopupdate: no ready update to restart into")
	// ErrWorkBusy 表示桌面程式仍有進行中的工作，工作閘門拒絕維護權（見 WorkBusyError）。
	ErrWorkBusy = errors.New("desktopupdate: work in progress")
)

// ShutdownDeadline 是 Restart 成功後，等待程序正常結束的上限。
// 必須小於 Wails helper 等待父程序結束的 30 秒，超過即強制結束，讓 helper 能完成替換與重啟。
const ShutdownDeadline = 20 * time.Second

// 本階段新增的錯誤代碼（前端以 desktopUpdateErrorText 顯示）。
const (
	CodeUpdateBusy        ErrorCode = "update_busy"         // 仍有工作，未啟動 helper，保持 ready
	CodeHelperFailed      ErrorCode = "helper_failed"       // Restart／helper 失敗，已釋放閘門，保持 ready
	CodeUpdateCheckFailed ErrorCode = "update_check_failed" // 無法確認工作狀態（DB 查詢失敗），已取消更新
)

// WorkGate 取得「沒有任何進行中工作」的維護權。成功回傳釋放函式；忙碌回傳 *WorkBusyError。
// 由桌面端注入（實作為 ws.TryBeginDesktopUpdate），本套件不直接依賴 ws，測試可換成假實作。
type WorkGate func() (release func(), err error)

// WorkBusyError 表示工作閘門判定仍有進行中的工作；Reason 為原因分類（同 ws.BusyError）。
type WorkBusyError struct {
	Reason  string
	Details string
}

func (e *WorkBusyError) Error() string {
	return ErrWorkBusy.Error() + "（" + e.Reason + "）" + e.Details
}

func (e *WorkBusyError) Unwrap() error { return ErrWorkBusy }

type options struct {
	currentVersion string
	revision       string
	installPath    string
	apiBaseURL     string
	client         *http.Client
	probeWritable  func(dir string) error

	gate         WorkGate
	restart      func(context.Context) error // 預設為 Wails updater 的 Restart；測試一律注入假實作
	quit         func()                      // Wails Restart 成功前會呼叫 host.Quit，這裡轉為關閉應用程式
	after        func(time.Duration) <-chan time.Time
	forceExit    func(int)
	exitDeadline time.Duration
}

// Manager 協調單一的使用者觸發更新操作。
//
// 所有 Check/Download 經由 opMu 序列化（TryLock，衝突即回 ErrBusy，不排隊）。
// Wails 的 DownloadAndInstall 會丟棄既有的 staging，因此 ready 時絕不再呼叫它。
type Manager struct {
	opMu sync.Mutex // 序列化 Check/Download；Status 不需要它

	mu      sync.Mutex // 保護以下欄位
	st      Status
	pending *updater.Release // 最近一次 Check 確認的候選版本
	stage   updater.Stage    // 目前操作中最後一次 Wails 錯誤的階段

	upd         *updater.Updater
	prov        *stableProvider
	installPath string
	probe       func(dir string) error

	gate         WorkGate
	restart      func(context.Context) error
	quit         func()
	after        func(time.Duration) <-chan time.Time
	forceExit    func(int)
	exitDeadline time.Duration

	pubMu    sync.Mutex
	observer func(Status) // 狀態變更時通知（於 m.mu 外、pubMu 內呼叫），由桌面端推送到前端
}

// New 建立正式環境的協調器（固定 GitHub 來源，不帶 token）。
// revision 只用於顯示 SHA，不參與版本比較。gate 必填（工作閘門）；quit 由 Wails host.Quit 呼叫以關閉應用程式。
func New(currentVersion, revision, installPath string, gate WorkGate, quit func()) (*Manager, error) {
	return newManager(options{
		currentVersion: currentVersion,
		revision:       revision,
		installPath:    installPath,
		apiBaseURL:     defaultAPIBase(),
		gate:           gate,
		quit:           quit,
	})
}

// SetObserver 設定狀態觀察者；每次狀態變更後以目前快照呼叫一次。
func (m *Manager) SetObserver(fn func(Status)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.observer = fn
}

// Publish 立即以目前快照通知觀察者（例如前端要求 status 時）。
func (m *Manager) Publish() { m.publish() }

// publish 以 pubMu 序列化「讀快照 + 通知」，避免併發時舊快照晚送達覆蓋新狀態。
// observer 不可在內部再呼叫 publish。
func (m *Manager) publish() {
	m.pubMu.Lock()
	defer m.pubMu.Unlock()
	m.mu.Lock()
	st, obs := m.st, m.observer
	m.mu.Unlock()
	if obs != nil {
		obs(st)
	}
}

func newManager(o options) (*Manager, error) {
	if o.currentVersion == "" {
		return nil, errors.New("desktopupdate: current version is required")
	}
	if o.installPath == "" {
		return nil, errors.New("desktopupdate: install path is required")
	}
	prov, err := newStableProvider(o.apiBaseURL, o.client)
	if err != nil {
		return nil, err
	}
	if o.gate == nil {
		return nil, errors.New("desktopupdate: work gate is required")
	}
	m := &Manager{
		prov:         prov,
		installPath:  o.installPath,
		probe:        o.probeWritable,
		st:           Status{State: StateIdle, CurrentVersion: o.currentVersion, CurrentSHA: o.revision},
		gate:         o.gate,
		restart:      o.restart,
		quit:         o.quit,
		after:        o.after,
		forceExit:    o.forceExit,
		exitDeadline: o.exitDeadline,
	}
	if m.probe == nil {
		m.probe = probeWritable
	}
	if m.after == nil {
		m.after = time.After
	}
	if m.forceExit == nil {
		m.forceExit = os.Exit
	}
	if m.exitDeadline == 0 {
		m.exitDeadline = ShutdownDeadline
	}
	m.upd = updater.New(hostAdapter{m: m})
	err = m.upd.Init(updater.Config{
		CurrentVersion: o.currentVersion,
		Providers:      []updater.Provider{prov},
		Window:         updater.WindowNone, // 無視窗：只由明確的 Check/DownloadAndInstall 驅動
		Platform:       "windows",
		Arch:           "amd64",
	})
	if err != nil {
		return nil, err
	}
	if m.restart == nil {
		m.restart = m.upd.Restart
	}
	return m, nil
}

// Restart 在使用者確認後套用已 ready 的更新，順序固定：
//  1. 先取得工作閘門。忙碌 → update_busy（附原因），不啟動 helper、保持 ready；
//     閘門內部錯誤（例如 DB 查詢失敗）→ update_check_failed，保持 ready。
//  2. 呼叫 Restart（Wails 先啟動 helper，helper 就緒後經 host.Quit 關閉應用程式）。
//     失敗 → helper_failed：釋放閘門、保持 ready，可重試。
//  3. 成功 → 閘門與 opMu 維持到程序結束，任何新工作與更新操作都被拒絕；
//     同時武裝 watchdog，關閉卡住超過 ShutdownDeadline 就強制結束，避免永久封鎖。
func (m *Manager) Restart(ctx context.Context) error {
	if !m.opMu.TryLock() {
		return ErrBusy
	}
	kept := false // 成功後保留 opMu 到程序結束
	defer func() {
		if !kept {
			m.opMu.Unlock()
		}
	}()
	defer m.publish()

	m.mu.Lock()
	state := m.st.State
	m.mu.Unlock()
	if state != StateReady {
		return ErrNotReadyToRestart
	}

	release, err := m.gate()
	if err != nil {
		code, reason := gateFailure(err)
		m.restoreReady(code, reason, err)
		return err
	}

	m.mu.Lock()
	m.st.State = StateRestarting
	m.clearErrorLocked()
	m.mu.Unlock()
	m.publish()

	err = e2eRestartFault() // 一般建置恆為 nil；e2e 可注入啟動失敗
	if err == nil {
		err = m.restart(ctx)
	}
	if err != nil {
		release()
		m.restoreReady(CodeHelperFailed, "", err)
		return &Error{Code: CodeHelperFailed, Err: err}
	}
	kept = true
	m.armShutdownWatchdog()
	return nil
}

// gateFailure 把工作閘門的錯誤對應到錯誤代碼；忙碌時附上原因分類。
func gateFailure(err error) (ErrorCode, string) {
	var be *WorkBusyError
	if errors.As(err, &be) {
		return CodeUpdateBusy, be.Reason
	}
	if errors.Is(err, ErrWorkBusy) {
		return CodeUpdateBusy, ""
	}
	return CodeUpdateCheckFailed, ""
}

// restoreReady 回到 ready（已下載的更新仍可套用），並附上錯誤代碼與說明。
func (m *Manager) restoreReady(code ErrorCode, reason string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.st.State = StateReady
	m.st.ErrorCode = code
	m.st.ErrorMessage = err.Error()
	m.st.BusyReason = reason
}

// armShutdownWatchdog 在 Restart 成功後啟動。閘門在取得時沒有任何工作，且之後一直持有，
// 因此超時強制結束不會中斷任何進行中的工作；它只是讓 helper 能在它自己的 30 秒等待內完成替換。
func (m *Manager) armShutdownWatchdog() {
	ch := m.after(m.exitDeadline)
	go func() {
		<-ch
		slog.Warn("desktopupdate: shutdown did not finish in time; forcing exit so the update helper can proceed", "deadline", m.exitDeadline)
		m.forceExit(0)
	}()
}

// Status 回傳目前狀態的快照。
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.st
}

// Check 向固定來源查詢候選版本；不下載。ready/下載中/已有操作時回傳 ErrBusy 且不發出網路請求。
func (m *Manager) Check(ctx context.Context) error {
	if !m.opMu.TryLock() {
		return ErrBusy
	}
	defer m.opMu.Unlock()
	defer m.publish()

	m.mu.Lock()
	if m.st.State == StateReady {
		m.mu.Unlock()
		return ErrBusy
	}
	m.pending = nil
	m.st.State = StateChecking
	m.st.AvailableVersion = ""
	m.st.ReleaseNotes = ""
	m.st.DownloadedBytes, m.st.TotalBytes = 0, 0
	m.clearErrorLocked()
	m.mu.Unlock()

	rel, err := m.upd.Check(ctx)
	if err != nil {
		e := m.prov.lastError()
		if e == nil {
			e = &Error{Code: CodeCheckFailed, Err: err}
		}
		m.fail(e)
		return e
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if rel == nil {
		m.st.State = StateUpToDate
		return nil
	}
	m.pending = rel
	m.st.State = StateAvailable
	m.st.AvailableVersion = rel.Version
	m.st.ReleaseNotes = rel.Notes
	return nil
}

// Download 只在使用者確認後執行：先驗證候選與安裝目錄可寫，再透過 Wails 下載、驗證並進入 ready。
// 已 ready 時為冪等 no-op；已有操作進行中回傳 ErrBusy。
func (m *Manager) Download(ctx context.Context) error {
	if !m.opMu.TryLock() {
		return ErrBusy
	}
	defer m.opMu.Unlock()
	defer m.publish()

	m.mu.Lock()
	state, pending := m.st.State, m.pending
	m.mu.Unlock()

	if state == StateReady {
		return nil
	}
	if pending == nil || (state != StateAvailable && state != StateError) {
		return ErrNothingToDownload
	}
	if verr := validateRelease(pending); verr != nil {
		m.fail(verr)
		return verr
	}
	if err := m.probe(filepath.Dir(m.installPath)); err != nil {
		e := &Error{Code: CodeInstallNotWritable, Err: err}
		m.fail(e)
		return e
	}

	m.mu.Lock()
	m.stage = ""
	m.clearErrorLocked()
	m.mu.Unlock()

	err := m.upd.DownloadAndInstall(ctx)
	if err == nil {
		return nil
	}

	m.mu.Lock()
	stage := m.stage
	m.mu.Unlock()
	e := &Error{Code: failureCode(stage, err), Err: err}
	m.fail(e)
	return e
}

func (m *Manager) fail(e *Error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.st.State = StateError
	m.st.ErrorCode = e.Code
	m.st.ErrorMessage = e.Err.Error()
}

// failureCode 把 Wails 的失敗階段對應到設計內的錯誤代碼：
//   - verify 階段（digest 不符）→ checksum_mismatch；
//   - install 階段的權限拒絕（fs.ErrPermission，含 Windows 拒絕存取）→ install_not_writable；
//   - 其餘皆維持 download_failed（設計未為安裝階段其他錯誤定義獨立代碼，不另增）。
func failureCode(stage updater.Stage, err error) ErrorCode {
	switch {
	case stage == updater.StageVerify:
		return CodeChecksumMismatch
	case stage == updater.StageInstall && errors.Is(err, fs.ErrPermission):
		return CodeInstallNotWritable
	default:
		return CodeDownloadFailed
	}
}

// clearErrorLocked 需在 m.mu 保護下呼叫。
func (m *Manager) clearErrorLocked() {
	m.st.ErrorCode = ""
	m.st.ErrorMessage = ""
	m.st.BusyReason = ""
}

// onEvent 接收 Wails 的進度與階段事件（呼叫端為 Wails 的同一執行緒，但仍加鎖以便 Status 併發讀取）。
func (m *Manager) onEvent(name string, data []any) {
	m.applyEvent(name, data)
	m.publish()
}

func (m *Manager) applyEvent(name string, data []any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch name {
	case updater.EventDownloadStarted:
		m.st.State = StateDownloading
		m.st.DownloadedBytes = 0
		if rel, ok := firstOf[*updater.Release](data); ok {
			m.st.TotalBytes = rel.Artifact.Size
		}
	case updater.EventDownloadProgress:
		if p, ok := firstOf[updater.Progress](data); ok {
			m.st.DownloadedBytes = p.Written
			if p.Total > 0 {
				m.st.TotalBytes = p.Total
			}
		}
	case updater.EventDownloadComplete:
		m.st.DownloadedBytes = m.st.TotalBytes
	case updater.EventVerifying:
		m.st.State = StateVerifying
	case updater.EventUpdateReady:
		m.st.State = StateReady
		m.st.DownloadedBytes = m.st.TotalBytes
	case updater.EventError:
		if info, ok := firstOf[updater.ErrorInfo](data); ok {
			m.stage = info.Stage
		}
	}
}

func firstOf[T any](data []any) (T, bool) {
	var zero T
	if len(data) == 0 {
		return zero, false
	}
	v, ok := data[0].(T)
	return v, ok
}

// hostAdapter 實作 updater.Host；無視窗模式下 OpenWindow 與 Quit 不會被呼叫。
type hostAdapter struct {
	m *Manager
}

func (h hostAdapter) Emit(name string, data ...any) bool {
	h.m.onEvent(name, data)
	return true
}

func (h hostAdapter) OnEvent(string, func(any)) func() { return func() {} }

func (h hostAdapter) OpenWindow(updater.WindowOptions) updater.WindowHandle { return nil }

// Quit 由 Wails Restart 在 helper 就緒後呼叫，轉為關閉桌面應用程式（同一般關閉流程：HTTP → DB → 退出）。
func (h hostAdapter) Quit() {
	if h.m.quit != nil {
		h.m.quit()
	}
}

// probeWritable 以建立並刪除暫存檔的方式確認安裝目錄可寫，不提升權限。
func probeWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".update-write-probe-*")
	if err != nil {
		return err
	}
	name := f.Name()
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	return os.Remove(name)
}
