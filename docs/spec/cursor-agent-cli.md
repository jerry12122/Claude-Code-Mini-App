把 Cursor Agent 當成一個 **CLI-based provider** 即可；你要補進既有 AI agent framework 的，核心是 **transport contract、session contract、stream contract、error contract、capability contract** 這五塊。 

## Provider 介面定位

Cursor Agent 不是 HTTP API，而是 **本機 subprocess provider**；啟動方式是呼叫 `cursor-agent`，在 non-interactive 情境要用 `--print`，並且可選 `--output-format text|json|stream-json`，其中 `stream-json` 是預設結構化串流格式。   
因此你的 agent framework 應把它歸類為「local executable provider」而不是「remote SDK provider」，因為生命週期、超時、中止、stderr、exit code 都是 host process 負責。 

## 啟動契約

當你要啟動一次 Cursor run，請視為一個獨立的 process invocation；prompt 是 **positional argument**，不是 flag，而 `-p` 代表的是 `--print`。 
你的 request contract 至少要支援：`cwd`、`prompt`、`session_id?`、`model?`、`force?`、`api_key?`、`output_mode`，因為 Cursor 官方參數直接支援 `--resume [chatId]`、`-m/--model`、`-f/--force`、`-a/--api-key`。 

### 必備 request 欄位
- `provider = cursor`
- `cwd`: agent 執行工作目錄，system/init 事件會回報實際 `cwd`。 
- `prompt`: 初始 prompt，作為 positional argument。 
- `session_id`: 對應 `--resume [chatId]`。 
- `model`: 對應 `-m, --model`。 
- `force`: 對應 `-f, --force`。 
- `print_mode = true`: 若要程式串接，應固定開 `--print`。 
- `output_format = stream-json|json`: 若要 streaming，使用 `stream-json`；若只要最終結果，使用 `json`。 

## Session 契約

Cursor 用 `session_id` 作為會話延續鍵，並透過 `--resume [chatId]` 接續既有 session；官方也提供 `ls` 與 `resume` 指令操作歷史 session。 
因此你的 agent system 不應自己重播完整 message history 給 Cursor，而應把 Cursor session 視為 **provider-owned conversation state**，你只需要保存 `session_id` 並在續跑時帶回去。 

### Session 技術定義
- `session_id` 來源：`system/init.session_id` 或 terminal `result.session_id`。 
- `session scope`：單次 agent execution 內一致不變。 
- `session continuity method`：`--resume=<session_id>`。 
- `session discovery`：可用 `cursor-agent ls` / `cursor-agent resume`，但對系統整合而言，應以你自家 DB 為主，不依賴 CLI 查詢歷史。 

### Session 管理規則
- 你的 framework 應把 Cursor session 視為 opaque token，不要自行生成或推導。 
- `session_id` 應與你的 thread / workspace / task domain 做綁定，避免跨任務污染上下文。 
- session persistence 應在 **收到 init event 即可落庫**，不要等成功結束才存，因為失敗 run 也可能需要繼續診斷。 

## Stream 契約

`stream-json` 是 **NDJSON**，每一行一個 JSON event，事件順序是即時發生順序；成功時會以 terminal `result` event 收尾，失敗時可能提前中止且沒有 terminal event，錯誤訊息寫到 stderr。 
因此你的 stream consumer 必須被設計成 **line-oriented parser + terminal-state machine**，不能假設一定有完結事件。 

### Stream 基本規格
- 傳輸層：stdout
- 編碼形式：newline-delimited JSON，每行單一 JSON 物件。 
- 成功結束：有 `type="result"`、`subtype="success"`。 
- 失敗結束：process exit non-zero，可能沒有 terminal JSON，stderr 為主要錯誤來源。 
- 未來擴充：官方明示欄位可能新增，consumer 應忽略 unknown fields。 

## Event 型別契約

官方文件定義了至少五類 runtime event：`system/init`、`user`、`assistant`、`tool_call started/completed`、`result success`。 
所以你的 agent event bus 應至少抽象出這五類，而不要只做「text delta + final text」兩類，否則會失去工具活動與 session metadata。 

### 1. System Init
用途：告知本次 session 的初始化資訊。 

