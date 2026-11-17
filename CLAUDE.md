<!-- SPECTRA:START v1.3.0 -->

# Spectra Instructions

This project uses Spectra for Spec-Driven Development(SDD). Specs live in `docs/spectra/specs/`, change proposals in `docs/spectra/changes/`.

## Skills

Each `/spectra-*` skill carries its own trigger description; these are the groups:

- Shape and plan → `/spectra-discuss`, `/spectra-propose`
- Continue tasks for an identified change → `/spectra-apply`
- Update requirements or plans for an identified change → `/spectra-ingest`
- Quality gate → `/spectra-verify`, `/spectra-review`, `/spectra-analyze`, `/spectra-audit`, `/spectra-drift`, `/spectra-debug`
- Finish → `/spectra-archive`, `/spectra-commit`

Explicit skill invocation takes precedence. Apply existing authorization within its unchanged scope.

## Workflow

discuss? → propose → apply ⇄ ingest → verify / review → archive

- `discuss` is optional — skip if requirements are clear
- Requirements change mid-work? Plan mode → `ingest` → resume `apply`

## Parked Changes

Changes can be parked（暫存）— temporarily moved out of `docs/spectra/changes/`. Parked changes won't appear in `spectra list` but can be found with `spectra list --parked`. To restore: `spectra unpark <name>`. The `/spectra-apply` and `/spectra-ingest` skills disclose parking and restore when the named operation is already explicitly requested; respect a known refusal, otherwise ask for missing authorization.

<!-- SPECTRA:END -->

# Claude Code 指引 (CLAUDE.md)

## 語言規則
**一律使用正體中文**回應所有訊息與註解，程式碼變數/函式名稱除外。

---

## 項目概述
Telegram Mini App，讓使用者透過手機遠端操控伺服器上的 `claude code` CLI。

詳細規格：`docs/spec/plan.md`
工作清單：`docs/plan/todo.md`

---

