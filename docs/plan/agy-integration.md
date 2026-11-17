# Antigravity CLI（agy v1.3.3）整合升級計畫

> **狀態**：核心實作、回歸測試與 Chrome 元件驗證完成；真實 CLI／TG WebView 驗收及回合 Token 統計串接待補
> **日期**：2026-10-12
> **目標版本**：Antigravity CLI **1.3.3**（前次規劃為 1.1.12，歷史停用點為 1.0.14）
> **關聯文件**：[`docs/spec/antigravity-cli.md`](../spec/antigravity-cli.md)、[`internal/antigravity/`](../../internal/antigravity/)；本機 PoC：`poc/antigravity-cli/`（Git 忽略目錄）

---

## 1. 歷史限制與目前實作

本專案於 2026 年 6 月曾以 `agent_type=antigravity` 與子進程 `agy` 整合，但因受限於官方 CLI 的兩大致命阻礙而宣告停用：
1. **Issue #76**：在 Windows 非 TTY（Go 子進程 pipe）環境下執行 `--print` 會 exit 0 但 stdout 完全空白。
2. **缺乏穩定 NDJSON 串流**：舊版無 `--output-format stream-json`。

### 目前實作與驗證（2026-10-12）

- 既有本機 agy 1.1.12 PoC（`poc/antigravity-cli/samples/poc-results.md`）已記錄非 TTY 輸出及 NDJSON 成功；本輪沒有重跑真實 CLI。
- runner 已改為預設 `stream-json`，解析 `init`、`step_update`、`result`，並以 `--conversation` 延續會話。
- 權限模式映射、模型／推理強度參數與 Gemini／Claude/GPT 兩組帳戶配額已實作。
- 空白 print 輸出收尾、模型標籤同步及手機最高使用率警示已有回歸測試；全專案測試、建置與 Chrome 元件驗證通過。
- `usage` 欄位已解析，但尚未透過 `agent.Event` 傳遞回合 Token 統計。真實多輪對話、權限模式、帳戶配額及 TG WebView 驗收仍待補。

詳細驗證見 [審查修復紀錄](done/agy-review-fixes.md)。

---

## 2. 整合架構與契約對照

```
桌面視窗 / 瀏覽器 / Telegram Mini App
        ↕ WebSocket
┌──────────────────────────────────────────────┐
│  Go 後端 (internal/ws, internal/agent)        │
│  - factory.go 註冊檢查與舊值別名              │
│  - runner.go 啟動子進程                      │
│  - events.go 解析 NDJSON 串流                │
└──────────────────────┬───────────────────────┘
                       ↓ stdin: prompt (pipe)
  agy --output-format stream-json
      --conversation <session_id>
      --model <model>
      --mode <accept-edits | plan>
      --dangerously-skip-permissions
      --add-dir <work_dir>
                       ↓ stdout: NDJSON events
┌──────────────────────────────────────────────┐
│  event: init (conversation_id)               │
│  event: step_update (agent_response / tool)  │
│  event: result (status, response, usage)     │
└──────────────────────────────────────────────┘
```

### 2.1 事件對應表

| agy 1.3.3 事件 | 欄位細節 | 映射為 `agent.Event` | 處理方式 |
|---|---|---|---|
| `event: "init"` | `conversation_id`, `init.tools` | `EventSessionInit` | 綁定會話 ID |
| `event: "step_update"` | `step_type: "agent_response"`, `text_delta` | `EventStreamStart` (首筆) + `EventDelta` | 增量文字串流廣播 |
| `event: "step_update"` | `step_type: "tool"`, `state: "ACTIVE"` | `EventActivity` / `EventToolStarted` | 提示工具執行中 |
| `event: "step_update"` | `step_type: "tool"`, `state: "DONE"` | `EventToolCompleted` | 工具完成 |
| `event: "result"` | `status: "SUCCESS"`, `response`, `usage` | `EventDone` | 回合完成並保留回覆；usage 僅解析，尚未轉交 Token 統計 |
| `event: "result"` | `status: "ERROR"`, `error` | `EventError` | 錯誤通知 |

### 2.2 權限模式 (Permission Mode) 映射

