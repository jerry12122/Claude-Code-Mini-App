## Why

會話專屬指令只能在建立會話時逐一填寫，沒有「所有新會話共用」的規則（例如固定使用繁體中文、先說結論）。每次新建都要重貼，轉發、接手、MCP 與聊天室工作者建立的會話更容易漏掉。需要一個在設定中維護一次、之後自動套用的全域指令。

本 change 為事後補錄：實作已提交於 commit `6b47e0e`，此處補上規格與設計紀錄。

## What Changes

- 設定 → 一般 新增「全域指令」文字欄與注入模式：會話第一則訊息（`first`）、每則訊息（`every`）、僅原生支援（`native`，預設）。透過 `GET/PUT /settings/global-instruction` 讀寫。
- 建立 agent 模式會話時（新建、轉發新建、接手、MCP `create_session`、聊天室工作者），把當下的全域文字與模式存入該會話；之後修改設定只影響新會話。Telegram 與 shell 會話不存。
- 送出時依模式組合：專屬指令固定走原生管道；全域指令走原生（與專屬以空行合併）或聊天注入（加在使用者訊息前，兩段標題固定）。聊天歷史、佇列與轉發內容保留原始訊息。
- `first` 模式以「模型回傳事件」確認送達；啟動失敗可重帶，清除上下文後重帶。
- 驗證：原生合併後超過 16384 UTF-8 bytes 拒絕建立；Claude 有原生指令時，與 CLI system-prompt 旗標衝突者拒絕，包含 PATCH `cli_extra_args`。
- 資料庫新增 `sessions.global_instruction_text`、`sessions.global_instruction_mode`、`messages.global_instruction_sent`。

## Non-Goals

- 不讓專屬指令走聊天注入（不支援原生的 Agent 沒有專屬欄位）。
- 不回溯修改既有會話的全域指令快照。
- Shell 指令與 Telegram 私訊不注入。
- 不提供多組全域指令或依 Agent 分別設定。

## Capabilities

### New Capabilities

（無）

### Modified Capabilities

- `session-custom-instructions`: 新增全域指令的設定、建立時快照、原生合併、聊天注入與 `first` 送達確認等需求；既有專屬指令需求不變。

## Impact

- 後端：`internal/db/global_instruction.go`、`internal/db/db.go`、`internal/db/session.go`、`internal/db/room_workflow.go`、`internal/api/settings.go`、`internal/api/session.go`、`internal/ws/instruction.go`、`internal/ws/handler.go`、`internal/ws/handoff.go`、`internal/mcp/tools.go`、`internal/agent/runner.go`、`internal/server/server.go`
- 前端：`internal/static/js/ui/SettingsModal.js`
- 測試：`internal/api/session_instruction_test.go`、`internal/ws/session_instruction_test.go`、`scripts/mobile-settings.test.py`
- API：新增 `/settings/global-instruction`；資料庫以 `ALTER TABLE ADD COLUMN` 遷移，既有資料預設值相容。
