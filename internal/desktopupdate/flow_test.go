package desktopupdate

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/updater"
)

const testVersion = "1.0.0"

// testHome 是主視窗首頁的 origin（只用於字串比對，不會實際連線）。
const testHome = "http://127.0.0.1:53817/"

// newFlow 建立指向本機 fixture 的協調器；安裝目錄與 Wails 暫存目錄都在測試專用的暫存目錄內。
func newFlow(t *testing.T, mutate func(o *options)) (*Manager, *fixture, string) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("TMP", tmp)
	t.Setenv("TEMP", tmp)

	install := filepath.Join(t.TempDir(), "claude-miniapp-desktop.exe")
	writeFile(t, install, "ORIGINAL-EXE")

	f := newFixture(t)
	o := options{
		currentVersion: testVersion, installPath: install, apiBaseURL: f.srv.URL,
		gate: (&fakeGate{}).gate,
		// 預設的 Restart／計時器／強制結束都是替身：沒有明確安裝 restartRig 的測試若碰到 Restart 會直接失敗，
		// 因此不可能誤觸真實的 Wails Restart（會替換 go test 二進位）。
		restart: func(context.Context) error {
			t.Errorf("unexpected Restart without restartRig")
			return errors.New("no rig")
		},
		after:     func(time.Duration) <-chan time.Time { return make(chan time.Time) },
		forceExit: func(code int) { t.Errorf("unexpected forced exit %d", code) },
	}
	if mutate != nil {
		mutate(&o)
	}
	m, err := newManager(o)
	if err != nil {
		t.Fatalf("newManager: %v", err)
	}
	return m, f, install
}