| Mini App `permission_mode` | agy 1.3.3 旗標 | 實測行為 |
|---|---|---|
| `default` | *(無額外旗標)* | 預設審核模式；無權限工具軟性拒絕並帶 `denied_actions` |
| `acceptEdits` | `--mode accept-edits` | 自動允許檔案讀寫與編輯，終端機指令仍需白名單 |
| `plan` | `--mode plan` | 唯讀規劃模式，不進行實際檔案或指令執行 |
| `bypassPermissions` / `yolo` | `--dangerously-skip-permissions` | 全自動放行所有工具 |

---

## 3. 工作項目 (Action Items)

### Phase 1：核心協定與 Runner 重構 (`internal/antigravity`)
- [x] **重寫 `internal/antigravity/events.go`**：
  - 定義 `StreamTopEvent`（含 `Event`、`ConversationID`）。
  - 定義 `StepUpdatePayload`（含 `StepType`, `State`, `TextDelta`, `ToolName`, `ToolInfo`）。
  - 定義 `ResultPayload`（含 `Status`, `Response`, `NumTurns`, `Usage`, `DeniedActions`）。
  - 清理過時的 legacy Gemini 結構。
- [x] **改寫 `internal/antigravity/runner.go`**：
  - 預設啟用 `stream-json` 模式（移除 `CC_AGY_STREAM_JSON` 環境變數門檻）。
  - 完善 `appendCommonArgs()`：對應 `--conversation`, `--model`, `--mode accept-edits`, `--mode plan`, `--dangerously-skip-permissions`, `--add-dir`。
  - 重構 `dispatch()`：處理 `agent_response` 增量串流與 `result` 完成事件。
  - 移除 Issue #76 專用錯誤型別，保留一般空白 print 輸出的錯誤收尾。
- [x] **更新 `internal/antigravity/runner_test.go`**：
  - 以新 NDJSON schema 建立測試案例，並用假 CLI 驗證空白輸出、程序失敗與取消。

### Phase 2：解除停用、Model 擷取與 Quota 實作
- [x] **解除後端停用開關**：
  - 移除空的 `disabledAgentTypes` 與無效分支；保留註冊檢查及 Kiro／Gemini 新建限制。
- [x] **Model 資訊擷取**：
  - 更新 `ExtractFromAntigravityLines()`，並在 WebSocket 切換、執行及重連時同步模型標籤。
- [x] **實作 Quota 配額擷取**：
  - 在 `internal/usage/quota.go` 實作 `FromAgyUsageText()` 解析 TSV。
  - 在 `internal/quota/antigravity.go` 實作 `Fetch()` 呼叫 `agy -p "/usage"`。

### Phase 3：前端介面解鎖
- [x] **開放 Session 建立選項**：
  - 更新 `internal/static/js/session/NewSessionComposer.js`，允許選擇 Antigravity 與自訂模型。
- [x] **校準權限模式標籤**：
  - 更新 `internal/static/js/ui/uiAtoms.js`，顯示標準權限選項。
- [x] **開放 Quota 徽章**：
  - 更新 `internal/static/js/chat/ChatHeader.js`，解除 Antigravity 配額重新整理限制。

### Phase 4：文件更新與端到端驗證
- [x] 更新 `docs/spec/antigravity-cli.md`。
- [x] 執行 `go test ./...` 與 `go build ./cmd/server ./cmd/desktop`。
- [x] 執行 `node scripts/quota-compact.test.cjs`，並以 Chrome 驗證桌面／手機配額元件。
- [ ] 驗證多輪對話、權限模式與用量更新。
- [ ] 將 `result.usage` 串接至回合 Token 統計。

---

## 4. 完成定義 (Definition of Done)

1. `go build ./cmd/server` 與 `go test ./...` 無報錯通過。
2. 前端可成功建立 `agent_type=antigravity` 之 Session 並正常收發訊息。
3. 對話即時串流與 Markdown 渲染流暢，回合收尾帶有正確 Token 用量（Token 統計串接尚未完成）。
4. 第二則訊息帶相同 `--conversation <id>`，可成功保留上下文。
5. 點擊頂部 Quota 按鈕可正常顯示並更新配額資訊。
