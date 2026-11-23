# 桌面輸入法診斷匯出

Windows 桌面版托盤有一項「匯出輸入法診斷」。按下後先凍結記憶體裡的事件，再開系統儲存對話框。取消不會寫檔。

只在桌面頁面路徑 `/`、`window.top` 為自己、且 origin 等於擁有者時注入。一般瀏覽器、Telegram、分享頁不安裝。Wails 在 Windows 沒有可靠的 `IsMainFrame`；通道只核對視窗名稱與 origin／top origin 是否等於首頁。這項比對不能證明原生 main frame。

主環與匯出期間的 `afterExportUI` 都最多 256 筆。匯出結束（寫完、取消或失敗）後停止收集 after，不把視窗事件一直留在額外 slice。一次匯出還沒結束時，再按一次不會清掉這次快照，也不會再開一個儲存對話框。不寫磁碟緩衝、不上傳。

會留下的欄位在 `dom.events` 與 `native.events`：元素 focus／blur、`window-focus`／`window-blur`（頁面 window 事件，附 `documentHasFocus` 與當時 activeElement 的種類、是否可編輯、矩形）、composition start／end／update（沒有 composition data）、Shift 按下與放開（只有階段、左右、是否已 preventDefault）、viewport、devicePixelRatio、視窗 active／inactive／set-focus／kill-focus／move／resize／show／hide 與座標。`window-focus` 不是原生 HWND 焦點；原生焦點只看 native 那些視窗事件。原生 DPI 數值、IME 模式與 WebView2 runtime 寫 `unknown`。

不記錄按鍵內容、文字、輸入框內容、網址、DOM id、session、路徑。選單可能在按下匯出前就造成失焦，那段會留在快照裡；儲存對話框之後的 focus 不在 `native.events`，不能當成異常已經恢復。WebView2 runtime 目前沒有不啟動畫面就能呼叫的現成 API，匯出固定為 `unknown`。

這份匯出只幫下一次現場留證據，不改變輸入、不切換輸入法、也不代表注音問題已修復。