// TestDesktopUpdateFlow 驗證使用者觸發的單一更新操作：確認前零下載、進度與 ready 可查詢、
// 失敗分類正確、重複操作不產生第二次下載，且失敗時原 EXE 不被改動。
func TestDesktopUpdateFlow(t *testing.T) {
	ctx := context.Background()
	exe := desktopExe("1.0.1")
	latest := stableRelease("1.0.1", withSums(exe)...)

	t.Run("確認前零下載：Check 只回報可用版本", func(t *testing.T) {
		m, f, _ := newFlow(t, nil)
		f.setLatest(latest)
		if err := m.Check(ctx); err != nil {
			t.Fatalf("Check: %v", err)
		}
		st := m.Status()
		if st.State != StateAvailable || st.AvailableVersion != "1.0.1" {
			t.Fatalf("status = %+v, want available 1.0.1", st)
		}
		if st.ReleaseNotes != latest.Notes {
			t.Errorf("ReleaseNotes = %q, want %q", st.ReleaseNotes, latest.Notes)
		}
		if n := f.downloadCount(exe.Name); n != 0 {
			t.Fatalf("downloads before confirm = %d, want 0", n)
		}
	})

	t.Run("確認後下載、進度與 ready 可查詢，原 EXE 不變", func(t *testing.T) {
		m, f, install := newFlow(t, nil)
		f.setLatest(latest)
		mustCheck(t, m)

		if err := m.Download(ctx); err != nil {
			t.Fatalf("Download: %v", err)
		}
		st := m.Status()
		if st.State != StateReady {
			t.Fatalf("State = %s, want ready (status %+v)", st.State, st)
		}
		if st.TotalBytes != int64(len(exe.Body)) || st.DownloadedBytes != st.TotalBytes {
			t.Errorf("progress = %d/%d, want %d/%d", st.DownloadedBytes, st.TotalBytes, len(exe.Body), len(exe.Body))
		}
		if st.ErrorCode != "" {
			t.Errorf("ErrorCode = %s, want empty", st.ErrorCode)
		}
		if n := f.downloadCount(exe.Name); n != 1 {
			t.Errorf("downloads = %d, want 1", n)
		}
		staged, err := os.ReadFile(m.upd.DownloadedPath())
		if err != nil {
			t.Fatalf("read staged artifact: %v", err)
		}
		if string(staged) != string(exe.Body) {
			t.Errorf("staged bytes differ from served bytes")
		}
		assertInstallUnchanged(t, install)
	})

	t.Run("hash 不符 → checksum_mismatch，不 ready，原 EXE 不變", func(t *testing.T) {
		m, f, install := newFlow(t, nil)
		tampered := fakeAsset{Name: exe.Name, Body: []byte("tampered-bytes")}
		f.setLatest(&fakeRelease{Tag: "v1.0.1", Notes: "n", Assets: []fakeAsset{tampered, sumsFor(exe)}})
		mustCheck(t, m)

		err := m.Download(ctx)
		expectFailure(t, m, err, CodeChecksumMismatch)
		if p := m.upd.DownloadedPath(); p != "" {
			t.Errorf("DownloadedPath = %q after mismatch, want empty", p)
		}
		assertInstallUnchanged(t, install)
	})

	t.Run("連線失敗 → download_failed，原 EXE 不變", func(t *testing.T) {
		m, f, install := newFlow(t, nil)
		unreachable := fakeAsset{Name: exe.Name, Body: exe.Body, URL: closedURL(t) + "/" + exe.Name}
		f.setLatest(&fakeRelease{Tag: "v1.0.1", Notes: "n", Assets: []fakeAsset{unreachable, sumsFor(exe)}})
		mustCheck(t, m)

		expectFailure(t, m, m.Download(ctx), CodeDownloadFailed)
		assertInstallUnchanged(t, install)
	})

	t.Run("暫時性 HTTP 500 失敗後可重試，重試成功進入 ready", func(t *testing.T) {
		m, f, install := newFlow(t, nil)
		f.setLatest(latest)
		mustCheck(t, m)
		f.failDownloads(exe.Name, 1)

		expectFailure(t, m, m.Download(ctx), CodeDownloadFailed)
		if err := m.Download(ctx); err != nil {
			t.Fatalf("retry Download: %v", err)
		}
		if st := m.Status(); st.State != StateReady || st.ErrorCode != "" {
			t.Fatalf("after retry status = %+v, want ready without error", st)
		}
		if n := f.downloadCount(exe.Name); n != 2 {
			t.Errorf("downloads = %d, want 2 (one failed, one retry)", n)
		}
		assertInstallUnchanged(t, install)
	})

	t.Run("下載被限流（HTTP 429）→ download_failed，不 ready", func(t *testing.T) {
		m, f, install := newFlow(t, nil)
		f.setLatest(latest)
		mustCheck(t, m)
		f.setDownloadCode(429)

		expectFailure(t, m, m.Download(ctx), CodeDownloadFailed)
		assertInstallUnchanged(t, install)
	})

	t.Run("API 限流（HTTP 403）→ check_failed，不回報已是最新", func(t *testing.T) {
		m, f, _ := newFlow(t, nil)
		f.setLatestStatus(403)

		expectFailure(t, m, m.Check(ctx), CodeCheckFailed)
	})

	t.Run("Check 連線失敗 → check_failed，不回報已是最新", func(t *testing.T) {
		m, _, _ := newFlow(t, func(o *options) { o.apiBaseURL = closedURL(t) })

		expectFailure(t, m, m.Check(ctx), CodeCheckFailed)
	})

	t.Run("安裝目錄不可寫 → install_not_writable，零下載；權限恢復後可重試", func(t *testing.T) {
		writable := false
		m, f, install := newFlow(t, func(o *options) {
			o.probeWritable = func(string) error {
				if !writable {
					return errors.New("access denied")
				}
				return nil
			}
		})
		f.setLatest(latest)
		mustCheck(t, m)

		expectFailure(t, m, m.Download(ctx), CodeInstallNotWritable)
		if n := f.downloadCount(exe.Name); n != 0 {
			t.Fatalf("downloads with non-writable directory = %d, want 0", n)
		}
		assertInstallUnchanged(t, install)

		writable = true
		if err := m.Download(ctx); err != nil {
			t.Fatalf("Download after permission restored: %v", err)
		}
		if st := m.Status(); st.State != StateReady {
			t.Fatalf("State = %s, want ready", st.State)
		}
	})

	t.Run("失敗階段對應：僅安裝階段權限拒絕為 install_not_writable，其餘維持 download_failed", func(t *testing.T) {
		denied := &fs.PathError{Op: "rename", Path: "staged.exe", Err: fs.ErrPermission}
		if got := failureCode(updater.StageInstall, denied); got != CodeInstallNotWritable {
			t.Errorf("install + permission denied = %s, want install_not_writable", got)
		}
		if got := failureCode(updater.StageInstall, errors.New("updater: zip has 999 entries")); got != CodeDownloadFailed {
			t.Errorf("install + other error = %s, want download_failed", got)
		}
		if got := failureCode(updater.StageDownload, denied); got != CodeDownloadFailed {
			t.Errorf("download + permission denied = %s, want download_failed", got)
		}
		if got := failureCode(updater.StageVerify, errors.New("updater: digest mismatch")); got != CodeChecksumMismatch {
			t.Errorf("verify = %s, want checksum_mismatch", got)
		}
	})

	t.Run("沒有可下載的更新 → ErrNothingToDownload，零下載", func(t *testing.T) {
		m, f, _ := newFlow(t, nil)
		f.setLatest(nil) // 沒有任何正式版
		mustCheck(t, m)

		if err := m.Download(ctx); !errors.Is(err, ErrNothingToDownload) {
			t.Fatalf("Download = %v, want ErrNothingToDownload", err)
		}
		if n := f.downloadCount(exe.Name); n != 0 {
			t.Errorf("downloads = %d, want 0", n)
		}
	})

	t.Run("重複操作只下載一次；ready 不被排程或重複下載覆寫", func(t *testing.T) {
		m, f, install := newFlow(t, nil)
		f.setLatest(latest)
		mustCheck(t, m)

		open := f.holdDownloads()
		t.Cleanup(open)
		done := make(chan error, 1)
		go func() { done <- m.Download(ctx) }()
		waitFor(t, func() bool { return m.Status().State == StateDownloading })

		if err := m.Download(ctx); !errors.Is(err, ErrBusy) {
			t.Errorf("duplicate Download = %v, want ErrBusy", err)
		}
		if err := m.Check(ctx); !errors.Is(err, ErrBusy) {
			t.Errorf("Check during download = %v, want ErrBusy", err)
		}
		open()
		if err := <-done; err != nil {
			t.Fatalf("first Download: %v", err)
		}

		if st := m.Status(); st.State != StateReady {
			t.Fatalf("State = %s, want ready", st.State)
		}
		if n := f.downloadCount(exe.Name); n != 1 {
			t.Fatalf("downloads = %d, want 1", n)
		}

		if err := m.Download(ctx); err != nil {
			t.Errorf("Download when ready = %v, want nil no-op", err)
		}
		if err := m.Check(ctx); !errors.Is(err, ErrBusy) {
			t.Errorf("scheduled/manual Check when ready = %v, want ErrBusy", err)
		}
		if st := m.Status(); st.State != StateReady {
			t.Errorf("State after duplicate ops = %s, want ready", st.State)
		}
		if n := f.downloadCount(exe.Name); n != 1 {
			t.Errorf("downloads after duplicate ops = %d, want still 1", n)
		}
		if n := f.latestCallCount(); n != 1 {
			t.Errorf("latest calls = %d, want 1 (busy Check must not hit network)", n)
		}
		assertInstallUnchanged(t, install)
	})

	// 以下為 bridge（前端 → Go）的信任來源與 action 測試。
	trusted := func(msg string) Inbound {
		return Inbound{Message: msg, Window: "main", Origin: testHome, TopOrigin: testHome}
	}

	t.Run("信任來源：非主視窗、子框架、訪客頁、外部或空 origin 的訊息一律拒絕，零更新操作", func(t *testing.T) {
		untrusted := []struct {
			name string
			in   Inbound
		}{
			{"其他視窗 settings", Inbound{Message: "desktop-update:check", Window: "settings", Origin: testHome, TopOrigin: testHome}},
			{"空 origin", Inbound{Message: "desktop-update:check", Window: "main"}},
			{"外部 origin", Inbound{Message: "desktop-update:check", Window: "main", Origin: "https://evil.example/", TopOrigin: "https://evil.example/"}},
			{"頂層外部、子框架為首頁", Inbound{Message: "desktop-update:check", Window: "main", Origin: testHome, TopOrigin: "https://evil.example/"}},
			{"同 origin 的訪客分享頁", Inbound{Message: "desktop-update:check", Window: "main", Origin: testHome + "share/abc", TopOrigin: testHome + "share/abc"}},
			{"同 origin 子框架（非首頁路徑）", Inbound{Message: "desktop-update:check", Window: "main", Origin: testHome + "embed", TopOrigin: testHome}},
			{"不同 port", Inbound{Message: "desktop-update:check", Window: "main", Origin: "http://127.0.0.1:1/", TopOrigin: "http://127.0.0.1:1/"}},
			{"不同 scheme", Inbound{Message: "desktop-update:check", Window: "main", Origin: "https://127.0.0.1:53817/", TopOrigin: "https://127.0.0.1:53817/"}},
		}
		for _, tc := range untrusted {
			t.Run(tc.name, func(t *testing.T) {
				m, f, install := newFlow(t, nil)
				f.setLatest(latest)
				b := NewBridge(m, testHome)

				if err := b.Handle(tc.in); !errors.Is(err, ErrUntrusted) {
					t.Fatalf("Handle = %v, want ErrUntrusted", err)
				}
				if n := f.latestCallCount(); n != 0 {
					t.Errorf("latest calls = %d, want 0", n)
				}
				if st := m.Status(); st.State != StateIdle {
					t.Errorf("State = %s, want idle", st.State)
				}
				assertInstallUnchanged(t, install)
			})
		}
	})

	t.Run("信任來源：主視窗首頁（含 IsMainFrame）可受理", func(t *testing.T) {
		m, f, _ := newFlow(t, nil)
		f.setLatest(latest)
		b := NewBridge(m, testHome)
		in := Inbound{Message: "desktop-update:check", Window: "main", Origin: testHome, TopOrigin: testHome, IsMainFrame: true}

		if err := b.Handle(in); err != nil {
			t.Fatalf("Handle trusted = %v, want nil", err)
		}
		if n := f.latestCallCount(); n != 1 {
			t.Errorf("latest calls = %d, want 1", n)
		}
	})

	t.Run("未知 action 與呼叫者指定的 URL/路徑 → 拒絕，零更新操作", func(t *testing.T) {
		bad := []string{
			"desktop-update:install",
			"desktop-update:",
			"desktop-update:CHECK",
			"desktop-update:check?url=https://evil.example/x.exe",
			"desktop-update:download:https://evil.example/x.exe",
			"desktop-update:../../evil",
			"desktop-update:check https://evil.example/",
			"other:check",
			"check",
		}
		for _, msg := range bad {
			t.Run(msg, func(t *testing.T) {
				m, f, install := newFlow(t, nil)
				f.setLatest(latest)
				b := NewBridge(m, testHome)

				if err := b.Handle(trusted(msg)); err == nil {
					t.Fatalf("Handle(%q) = nil, want rejection", msg)
				}
				if n := f.latestCallCount(); n != 0 {
					t.Errorf("latest calls = %d, want 0", n)
				}
				if n := f.downloadCount(exe.Name); n != 0 {
					t.Errorf("downloads = %d, want 0", n)
				}
				if st := m.Status(); st.State != StateIdle {
					t.Errorf("State = %s, want idle", st.State)
				}
				assertInstallUnchanged(t, install)
			})
		}
	})

	t.Run("主視窗首頁：check/download/status 可執行並推送狀態；available 時 restart 被拒且不觸發任何操作", func(t *testing.T) {
		m, f, install := newFlow(t, nil)
		f.setLatest(latest)

		var mu sync.Mutex
		var seen []Status
		m.SetObserver(func(st Status) {
			_ = m.Status() // 觀察者於鎖外呼叫，不可死結
			mu.Lock()
			seen = append(seen, st)
			mu.Unlock()
		})
		b := NewBridge(m, testHome)

		if err := b.Handle(trusted("desktop-update:check")); err != nil {
			t.Fatalf("check: %v", err)
		}
		if st := m.Status(); st.State != StateAvailable || st.AvailableVersion != "1.0.1" {
			t.Fatalf("after check status = %+v, want available 1.0.1", st)
		}

		if err := b.Handle(trusted("desktop-update:status")); err != nil {
			t.Fatalf("status: %v", err)
		}
		mu.Lock()
		if len(seen) == 0 || seen[len(seen)-1].State != StateAvailable {
			t.Errorf("observer last snapshot = %+v, want available", seen)
		}
		mu.Unlock()
		if n := f.latestCallCount(); n != 1 {
			t.Errorf("latest calls after status = %d, want 1 (status must not hit network)", n)
		}

		if err := b.Handle(trusted("desktop-update:restart")); !errors.Is(err, ErrNotReadyToRestart) {
			t.Fatalf("restart while available = %v, want ErrNotReadyToRestart", err)
		}
		if n := f.downloadCount(exe.Name); n != 0 {
			t.Errorf("downloads after restart = %d, want 0", n)
		}
		if st := m.Status(); st.State != StateAvailable {
			t.Errorf("State after restart = %s, want unchanged available", st.State)
		}

		if err := b.Handle(trusted("desktop-update:download")); err != nil {
			t.Fatalf("download: %v", err)
		}
		if st := m.Status(); st.State != StateReady {
			t.Fatalf("after download State = %s, want ready", st.State)
		}
		assertInstallUnchanged(t, install)
	})

	// 以下為「確認重新啟動並更新」：閘門、Restart、計時器皆為假實作（見 restartRig），絕不呼叫真實 Wails Restart。
	t.Run("重啟：非 ready 時拒絕，零閘門、零 Restart", func(t *testing.T) {
		m, f, _ := newFlow(t, nil)
		rig := installRig(t, m)
		f.setLatest(latest)
		mustCheck(t, m)

		if err := m.Restart(ctx); !errors.Is(err, ErrNotReadyToRestart) {
			t.Fatalf("Restart = %v, want ErrNotReadyToRestart", err)
		}
		if n := rig.gate.callCount(); n != 0 {
			t.Errorf("gate calls = %d, want 0", n)
		}
		if n := rig.restartCount(); n != 0 {
			t.Errorf("Restart calls = %d, want 0", n)
		}
		if st := m.Status(); st.State != StateAvailable {
			t.Errorf("State = %s, want unchanged available", st.State)
		}
	})

	t.Run("重啟：工作忙碌 → update_busy 與原因分類；不啟動 helper、保持 ready，忙碌結束後可再次套用", func(t *testing.T) {
		m, f, install := newFlow(t, nil)
		rig := installRig(t, m)
		reachReady(t, m, f, latest)

		rig.gate.setBusy(&WorkBusyError{Reason: "queued", Details: "game-rule：暫停佇列 1 筆"})
		err := m.Restart(ctx)
		if !errors.Is(err, ErrWorkBusy) {
			t.Fatalf("Restart = %v, want ErrWorkBusy", err)
		}
		st := m.Status()
		if st.State != StateReady || st.ErrorCode != CodeUpdateBusy || st.BusyReason != "queued" {
			t.Fatalf("status = %+v, want ready + update_busy + busyReason queued", st)
		}
		if !strings.Contains(st.ErrorMessage, "game-rule：暫停佇列 1 筆") {
			t.Fatalf("blocker details lost: %s", st.ErrorMessage)
		}
		if n := rig.restartCount(); n != 0 {
			t.Fatalf("Restart calls = %d, want 0（忙碌時不得啟動 helper）", n)
		}
		assertInstallUnchanged(t, install)
		if _, err := os.Stat(m.upd.DownloadedPath()); err != nil {
			t.Errorf("staged artifact should survive busy refusal: %v", err)
		}

		rig.gate.setBusy(nil)
		if err := m.Restart(ctx); err != nil {
			t.Fatalf("Restart after busy cleared: %v", err)
		}
		if n := rig.restartCount(); n != 1 {
			t.Errorf("Restart calls = %d, want 1", n)
		}
	})

	t.Run("重啟：閘門先於 Restart 取得（Restart 呼叫當下閘門已持有）", func(t *testing.T) {
		m, f, _ := newFlow(t, nil)
		rig := installRig(t, m)
		reachReady(t, m, f, latest)

		if err := m.Restart(ctx); err != nil {
			t.Fatalf("Restart: %v", err)
		}
		if !rig.heldAtRestart() {
			t.Errorf("gate was not held when Restart was called")
		}
		if n := rig.gate.callCount(); n != 1 {
			t.Errorf("gate calls = %d, want 1", n)
		}
	})

	t.Run("重啟：helper 失敗（helper_failed）→ 釋放閘門、保持 ready、原 EXE 不變，可重試", func(t *testing.T) {
		m, f, install := newFlow(t, nil)
		rig := installRig(t, m)
		reachReady(t, m, f, latest)

		rig.setRestartErr(errors.New("helper did not become ready within 30s"))
		if err := m.Restart(ctx); err == nil {
			t.Fatal("Restart want error on helper failure")
		}
		st := m.Status()
		if st.State != StateReady || st.ErrorCode != CodeHelperFailed {
			t.Fatalf("status = %+v, want ready + helper_failed", st)
		}
		if n := rig.gate.releaseCount(); n != 1 {
			t.Errorf("gate releases = %d, want 1（helper 失敗必須釋放閘門）", n)
		}
		if rig.gate.isHeld() {
			t.Errorf("gate still held after helper failure")
		}
		if n := rig.quitCount(); n != 0 {
			t.Errorf("quit calls = %d, want 0（失敗時不得關閉應用程式）", n)
		}
		assertInstallUnchanged(t, install)
		if _, err := os.Stat(m.upd.DownloadedPath()); err != nil {
			t.Errorf("staged artifact should survive helper failure: %v", err)
		}

		rig.setRestartErr(nil)
		if err := m.Restart(ctx); err != nil {
			t.Fatalf("retry Restart: %v", err)
		}
		if n := rig.restartCount(); n != 2 {
			t.Errorf("Restart calls = %d, want 2", n)
		}
	})

	t.Run("重啟：閘門內部檢查失敗（非忙碌）→ update_check_failed，零 helper、保持 ready", func(t *testing.T) {
		m, f, _ := newFlow(t, nil)
		rig := installRig(t, m)
		reachReady(t, m, f, latest)

		rig.gate.setErr(errors.New("db unavailable"))
		if err := m.Restart(ctx); err == nil || errors.Is(err, ErrWorkBusy) {
			t.Fatalf("Restart = %v, want non-busy error", err)
		}
		st := m.Status()
		if st.State != StateReady || st.ErrorCode != CodeUpdateCheckFailed {
			t.Fatalf("status = %+v, want ready + update_check_failed", st)
		}
		if n := rig.restartCount(); n != 0 {
			t.Errorf("Restart calls = %d, want 0", n)
		}
	})

	t.Run("重啟成功：維持閘門不釋放、經 host.Quit 進入關閉流程；之後 check/download/重啟皆拒絕且零網路", func(t *testing.T) {
		m, f, _ := newFlow(t, nil)
		rig := installRig(t, m)
		reachReady(t, m, f, latest)
		before := f.latestCallCount()

		if err := m.Restart(ctx); err != nil {
			t.Fatalf("Restart: %v", err)
		}
		if n := rig.quitCount(); n != 1 {
			t.Errorf("quit calls = %d, want 1（Wails Restart 成功後經 host.Quit 關閉）", n)
		}
		if !rig.gate.isHeld() || rig.gate.releaseCount() != 0 {
			t.Errorf("gate held=%v releases=%d, want held and never released after success", rig.gate.isHeld(), rig.gate.releaseCount())
		}
		if st := m.Status(); st.State != StateRestarting {
			t.Errorf("State = %s, want restarting", st.State)
		}

		if err := m.Check(ctx); !errors.Is(err, ErrBusy) {
			t.Errorf("Check after restart = %v, want ErrBusy", err)
		}
		if err := m.Download(ctx); !errors.Is(err, ErrBusy) {
			t.Errorf("Download after restart = %v, want ErrBusy", err)
		}
		if err := m.Restart(ctx); !errors.Is(err, ErrBusy) {
			t.Errorf("second Restart = %v, want ErrBusy", err)
		}
		if n := f.latestCallCount(); n != before {
			t.Errorf("network checks after restart = %d, want no new calls", n-before)
		}
		if n := rig.restartCount(); n != 1 {
			t.Errorf("Restart calls = %d, want 1", n)
		}
		if n := rig.gate.callCount(); n != 1 {
			t.Errorf("gate calls = %d, want 1（成功後不得再取得閘門）", n)
		}
	})

	t.Run("重啟成功後逾時：未到期不強制結束；超過 ShutdownDeadline 仍未結束 → 強制結束，閘門仍不釋放", func(t *testing.T) {
		m, f, _ := newFlow(t, nil)
		rig := installRig(t, m)
		reachReady(t, m, f, latest)

		if err := m.Restart(ctx); err != nil {
			t.Fatalf("Restart: %v", err)
		}
		timer := rig.nextTimer(t)
		if d := rig.durations(); len(d) != 1 || d[0] != ShutdownDeadline {
			t.Fatalf("watchdog durations = %v, want [%v]", d, ShutdownDeadline)
		}
		select {
		case code := <-rig.exits:
			t.Fatalf("forced exit %d before deadline", code)
		case <-time.After(20 * time.Millisecond):
		}
		timer <- time.Now() // 模擬計時到期（程序仍未結束）
		select {
		case <-rig.exits:
		case <-time.After(2 * time.Second):
			t.Fatal("watchdog did not force exit after deadline")
		}
		if rig.gate.releaseCount() != 0 {
			t.Errorf("gate released by watchdog; must stay held until process exit")
		}
	})

	t.Run("資料保留：重啟失敗與成功都不改動安裝 EXE、安裝目錄其他檔案與工作目錄", func(t *testing.T) {
		m, f, install := newFlow(t, nil)
		rig := installRig(t, m)
		reachReady(t, m, f, latest)

		installDir := filepath.Dir(install)
		workDir := t.TempDir()
		t.Chdir(workDir)
		writeFile(t, filepath.Join(installDir, "config.yaml"), "port: 0\n")
		writeFile(t, filepath.Join(installDir, "data.db"), "DB-BYTES")
		writeFile(t, filepath.Join(workDir, "config.yaml"), "port: 0\n")
		dirBefore := listNames(t, installDir)

		rig.setRestartErr(errors.New("replacement failed"))
		_ = m.Restart(ctx)
		rig.setRestartErr(nil)
		if err := m.Restart(ctx); err != nil {
			t.Fatalf("Restart: %v", err)
		}

		assertInstallUnchanged(t, install)
		if got := readFile(t, filepath.Join(installDir, "data.db")); got != "DB-BYTES" {
			t.Errorf("data.db = %q, want unchanged", got)
		}
		if got := readFile(t, filepath.Join(installDir, "config.yaml")); got != "port: 0\n" {
			t.Errorf("install config.yaml changed: %q", got)
		}
		if got := readFile(t, filepath.Join(workDir, "config.yaml")); got != "port: 0\n" {
			t.Errorf("working-dir config.yaml changed: %q", got)
		}
		if wd, _ := os.Getwd(); !sameDir(t, wd, workDir) {
			t.Errorf("cwd = %s, want %s", wd, workDir)
		}
		if dirAfter := listNames(t, installDir); strings.Join(dirAfter, ",") != strings.Join(dirBefore, ",") {
			t.Errorf("install dir entries changed: before %v after %v", dirBefore, dirAfter)
		}
	})

	t.Run("主視窗首頁：ready 後經 bridge 送出 restart → 先取得閘門再 Restart", func(t *testing.T) {
		m, f, _ := newFlow(t, nil)
		rig := installRig(t, m)
		reachReady(t, m, f, latest)
		b := NewBridge(m, testHome)

		if err := b.Handle(trusted("desktop-update:restart")); err != nil {
			t.Fatalf("restart via bridge: %v", err)
		}
		if n := rig.restartCount(); n != 1 || !rig.heldAtRestart() {
			t.Errorf("restart calls = %d heldAtRestart=%v, want 1 and held", n, rig.heldAtRestart())
		}
	})

	t.Run("Status JSON 欄位名稱固定（送往前端）", func(t *testing.T) {
		raw, err := json.Marshal(Status{State: StateIdle, CurrentVersion: "0.12.0", CurrentSHA: "abc1234"})
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{`"state":"idle"`, `"currentVersion":"0.12.0"`, `"currentSha":"abc1234"`, `"errorCode":""`} {
			if !strings.Contains(string(raw), want) {
				t.Errorf("json %s missing %s", raw, want)
			}
		}
	})

	// 以下為背景排程（假時鐘）：不等待真實 1 小時，計時器由測試手動觸發。
	t.Run("背景排程：啟動時檢查一次，之後每小時只檢查一次；不下載、不改安裝檔", func(t *testing.T) {
		m, f, install := newFlow(t, nil)
		f.setLatest(latest)
		clk, _, _ := startSched(t, m)

		timer := clk.nextTimer(t) // 啟動檢查完成後才會建立第一個計時器
		if n := f.latestCallCount(); n != 1 {
			t.Fatalf("startup checks = %d, want 1", n)
		}
		if got := clk.durations(); len(got) != 1 || got[0] != time.Hour {
			t.Fatalf("scheduled durations = %v, want [1h]", got)
		}

		tick(t, clk, timer)
		if n := f.latestCallCount(); n != 2 {
			t.Errorf("checks after one interval = %d, want 2", n)
		}
		if st := m.Status(); st.State != StateAvailable {
			t.Errorf("State = %s, want available (background only updates snapshot)", st.State)
		}
		if n := f.downloadCount(exe.Name); n != 0 {
			t.Errorf("downloads from background = %d, want 0", n)
		}
		assertInstallUnchanged(t, install)
	})

	t.Run("背景錯誤可查詢（check_failed），網路恢復後回到可用更新", func(t *testing.T) {
		m, f, _ := newFlow(t, nil)
		f.setLatest(latest)
		clk, _, _ := startSched(t, m)
		timer := clk.nextTimer(t)

		f.setLatestStatus(403)
		timer = tick(t, clk, timer)
		if st := m.Status(); st.State != StateError || st.ErrorCode != CodeCheckFailed || st.ErrorMessage == "" {
			t.Fatalf("after background failure status = %+v, want error check_failed with message", st)
		}

		f.setLatestStatus(0)
		tick(t, clk, timer)
		if st := m.Status(); st.State != StateAvailable || st.ErrorCode != "" {
			t.Fatalf("after recovery status = %+v, want available without error", st)
		}
	})

	t.Run("忙碌狀態零重入：下載中與 ready 時背景檢查不發出任何網路請求", func(t *testing.T) {
		m, f, install := newFlow(t, nil)
		f.setLatest(latest)
		clk, _, _ := startSched(t, m)
		timer := clk.nextTimer(t)

		open := f.holdDownloads()
		t.Cleanup(open)
		done := make(chan error, 1)
		go func() { done <- m.Download(ctx) }()
		waitFor(t, func() bool { return m.Status().State == StateDownloading })

		before := f.latestCallCount()
		timer = tick(t, clk, timer)
		if n := f.latestCallCount(); n != before {
			t.Fatalf("background check while downloading hit network (%d → %d)", before, n)
		}

		open()
		if err := <-done; err != nil {
			t.Fatalf("Download: %v", err)
		}
		if st := m.Status(); st.State != StateReady {
			t.Fatalf("State = %s, want ready", st.State)
		}

		before = f.latestCallCount()
		tick(t, clk, timer)
		if n := f.latestCallCount(); n != before {
			t.Errorf("background check while ready hit network (%d → %d)", before, n)
		}
		if st := m.Status(); st.State != StateReady {
			t.Errorf("State after background tick = %s, want ready", st.State)
		}
		if n := f.downloadCount(exe.Name); n != 1 {
			t.Errorf("downloads = %d, want 1", n)
		}
		assertInstallUnchanged(t, install)
	})

	t.Run("停止排程：取消 context 後迴圈結束且不再排程", func(t *testing.T) {
		m, f, _ := newFlow(t, nil)
		f.setLatest(latest)
		clk, done, cancel := startSched(t, m)
		clk.nextTimer(t)

		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("scheduler did not stop after cancel")
		}
		if n := len(clk.durations()); n != 1 {
			t.Errorf("timers armed = %d, want 1 (no scheduling after stop)", n)
		}
		if n := f.latestCallCount(); n != 1 {
			t.Errorf("checks = %d, want 1 (startup only)", n)
		}
	})
}

