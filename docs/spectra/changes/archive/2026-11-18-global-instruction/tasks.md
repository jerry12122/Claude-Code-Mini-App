## 1. 設定與資料

- [x] 1.1 設定 → 一般 可編輯全域指令與注入模式，預設 `native`，驗證不回顯文字（Global instruction is configured in Settings）；驗證：`go test ./internal/api -run TestGlobalInstructionSettingsAndCreation`、`python scripts/mobile-settings.test.py`
- [x] 1.2 資料庫新增 `global_instruction_text`、`global_instruction_mode`、`global_instruction_sent` 欄位，重複遷移不報錯（會話快照的資料基礎）；驗證：`go test ./internal/db`

## 2. 建立時快照

- [x] 2.1 一般新建在建立時快照全域設定，之後修改設定不影響既有會話（Sessions snapshot the global instruction at creation）；驗證：`TestGlobalInstructionSettingsAndCreation` 的快照斷言與 `TestGlobalInstructionDelivery` 先改回預設設定再送訊息
- [x] 2.2 轉發新建、接手、MCP `create_session`、聊天室工作者皆套用同一快照規則，Telegram 與 shell 不存；驗證：`go test ./internal/ws -run TestCreateSessionGlobalInstructionBoundaries`（Telegram 與 shell 不存）、`go test ./internal/db -run TestCreateRoomWorkerSnapshotsGlobalInstruction`（聊天室工作者）；接手與 MCP `create_session` 以程式碼檢視確認（共用 `ws.CreateSession`，沒有各自的自動化測試）

## 3. 送出合併與注入

- [x] 3.1 原生模式將全域與專屬指令以空行合併，超過 16384 bytes 拒絕建立，不支援原生的 Agent 原樣送出（Native delivery merges global and session instructions）；驗證：`TestGlobalInstructionDelivery` 的 native 與 native unsupported 案例；長度上限由 `TestCreateSessionGlobalInstructionBoundaries` 驗證
- [x] 3.2 聊天注入以固定標題格式加在使用者訊息前，歷史、佇列與轉發保留原文（Chat injection prefixes the user message with the global instruction；設計：聊天注入只改送給模型的輸入）；驗證：`TestGlobalInstructionDelivery` 的 chat 案例與使用者歷史斷言
- [x] 3.3 `first` 模式只在模型回應事件後標記送達，失敗後重試與清除上下文後會再次注入（First-message delivery is confirmed by the model；設計：首則注入以模型事件確認送達）；驗證：`TestGlobalInstructionDelivery` 的 first 案例（先失敗、暫停佇列、續送、`reset_context`）

## 4. 衝突檢查

- [x] 4.1 Claude 有原生指令時，建立與 PATCH `cli_extra_args` 都拒絕 system-prompt 旗標（Native instruction conflicts with CLI system-prompt flags are rejected）；驗證：`TestGlobalInstructionSettingsAndCreation` 的 POST 與 PATCH 400 斷言

## 5. 驗證與收尾

- [x] 5.1 全部相關自動化檢查通過，工作樹相關 commit 已建立；驗證：`go test ./internal/...`、`node scripts/frontend-layering.test.cjs`、`python scripts/mobile-settings.test.py`
