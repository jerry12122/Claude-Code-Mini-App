# Claude Code Mini App

Telegram Mini App，讓你在手機上遠端操控伺服器上的 AI 編碼 CLI。以**單一 Go 二進位**同時提供 REST API、WebSocket 與單檔 React 前端（無獨立建置步驟）。

## 功能概覽

- **多種後端工具** — 建立 Session 時可選 **Claude Code**、**Cursor Agent** 或 **Gemini CLI**（Codex 為預留選項，尚未實作 Runner）
- **遠端對話** — 透過 Telegram Mini App 或內網瀏覽器送出提示詞，於伺服器執行對應 CLI
- **Session 管理** — 建立、重新命名、刪除多個對話；每個 Session 綁定 `work_dir`、權限模式與 `agent_type`
- **即時串流** — WebSocket 雙向通訊，Markdown 串流顯示；支援多分頁／多連線廣播同步
- **權限流程** — Claude 的 `stream-json` 若回傳 `permission_denials`，會進入授權狀態；可「允許一次」或切換 `permission_mode`（Cursor／Gemini 亦支援模式切換，語意依各 CLI）
- **Telegram 驗證** — 以 Mini App `initData` HMAC 驗證 + 白名單 `tg_id`
- **網頁登入** — 內網 IP 範圍內可用密碼登入（HttpOnly Cookie），並可綁定白名單使用者以接收 **Telegram 完成／需授權通知**

## 架構

```
Telegram Mini App / 瀏覽器
        ↕ WebSocket（/sessions/:id/ws）
┌─────────────────────────────────────────┐
│           單一 Go 程式                     │
│  Fiber 路由 │ SQLite (WAL) sessions/messages│
│       每則使用者訊息 spawn 一個子進程       │
│  agent.Runner（claude / cursor / gemini） │
│  非互動 + 串流事件 → 前端狀態機             │
└─────────────────────────────────────────┘
```

- 每條訊息 **spawn 一個子進程**，用完即結束；**不使用 PTY**。
- Claude 路徑使用 `-p`、`--output-format stream-json`、`--resume <agent_session_id>` 等（詳見 `docs/headless.md`、`docs/claude-code-cli.md`）。
- Cursor、Gemini 各有對應 Runner 與事件轉換（見 `docs/cursor-agent-cli.md`、`docs/gemini-cli.md`）。

## 需求