// fakeClock 取代 time.After：每次排程計時器都把發送端交給測試，由測試以 tick 觸發。
type fakeClock struct {
	armed chan chan time.Time
	mu    sync.Mutex
	durs  []time.Duration
}

func newFakeClock() *fakeClock {
	return &fakeClock{armed: make(chan chan time.Time, 8)}
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	ch := make(chan time.Time)
	c.mu.Lock()
	c.durs = append(c.durs, d)
	c.mu.Unlock()
	c.armed <- ch
	return ch
}

func (c *fakeClock) durations() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.durs...)
}

// nextTimer 等待下一個計時器建立（代表上一輪背景檢查已結束、迴圈已重新排程）。
func (c *fakeClock) nextTimer(t *testing.T) chan time.Time {
	t.Helper()
	select {
	case ch := <-c.armed:
		return ch
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not arm a timer")
		return nil
	}
}

// tick 觸發目前的計時器，並回傳排程下一個計時器的發送端。
func tick(t *testing.T, c *fakeClock, ch chan time.Time) chan time.Time {
	t.Helper()
	select {
	case ch <- time.Now():
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not receive tick")
	}
	return c.nextTimer(t)
}

// startSched 以假時鐘啟動排程；測試結束時會取消 context。
func startSched(t *testing.T, m *Manager) (*fakeClock, <-chan struct{}, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	clk := newFakeClock()
	done := StartSchedule(ctx, m, CheckInterval, clk.After)
	return clk, done, cancel
}

