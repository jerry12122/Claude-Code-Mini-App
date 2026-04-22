# 功能計畫書：Shell 執行模式 (Shell Mode)

> 狀態：草稿  
> 建立日期：2026-04-18  
> 關聯規格：`docs/plan.md`

---

## 1. 功能概述

在現有 AI 代理模式（Agent Mode）的基礎上，為每個 Session 新增一個 **Shell 執行模式**，讓使用者可直接在聊天介面送出 Shell 指令（PowerShell / Bash / sh），並即時串流執行結果。

模式切換以 Session 為單位，切換後立即生效，且不影響 AI 對話的 session_id 或歷史紀錄。

---

## 2. 使用者故事

- 身為使用者，我可以在 AI 代理模式與 Shell 模式之間自由切換，無需離開聊天介面。
- 身為使用者，我在 Shell 模式下送出指令後，若未開啟「跳過授權模式」，系統會先顯示確認對話框，讓我確認後再執行。
- 身為使用者，在 Shell 模式下可即時看到指令的 stdout / stderr 串流輸出，並可中途強制中止。
- 身為使用者，UI 視覺設計會明確告訴我目前處於哪種模式，避免誤操作。

---

## 3. UI 設計方案

### 3.1 模式切換控制元件

**方案（已選定）：聊天頂欄 Tab 式切換**

```
┌──────────────────────────────────────────┐
│  [🤖 AI 代理]  [>_ Shell]               │  ← 頂欄 segmented control
│──────────────────────────────────────────│
│  訊息列表區域                            │
│                                          │
│──────────────────────────────────────────│
│  輸入欄位（隨模式變化外觀）              │
└──────────────────────────────────────────┘
```

- 兩個 Tab 同時可見，點擊即切換，無下拉選單。
- 當前模式的 Tab 高亮顯示；切換時輸入框動畫過渡。
- 模式狀態持久化到 SQLite `sessions.input_mode` 欄位，重新連線後恢復。

### 3.2 輸入框視覺區別

| 屬性 | AI 代理模式 | Shell 模式 |
|:---|:---|:---|
| 背景色 | 現有主題色（淺色卡片） | 深色 / 終端風格（`#1e1e1e` 或暗主題色） |
| 字型 | 預設 Sans-serif | Monospace（`font-mono`） |
| 前綴提示 | 無 | `$ ` 或 `>` （依 OS 環境顯示） |
| Placeholder | 「輸入訊息…」 | 「輸入 Shell 指令…」 |
| 邊框 / 色環 | 主題主色 | 黃色或橘色警示環（提示「危險」場境） |
| 送出按鈕 | 紙飛機 icon | 閃電 ⚡ icon |

### 3.3 輸出區塊視覺區別

- Shell 輸出以 `<pre>` 等寬字體渲染，不經 Markdown 解析。
- stdout 以白色文字，stderr 以紅色文字區分。
- 訊息角標顯示 `>_` 終端 icon，而非 Agent icon。
- 顯示 exit code 徽章（`✅ 0` / `❌ 1`）。

### 3.4 批准對話框

若非 `bypassPermissions` 模式，送出後彈出確認卡片：

```
┌──────────────────────────────────────────┐
│ ⚠️  即將執行以下 Shell 指令              │
│                                          │
│  Shell：PowerShell                       │
│  指令：rm -rf ./dist                     │
│  工作目錄：C:\Users\user\project         │
│                                          │
│  [取消]          [確認執行]              │
└──────────────────────────────────────────┘
```

- `bypassPermissions` → 直接執行，不顯示對話框。
- `default` / `acceptEdits` → 一律顯示批准對話框。
- 批准後進入 `RUNNING` 狀態，顯示中止按鈕。

---

## 4. 系統架構

### 4.1 Session 資料庫變更

**`sessions` 表新增欄位：**

| 欄位名稱 | 型別 | 預設值 | 說明 |
|:---|:---|:---|:---|
| `input_mode` | TEXT | `agent` | `agent` 或 `shell` |

### 4.2 後端新增模組：`internal/shell/`

```
internal/shell/
  runner.go     — Shell 指令執行器（偵測 OS、spawn 子進程、串流輸出）
  runner_test.go
```

**`shell.Runner` 介面規格：**

```go
type RunOptions struct {
    Command string   // 使用者輸入的原始指令字串
    WorkDir string   // Session 的工作目錄
    Timeout int      // 秒，預設 60
}

type Event struct {
    Type     string // "delta_stdout" | "delta_stderr" | "done" | "error"
    Text     string
    ExitCode int    // 僅 done 事件帶入
}

func Run(ctx context.Context, opts RunOptions, cb func(Event)) error
```

**Shell 環境偵測邏輯：**

```
Windows → powershell.exe -Command "<cmd>"
Linux / macOS → /bin/bash -c "<cmd>"  （fallback: /bin/sh）
```

偵測結果記錄在 session 層級，並透過 WS 推送給前端（供提示顯示）。

### 4.3 WebSocket 協議擴充

#### 客戶端 → 伺服器

