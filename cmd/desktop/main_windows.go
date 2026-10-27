//go:build windows

package main

import (
	"context"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/updater"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/desktopupdate"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/logging"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/server"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/static"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/version"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/ws"
)

func main() {
	// helper 模式必須最先處理：在此之前不得改工作目錄、讀設定、初始化 log/DB 或綁 port。
	// 非 helper 行程下為 no-op，一般啟動行為不變。
	updater.HandleHelperMode()

	chdirToExeIfConfigured()

	syncLogs := logging.Init()
	defer syncLogs()

	started, err := server.Start(context.Background())
	if err != nil {
		slog.Error(err.Error())
		messageBox("Claude Code Mini App", err.Error())
		os.Exit(1)
	}

	icon := favicon()

	// 清掉 Wails 更新後留在 exe 旁的舊檔（<exe>.old.<時間戳>），一個都不留。
	// 背景執行、失敗只記錄（下次啟動再試），不影響啟動；規則見 desktopupdate.CleanOldAsides。
	if exe, err := os.Executable(); err == nil {
		go func() {
			removed, err := desktopupdate.CleanOldAsides(exe)
			for _, p := range removed {
				slog.Info("已清除舊的更新殘留檔: " + p)
			}
			if err != nil {
				slog.Info("清除舊的更新殘留檔時有檔案無法刪除（稍後再試）: " + err.Error())
			}
		}()
	}

	// 桌面自我更新：只在桌面端建立；初始化失敗僅記錄，不影響一般功能。
	var (
		updates *desktopupdate.Manager
		bridge  *desktopupdate.Bridge
		quitApp func() // app 建立後才綁定；Wails Restart 經 host.Quit 呼叫，轉為正常關閉流程
	)
	if exe, err := os.Executable(); err != nil {
		slog.Error("無法取得執行檔路徑，停用自我更新: " + err.Error())
	} else if m, err := desktopupdate.New(version.Version, version.Revision(), exe, desktopWorkGate, func() {
		if quitApp != nil {
			application.InvokeAsync(quitApp)
		}
	}); err != nil {
		slog.Error("初始化自我更新失敗，停用: " + err.Error())
	} else {
		updates = m
		bridge = desktopupdate.NewBridge(m, started.URL()+"/")
	}
	bgCtx, bgCancel := context.WithCancel(context.Background())

	var show func()
	appOpts := application.Options{
		Name:        "Claude Code Mini App",
		Description: "本機視窗殼，網頁與 Telegram Mini App 仍走同一個 port",
		Icon:        icon,
		Assets:      application.AlphaAssets,
		Logger:      slog.Default(),
		LogLevel:    slog.LevelInfo,
		// 前端 → Go 的 raw message；Handle 會驗證來源與 action，這裡不做任何信任判斷。
		RawMessageHandler: func(w application.Window, msg string, origin *application.OriginInfo) {
			if msg == "open-devtools" {
				w.OpenDevTools()
				return
			}
			if bridge == nil {
				return
			}
			in := desktopupdate.Inbound{Message: msg, Window: w.Name()}
			if origin != nil {
				in.Origin, in.TopOrigin, in.IsMainFrame = origin.Origin, origin.TopOrigin, origin.IsMainFrame
			}
			go func() {
				if err := bridge.Handle(in); err != nil {
					slog.Debug("desktop-update bridge: " + err.Error())
				}
			}()
		},
		OnShutdown: func() {
			e2eShutdownHang() // 一般建置為 no-op；e2e 可模擬關閉卡住
			bgCancel()        // 停止背景檢查排程
			shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := started.Shutdown(shutCtx); err != nil {
				slog.Error("關閉 HTTP 服務失敗: " + err.Error())
			}
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "com.jerry12122.claude-miniapp",
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				if show != nil {
					show()
				}
			},
		},
	}
	e2eApplyOptions(&appOpts)
	app := application.New(appOpts)

	quitApp = app.Quit

	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            "Claude Code Mini App",
		Width:            1280,
		Height:           800,
		MinWidth:         960,
		MinHeight:        640,
		URL:              windowURL(started.EntryURL()),
		BackgroundColour: application.NewRGBA(0x15, 0x16, 0x1e, 255),
		// 預設在非 production 建置會開 devtools；這裡關掉，避免一般使用時出現。
		DevToolsEnabled: false,
		Windows: application.WindowsWindow{
			DisableMenu: true,
		},
		KeyBindings: map[string]func(application.Window){
			"ctrl+q": func(application.Window) { app.Quit() },
			// DisableMenu 後 WebView2 內建的重整快捷鍵不一定生效；前端改了靠這個重載。
			"f5":     func(w application.Window) { w.Reload() },
			"ctrl+r": func(w application.Window) { w.Reload() },
			"f12":    func(w application.Window) { w.OpenDevTools() }, // -tags "production,devtools" 或非 production 建置時啟用
		},
	})
	show = func() {
		window.UnMinimise()
		window.Show()
		window.Focus()
	}
	window.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		window.Hide()
		e.Cancel()
	})

	if updates != nil {
		// 狀態變更 → 主執行緒上以事件推送到前端（非首頁頁面因沒有 bridge 而不會派發）。
		updates.SetObserver(func(st desktopupdate.Status) {
			js := desktopupdate.StatusScript(st)
			application.InvokeAsync(func() { window.ExecJS(js) })
		})
		// Windows beta.26 不支援 URL 視窗的 JS 注入，改在每次導航完成後注入；
		// 腳本本身只在首頁（頂層、同 origin、路徑 "/"）且 WebView2 可用時安裝 bridge。
		owner := originOf(started.URL())
		window.RegisterHook(events.Windows.WebViewNavigationCompleted, func(*application.WindowEvent) {
			window.ExecJS(desktopupdate.BridgeScript(owner))
		})
		// 背景檢查：啟動時一次、之後每 6 小時一次，只更新狀態，不下載、不開設定頁。
		// 必須等應用程式開始執行後才啟動：Check 會經 application.InvokeAsync 派送狀態，
		// 若在 app.Run 之前呼叫，Wails 主執行緒尚未就緒會 nil 指標 panic（5.1 e2e 發現）。
		app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
			desktopupdate.StartSchedule(bgCtx, updates, desktopupdate.CheckInterval, time.After)
		})
	}

	tray := app.SystemTray.New()
	tray.SetTooltip("Claude Code Mini App")
	if icon != nil {
		tray.SetIcon(icon)
	}
	tray.OnClick(show)
	tray.OnDoubleClick(show)
	trayMenu := app.NewMenu()
	trayMenu.Add("開啟視窗").OnClick(func(*application.Context) { show() })
	if updates != nil {
		trayMenu.Add("檢查更新").OnClick(func(*application.Context) {
			show()
			window.ExecJS(desktopupdate.OpenScript())
			go func() {
				if err := updates.Check(bgCtx); err != nil {
					slog.Info("手動檢查更新: " + err.Error())
				}
			}()
		})
	}
	trayMenu.AddSeparator()
	trayMenu.Add("結束").OnClick(func(*application.Context) { app.Quit() })
	tray.SetMenu(trayMenu)

	if err := app.Run(); err != nil {
		slog.Error(err.Error())
		messageBox("Claude Code Mini App", err.Error())
		os.Exit(1)
	}
}