最低保留欄位：
- `type = system`
- `subtype = init`
- `session_id`
- `cwd`
- `model`
- `permissionMode`
- `apiKeySource`。 

### 2. User Message
用途：反映送入的 prompt；對整合不是必要核心，但可用於 audit trail。 

最低保留欄位：
- `type = user`
- `message.role = user`
- `message.content[].text`
- `session_id`。 

### 3. Assistant Delta
用途：即時文字輸出；完整結果需由所有 `message.content[].text` 依序串接重建。 

最低保留欄位：
- `type = assistant`
- `message.role = assistant`
- `message.content[]`
- `session_id`。 

### 4. Tool Call
用途：工具開始與完成通知，可用於 UI、audit、latency 與 file-op trace。 

最低保留欄位：
- `type = tool_call`
- `subtype = started|completed`
- `call_id`
- `tool_call`
- `session_id`。 

### 5. Terminal Result
用途：成功結束的最終聚合結果。 

最低保留欄位：
- `type = result`
- `subtype = success`
- `is_error = false`
- `result`
- `duration_ms`
- `duration_api_ms`
- `session_id`
- `request_id?`。 

## Tool Call 契約

Cursor 至少明確定義了 `readToolCall` 與 `writeToolCall` 的 schema，其他工具可能走 `tool_call.function` 結構，帶 `name` 與 `arguments`。 
因此你的工具事件正規化應採 **多型 schema**，不可把 tool event model 寫死成單一格式。 

### 已知工具 schema
- `tool_call.readToolCall.args.path`。 
- `tool_call.readToolCall.result.success.content / isEmpty / exceededLimit / totalLines / totalChars`。 
- `tool_call.writeToolCall.args.path / fileText / toolCallId`。 
- `tool_call.writeToolCall.result.success.path / linesCreated / fileSize`。 
- fallback：`tool_call.function.name / arguments`。 

### Tool correlation 規格
- `call_id` 是 start/completed 對應鍵，應保存於你的 internal trace graph。 
- 若你的系統已有 tool lifecycle 事件，Cursor 的 `call_id` 可直接映射為外部 tool invocation id。 

## Completion 契約

若使用 `json` 模式，成功時 stdout 只會輸出一個 JSON 物件，內容是聚合後的 `result`、`session_id`、`duration_ms`、`request_id?`；不會有 delta 與 tool events。 
因此對你的 agent framework 而言，`json` 模式只適合「一次性 completion provider」，不適合作為 full-fidelity agent stream provider。 

### `json` 模式用途
- batch job
- 非互動式 pipeline
- 只需最終自然語言答案
- 不需要工具中間狀態或 live UI。 

## Error 契約

官方文件明示：失敗時 process 以 non-zero exit code 結束，錯誤輸出到 stderr，而且不保證產出合法 JSON terminal object。 
因此你的 provider error 規格必須以 **process semantics 優先於 JSON semantics**，換句話說，`stdout parse success != run success`。 

### 成功判定標準
一個 run 應同時滿足：
- process exit code = 0； 
- 若為 `stream-json`，理想上收到 terminal `result success` event； 
- 若為 `json`，stdout 可 parse 成單一合法 success object。 

### 失敗判定標準
任一成立即可視為 run failure：
- process exit code != 0； 
- stdout NDJSON 中斷且無 terminal success； 
- stdout 非法 JSON 且 process 結束異常； 
- stderr 有錯且 process 失敗。 

### Error object 最少應含
- `provider = cursor`
- `phase = spawn|stream|parse|wait|provider`
- `exit_code?`
- `stderr`
- `session_id?`
- `request_id?`
- `partial_text?`
- `last_event_type?`

## Lifecycle 契約

由於 Cursor 是 subprocess provider，你的 agent runtime 需要完整管理：
- spawn
- stdout stream read
- stderr stream read
- cancellation
- wait/reap child process。 

### Cancellation 規格
- 以 host-side context cancel / process kill 為主。
- 被取消的 run 應視為 `aborted` 而非 `failed`，除非 Cursor 回報明確 provider error。
- 若 cancel 發生在 session 已建立後，應保留該 `session_id`，因為後續可能要 resume 繼續。 

