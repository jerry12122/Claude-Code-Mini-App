## Why

目前 claude-miniapp 作為遠端遠程操作工具，核心使用情境包含 Telegram Mini App (TMA)、Windows 桌面版與瀏覽器 Web 版。然而在一般行動裝置（如 iOS Safari、Android Chrome）直接開啟 Web 版時，受到瀏覽器網址列與工具列動態伸縮干擾，高度體驗受限，且虛擬鍵盤彈起時缺乏動態視口尺寸適配；同時，若使用者在公網或私有伺服器上部署本服務，因缺乏主動的爬蟲阻擋機制（如 robots.txt 與防檢索標頭），存在被網路搜尋引擎或自動化安全掃描工具意外收錄與探測的隱私與安全風險。

因此，本提案旨在：
1. 建立 PWA (Progressive Web App) 獨立安裝支援與行動視口適配，提供乾淨無網址列的 standalone 全螢幕體驗。
2. 建立防爬蟲防掃描保護（Robots & Security Headers Guard），確保服務即便誤開於公網也能杜絕搜尋引擎收錄與自動化工具掃描。

## What Changes

- **PWA 支援與資源配置**：
  - 新增 Web App Manifest (`manifest.webmanifest`)，宣告獨立模式 (`display: standalone`)、色彩與圖示定義。
  - 在 `index.html` 補齊 PWA 與 Apple 行動裝置專屬標籤（`apple-mobile-web-app-capable`、`apple-mobile-web-app-status-bar-style`、`theme-color`）。
  - 新增輕量級 Service Worker (`sw.js`)：僅針對前端靜態外殼（HTML/CSS/JS/字型）進行離線與快取加速，明確排除所有 `/api/` 路由與 WebSocket/SSE 串流連線。
  - 優化行動視口設定：於 `<meta name="viewport">` 補齊 `interactive-widget=resizes-content`，讓一般行動瀏覽器在鍵盤彈出時自動縮排視口。
- **防爬蟲與隱私安全標頭防護**：
  - 新增 `/robots.txt` 路由與檔案，預設對所有搜尋引擎與爬蟲全面宣告拒絕檢索（`User-agent: *\nDisallow: /`）。
  - 於 HTTP 伺服器回應加入防檢索標頭 `X-Robots-Tag: noindex, nofollow, noarchive, nosnippet`。
  - 於 `index.html` 加上 `<meta name="robots" content="noindex, nofollow, noarchive" />` 雙重保險。
  - 補齊基本隱私與防探測標頭（`X-Content-Type-Options: nosniff`、`Referrer-Policy: no-referrer`）。

## Capabilities

### New Capabilities

- `web-pwa-support`: 提供 Web App Manifest、PWA 獨立安裝、Apple 行動全螢幕標籤、安全靜態資源快取 Service Worker（嚴格排除 API/SSE），以及 `interactive-widget=resizes-content` 鍵盤視口適配。
- `crawler-stealth-guard`: 提供 `/robots.txt` 路由、全域 `X-Robots-Tag` 防索引標頭、HTML meta 標籤與隱私防探測標頭，阻擋搜尋引擎收錄與自動化爬蟲探測。

### Modified Capabilities

None.

## Impact

- **Affected code**:
  - `internal/server/server.go`: 註冊 `/robots.txt`、`/manifest.webmanifest`、`/sw.js` 靜態檔案路由，並於全域 HTTP 中介層注入防爬蟲與安全標頭。
  - `internal/static/index.html`: 加入 manifest 連結、Apple PWA meta 標籤、robots meta 標籤、`interactive-widget` 視口屬性，以及 Service Worker 註冊邏輯。
  - `internal/static/manifest.webmanifest`: 新增 PWA 資訊清單。
  - `internal/static/sw.js`: 新增 Service Worker 腳本。
  - `internal/static/robots.txt`: 新增爬蟲排除設定。
- **APIs**: 無破壞性變更；新增靜態資產端點。
- **Dependencies**: 無新增外部依賴，沿用現有 Go 標準函式庫與靜態資源機制。