func mustCheck(t *testing.T, m *Manager) {
	t.Helper()
	if err := m.Check(context.Background()); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func expectFailure(t *testing.T, m *Manager, err error, want ErrorCode) {
	t.Helper()
	if err == nil {
		t.Fatalf("want error code %s, got nil (status %+v)", want, m.Status())
	}
	st := m.Status()
	if st.State != StateError {
		t.Errorf("State = %s, want error", st.State)
	}
	if st.ErrorCode != want {
		t.Errorf("ErrorCode = %s, want %s (err: %v)", st.ErrorCode, want, err)
	}
	if st.ErrorMessage == "" {
		t.Errorf("ErrorMessage empty, want diagnosable message")
	}
	if st.State == StateUpToDate {
		t.Errorf("failure reported as up-to-date")
	}
}

func assertInstallUnchanged(t *testing.T, install string) {
	t.Helper()
	if got := readFile(t, install); got != "ORIGINAL-EXE" {
		t.Errorf("installed EXE = %q, want unchanged ORIGINAL-EXE", got)
	}
}

// closedURL 回傳一個確定沒有監聽的本機 URL，用來模擬連線失敗。
func closedURL(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return "http://" + addr
}

// reachReady 把協調器推進到 ready（Check + Download），用於重啟相關測試。
func reachReady(t *testing.T, m *Manager, f *fixture, latest *fakeRelease) {
	t.Helper()
	f.setLatest(latest)
	mustCheck(t, m)
	if err := m.Download(context.Background()); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if st := m.Status(); st.State != StateReady {
		t.Fatalf("State = %s, want ready", st.State)
	}
}

// fakeGate 是 WorkGate 的假實作：可模擬忙碌、一般錯誤，並記錄取得與釋放次數。
type fakeGate struct {
	mu       sync.Mutex
	busy     *WorkBusyError
	err      error
	calls    int
	releases int
	held     bool
}

func (g *fakeGate) gate() (func(), error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls++
	if g.busy != nil {
		return nil, g.busy
	}
	if g.err != nil {
		return nil, g.err
	}
	g.held = true
	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			defer g.mu.Unlock()
			g.held = false
			g.releases++
		})
	}, nil
}

