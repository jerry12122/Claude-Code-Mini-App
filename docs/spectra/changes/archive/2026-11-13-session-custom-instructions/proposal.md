## Why

使用者希望像 Gemini Gem 一樣，在建立 session 時設定該對話的角色、回答風格與工作規則，不必在每則訊息重貼。既有 POC 已確認 Claude、Codex 與 Antigravity 有不建立自訂 agent 檔的注入入口，適合先提供這三種 runner 的會話專屬指令。

## What Changes

- 新建 Session 與轉發時新建會話提供「專屬指令（選填）」多行欄位，只支援 claude、codex、antigravity；與第一則訊息分開儲存和傳遞。
- 增加 session 的 system_instruction 欄位，建立時設定並固定；省略或空白沿用原有行為，舊 session 不需要重新建立。
- Claude 追加 system prompt；Codex 透過 developer_instructions 建立會話；Antigravity 只有非空指令的 session 改走原生 ACP，透過 _meta.antigravity.systemPrompt.append 注入，不建立自訂 agent 檔。
- 重新啟動、重連、佇列執行、清除 CLI 對話後重建及同 runner 複製／接手時保留指令，不影響其他 session；不承諾更新既有會話的指令。
- API 對不支援的 runner、shell／Telegram 會話及超過規格上限的非空指令明確拒絕，不以一般使用者訊息冒充 system instruction。

## Capabilities

### New Capabilities

- `session-custom-instructions`: 建立時設定、持久化與隔離傳遞會話專屬指令，限定 Claude、Codex、Antigravity，並定義續接與失敗行為。

### Modified Capabilities

無。既有 console-usability、composer-model-controls 與 session-failure-record 的要求維持，新增能力沿用其可存取性與錯誤呈現基準。

## Impact

- 前端：internal/static/js/session/NewSessionComposer.js、internal/static/js/chat/ForwardModal.js、internal/static/js/hooks/useNewSessionForm.js，以及 internal/static/js/chat/ChatView.js 的手動接手設定。
- 資料與建立 API：internal/db/db.go、internal/db/session.go、internal/api/session.go；共用執行參數 internal/agent/runner.go 與 internal/ws/handler.go、internal/ws/handoff.go。
- Runner：internal/claude/runner.go、internal/codex/runner.go、internal/antigravity/，後者增加 provider 內部 ACP 路徑，不新增公開 runner 類型或 npm／SDK 依賴。
- 測試與文件：受影響 Go 套件、scripts/session-instructions.test.cjs、docs/spec/claude-code-cli.md、docs/spec/codex-cli.md、docs/spec/antigravity-cli.md。
- 規格政策上限：修剪外圍空白後最多 16384 個 UTF-8 bytes；這是明確的產品限制，不是效能估計，超限不截斷、不建立半成品 session。
