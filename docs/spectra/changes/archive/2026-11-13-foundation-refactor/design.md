## Context

目前系統是 Go 單一二進位服務，支援多 Agent CLI 與 Telegram Mini App。然而目前的架構面臨以下三個基礎問題：
1. 前端在 internal/static 透過 index.html 以 script 標籤載入 19 個 JavaScript 檔案，並在手機端瀏覽器透過 @babel/standalone 即時轉譯 JSX，且強烈依賴 6 個外部 CDN（Tailwind Play CDN、React 18 Dev、Babel、highlight.js 等）。這導致行動端開啟延遲嚴重、耗電，且在 CDN 異常或離線弱網時完全無法運作。
2. SQLite 資料庫遷移機制脆弱，開機時重複嘗試執行 CREATE TABLE 與 15+ 個 ALTER TABLE 語法，僅依賴字串匹配 "duplicate column" 來忽略錯誤，缺乏 PRAGMA user_version 版本號控制。
3. 執行期產物（資料庫檔案、使用者上傳圖片、GPU/桌面快取）散落於專案根目錄與原始碼目錄。

為奠定後續領域分層與架構演進的基礎，本設計規劃第一階段的基礎整備方案。

## Goals / Non-Goals

### Goals
- 建立標準的前端打包管線（Vite + React 18 + Tailwind），移除所有外部 CDN 與瀏覽器端 Babel 轉譯，將前端產物透過 Go embed 內嵌至二進位檔案。
- 引入基於 SQLite PRAGMA user_version 的順序版本化遷移架構，取代脆弱的開機 tryAlter 字串匹配。
- 規範執行期資料目錄（data/），將 SQLite 資料庫與使用者上傳檔案隔離至專屬目錄，並完善 .gitignore。
- 清理已廢棄的 internal/gemini/ 空目錄。

### Non-Goals
- 不更動現有 Telegram Mini App 的使用者操作介面外觀、功能與 WebSocket 通訊協定。
- 不在本次變更中拆解 internal/ws/handler.go 上帝函式（此為第二階段重構目標）。
- 不更動 Agent Runner（Claude, Codex, Cursor, Kiro, Antigravity）的呼叫模式與權限旗標。

## Decisions

### 1. 前端建置工具：採用 Vite + React 18 + Tailwind CSS
- **決策**：在 web/ 目錄下建立標準的前端專案，使用 Vite 作為 Bundler，產出 Minified 靜態資源至 web/dist。
- **原因**：Vite 構建速度極快、支援原生 ES Modules，並原生支援 Tailwind CSS PostCSS 插件，能將現有程式碼平滑轉為標準模組化語法，徹底告別全域 window 污染與瀏覽器即時 Babel 轉譯。
- **替代方案評估**：曾考慮僅用 esbuild 單檔打包，但缺乏 Tailwind CSS 的官方優化管線與開發時 HMR 支援，長遠維護性不如 Vite。

### 2. 靜態資源內嵌：採用 Go embed.FS
- **決策**：由 Go server 透過 `//go:embed all:dist` 內嵌前端編譯產物。
- **原因**：達成真正自給自足的 Single Binary 部署，不依賴外部檔案路徑，亦不受外部 CDN 網路連線影響。

### 3. 資料庫遷移：基於 PRAGMA user_version 的順序版本化遷移
- **決策**：建立 Versioned Migrations 機制。定義結構體：
  ```go
  type Migration struct {
      Version int
      Name    string
      Up      func(tx *sql.Tx) error
  }
  ```
  啟動時讀取 `PRAGMA user_version`，僅依序執行版本號大於目前版本的 Migration，並在交易中以 `PRAGMA user_version = N` 更新版本號。現有資料庫結構作為 Baseline (Version 1)。
- **原因**：具備確定性、可追溯性與交易安全性，徹底消弭啟動時不必要的 ALTER TABLE 錯誤日誌。

### 4. 執行期資料隔離：規範 data/ 目錄與平滑向下相容
- **決策**：
  - 預設資料庫路徑調整為 `./data/claude-miniapp.db`。
  - 使用者上傳目錄調整為 `./data/uploads/`。
  - 向下相容邏輯：若 `./data/claude-miniapp.db` 不存在但根目錄 `./claude-miniapp.db` 存在，自動載入根目錄檔案或提示平滑轉移，確保既有使用者資料不丟失。

## Implementation Contract

- 建置流程契約：
  - 執行 `npm --prefix web run build` 後產出 `web/dist/index.html` 與相關 assets。
  - Go 編譯（`go build ./cmd/server`）能成功打包內嵌資源。
- 資料庫契約：
  - 任何全新資料庫開機後，`PRAGMA user_version` 正確提升至最新版本號。
  - 既有包含現存資料的資料庫能平滑識別為 Baseline 並安全升級，無資料遺失。
- 功能行為相容性：
  - 系統對外的所有 HTTP API 與 WebSocket 協議格式維持 100% 相容，無 observable capability-level 變更。

## Risks / Trade-offs

- [Risk] 既有使用者升級後找不到原本位於根目錄的資料庫 -> [Mitigation] 在 config 預設值與 db 初始化流程中保留相容性路徑判斷：若 `./data/` 尚無 DB 且根目錄有舊 DB，維持載入舊路徑並於日誌提醒。
- [Risk] 前端由全域腳本轉為 ES modules 時可能漏掉變數引用 -> [Mitigation] 依賴 Vite 編譯時的嚴格語法與引用檢查，建置通過前不部署。
- [Risk] Windows 與 Linux 在 data/ 目錄權限上的差異 -> [Mitigation] 在應用啟動時以 os.MkdirAll 自動建立目錄並給予標準權限。

## Migration Plan

1. 建立 `web/` 目錄與 Vite 設定，遷移現有 JavaScript 程式碼為標準 ES Modules，配置 Tailwind CSS。
2. 驗證前端打包產物在純離線環境下的運作，確認移除所有外部 CDN。
3. 實作 `internal/db/migrations.go`，將現有 Schema 結構標準化為 Version 1 Baseline。
4. 重構 `internal/server` 靜態檔案服務，改用 `embed.FS` 掛載 `web/dist`。
5. 調整資料庫與上傳目錄的預設路徑至 `data/`，補充相容性讀取機制。
6. 更新 `.gitignore` 與清理 `internal/gemini/` 空目錄。

## Open Questions

- (none)
