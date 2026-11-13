## 1. 防爬蟲與隱私安全標頭

- [x] 1.1 實作 Robots Exclusion Protocol 與 /robots.txt 端點：於 `internal/server/server.go` 新增 `/robots.txt` 路由處理器，回傳純文字 `User-agent: *\nDisallow: /\n`，達成「Decision 3: 階層式防爬蟲與防探測保護（Robots Protocol + HTTP Header + HTML Meta）」的協議層防護。驗證方式：以 `go test ./internal/server` 新增單元測試確認 GET `/robots.txt` 回應碼為 200 且內容為完全禁止。
- [x] 1.2 注入 Anti-Indexing HTTP Response Headers：在 `internal/server/server.go` 的全域 HTTP 中介層（middleware）對所有頁面與靜態請求加入 `X-Robots-Tag: noindex, nofollow, noarchive, nosnippet`、`X-Content-Type-Options: nosniff` 與 `Referrer-Policy: no-referrer`。驗證方式：撰寫 Go 測試對首頁與靜態端點發送請求，斷言回應標頭包含前述防索引與隱私標頭。
- [x] 1.3 設置 HTML Robots Meta Tag [after: 1.2]：在 `internal/static/index.html` 的 `<head>` 區塊加入 `<meta name="robots" content="noindex, nofollow, noarchive" />` 確保文件層防爬兜底。驗證方式：檢查 `index.html` 包含該標籤，並透過 `go test ./internal/static` 驗證靜態資產載入正常。

## 2. PWA 獨立安裝與行動視口支援

- [x] 2.1 實作 PWA Manifest Definition 與端點路由 [after: 1.3]：建立 `internal/static/manifest.webmanifest`，定義名稱、`display: "standalone"`、啟動路徑與圖示，並於 `internal/server/server.go` 註冊相應靜態提供路由與正確 MIME 類型 `application/manifest+json`，落實「Decision 2: 原生標準 Web App Manifest 與 Apple Web Clip 雙軌設定」。驗證方式：Go 單元測試驗證 GET `/manifest.webmanifest` 回傳正確 JSON 結構與 Content-Type。
- [x] 2.2 配置 Mobile Standalone and Viewport Metadata [after: 2.1]：在 `internal/static/index.html` 中引入 manifest 連結、Apple Web Clip 標籤（`apple-mobile-web-app-capable`、`apple-mobile-web-app-status-bar-style`）以及「Decision 4: 行動視口互動適配 (interactive-widget=resizes-content)」視口屬性。驗證方式：靜態檢查 `index.html` 具有完整的 PWA meta 標籤與 `interactive-widget=resizes-content`。
- [x] 2.3 實作 Service Worker Static Asset Caching with API Bypass [after: 2.2]：建立 `internal/static/sw.js`，實作靜態外殼資產快取，並在 `fetch` 攔截中強制略過所有 `/api/` 路由與串流連線，貫徹「Decision 1: Service Worker 靜態外殼快取與 API/即時串流絕對旁路 (Bypass)」；於 `internal/static/index.html` 加入 Service Worker 註冊邏輯。驗證方式：以 Node.js 測試或單元測試驗證 `sw.js` 的路由過濾規則不攔截 `/api/`，且靜態外殼清單包含必要核心檔案。

## 3. 整合驗證

- [x] 3.1 執行全專案自動化整合驗證 [after: 2.3]：執行 `go test ./internal/...`、`go test -tags production ./internal/...` 以及 `node --test scripts/*.test.cjs`，確保新增的 robots.txt、PWA 資產與標頭不破壞既有 Telegram WebApp、桌面 Wails 與 Web Session 功能。驗證方式：所有測試套件無失敗通過。
