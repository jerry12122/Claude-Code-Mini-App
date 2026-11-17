# 跨環境 UI 視覺驗收

日期：2026-11-17～18（Asia/Taipei）。結論：尚未全部符合預期，確認兩項 UI 缺陷；桌面 WebKit 的聊天流程另受連線阻塞。

測試分支：`codex/browserstack-4229eb6a`，最新提交 `caf3b5c`，尚未合併 main。
[前輪六環境 Actions](https://github.com/jerry12122/Claude-Code-Mini-App/actions/runs/44114476735)；
[最終更新的 Actions](https://github.com/jerry12122/Claude-Code-Mini-App/actions/runs/44119052886)（已完成；整體失敗，原因如下）。
既有七項功能回歸在本機 Edge 與 BrowserStack Chrome 均通過（合計 14／14）。
六環境 UI 共 18 個案例：10 通過、8 失敗、0 error、0 skip；失敗包含三個 UI-01、一個 UI-02 與四個 WebKit 通道阻塞。共保存 230 張 PNG（含結束時快照）。
[BrowserStack UI build](https://automate.browserstack.com/dashboard/v2/builds/4a7160b84e2893cd0fe0713a42141a55dba1e066)。

使用 production Go server、隔離設定／DB、隨機密碼與假 Claude CLI，不使用正式資料或真實模型。
使用者工作目錄中尚未提交的 ChatHeader 間距修改未包含在雲端測試版本。

## UI-01：768px 單欄聊天沒有返回列表入口（P1）

重現：768×1024，登入 → 建立會話 → 進入聊天。
預期：單欄畫面可用可見按鈕回到會話列表。
實際：側欄已消失，聊天頂欄也沒有返回按鈕，觸控使用者缺乏會話切換入口。
已在本機 Edge、BrowserStack Chrome／Edge／Firefox 重現；1280／390／320 的對應入口檢查通過。

程式碼顯示 App 以 1024px 分隔桌面與單欄布局，而返回鈕放在 `sm:hidden` 的手機頂欄內。
640／1024px 斷點差異可解釋 768px 的結果；尚未逐一測試該區間所有寬度。
定位：`internal/static/js/App.js` 的單欄判定與 `internal/static/js/chat/ChatHeader.js` 的手機頂欄。

- [Chrome 原圖](../../.spectra/design-cache/browserstack-ui/44119052886/ui-chrome/ui/chrome/768/08-chat-empty-dark.png)
- [Edge 原圖](../../.spectra/design-cache/browserstack-ui/44119052886/ui-edge/ui/edge/768/08-chat-empty-dark.png)
- [首輪 Chrome 768px 全流程索引](../../.spectra/design-cache/browserstack-ui/44111410841/contacts/chrome-768.png)

## UI-02：iPhone 分享視窗內容被壓縮（P1）

重現：iPhone 17 Pro Safari，登入 → 建立會話 → 產生聊天歷史 → 展開會話資訊 → 分享。
預期：分享權限、內容、有效時間與「建立分享連結」可在內容區域完整呈現或透過正常捲動操作。
實際：只看到標題與一小條內容；分享表單被壓縮，主要按鈕無法在該區域完整顯示。

補上量測後，本機發起的真機重跑與最終 CI 均得到：dialog 高度 84px、內容區 20px、內容 scrollHeight 398px，而建立按鈕高度 36px。
同一檢查在本機 Edge 1280px 為 dialog 430px、內容區 366px、按鈕 36px，通過。
初版只檢查關閉鈕是否越界，曾回報通過；人工查看原圖後才發現此問題，已新增內容區高度斷言並實跑重現失敗。

定位：`internal/static/js/share/ShareModal.js` 的原生 dialog 與其 flex 捲動內容；
`internal/static/index.html` 的 `.ra-dialog-share` 僅設定 max-height。尚未以產品修改證明完整根因，不能直接宣稱修復方式。

- [iPhone 問題原圖](../../.spectra/design-cache/browserstack-ui/44119052886/ui-ios/ui/ios/native/14-share-dark.png)
- [高度量測](../../.spectra/design-cache/browserstack-ui/44119052886/ui-ios/ui/ios/native/14-share-layout.json)
- [失敗斷言紀錄](../../.spectra/design-cache/browserstack-ui/44119052886/ui-ios/ui.xml)
- [iPhone 全流程索引](../../.spectra/design-cache/browserstack-ui/44119052886/contacts/ios-native.png)

兩項 UI 問題均保留為失敗斷言，沒有略過、降低標準或使用 retries 隱藏。本次新增測試與報告，尚未修改應用程式修復。

## 執行矩陣與視覺觀察

| 環境 | 已執行結果 | 視覺觀察 |
|---|---|---|
| 本機 Edge | 四寬度 3 通過、1 失敗；新增分享斷言的 1280px 定向檢查通過 | 768px 有 UI-01；其餘已查看狀態未見明顯遮擋或頁面橫向溢出 |
| Windows 11 Chrome 157 | 四寬度 3 通過、1 失敗 | UI-01；深淺色設定、手機工具／模型、授權與分享可呈現 |
| Windows 11 Edge 157 | 四寬度 3 通過、1 失敗 | UI-01；主要布局與 Chrome 一致 |
| Windows 11 Firefox 153 | 四寬度 3 通過、1 失敗 | UI-01；原生 range 外觀有差異，未見因此遮擋操作 |
| macOS Sequoia Playwright WebKit 26.5 | 四個案例卡在聊天歷史載入，未完成 | 登入、設定與新會話畫面可呈現；授權／歷史／分享尚不能驗收 |
| Samsung Galaxy S25 Ultra／Android 15 Chrome | 真機完整流程通過 | 已看 14 個狀態；鍵盤開啟時 CSS viewport 高約 387px，輸入框與送出鍵仍可見 |
| iPhone 17 Pro／Safari，最終 CI 的 API 回報 iOS 26.3 | 真機流程可執行；補強分享檢查後因 UI-02 失敗 | 已看 14 個狀態；分享視窗不合格；其餘主要操作未見明顯越界 |

桌面實測 1280×900、768×1024、390×844、320×640；後兩者是尺寸模擬，不能當真機。
Android CSS 寬 384px，iPhone CSS 寬 402px。裝置／OS 以 BrowserStack session API 核對，保留完整 UA 與 viewport JSON。
iPhone capability 要求 26.0，但服務在本機發起的重跑回報 26.6、最終 CI 回報 26.3；兩者均重現 UI-02。UA 的 CPU OS 字串另顯示 18_7，因此不能只用 UA 宣稱 OS 版本，也不能宣稱已覆蓋精確 iOS 26.0。若要以精確版本作為發版門檻，還須釐清服務的版本選擇並加入平台核對。

Android 的 [鍵盤原圖](../../.spectra/design-cache/browserstack-ui/44119052886/ui-android/ui/android/native/09-chat-draft-keyboard.png)
與 [量測](../../.spectra/design-cache/browserstack-ui/44119052886/ui-android/ui/android/native/09-chat-draft-keyboard.json) 已核對。
iPhone 草稿畫面未捕捉到軟鍵盤或 viewport 縮小，不把這次結果當作 iOS 軟鍵盤遮擋驗收通過。

## WebKit 通道阻塞與測試修正證據

1. WebKit 能開啟頁面、登入、呼叫帶合法 token 的 `/sessions`（HTTP 200），但 `/events` WebSocket 回報 bad response／readyState 3，聊天歷史不結束載入。
2. 改用官方建議的 `bs-local.com` 後仍可重現；再測 force-local 也未改善，已移除無效選項。參考 [BrowserStack Local FAQ](https://www.browserstack.com/docs/local-testing/references/faqs)。
3. 暫存透明 TCP 探測中，HTTP 可經過探測埠，WebSocket 錯誤時沒有觀測到 `/events` 握手抵達該埠。這支持通道問題，但尚不能確定 BrowserStack／遠端引擎哪一層負責；沒有修改產品授權或關閉瀏覽器安全設定。
4. iPhone 的 Python `expect(...).to_be_visible()` 對可見登入欄位回報空白 AssertionError；改用 locator 等待及明確狀態斷言後，兩次完整流程執行成功，之後以新分享斷言揭露 UI-02。
5. 桌面測試曾在跨寬度切換時恢復前一個會話；改在應用啟動前清除 localStorage，再測恢復為 3 通過、1 個預期 UI-01 失敗。未把初始化失敗當成產品缺陷。
6. 同樣的 expect／密碼 role 定位也存在於 `scripts/browser/test_regression.py` 與 `conftest.py`；這兩者目前只在 Edge／Chrome 執行且已通過，沒有擴大修改或宣稱也支援 iOS。

[WebKit 探測紀錄](../../.spectra/design-cache/browserstack-ui/diagnostics/webkit-transport.json)、
[iPhone HTTP／WS 探測](../../.spectra/design-cache/browserstack-ui/diagnostics/ios-transport.json)、
[本機發起的 session 平台資訊](../../.spectra/design-cache/browserstack-ui/diagnostics/sessions-local.json)、
[最終 CI 平台資訊](../../.spectra/design-cache/browserstack-ui/diagnostics/sessions-44119052886.json)。探測腳本只取得診斷資料，其 runner 通過不等於 UI 通過。

## 檢查方法與界線

保存登入、列表、深／淺色設定、新會話、長標題、草稿、手機工具／模型、授權、歷史與分享畫面。
自動檢查頁面橫向溢出、主要控制項越界、單欄返回入口、分享內容區域、未處理前端例外及操作結果。
人工查看各寬度全流程截圖索引，問題與手機畫面另看原圖；截圖停用動畫。
本次沒有既有設計稿像素基線，不能證明所有視覺細節與設計稿一致，也不是完整可及性驗收。

淺色外觀頁部分次要文字看起來偏淡；桌面 WebKit 深色外觀頁的滑桿軌道接近黑色，與背景不易辨認。兩者尚未量測對比度或完成對應可及性操作，列為後續驗收，未宣稱為已量測的 WCAG 失敗。
Telegram 真實容器的 SDK／返回鍵、Wails WebView2 原生視窗與更新、手機旋轉與全部鍵盤情境仍未涵蓋。
CI artifact 保留七天；本機原圖另保存於 `.spectra/design-cache/browserstack-ui/`，不加入版控。報告內這些圖片／量測連結僅在本機有效；GitHub 上可由 Actions 連結下載對應 `ui-*` artifact。

後續優先修復 UI-01、UI-02，再釐清 WebKit 通道並重跑；修復前不能把此矩陣作為已全綠的發版 gate。可另以 `$spectra-propose` 建立 UI 修復變更，追蹤修復與驗證。