func (g *fakeGate) setBusy(b *WorkBusyError) { g.mu.Lock(); defer g.mu.Unlock(); g.busy = b }
func (g *fakeGate) setErr(err error)         { g.mu.Lock(); defer g.mu.Unlock(); g.err = err }
func (g *fakeGate) callCount() int           { g.mu.Lock(); defer g.mu.Unlock(); return g.calls }
func (g *fakeGate) releaseCount() int        { g.mu.Lock(); defer g.mu.Unlock(); return g.releases }
func (g *fakeGate) isHeld() bool             { g.mu.Lock(); defer g.mu.Unlock(); return g.held }

// restartRig 以假閘門、假 Restart、假計時器取代所有外部副作用。
// 假 Restart 成功時會經由 hostAdapter.Quit 呼叫 m.quit，模擬 Wails 在 helper 就緒後的行為。
type restartRig struct {
	gate       *fakeGate
	mu         sync.Mutex
	restarts   int
	heldAtCall bool
	restartErr error
	quits      int
	durs       []time.Duration
	timers     chan chan time.Time
	exits      chan int
}

func installRig(t *testing.T, m *Manager) *restartRig {
	t.Helper()
	r := &restartRig{gate: &fakeGate{}, timers: make(chan chan time.Time, 4), exits: make(chan int, 4)}
	m.gate = r.gate.gate
	m.restart = func(context.Context) error {
		r.mu.Lock()
		r.restarts++
		r.heldAtCall = r.gate.isHeld()
		err := r.restartErr
		r.mu.Unlock()
		if err != nil {
			return err
		}
		hostAdapter{m: m}.Quit()
		return nil
	}
	m.quit = func() { r.mu.Lock(); r.quits++; r.mu.Unlock() }
	m.after = func(d time.Duration) <-chan time.Time {
		ch := make(chan time.Time)
		r.mu.Lock()
		r.durs = append(r.durs, d)
		r.mu.Unlock()
		r.timers <- ch
		return ch
	}
	m.forceExit = func(code int) { r.exits <- code }
	return r
}

func (r *restartRig) restartCount() int   { r.mu.Lock(); defer r.mu.Unlock(); return r.restarts }
func (r *restartRig) heldAtRestart() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.heldAtCall }
func (r *restartRig) quitCount() int      { r.mu.Lock(); defer r.mu.Unlock(); return r.quits }
func (r *restartRig) setRestartErr(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.restartErr = err
}
func (r *restartRig) durations() []time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Duration(nil), r.durs...)
}

// nextTimer 等待下一個 watchdog 計時器建立，回傳可由測試觸發的發送端。
func (r *restartRig) nextTimer(t *testing.T) chan time.Time {
	t.Helper()
	select {
	case ch := <-r.timers:
		return ch
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog timer was not armed")
		return nil
	}
}

// listNames 回傳目錄內的項目名稱（os.ReadDir 已排序）。
func listNames(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(ents))
	for _, e := range ents {
		names = append(names, e.Name())
	}
	return names
}

func sameDir(t *testing.T, a, b string) bool {
	t.Helper()
	fa, err := os.Stat(a)
	if err != nil {
		return false
	}
	fb, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(fa, fb)
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for condition")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
