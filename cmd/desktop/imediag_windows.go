//go:build windows

package main

import (
	"log/slog"
	"os"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/imediag"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/version"
)

func attachImeDiag(window *application.WebviewWindow, log *imediag.Log, owner string) {
	hooks := []struct {
		event events.WindowEventType
		kind  string
	}{
		{events.Windows.WindowActive, "active"},
		{events.Windows.WindowInactive, "inactive"},
		{events.Windows.WindowSetFocus, "set-focus"},
		{events.Windows.WindowKillFocus, "kill-focus"},
		{events.Common.WindowDidMove, "move"},
		{events.Common.WindowDidResize, "resize"},
		{events.Common.WindowDPIChanged, "dpi"},
		{events.Common.WindowShow, "show"},
		{events.Common.WindowHide, "hide"},
	}
	for _, hook := range hooks {
		kind := hook.kind
		window.RegisterHook(hook.event, func(*application.WindowEvent) {
			log.AddNative(kind, windowGeo(window))
		})
	}
	window.RegisterHook(events.Windows.WebViewNavigationCompleted, func(*application.WindowEvent) {
		if js := imediag.Script(owner); js != "" {
			window.ExecJS(js)
		}
	})
}

func windowGeo(window *application.WebviewWindow) imediag.Geo {
	x, y := window.Position()
	w, h := window.Size()
	return imediag.Geo{Known: true, X: x, Y: y, W: w, H: h}
}

func exportImeDiag(window *application.WebviewWindow, log *imediag.Log) {
	if !log.BeginExport() {
		return
	}
	handed := false
	defer func() {
		if !handed {
			log.EndExport()
		}
	}()
	native := log.FreezeNative()
	log.Arm()
	window.ExecJS(imediag.FreezeCall())
	handed = true
	go func() {
		defer log.EndExport()
		dom, ok := log.WaitDOM(500 * time.Millisecond)
		body, err := imediag.Build(dom, ok, native, log.NativeAfter(), imediag.Meta{
			AppVersion:      version.Version,
			WebView2Runtime: webview2Runtime(),
		})
		if err != nil {
			slog.Info("輸入法診斷匯出失敗")
			messageBox("MiniApp", "輸入法診斷匯出失敗")
			return
		}
		if _, err := imediag.Commit(body, chooseImeSave, writeImeFile); err != nil {
			slog.Info("輸入法診斷匯出失敗")
			messageBox("MiniApp", "輸入法診斷匯出失敗")
		}
	}()
}

func chooseImeSave() (string, error) {
	path, err := application.Get().Dialog.SaveFile().
		SetFilename("ime-diagnostic.json").
		AddFilter("JSON", "*.json").
		PromptForSingleSelection()
	return imediag.NormalizeChoice(path, err)
}

func writeImeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}

func webview2Runtime() string {
	// 版本字串在 Wails 啟動 WebView 時才讀得到，此套件沒有現成的公開查詢。
	// 不為這個欄位新增模組，匯出固定記 unknown。
	return "unknown"
}
