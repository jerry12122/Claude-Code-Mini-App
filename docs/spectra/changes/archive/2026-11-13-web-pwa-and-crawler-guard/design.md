## Context

claude-miniapp 主要作為遠端作業的 Web 與 Telegram 控制中心。當使用者透過電腦或行動瀏覽器直接以 Web 模式（密碼登入或 `/share/` 訪客模式）連線時，行動瀏覽器（如 iOS Safari、Android Chrome）動態網址列與工具列常導致視口高度被擠壓或滾動不穩；此外，一般行動瀏覽器在點擊輸入框彈出虛擬鍵盤時，缺乏動態調整視口大小的機制。
另一方面，由於遠程操作工具若因使用者設定不慎被暴露於公網，可能遭搜尋引擎或自動化爬蟲程式掃描收錄，引發安全與隱私隱患。因此本設計旨在補齊 PWA 獨立運行支援、行動鍵盤視口適配，並建構完整且多層的防爬蟲隱私防護。

## Goals / Non-Goals

**Goals:**
- 提供完整的 Web App Manifest 與 Service Worker，支援手機與桌面瀏覽器「安裝為獨立應用（Standalone Mode）」。
- Service Worker 嚴格放行所有 `/api/` 路由、WebSocket 與 SSE 即時串流，絕不破壞即時打字機效果與連線同步。
- 於 HTML `<meta name="viewport">` 導入 `interactive-widget=resizes-content`，解決行動瀏覽器鍵盤彈出時的高度擠壓問題。
- 建立三道防爬蟲與防掃描防線：`/robots.txt`、HTTP `X-Robots-Tag` 標頭、HTML `<meta name="robots">` 標籤，並附加隱私防探測標頭。

**Non-Goals:**
- 不實作 Web Push Notifications（桌面推播已由既有更新器與 Telegram 機器人處理）。
- 不實作離線聊天發送（遠程操作工具需即時連線執行緒，不支援離線假執行）。
- 不實作公網人機驗證（CAPTCHA），防護聚焦於禁止爬蟲收錄與隱私標頭。

## Decisions

### Decision 1: Service Worker 靜態外殼快取與 API/即時串流絕對旁路 (Bypass)

Service Worker (`sw.js`) 僅快取應用靜態資產（如核心 HTML、CSS、JS、圖示），加速冷啟動並提供獨立安裝必要條件。
在 `fetch` 事件中，只要請求路徑為 `/api/*` 或連線協議為 WebSocket/SSE，一律直接轉發至網路（`fetch(event.request)`），絕不進行任何快取攔截，保證通訊即時性與數據一致性。

### Decision 2: 原生標準 Web App Manifest 與 Apple Web Clip 雙軌設定

在伺服器提供標準 `/manifest.webmanifest`（Content-Type 為 `application/manifest+json`），設定 `display: "standalone"`、主題色彩與圖示定義；同時在 `index.html` 配置 Apple 專屬的 `apple-mobile-web-app-capable` 與狀態列樣式，確保跨 iOS 與 Android 皆具備一致的無網址列全螢幕體驗。

### Decision 3: 階層式防爬蟲與防探測保護（Robots Protocol + HTTP Header + HTML Meta）

採取三層立體防禦，避免任何公網意外收錄：
1. **協議層**：提供 `/robots.txt` 路由，宣告 `User-agent: *\nDisallow: /`。
2. **傳輸層**：在伺服器全域中介層對所有網頁與靜態頁面注入 `X-Robots-Tag: noindex, nofollow, noarchive, nosnippet`，並附帶 `X-Content-Type-Options: nosniff` 與 `Referrer-Policy: no-referrer`。
3. **文件層**：在 `index.html` 內建 `<meta name="robots" content="noindex, nofollow, noarchive" />` 作為防護兜底。

### Decision 4: 行動視口互動適配 (interactive-widget=resizes-content)

在 `<meta name="viewport">` 中加入 `interactive-widget=resizes-content` 屬性。在支援的現代行動瀏覽器（Chrome 108+、iOS Safari 16.4+）中，當虛擬鍵盤升起時會動態將視口高度扣除鍵盤高度，避免傳統行動瀏覽器將頁面硬性頂推產生的位移白邊。

## Implementation Contract

- **端點與資產契約**：
  - `GET /robots.txt`: 回傳 HTTP 200，`Content-Type: text/plain; charset=utf-8`，內容固定為 `User-agent: *\nDisallow: /\n`。
  - `GET /manifest.webmanifest`: 回傳 HTTP 200，`Content-Type: application/manifest+json`，包含 `name: "Claude Code MiniApp"`, `display: "standalone"`, `start_url: "/"`, `theme_color`, `icons`。
  - `GET /sw.js`: 回傳 HTTP 200，`Content-Type: application/javascript`，實作安全的靜態資源快取生命週期。
- **HTTP 標頭契約**：
  - 伺服器輸出的所有 HTML 回應與靜態資源回應皆包含：
    - `X-Robots-Tag: noindex, nofollow, noarchive, nosnippet`
    - `X-Content-Type-Options: nosniff`
    - `Referrer-Policy: no-referrer`
- **驗收標準 (Acceptance Criteria)**：
  - 以 `curl` 或 Go 單元測試請求 `/robots.txt`，驗證內容為全路徑禁止。
  - 驗證主要 HTTP 回應中皆帶有 `X-Robots-Tag` 與安全性標頭。
  - 在瀏覽器中載入首頁，確認 Service Worker 正確註冊且不影響 `/api/events` 與聊天對話。
  - 驗證 `manifest.webmanifest` 可被 Chrome / Safari 讀取並識別為 PWA。
- **範疇邊界 (Scope Boundaries)**：
  - **範圍內**：`internal/server` 路由與標頭中介層、`internal/static` 中的 manifest、sw.js、robots.txt 及 index.html meta 標籤調整。
  - **範圍外**：不變更 Telegram WebApp 現有的 `--tg-viewport-height` 運算邏輯，不介入後端 Agent 執行流程。

## Risks / Trade-offs

- **[Risk] Service Worker 快取過期導致前端更新延遲** → **Mitigation**: Service Worker 在 `activate` 事件中即時清理舊版快取，且靜態外殼採用 network-first 或快取版本比對，並在必要時支援 `self.skipWaiting()`。
- **[Risk] 爬蟲可能無視 robots.txt** → **Mitigation**: 搭配 HTTP 標頭 `X-Robots-Tag` 與 HTML Meta Robots 標籤，主流合法搜尋引擎皆會嚴格遵守；惡意掃描者則由既有的登入密碼驗證、Telegram initData 簽名及 IP 限制進行邊界攔截。
