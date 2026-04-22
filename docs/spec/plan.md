# Claude Code TMA 項目規格書 (Technical Specification)

## 1. 項目概述
建立一個基於 Telegram Mini App 的轉介服務，讓使用者能透過行動端遠端操控伺服器上的 `claude code` CLI 工具。支援多會話管理、權限控制與自動化狀態回復。

## 2. 系統架構 (System Architecture)

### 2.1 組件架構
* **前端 (Frontend)**: 單檔案 React SPA，由 Go 直接內嵌提供（無需 CDN 或分離部署）。
* **後端 (Backend)**: Go (Fiber 框架)，單一二進位同時提供 API、WebSocket 與靜態 HTML。
* **持久層 (Database)**: SQLite (WAL 模式)，儲存 Session 與對話。
* **通訊協議**: WebSocket（雙向即時，單一連線處理串流與輸入）。
* **CLI 執行模式**: 每條訊息 spawn 一個 `claude -p --resume <id>` 子進程，用後即棄。

### 2.2 部署架構圖

```
Telegram Mini App (WebView)
        ↕ WebSocket (:8080/sessions/:id/ws)
┌─────────────────────────────────────┐
│         Go Single Binary            │
│  ┌─────────┐  ┌──────────────────┐  │
│  │  Fiber  │  │   SQLite (WAL)   │  │
│  │ Router  │  │  sessions/msgs   │  │
│  └────┬────┘  └──────────────────┘  │
│       │ spawn per message           │
│  claude -p --resume <id>            │
│         --output-format stream-json │
│         --permission-mode <mode>    │
└─────────────────────────────────────┘
```

---

## 3. 核心功能規格

### 3.1 Session 管理
* **建立 Session**:
    * 參數：`Name` (自訂名稱)、`Description` (描述)、`WorkDir` (伺服器路徑)。
    * 權限模式：`permission_mode` 欄位，預設 `default`。
* **Session 切換與持久化**:
    * 重啟後可透過 `claude -p --resume <id>` 恢復。
    * SQLite 記錄所有 Session 元數據以便 Recall。
* **刪除 Session**: 強制結束進程並清理資源。

### 3.2 Permission Mode 系統

每個 Session 可在任意時刻切換模式，透過下次 resume 時帶入 `--permission-mode` 生效：

| 模式 | CLI 旗標 | 說明 | 適用場景 |
| :--- | :--- | :--- | :--- |
| `default` | `--permission-mode default` | 遇到寫入/執行操作時會觸發授權流程 | 一般使用（預設） |
| `acceptEdits` | `--permission-mode acceptEdits` | 自動允許檔案讀寫，Bash 仍需確認 | 編輯為主的任務 |
| `bypassPermissions` | `--permission-mode bypassPermissions` | 跳過所有權限檢查 | 危險模式，使用者明確開啟 |

> SQLite `sessions.permission_mode` 欄位記錄當前模式，每次執行 resume 時帶入。

### 3.3 聊天室狀態機 (Chatroom State Machine)
系統必須嚴格遵守以下狀態轉換，以確保 UI 與 CLI 同步：

| 狀態 (State) | 觸發條件 | UI 行為 |
| :--- | :--- | :--- |
| **`IDLE`** | 啟動或指令執行完畢 | 開放輸入框，顯示模式切換按鈕。 |
| **`THINKING`** | 送出指令後至收到首個輸出前 | 禁用輸入，顯示脈動動畫。 |
| **`STREAMING`** | 正在接收 `stream_event` 文字增量 | 實時渲染 Markdown，顯示中斷按鈕。 |
| **`AWAITING_CONFIRM`** | `result` 事件的 `permission_denials` 非空 | 鎖定輸入，顯示操作詳情與授權按鈕。 |

#### AWAITING_CONFIRM 授權流程

```
1. 後端收到 result 事件，permission_denials 非空
   → 解析 denied_tools（tool_name + tool_input）
   → 送 WS: { type: "permission_request", tools: [...] }

2. 前端顯示每個被拒絕的操作，提供兩個按鈕：
   ┌─────────────────────────────────┐
   │ Claude 想要寫入 `test.txt`      │
   │ [允許此操作]  [允許並記住]      │
   └─────────────────────────────────┘

3a. 使用者點「允許此操作」
    → 前端送 WS: { type: "allow_once", tools: ["Write"] }
    → 後端以 --resume <id> --allowedTools "Write" 重新執行

3b. 使用者點「允許並記住」
    → 前端送 WS: { type: "set_mode", mode: "acceptEdits" }
    → 後端更新 sessions.permission_mode = "acceptEdits"
    → 以 --resume <id> --permission-mode acceptEdits 重新執行
```

