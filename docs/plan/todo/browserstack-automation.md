# BrowserStack 自動化測試規劃

狀態：GitHub／BrowserStack 串接完成；768px 返回入口、iPhone 分享內容與 WebKit 測試通道已修復，完整六環境 CI 通過：UI 18／18、功能回歸各 7／7；截圖已複核。程式位於獨立分支，尚未合併至 main。
分支：`codex/browserstack-4229eb6a`；最新測試提交 `74da7e2`。
[修復後全通過 CI](https://github.com/jerry12122/Claude-Code-Mini-App/actions/runs/44217281764)。
[通過的 Actions 執行](https://github.com/jerry12122/Claude-Code-Mini-App/actions/runs/44107518837)：Edge 與 BrowserStack Windows 11 Chrome 各七個案例，均為零失敗、零錯誤、零跳過。
[BrowserStack build](https://automate.browserstack.com/dashboard/v2/builds/dfb54960a3b9ea679ebe486f4e5dd3e2c37a17ff)。
首次執行發現 Windows 中文設定檔編碼問題，已設定 `PYTHONUTF8=1`，並改用官方維護下載來源的 SDK（本次版本 1.63.0）。
使用者已確認測試標的是此專案，且已有 BrowserStack 帳號與方案。
已透過 API 確認方案為 Automate Mobile，最多五個並行 session；裝置清單包含 Samsung Galaxy S25 Ultra／Android 15 與 iPhone 17 Pro／iOS 26。
新增需求：除功能測試外，需截圖並實際檢查各環境 UI。已新增六環境視覺檢查，發現的 768px 返回入口與 iPhone 分享視窗問題已修復；WebKit 經隔離 HTTPS/WSS 測試通道完成定向驗證。
執行結果、畫面證據與尚未驗收範圍見 [UI 驗收報告](../../reviews/2026-11-17-browserstack-ui.md)。

## 目標與工具

保留 Go 單一二進位、無 npm 前端建置的架構，沿用 Go 測試、Node.js 標準測試工具及 Python Playwright。
BrowserStack Automate 提供遠端瀏覽器與裝置；測試案例、資料隔離與斷言仍由專案維護。
功能回歸採官方 Python SDK；UI 採 BrowserStack Playwright fork 與官方 Local 套件，不自行實作遠端驅動。

## 測試分層與執行時機

| 層級 | 範圍 | 工具／環境 | 執行時機 |
|---|---|---|---|
| 單元與後端整合 | DB、授權、API、WebSocket、runner 解析與分層 | 既有 Go／Node 測試 | 每次 PR、main push，既有 `ci.yml` |
| 瀏覽器回歸 | 真實 server、正式版前端、HTTP／WS／DB 流程 | pytest + Playwright + Windows Edge | 規劃每次 PR、main push；新增專用 workflow |
| 跨瀏覽器 | 相同回歸案例在遠端瀏覽器執行 | BrowserStack Automate + Local | 規劃先手動觸發；雲端驗收後增加每日及發版前執行 |
| 真機網頁 | Android Chrome、iOS Safari、鍵盤與觸控 | BrowserStack Playwright fork；獨立 Python 環境 | 已啟用兩台真機 UI 流程；證據與限制分列於報告 |
| 原生容器 | Wails WebView2 橋接、更新重啟、Telegram WebView／SDK | 既有 Windows E2E；Telegram 專用裝置流程 | 發版前，保留獨立驗證 |

手機尺寸模擬、桌面 WebKit、真機 Safari、Telegram WKWebView 是不同覆蓋範圍，報告必須分別標示。
BrowserStack 的網頁測試不能證明 Wails 原生更新、Telegram initData 真實簽章或容器返回鍵已通過。

## 案例與驗收順序

| 優先序 | 規劃案例 | 驗收方式 |
|---|---|---|
| P0 | 錯誤密碼、正常登入、重新整理仍登入 | 驗證拒絕結果、成功畫面及登入持久化 |
| P0 | UI 建立會話、WS 串流、重新整理保留歷史 | 真實 HTTP／WS／DB；CLI 使用 fixture |
| P0 | 權限提示與拒絕後恢復輸入 | 提示出現、拒絕後消失且可繼續操作 |
| P0 | 透過檔案選擇器上傳附件、送出、重新整理仍可見 | 使用合成文字檔，驗證上傳成功與歷史附件 |
| P0 | 設定對話框、主題持久化、1280／390／320 寬度不溢出 | 對話框可操作、主題重新整理後保留；屬尺寸模擬 |
| P0 | 未處理前端例外、失敗截圖、trace、JUnit | 例外使測試失敗；故意失敗時能取得診斷產物 |
| P1 | 中斷、佇列、重送防重複、重新連線與離線草稿 | 加入可控制的 runner／網路失敗案例；確認訊息與順序不遺失、不重複 |
| P1 | 允許一次、記住權限、只讀／可參與分享、訪客越權 | 以兩個獨立 browser context 驗證 UI 與伺服器拒絕結果 |
| P1 | 附件失敗／取消／重試、跨會話草稿、圖片預覽 | 補合成圖片與失敗注入；不使用真實私人檔案 |
| P1 | 多會話、搜尋、分頁、切換、釘選、分享與清除 | 先驗證既有獨立腳本可遷移的案例，再加核心跨頁流程 |
| P1 | 首次設定與錯誤設定 | 獨立無設定檔 fixture；驗證一次性設定憑證與儲存後切換 |
| P1 | 真機鍵盤、旋轉、safe area、焦點／觸控 | 以真機 capability 和驅動驗收；不能只改 viewport 代替 |
| P1 | Telegram 驗證與返回鍵、Wails 更新／重啟 | 沿用後端授權測試、Telegram SDK 測試與 `scripts/e2e/`；再安排容器實測 |
| P2 | 視覺差異、可及性自動掃描、長時間穩定性與負載 | 核心回歸穩定後另訂基線與門檻；視需要評估 Percy／Accessibility 方案 |

每個新流程先定義可觀察結果，再寫測試。不得以固定 sleep、整頁快照或「沒有丟例外」取代業務斷言。
現有獨立 `.test.py` 包含 component fixture、`file://`、本機 Edge 與桌面假設，不能直接宣稱已全數遷移到雲端。

## BrowserStack 瀏覽器／裝置矩陣

第一個連線驗收預計採 Windows 11 + 最新 Chrome，驗證憑證、Local tunnel、影片及結果上報。
完整相容性矩陣依帳號支援清單與流量分布逐項啟用：

| 項目 | 用途 | 啟用前確認 |
|---|---|---|
| Windows Chrome／Edge 最新版 | 一般網頁與 Chromium 行為 | 遠端 session 成功、基本回歸全過 |
| Firefox 最新版 | Gecko 行為 | BrowserStack 對應 Playwright 版本與平台支援 |
| macOS Playwright WebKit | WebKit 引擎回歸 | 支援的 macOS 組合；報告不可標成真機 Safari |
| Android 真機 Chrome | 觸控、鍵盤、旋轉 | 方案、裝置可用性、SDK／驅動支援能力 |
| iPhone 真機 Safari | iOS 鍵盤、safe area、WebKit | iOS 專用整合及 Playwright API 限制，先做小型驗收 |
| 最低支援版本 | README／CLAUDE.md 的支援政策 | 雲端可用版本與驅動限制；缺口明確記錄，不默默以最新版取代 |

`parallelsPerPlatform: 1` 是每平台併發數，增加多個平台仍可能同時耗用多個 session。
併發額度未確認前預計使用單一平台；擴充後依額度安排分批或序列執行。
不預設新增付費加購產品；排程待連線驗收及核心回歸穩定後啟用。

## 預計實作項目與隔離方式

需要 Go（依 `go.mod`）、Node.js、Python 3.10+、Microsoft Edge。
測試只新增 Python 測試依賴，不改應用程式建置方式。以下檔案已在獨立分支建立，尚未合併到 main：

- `scripts/browser/`：共用 pytest fixture、P0／P1 回歸案例及固定版本的依賴清單。
- `browserstack.yml`：平台矩陣、Local tunnel 與診斷設定；不含憑證。
- `.github/workflows/browser-tests.yml`：本機回歸與可手動觸發的雲端測試。

預計每次測試建立獨立暫存目錄、隨機密碼、SQLite DB、fixture CLI 與 OS 分配的臨時埠。
使用正式版 Go build 與實際 HTTP／WebSocket，不連正式站、不讀取 repo 的 `config.yaml` 或 DB。
伺服器不繼承個人 agent CLI 的 PATH；只有假 Claude 可以回覆，不能代表真實模型／各廠 CLI 相容性。
server 沿用產品監聽介面，測試埠不是僅綁 loopback；仍保留密碼驗證，不能改為 `no_auth`。
只終止測試啟動的子程序。

## 未來串接步驟

1. 確認既有方案是否涵蓋 Automate、Local Testing、目標真機與所需併發額度。
2. 在本機環境或秘密管理器設定 `BROWSERSTACK_USERNAME`、`BROWSERSTACK_ACCESS_KEY`，不要貼在對話或寫入版控。
3. 建立隔離 fixture 與 P0 案例，先在本機驗證；雲端依賴的版本及 Java 需求於實作時依官方相容表確認。
4. 建立 `browserstack.yml`，讓 SDK 啟動 Local tunnel，遠端瀏覽器透過 tunnel 存取隔離 fixture。
5. 在 BrowserStack dashboard 確認這次 build 的 session、影片、結果及失敗原因。只安裝 SDK 不算串接驗收通過。
6. 把兩個同名憑證設為 GitHub repository secrets；新增 workflow，先以 Actions 手動觸發完成 CI 連線驗收。

預計本機回歸通過後，CI 才進入雲端 job。雲端憑證缺失須明確失敗，不回退成本機成功。
PR／push 僅執行本機案例；不向 fork PR 提供 secrets，也不使用 `pull_request_target` 執行外部程式碼。
本機 trace／截圖／JUnit artifact 保留 7 天；雲端影片依 BrowserStack 帳號設定保留。
預計僅上傳合成測試資料產生的結果，SDK 原始日誌不列入 artifact。

## 完成條件與未完成項目

- 第一階段：本機全部案例通過；雲端完成一次 Chrome build 並核對通過／失敗上報與資源清理。
- 第二階段：補上 P1 高風險流程、確認所選瀏覽器／裝置都能執行；至少一台 Android 與一台 iPhone 真機驗收。
- 第三階段：穩定後啟用每日完整矩陣；發版前檢查瀏覽器、容器與既有桌面更新測試。
- CI 不以 retries 隱藏失敗；不穩定案例需有原因、負責人與修復期限，不能永久略過。
- 已確認：標的是此 repo、已有 BrowserStack 帳號與方案；使用者後續已授權 GitHub 設定與實跑。
- 已確認：Automate Mobile、五個並行 session；六環境 UI matrix 最多兩個 UI session，加一個 Chrome 功能回歸。
- 待確認：最低版本政策驗收、排程耗時預算與報告接收方式；目前尚未啟用每日排程。
- 憑證設於 GitHub Actions repository secrets，不寫入版控；本次已擴充六種 UI 環境，失敗檢查保留。

## 官方參考

- [Python Playwright SDK 串接](https://www.browserstack.com/docs/automate/playwright/getting-started/python/integrate-your-tests)
- [Local Testing](https://www.browserstack.com/docs/automate/playwright/local-testing/introduction)
- [Playwright 版本相容性](https://www.browserstack.com/docs/automate/playwright/browsers-and-os)
- [Python iOS 整合](https://www.browserstack.com/docs/automate/playwright/playwright-ios/python)
