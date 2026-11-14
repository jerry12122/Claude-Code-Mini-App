## 狀態：已取消

2026-11-13：依使用者決定，本變更不再實作，保留提案與設計供歷史查閱。以下內容為取消前的規劃，並非已實作功能。

## Why

目前系統雖具備完整多 Agent 支援與單一二進位部署，但前端缺乏打包建置管線，強制在手機端下載並以 Babel 即時轉譯 19 個腳本且強烈依賴 6 個外部 CDN，在弱網環境下容易白屏；同時 SQLite 資料庫採用開機時以字串匹配忽略錯誤的脆弱 tryAlter 機制，缺乏版本號控制；執行期產物（DB、上傳檔案、GPU 日誌）與原始碼目錄混雜。此重構為系統整備出穩健的前端打包管線、版本化資料庫遷移與資料隔離架構。

## What Changes

- 前端建置現代化：建立 Vite + React 18 + Tailwind 打包管線，將散落的 19 個 JS 模組編譯為 Minified 靜態產物，移除 @babel/standalone、Tailwind Play CDN 與 React Dev CDN，透過 Go embed.FS 實現真正自包含、零外部 CDN 依賴的單一二進位。
- 版本化資料庫遷移：引入基於 PRAGMA user_version 的順序版本化遷移架構，取代開機時高達 15+ 次且依賴錯誤字串比對的 tryAlter 邏輯。
- 執行期產物與資料隔離：將 SQLite 資料庫預設路徑與使用者上傳檔案（uploads）自專案根目錄與 internal/static/uploads 移至統一的 data/ 目錄（data/db/ 與 data/uploads/），並在 .gitignore 加入 GPU 快取（NVIDIA Corporation/）與本機日誌過濾規則。
- 清理殘留目錄：移除已廢棄且無檔案的 internal/gemini/ 空目錄。

## Non-Goals (optional)

- 不更動現有 Telegram Mini App 的使用者操作流程、畫面功能與 WebSocket 通訊通訊協議。
- 不在本次變更中重構 internal/ws/handler.go 業務領域分層（保留至階段二執行）。
- 不更動 Claude、Codex、Cursor、KiroACP、Antigravity 的 CLI 呼叫模式與權限旗標。

## Impact

- Affected specs: none
- Affected code:
  - New: web/package.json, web/vite.config.js, web/src/, internal/db/migrations.go
  - Modified: internal/server/server.go, internal/db/db.go, internal/api/upload.go, config.example.yaml, .gitignore
  - Removed: internal/gemini/, internal/static/uploads/
- Compatibility: no capability-level observable behavior changes