### 3.4 安全與驗證
* **TG 白名單**: 僅允許 SQLite `users` 表中的 `tg_id` 訪問。
* **簽名校驗**: 後端使用 `BOT_TOKEN` 校驗 TMA 傳入的 `initData`。

---

## 4. 數據結構 (Database Schema)

```sql
-- 使用者白名單
CREATE TABLE users (
    tg_id INTEGER PRIMARY KEY,
    username TEXT
);

-- Session 主表
CREATE TABLE sessions (
    id TEXT PRIMARY KEY,        -- Claude 原生 Session ID（UUID）
    tg_id INTEGER,
    name TEXT,
    description TEXT,
    work_dir TEXT,
    permission_mode TEXT DEFAULT 'default', -- default | acceptEdits | bypassPermissions
    allowed_tools TEXT DEFAULT '',          -- 累計允許的工具，逗號分隔
    status TEXT DEFAULT 'IDLE',
    last_active DATETIME
);

-- 對話紀錄 (Recall 用)
CREATE TABLE messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT,
    role TEXT,       -- 'user' | 'claude'
    content TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

---

## 5. 接口與通訊定義 (API & WebSocket)

### 5.1 REST API
| 方法 | 路徑 | 說明 |
| :--- | :--- | :--- |
| `GET` | `/sessions` | 列出所有 Session |
| `POST` | `/sessions` | 建立新 Session |
| `DELETE` | `/sessions/:id` | 刪除 Session |
| `WS` | `/sessions/:id/ws` | WebSocket 連線 |

### 5.2 WebSocket 訊息格式

**C2S (Client to Server)**:
```json
{ "type": "input",      "data": "ls -al" }
{ "type": "allow_once", "tools": ["Write"] }
{ "type": "set_mode",   "mode": "acceptEdits" }
{ "type": "interrupt" }
```

**S2C (Server to Client)**:
```json
{ "type": "status",             "value": "STREAMING" }
{ "type": "delta",              "content": "### Hello" }
{ "type": "permission_request", "tools": [{ "name": "Write", "input": { "file_path": "..." } }] }
{ "type": "result",             "session_id": "...", "cost_usd": 0.01 }
```

### 5.3 CLI 執行範本

```bash
# 一般執行
claude -p "<prompt>" \
  --output-format stream-json \
  --resume <session_id> \
  --permission-mode <mode>

# 允許特定工具後重新執行
claude -p "please retry" \
  --output-format stream-json \
  --resume <session_id> \
  --permission-mode <mode> \
  --allowedTools "Write,Edit"
```

---

## 6. 前端開發技術指標 (Single-file SPA)

* **無建置模式**: 使用 `htm` + Preact（避免 Babel CSP 問題）或 `<script type="text/babel">`。
* **CDN 依賴清單**:
    * `react@18`, `react-dom@18`
    * `babel-standalone`（或改用 Preact + htm 繞過 CSP）
    * `tailwindcss` (Play CDN)
    * `marked.js` (Markdown 解析)
    * `highlight.js` (代碼高亮)

---

## 7. 開發里程碑 (Roadmap)

1. **Phase 1**: Go 後端實作 `claude -p --output-format stream-json` 串接，解析事件流。
2. **Phase 2**: SQLite 整合，實作 Session 建立、resume 與 permission_mode 切換邏輯。
3. **Phase 3**: 單檔 React SPA 介面，包含 Markdown 渲染、狀態機 UI 與授權確認對話框。
4. **Phase 4**: 串接 Telegram Bot API，實作白名單校驗與 TMA initData 簽名驗證。

---

## 8. 注意事項
* **非互動模式**: 全程使用 `-p` 旗標，不走 PTY，無需 ANSI 清理。
* **Session ID 來源**: 從 `result` 事件的 `session_id` 欄位取得，首次建立後存入 SQLite。
* **逾時處理**: Session 閒置超過 1 小時自動標記為 `IDLE`，進程由 OS 回收。
* **allowed_tools 累積**: 每次 `allow_once` 可選擇性更新 SQLite 的 `allowed_tools`，下次執行自動帶入。