## Capability 契約

在 `--print` 模式下，官方文件說 Cursor agent 具有包含 `write` 與 `bash` 在內的全部工具能力；若加 `--force`，則是「除非明確被拒絕，否則強制允許命令」。 
所以你應把 Cursor provider 標記為 **agentic provider with filesystem + shell side effects**，不要把它當成純文字 completion engine。 

### 建議 capability metadata
- `supports_session_resume = true`。 
- `supports_streaming = true` (`stream-json`)。 
- `supports_final_json = true` (`json`)。 
- `supports_tool_events = true`。 
- `supports_model_selection = true` (`--model`)。 
- `supports_force_execution = true` (`--force`)。 
- `has_side_effects = true`，因為可寫檔與執行 bash。 

## Thinking / Reasoning 契約

官方文件特別指出，`thinking` events 在 print mode 會被 suppressed，不會出現在 `json` 或 `stream-json` 輸出裡。 
所以你的統一 agent schema 若有 reasoning stream 欄位，對 Cursor provider 應標記為 **unsupported / unavailable**，而不是用假的中間事件補齊。 

## Forward Compatibility 契約

官方已經寫明未來可能新增欄位，例如 `system/init` 之下可能增加 `tools`、`mcp_servers`，並要求 consumer 忽略未知欄位。 
因此你的解析策略應是：
- 外層 event envelope 鬆綁；
- 只解析所需核心欄位；
- 原始 JSON 保留一份作 raw payload 以利除錯與後續升級。 

## 認證契約

認證可用 `-a, --api-key <key>` 或 `CURSOR_API_KEY` 環境變數；此外 CLI 也支援 `login/logout/status` 命令。 
對系統整合而言，建議把認證抽象成 provider credential source，優先順序可設計為：runtime env > explicit request override > host login state。 

## MCP 契約

Cursor CLI 原生支援 MCP 管理，包含 `mcp list`、`mcp list-tools <identifier>`、`mcp login <identifier>`。 
如果你的 agent framework 已有 tool registry，應把 Cursor MCP 視為 **provider-side external tool substrate**，不是你 framework 直接可觀測的 tool registry，除非你額外同步兩邊定義。 

## 你的框架需要補的抽象

如果你說的是「讓我的 AI agent 可以增加實作」，那最實際要補的不是範例，而是以下抽象：

### Provider Registration
- provider id: `cursor`
- transport: `subprocess`
- session model: `provider-owned`
- stream format: `ndjson`
- completion mode: `json` / `stream-json`
- reasoning visibility: `none`
- side effects: `filesystem`, `shell`。 

### Internal Normalized Events
- `run.started`
- `session.bound`
- `message.delta`
- `tool.started`
- `tool.completed`
- `run.completed`
- `run.failed`
- `run.aborted`。 

### Persistence Domain
- `provider`
- `thread_key`
- `session_id`
- `workspace`
- `model`
- `request_id`
- `started_at`
- `completed_at`
- `exit_code`
- `stderr`
- `partial_output`。 

## 建議的最終技術規格

你可以把 Cursor provider 寫成下面這個規格：

| 面向 | 規格 |
|---|---|
| Provider 類型 | Local subprocess agent provider。  |
| 啟動方式 | `cursor-agent --print [--output-format <format>] [--resume <session>] [--model <model>] [--force] <prompt>`。  |
| Prompt 傳遞 | Positional argument。  |
| Streaming | `stream-json`，stdout NDJSON，一行一事件。  |
| Final-only | `json`，成功時單一 JSON object。  |
| Session continue | `--resume [chatId]`，session_id 由 provider 產生。  |
| Tool telemetry | `tool_call started/completed` + `call_id`。  |
| Final result | `type=result, subtype=success, result, session_id, duration_ms, request_id?`。  |
| Failure source | exit code + stderr；可能無 terminal JSON。  |
| Unknown fields | 必須忽略，以保持前向相容。  |
| Reasoning stream | 不提供，print mode 會 suppress thinking。  |
| Side effects | 可讀檔、寫檔、執行 bash；`--force` 放寬執行限制。  |