// desktopWorkGate 把 ws 的工作閘門接到桌面更新：忙碌時轉成 desktopupdate.WorkBusyError（保留原因分類）。
// 非忙碌錯誤（例如 DB 查詢失敗）原樣回傳，由桌面更新歸為 update_check_failed。
func desktopWorkGate() (func(), error) {
	release, err := ws.TryBeginDesktopUpdate()
	if err != nil {
		if reason := ws.BusyReason(err); reason != "" {
			return nil, &desktopupdate.WorkBusyError{Reason: reason}
		}
		return nil, err
	}
	return release, nil
}

// chdirToExeIfConfigured 依 firstRunWorkDir 決定要不要切到 exe 旁邊。
// 旁邊已有 config.yaml，或發布包只有 config.example.yaml 且目前目錄還沒有設定檔時才切。
func chdirToExeIfConfigured() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	cwd, err := os.Getwd()
	if err != nil {
		return
	}
	dir := firstRunWorkDir(filepath.Dir(exe), cwd, fileExists)
	if dir == "" {
		return
	}
	_ = os.Chdir(dir)
}

// originOf 回傳 scheme://host（不含路徑），解析失敗時回傳空字串（BridgeScript 會因此不安裝 bridge）。
func originOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func messageBox(title, text string) {
	user32 := syscall.NewLazyDLL("user32.dll")
	proc := user32.NewProc("MessageBoxW")
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	textPtr, _ := syscall.UTF16PtrFromString(text)
	const flags = uintptr(0x00000010 | 0x00010000) // MB_ICONERROR | MB_SETFOREGROUND
	_, _, _ = proc.Call(0, uintptr(unsafe.Pointer(textPtr)), uintptr(unsafe.Pointer(titlePtr)), flags)
}

// favicon 與網頁共用同一個前端資源來源（production 為嵌入版本），不再直接讀磁碟。
func favicon() []byte {
	b, err := fs.ReadFile(static.Assets, "favicon.ico")
	if err != nil {
		slog.Error("讀取 favicon 失敗: " + err.Error())
		return nil
	}
	return b
}
