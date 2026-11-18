## Context

會話專屬指令（`session-custom-instructions`）只能在建立會話時填寫，並固定走 Claude、Codex、Antigravity 的原生指令管道。需要一個在設定中維護、對所有新會話生效的全域指令，同時要涵蓋不支援原生管道的 Agent。本 change 為事後補錄，實作已於 commit `6b47e0e` 完成，此文件記錄當時的決策。

## Goals / Non-Goals

**Goals:**

- 在設定 → 一般 維護一份全域指令與注入模式，新建會話時快照。
- 支援三種送出組合：全域＋專屬皆原生、全域聊天＋專屬原生、只有全域聊天。
- 聊天注入不污染聊天歷史、佇列與轉發內容。
- `first` 模式在啟動失敗、重連、清除上下文時行為可預期。

**Non-Goals:**

- 專屬指令不提供聊天注入（不支援原生的 Agent 沒有專屬欄位）。
- 不回溯更新既有會話，不支援多組全域指令或依 Agent 分別設定。
- Shell 指令與 Telegram 私訊不注入。

## Decisions

### 建立會話時快照全域指令

建立會話時把全域文字與模式寫入 `sessions.global_instruction_text` 與 `sessions.global_instruction_mode`，送出時只讀會話上的值。替代方案是送出時讀即時設定，但這會讓同一會話的行為在中途改變，也讓 `first` 的判斷失去基準。所有建立入口共用 `ws.CreateSession`；聊天室工作者因為走 SQL 直接建立，在 `CreateRoomWorker` 內另外讀取並寫入同兩個欄位。

### 全域與專屬指令在送出時合併

專屬指令永遠走原生。全域為 `native` 且 Agent 支援原生時，以 `全域 + 空行 + 專屬` 合併成單一原生指令；否則全域指令走聊天注入。合併長度上限沿用 16384 bytes，於建立時檢查，避免送出時才失敗。替代方案是兩段各自傳送，但 Claude 只有單一 `--append-system-prompt`，合併才能一致。

### 聊天注入只改送給模型的輸入

注入發生在組出 `runnerPrompt` 時，格式為 `全域指令` 標題、全域文字、空行、`使用者訊息` 標題、原訊息。聊天紀錄、佇列與轉發仍使用原訊息。替代方案是改寫儲存的訊息，但會讓歷史與轉發內容夾帶指令。

### 首則注入以模型事件確認送達

`first` 模式把「已送達」記在原始使用者訊息的 `messages.global_instruction_sent`，只有收到模型回應事件（文字、思考、工具開始、授權被拒、完成）才標記。進程啟動或在模型回應前失敗不算送達，因此重試會重帶。判斷方式是查該會話是否存在已標記的訊息，所以清除上下文刪除訊息後會自動重新生效。替代方案是在 `messages` 以外另存旗標，但會需要在清除上下文時同步重置。

### Claude 原生指令與 CLI system-prompt 旗標互斥

只要 Claude 會話有非空的原生指令（含全域合併後），建立與 PATCH `cli_extra_args` 都拒絕四個 system-prompt 旗標，含等號形式。判斷集中在 `ws.ValidateInstructionArgs`，API 兩處共用。

## Implementation Contract

**Behavior**

- 設定 API：`GET/PUT /settings/global-instruction`，JSON 為 `{"text": string, "mode": "first"|"every"|"native"}`；PUT 兩個欄位皆必填且須為字串，錯誤訊息不回顯文字，回應帶 `Cache-Control: no-store`。
- 送出：先決定本輪是否注入，再決定原生指令。原生指令 = `nativeSystemInstruction(session)`；聊天輸入 = 注入格式或原訊息。
- 失敗：讀取送達狀態失敗時回報錯誤並中止本輪；標記送達失敗只記 log，下一輪會再次注入（寧可重複不遺漏）。
- 資料：新增三個欄位並以 `ALTER TABLE ADD COLUMN` 遷移，重複欄位錯誤略過，既有資料以空字串與 `native` 視為未設定。

**Acceptance**

- `go test ./internal/api -run TestGlobalInstructionSettingsAndCreation` 通過：設定驗證、快照、衝突拒絕。
- `go test ./internal/ws -run TestGlobalInstructionDelivery` 通過：四種組合、首則失敗重帶、清除上下文後重帶、歷史保留原文。
- `python scripts/mobile-settings.test.py` 通過：設定頁輸入、鍵盤切換模式、儲存請求內容。

**Scope**

- In scope：設定 API 與 UI、會話快照、送出合併、聊天注入、`first` 送達確認、旗標衝突檢查。
- Out of scope：專屬指令的聊天注入、既有會話回溯、全域指令的版本歷史。

## Risks / Trade-offs

- [`first` 模式在模型回應後、標記送達前程序中斷會重複注入] → 可接受，寧可多帶一次；標記失敗時僅記 log。
- [合併長度只在建立時檢查，之後不可更動專屬指令，因此不會再超限] → 專屬指令建立後固定，無額外處理。
- [MCP `create_session` 與接手的快照沒有各自的自動化測試] → 兩者共用 `ws.CreateSession`，已在 tasks 標明以程式碼檢視確認；聊天室工作者有 `TestCreateRoomWorkerSnapshotsGlobalInstruction`。
- [`poc/session-instructions` 有重複 `main`，使 `go build ./...` 失敗] → 與本 change 無關，不處理。