| `type` | 欄位 | 說明 |
|:---|:---|:---|
| `set_input_mode` | `mode: "agent"\|"shell"` | 切換 Session 的輸入模式 |
| `shell_run` | `data: "<command>"` | 送出 Shell 指令（等待批准或直接執行） |
| `shell_approve` | — | 使用者確認執行批准中的指令 |
| `shell_cancel` | — | 取消批准中的指令 |
| `shell_interrupt` | — | 中止正在執行的 Shell 子進程 |

#### 伺服器 → 客戶端

| `type` | 欄位 | 說明 |
|:---|:---|:---|
| `input_mode_changed` | `mode`, `shell_type` | 模式切換確認，附帶 shell 種類 |
| `shell_approval_request` | `command`, `shell_type`, `work_dir` | 要求使用者批准 |
| `shell_delta` | `stream: "stdout"\|"stderr"`, `value` | 串流輸出 |
| `shell_done` | `exit_code` | 指令執行完成 |
| `shell_error` | `message` | 執行異常（timeout、spawn 失敗等） |

### 4.4 狀態機擴充（Shell 模式下）

Shell 模式有獨立的狀態機，與 AI 代理模式並列：

```
IDLE
  │ 使用者送出 shell_run
  ▼
AWAITING_APPROVAL（bypassPermissions 時跳過）
  │ shell_approve
  ▼
RUNNING ──── shell_interrupt ──→ IDLE（進程被殺）
  │
  ▼（exit）
IDLE（顯示 exit code）
```

---

## 5. 安全性考量

| 風險 | 對策 |
|:---|:---|
| 任意指令執行 | Session 綁定單一 `work_dir`，後端不改變工作目錄 |
| 執行無限迴圈 | 預設 60 秒 timeout，超時強制殺進程 |
| 輸出量爆炸 | 限制單次 delta 推送 buffer（每行最多 4096 bytes），超出截斷 |
| 多重併發執行 | 同一 session 同時只允許一個 Shell 子進程，送出第二個前需等待或中止 |
| 非授權使用 | 既有 Telegram initData 白名單機制，Shell 模式不另設額外驗證層 |
| `bypassPermissions` 誤用 | UI 以橘色危險標示當前模式，並在批准對話框說明後果 |

---

## 6. 實作任務拆分

### Phase A — 後端基礎

- [ ] `sessions` 表新增 `input_mode` 欄位（migration）
- [ ] 實作 `internal/shell/runner.go`：OS 偵測、spawn、stdout/stderr 串流、timeout、中止
- [ ] 在 `ws/handler.go` 處理 `set_input_mode` / `shell_run` / `shell_approve` / `shell_cancel` / `shell_interrupt`
- [ ] 推送 `shell_approval_request`（`default` / `acceptEdits` 模式）或直接執行（`bypassPermissions`）
- [ ] 推送 `shell_delta` / `shell_done` / `shell_error`
- [ ] 將 Shell 執行紀錄寫入 `messages` 表（role: `shell`，content: 完整輸出）

### Phase B — 前端 UI

- [ ] 頂欄新增 `[🤖 AI 代理]` / `[>_ Shell]` Tab 切換元件
- [ ] 切換時送 `set_input_mode`，並接收 `input_mode_changed` 更新本地狀態
- [ ] Shell 模式輸入框：深色背景、Mono 字型、前綴提示、橘色邊框
- [ ] Shell 批准對話框元件（`AWAITING_APPROVAL` 狀態）
- [ ] Shell 輸出渲染：`<pre>` 等寬、stdout/stderr 分色、exit code 徽章
- [ ] Shell 模式訊息角標顯示 `>_` icon
- [ ] 中止按鈕（送 `shell_interrupt`）

### Phase C — 細節與邊界

- [ ] Session 建立 API 支援傳入 `input_mode`（可選，預設 `agent`）
- [ ] 側欄 Session 列表顯示 input_mode 徽章
- [ ] shell_type（PowerShell / Bash）顯示於輸入框前綴或提示文字
- [ ] Shell runner 單元測試

---

## 7. 不在本次範圍內（Out of Scope）

- 互動式 Shell（TTY / stdin 輸入）：本次僅支援單次指令執行，不支援 REPL 互動。
- Shell 歷史自動補全（up arrow 歷史）：留待後續迭代。
- 自訂 Shell 路徑（如 zsh、fish）：首版僅支援 PowerShell / bash / sh 自動偵測。
- 多指令 pipeline 的細粒度事件拆分：整條指令視為一個 unit 執行。

---

## 8. 已確認決策

| 編號 | 問題 | 決策 |
|:---|:---|:---|
| Q1 | `acceptEdits` 模式下 Shell 是否也需批准？ | ✅ **需要**。`acceptEdits` 僅豁免 AI 工具的檔案操作，Shell 執行一律需批准（bypassPermissions 除外）。 |
| Q2 | Shell 輸出是否納入 AI 對話 context？ | ✅ **否**。Shell 歷史與 AI 歷史分開儲存，不污染 AI context。 |
| Q3 | 切換模式時若有執行中的 AI 任務如何處理？ | ✅ **禁用切換**。AI 任務執行中或 Shell 指令執行中，模式切換 Tab 均不可點擊，直到當前任務完成或被中止。 |