- Go 1.25+
- 伺服器已安裝並登入你要使用的 CLI（例如 `claude`、`cursor agent`、`gemini` 等）
- Telegram Bot Token（[@BotFather](https://t.me/BotFather)）

## 建置與執行

### 1. 複製並編譯

```bash
git clone https://github.com/jerry12122/Claude-Code-Mini-App
cd claude-miniapp
go build -o claude-miniapp ./cmd/server
```

### 2. 設定

```bash
cp config.example.yaml config.yaml
```

**`config.yaml` 重點：**

```yaml
bot_token: "YOUR_BOT_TOKEN_HERE"

whitelist_tg_ids:
  - 123456789  # 允許的 Telegram 使用者 ID

web:
  # 網頁登入密碼（POST /auth/login 的 JSON body，勿放 query）
  password: "change-me"
  # 僅允許此 CIDR 範圍使用網頁密碼登入（真實 IP：CF-Connecting-IP > X-Forwarded-For > 直連）
  allowed_cidrs:
    - "127.0.0.0/8"
    - "10.0.0.0/8"
    - "172.16.0.0/12"
    - "192.168.0.0/16"
  session_ttl: "24h"
  # 網頁登入時預設綁定的通知對象（須在白名單）；多人時建議填寫
  # default_notify_tg_id: 123456789

no_auth: false  # true = 跳過驗證，僅限本機開發

server:
  port: 8080

db:
  path: "./claude-miniapp.db"
```

> **安全：** 勿將含真實憑證的 `config.yaml` 提交版本庫；生產環境勿開啟 `no_auth`。

### 3. 啟動

```bash
./claude-miniapp
```

預設監聽 `:8080`。靜態前端由 `./internal/static` 提供。

## 設定欄位說明

| 欄位 | 說明 | 預設 |
|---|---|---|
| `bot_token` | Telegram Bot API Token | 必填（`no_auth` 時可略） |
| `whitelist_tg_ids` | 允許的 Telegram 使用者 ID | `[]` |
| `web.password` | 網頁登入密碼 | `""`（未設定則無法用密碼登入） |
| `web.allowed_cidrs` | 允許使用網頁登入的來源 IP 範圍 | 內網私有位址 |
| `web.session_ttl` | 登入 Cookie 有效時間 | `24h` |
| `web.default_notify_tg_id` | 網頁登入預設綁定的通知對象 | `0`（未指定） |
| `no_auth` | 關閉所有驗證 | `false` |
| `server.port` | HTTP 埠號 | `8080` |
| `db.path` | SQLite 檔案路徑 | `./claude-miniapp.db` |

## REST API

| 方法 | 路徑 | 說明 |
|---|---|---|
| `GET` | `/sessions` | 列出所有 Session |
| `POST` | `/sessions` | 建立 Session（JSON 可含 `name`、`description`、`work_dir`、`permission_mode`、`agent_type`） |
| `PATCH` | `/sessions/:id` | 重新命名（`{"name":"..."}`） |
| `DELETE` | `/sessions/:id` | 刪除 Session |
| `GET` | `/sessions/:id/messages` | 訊息歷史 |
| `POST` | `/auth/login` | 網頁登入（僅限 `allowed_cidrs` 內 IP） |
| `POST` | `/auth/logout` | 登出並清除 Cookie |
| `WS` | `/sessions/:id/ws` | WebSocket 對話 |

除靜態檔與登入外，上述端點需通過驗證：Telegram `initData`（標頭 `X-Telegram-Init-Data` 或 query）或有效之網頁 Session Cookie。

## WebSocket 協議摘要

**Client → Server：**

```json
{ "type": "input", "data": "使用者提示" }
{ "type": "allow_once", "tools": ["Write"] }
{ "type": "set_mode", "mode": "acceptEdits" }
{ "type": "interrupt" }
{ "type": "reset_context" }
```

**Server → Client：**

```json
{ "type": "sync", "value": "IDLE", "messages": [...] }
{ "type": "status", "value": "STREAMING" }
{ "type": "delta", "content": "..." }
{ "type": "user_message", "content": "..." }
{ "type": "permission_request", "tools": [...] }
{ "type": "reset" }
{ "type": "error", "content": "..." }
```

連線建立時會收到 `sync`（還原 UI 狀態與歷史）。背景任務與授權狀態會反映在 Session 的 `status` 欄位（如 `idle`、`running`、`awaiting_confirm`）。

## 權限模式（Claude / Cursor / Gemini）

| 模式 | 說明 |
|---|---|
| `default` | 預設；寫入／執行等依 CLI 觸發授權或確認 |
| `acceptEdits` | 較寬鬆的檔案編輯（Claude 對應 `--permission-mode acceptEdits`） |
| `bypassPermissions` | 跳過權限檢查（高風險；Cursor 在 bypass 時會帶額外 force 行為） |

Claude 遭拒時可於前端選擇「允許一次」或永久切換模式；非 Claude 代理不處理 `allow_once`。

## Telegram 通知

若請求已綁定 `tg_id`（Telegram 內開啟或網頁登入綁定白名單使用者），工作完成或需要授權時會透過 Bot API 推送簡短訊息。

## 技術棧

| 層級 | 技術 |
|---|---|
| 後端 | Go、[Fiber](https://gofiber.io/) |
| 資料庫 | SQLite（WAL）`modernc.org/sqlite` |
| WebSocket | `gofiber/contrib/websocket` |
| 設定 | [Viper](https://github.com/spf13/viper)（`config.yaml`） |
| 前端 | 單檔 React SPA（`internal/static`） |
| 驗證 | Telegram `initData` HMAC-SHA256；網頁 Session Cookie + IP CIDR |

## 說明文件

- `docs/plan.md` — 規格與路線圖  
- `docs/headless.md` — Claude `-p` 與 stream-json  
- `docs/claude-code-cli.md`、`docs/cursor-agent-cli.md`、`docs/gemini-cli.md` — 各 CLI 參考  

## 授權

MIT
