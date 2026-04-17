# 工作清單 (Todo)

> 對應 `docs/plan.md` 的四個 Phase，細項由上到下依序完成。

---

## Phase 1 — Go 後端 + stream-json 串接

### 1.1 專案初始化
- [ ] `go mod init`，建立基本目錄結構 (`cmd/`, `internal/`)
- [ ] 加入依賴：`gofiber/fiber/v2`、`mattn/go-sqlite3`（或 `modernc.org/sqlite`）、`gorilla/websocket`

### 1.2 Claude 子進程執行器
- [ ] 實作 `claude.Run(prompt, sessionID, mode, allowedTools string)` 函式
- [ ] 以 `exec.Command` spawn `claude -p` 子進程
- [ ] 捕捉 stdout（stream-json）並逐行解析

### 1.3 stream-json 事件解析
- [ ] 定義 Go struct：`StreamEvent`、`APIEvent`、`Delta`、`PermissionDenial`
- [ ] 實作 `ParseEvent(line []byte) (*StreamEvent, error)`
- [ ] 單元測試：用真實輸出樣本驗證解析正確性

### 1.4 WebSocket Handler（基礎版）
- [ ] Fiber WebSocket route：`/sessions/:id/ws`
- [ ] 接收使用者 `input` 訊息，spawn 子進程
- [ ] 將 `delta` 事件即時推送至前端
- [ ] 推送 `status` 狀態切換訊息

---

## Phase 2 — SQLite + Session 管理

### 2.1 資料庫初始化
- [ ] 建立 SQLite 連線（WAL 模式）
- [ ] 執行 schema migration（`users`、`sessions`、`messages` 三張表）

### 2.2 Session CRUD
- [ ] `GET /sessions` — 列出所有 Session
- [ ] `POST /sessions` — 建立 Session（name, description, work_dir, permission_mode）
- [ ] `DELETE /sessions/:id` — 刪除 Session

### 2.3 Session Resume 邏輯
- [ ] 首次執行：不帶 `--resume`，從 `result.session_id` 取得 ID 並存入 SQLite
- [ ] 後續執行：帶 `--resume <id>`

### 2.4 Permission 授權流程
- [ ] 解析 `result.permission_denials`，送 WS `permission_request` 事件
- [ ] 處理 `allow_once`：加 `--allowedTools` 重新 resume
- [ ] 處理 `set_mode`：更新 `sessions.permission_mode`，重新 resume
- [ ] `allowed_tools` 欄位累計更新邏輯

### 2.5 訊息紀錄
- [ ] 每次對話的 user/claude 內容寫入 `messages` 表
- [ ] `GET /sessions/:id/messages` — 回傳歷史紀錄

---

## Phase 3 — 單檔 React SPA

### 3.1 基礎架構
- [ ] 單一 `index.html`，由 Go Fiber 提供服務
- [ ] CDN 載入：React 18、Babel Standalone、Tailwind Play CDN、marked.js、highlight.js
- [ ] WebSocket 連線管理（自動重連）

### 3.2 狀態機 UI
- [ ] 實作四種狀態：`IDLE` / `THINKING` / `STREAMING` / `AWAITING_CONFIRM`
- [ ] `THINKING`：脈動動畫，禁用輸入框
- [ ] `STREAMING`：逐字渲染 Markdown（marked.js）
- [ ] `IDLE`：開放輸入框，顯示模式切換按鈕

### 3.3 授權確認對話框
- [ ] `AWAITING_CONFIRM`：顯示被拒絕的操作詳情
- [ ] 「允許此操作」按鈕 → 送 `allow_once`
- [ ] 「允許並記住」按鈕 → 送 `set_mode`

### 3.4 Session 管理 UI
- [ ] Session 列表側欄（切換、新增、刪除）
- [ ] 新增 Session 表單（name, work_dir, permission_mode）
- [ ] 模式切換下拉選單（顯示當前 permission_mode）

### 3.5 代碼渲染
- [ ] highlight.js 套用至 marked.js 輸出的 `<code>` 區塊
- [ ] 程式碼區塊加上複製按鈕

---

## Phase 4 — Telegram 整合

### 4.1 initData 簽名校驗
- [ ] 後端實作 HMAC-SHA256 校驗（依照 Telegram 官方規範）
- [ ] Middleware：所有 API/WS 請求必須帶有效 initData

### 4.2 使用者白名單
- [ ] 從 initData 取得 `tg_id`
- [ ] 查詢 SQLite `users` 表，不在白名單則回傳 403

### 4.3 TMA UI 調整
- [ ] 套用 Telegram WebApp 主題色（`window.Telegram.WebApp.themeParams`）
- [ ] 處理 safe area inset（底部導航列避讓）
- [ ] 呼叫 `Telegram.WebApp.ready()` 與 `expand()`