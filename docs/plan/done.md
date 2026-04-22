# 已完成項目 (Done)

---

## Phase 1 — Go 後端 + stream-json 串接
- [x] **專案初始化**：建立基本目錄結構 (`cmd/`, `internal/`)，加入 Go Fiber、SQLite、WebSocket 依賴。
- [x] **Claude 子進程執行器**：實作 `agent.Runner` 介面，以 `exec.Command` spawn `claude -p` 子進程。
- [x] **stream-json 事件解析**：定義 `StreamEvent` 等結構，正確解析並分發 CLI 輸出事件。
- [x] **WebSocket Handler**：實作即時串流推送至前端。

## Phase 2 — SQLite + Session 管理
- [x] **資料庫初始化**：建立 SQLite 連線並執行 schema migration (users, sessions, messages)。
- [x] **Session CRUD**：提供 API 進行會話的新增、刪除、列表與模式變更。
- [x] **Session Resume 邏輯**：正確處理 `--resume <id>` 以維持對話連續性。
- [x] **Permission 授權流程**：解析 `permission_denials` 並透過 WS 觸發前端授權對話框。
- [x] **訊息紀錄**：將對話歷史持久化至資料庫。

## Phase 3 — 單檔 React SPA
- [x] **基礎架構**：Go Fiber 提供靜態 `index.html`，內嵌完整 React SPA。
- [x] **狀態機 UI**：實作 `IDLE` / `THINKING` / `STREAMING` / `AWAITING_CONFIRM` 狀態切換。
- [x] **授權確認對話框**：提供「允許此操作」與「允許並記住」之即時授權介面。
- [x] **Session 管理 UI**：側欄會話列表、建立與切換。
- [x] **代碼渲染**：整合 `marked.js` 與 `highlight.js` 實現語法高亮。

## Phase 4 — Telegram 整合
- [x] **initData 簽名校驗**：實作 HMAC-SHA256 校驗 Telegram 登入資訊。
- [x] **使用者白名單**：限制僅允許特定 Telegram ID 使用系統。
- [x] **TMA UI 調整**：套用 Telegram 主題色與視口高度優化。

---

## 進階擴充功能 (Advanced Features)

### 🧩 Multi-Agent 整合
- [x] **Runner 抽象化**：定義統一的 `agent.Runner` 介面。
- [x] **Gemini 整合**：支援 `gemini` CLI 串流執行。
- [x] **Cursor 整合**：支援 `cursor-agent` CLI 串流執行。
- [x] **動態工廠**：依 Session 設定自動建立對應工具實例。

### 🐚 Shell 執行模式
- [x] **模式切換**：支援在 Agent 與 Shell 模式間一鍵切換。
- [x] **安全性檢核**：實作 Shell 指令白名單與工作目錄鎖定。
- [x] **即時串流**：即時回傳 stdout/stderr 輸出。

### 🛠️ 穩定性與效能優化
- [x] **中斷功能修正**：解決 Windows 下孤兒進程問題，確保中斷即時生效。
- [x] **並行 Session 支援**：任務在背景執行，WS 斷線不影響任務進度，新連線自動 `sync` 狀態。
- [x] **啟動自動修復**：伺服器重啟時自動標記當機前的 `running` 任務為 `done`。

### 🎨 UI/UX 改進
- [x] **目錄分組**：Session 列表支援依工作目錄收納。
- [x] **Slash 命令選單**：輸入 `/` 彈出常用命令快捷選單。
- [x] **訊息轉發 (Forward)**：支援將 assistant 訊息內容轉發至其他會話執行。
- [x] **多主題支援**：內建 Nexus、Focus 等不同視覺風格主題。