## 文件架構
- **docs/spec/**：技術規格書與通訊協議規範 (如：CLI 旗標、WebSocket Schema)
- **docs/plan/**：專案規劃與進度管理
    - `todo.md`：目前整體的待辦事項清單
    - `done/`：已完成任務的詳細實作紀錄與歷史存檔
- **docs/plan/todo/**：未來擴展功能的詳細規劃草案

### 文件維護規則
- **歸檔流程**：當計畫實作完成後，須檢查文件位置是否正確，並將該計畫文件從 `docs/plan/` 或 `docs/plan/todo/` 移至 **`docs/plan/done/`**。
- **狀態同步**：移動文件後，需同步更新 `docs/plan/todo.md`。

---

## 架構決策（不得更動）

### CLI 執行模式
- 每條訊息 **spawn 一個子進程**，用後即棄
- 全程使用 `-p`（非互動）+ `--output-format stream-json`
- 以 `--resume <session_id>` 保持對話連續性
- **不使用 PTY**

```bash
# 標準執行範本
claude -p "<prompt>" \
  --output-format stream-json \
  --resume <session_id> \
  --permission-mode <mode> \
  --allowedTools "<tools>"
```

### 通訊協議
- **WebSocket**（雙向單連線），不用 SSE

### 部署
- **Go 單一二進位**，同時提供 API + WebSocket + 靜態 HTML
- 不分離前後端部署

---

## 瀏覽器支援政策 (Browser Support)

前端跑在三種環境：Wails WebView2（Windows 桌面，Edge Chromium，隨系統更新）、
Telegram 內建 WebView（Android System WebView／iOS WKWebView，版本跟隨 OS）、
一般瀏覽器（LAN 或反向代理）。

- **允許 Baseline Widely available 與 Newly available**，不需 fallback。
  含 Dialog、Popover（Baseline 2025-01-27）、Invoker Commands（2025-12-12）。
- **Limited availability 功能只做漸進增強**，缺了要能優雅降級。
  例如 `closedby="any"`（Safari 未支援）缺失時，`Esc` 與返回手勢仍可關閉；
  anchor positioning 缺失時不要無條件寫 `inset: auto`／`margin: 0`。
- **不引入 polyfill 套件**。沒有 npm，Go 端的 esbuild 只做逐檔 JSX 轉換再串接、不解析 `import`
  （見 `docs/spec/frontend-architecture.md` 第 0 節），所以 `modern-web-guidance` 建議的
  `import('@oddbird/popover-polyfill')` 一律無法採用。只接受 20 行以內、無外部依賴的自訂 fallback；超過就改設計。

### 最低支援版本

| 瀏覽器 | 最低版本 | 卡在哪個功能 |
|---|---|---|
| Chrome／Edge／Android WebView（含 Telegram Android） | 111 | `oklch()`：全站配色都用它，不支援時整頁失色 |
| Safari／iOS WKWebView（含 Telegram iOS） | 15.6 | `:modal`（`oklch()`、`:has()`、`<dialog>` 為 15.4） |
| Firefox | 121 | `:has()` |
| Wails WebView2（桌面版） | 隨系統自動更新，不受限 | — |

數字取自 MDN browser-compat-data（2026-11-16 查詢），是各瀏覽器在 `oklch()`、`:has()`、`:modal`、
`dvh`、`<dialog>.showModal()` 之中最晚支援的那一個。**目前只寫明限制、不做降級**：
低於這些版本不保證可用。新增功能若會把底線往上推，要同步更新這張表與 README 的「瀏覽器支援」。

`overscroll-behavior`（Chrome 144／Safari 16／Firefox 150）與 `interactive-widget` 屬於漸進增強，
不支援時只是少了捲動遏止或鍵盤版面調整，不列入底線。

---

## stream-json 事件處理

### 關鍵頂層 type
| type | 用途 |
|---|---|
| `system` (subtype=`init`) | 取得 session_id、permissionMode |
| `stream_event` | 包裝 Anthropic API 事件，內層看 `event.type` |
| `result` | 最終結果，含 `session_id`、`permission_denials`、`cost` |

### 狀態機觸發對應
| stream-json 事件 | 切換至狀態 |
|---|---|
| 送出指令（進程啟動） | `THINKING` |
| `stream_event.event.type = "content_block_start"` (type=text) | `STREAMING` |
| `stream_event.event.type = "content_block_delta"` (text_delta) | 累積文字 |
| `result.stop_reason = "end_turn"` | `IDLE` |
| `result.permission_denials` 非空 | `AWAITING_CONFIRM` |

---

## Permission 授權流程

`AWAITING_CONFIRM` 不是 CLI 的 y/n 提示，而是後端解析 `result.permission_denials` 後主動觸發：

1. 後端收到 `result`，`permission_denials` 非空
2. 送 WS → 前端顯示授權對話框
3. 使用者選擇：
   - **允許此操作** → 加 `--allowedTools` 重新 resume
   - **允許並記住** → 更新 `sessions.permission_mode`，重新 resume

### Permission Mode
| 模式 | CLI 旗標 | 說明 |
|---|---|---|
| `default` | `--permission-mode default` | 預設，遇寫入/執行觸發授權流程 |
| `acceptEdits` | `--permission-mode acceptEdits` | 自動允許檔案讀寫 |
| `bypassPermissions` | `--permission-mode bypassPermissions` | 全部跳過（危險模式） |

---

## 本機運行（Windows）

- 進入點：`./cmd/server`；需有 `config.yaml`
- 開發：`go run ./cmd/server`
- Binary：`go build -o claude-miniapp.exe ./cmd/server`

---

## 參考文件
- `docs/spec/backend-architecture.md` — 後端（`internal/`、`cmd/`）分層與依賴規範（以 `go test ./internal/arch/` 驗證）
- `docs/spec/frontend-architecture.md` — 前端分層、命名與檔案大小規範（以 `node scripts/frontend-layering.test.cjs` 驗證）
- `docs/spec/claude-code-cli.md` — CLI 旗標完整參考
- `docs/spec/headless.md` — `-p` 模式與 stream-json 用法
- `docs/spec/plan.md` — 完整規格書（Schema、WebSocket 格式、Roadmap）
