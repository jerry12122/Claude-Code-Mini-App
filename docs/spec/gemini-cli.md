把 Gemini CLI 當成一個 **CLI-based provider** 即可；你要補進既有 AI agent framework 的，核心仍是 **transport contract、session contract、stream contract、error contract、capability contract** 這五塊。

## Provider 介面定位

Gemini CLI 不是 HTTP API，而是 **本機 subprocess provider**；啟動方式是呼叫 `gemini`，在 non-interactive 情境要以 `-p/--prompt` 指定 prompt 或透過 stdin pipe，並且可選 `--output-format text|json|stream-json`，其中 `stream-json` 是結構化串流格式。
因此你的 agent framework 應把它歸類為「local executable provider」而不是「remote SDK provider」，因為生命週期、超時、中止、stderr、exit code 都是 host process 負責。

> 與 Cursor Agent 的關鍵差異：Gemini 的 prompt 預設透過 `-p/--prompt` 旗標傳入（或 stdin pipe），positional argument 在 TTY 下會走互動模式；headless 一律建議使用 `-p`。

## 啟動契約

當你要啟動一次 Gemini run，請視為一個獨立的 process invocation；headless 模式下 prompt 透過 `-p/--prompt` 傳遞，或從 stdin pipe 進去（兩者可併用：`-p` 會附加在 stdin 之後）。
你的 request contract 至少要支援：`cwd`、`prompt`、`session_id?`、`model?`、`approval_mode?`、`sandbox?`、`include_directories?`、`output_mode`，因為 Gemini 官方參數直接支援 `--resume <id|latest>`、`-m/--model`、`--approval-mode`、`-s/--sandbox`、`--include-directories`。

### 必備 request 欄位
- `provider = gemini`
- `cwd`：agent 執行工作目錄，`init` 事件會隨 session metadata 反映環境。
- `prompt`：初始 prompt，在 headless 模式下透過 `-p/--prompt` 傳入。
- `session_id`：對應 `--resume <id>`；可接受 `"latest"` 或特定 session ID / 索引。
- `model`：對應 `-m, --model`。可使用別名（`auto`、`pro`、`flash`、`flash-lite`）或完整模型名。
- `approval_mode`：對應 `--approval-mode`，可接受 `default | auto_edit | yolo | plan`。
- `yolo`：對應 `-y/--yolo`（已被標記為 deprecated，等同 `--approval-mode=yolo`，新整合應直接用 `--approval-mode`）。
- `sandbox`：對應 `-s/--sandbox`，是否在 sandbox 環境執行工具。
- `include_directories`：對應 `--include-directories`，加入額外 workspace 目錄。
- `output_format = stream-json|json`：若要 streaming，使用 `stream-json`；若只要最終結果，使用 `json`。

### 建議呼叫範本

```
gemini \
  -p "<prompt>" \
  [--output-format stream-json] \
  [--resume <session_id|latest>] \
  [-m <model|alias>] \
  [--approval-mode default|auto_edit|yolo|plan] \
  [-s] \
  [--include-directories <dir1,dir2>]
```

## Session 契約

Gemini 用 session ID 作為會話延續鍵，並透過 `--resume <id|latest>` 接續既有 session；也提供 `--list-sessions` 與 `--delete-session` 指令操作歷史 session。
因此你的 agent system 不應自己重播完整 message history 給 Gemini，而應把 Gemini session 視為 **provider-owned conversation state**，你只需要保存 session ID 並在續跑時帶回去。

### Session 技術定義
- `session_id` 來源：`stream-json` 的首個 `init` 事件（metadata 帶有 session ID 與 model）；`json` 模式目前不保證在頂層輸出 session ID，若需 resume 能力必須走 `stream-json`。
- `session scope`：單次 agent execution 內一致不變。
- `session continuity method`：`--resume <session_id>` 或 `--resume latest`。
- `session discovery`：可用 `gemini --list-sessions` / `--delete-session` 管理，但對系統整合而言，應以你自家 DB 為主，不依賴 CLI 查詢歷史。

### Session 管理規則
- 你的 framework 應把 Gemini session 視為 opaque token，不要自行生成或推導。
- `session_id` 應與你的 thread / workspace / task domain 做綁定，避免跨任務污染上下文。
- session persistence 應在 **收到 `init` event 即可落庫**，不要等成功結束才存，因為失敗 run 也可能需要繼續診斷。
- 若使用 `json` 模式，應接受「此次 run 無法續跑」的限制，或改用 `stream-json` 取得 session ID 再存。

## Stream 契約

`stream-json` 是 **NDJSON**，每一行一個 JSON event，事件順序是即時發生順序；成功時會以 terminal `result` event 收尾，失敗時可能提前中止且沒有 terminal event，錯誤訊息寫到 stderr。
因此你的 stream consumer 必須被設計成 **line-oriented parser + terminal-state machine**，不能假設一定有完結事件。

### Stream 基本規格
- 傳輸層：stdout。
- 編碼形式：newline-delimited JSON（JSONL），每行單一 JSON 物件。
- 成功結束：有 `type="result"` 的 terminal 事件，並帶聚合後的 `stats`。
- 失敗結束：process exit non-zero，可能沒有 terminal JSON，stderr 為主要錯誤來源。
- 未來擴充：官方文件明示事件欄位可能擴張（例如 per-model token 細分），consumer 應忽略 unknown fields。

## Event 型別契約

官方文件定義 `stream-json` 至少六類 runtime event：`init`、`message`、`tool_use`、`tool_result`、`error`、`result`。
所以你的 agent event bus 應至少抽象出這六類，而不要只做「text delta + final text」兩類，否則會失去工具活動、session metadata 與非致命警告。

### 1. Init
用途：告知本次 session 的初始化資訊（session ID、model 等 metadata）。

最低保留欄位：
- `type = init`
- `session_id`
- `model`
- （可選）`cwd` / `approval_mode` / `tools` / `extensions` 等環境 metadata。

### 2. Message
用途：使用者與助理的訊息區塊；助理段可為串流 chunk，完整自然語言輸出需由所有 assistant `message` 依序串接重建。

最低保留欄位：
- `type = message`
- `role = user|assistant`
- `content`（文字或結構化 chunk）
- `session_id?`（同 run 內為常數，可省略再由 consumer 補齊）。

### 3. Tool Use
用途：模型發起的工具呼叫請求，含工具名稱與 arguments，對應 UI、audit、latency 與 file-op trace。

最低保留欄位：
- `type = tool_use`
- `call_id`（工具呼叫識別）
- `name`（工具名稱，例如 `read_file`、`write_file`、`shell`、`google_web_search`）
- `arguments`（原始 JSON）

### 4. Tool Result
用途：工具執行結果回傳，與 `tool_use` 以 `call_id` 對應。

最低保留欄位：
- `type = tool_result`
- `call_id`
- `ok = true|false` / `status`
- `content`（輸出內容，可能為文字或結構化 payload）
- `error?`（失敗時的 error detail）

### 5. Error（非致命）
用途：非致命警告與系統訊息；不等於 run failure。

最低保留欄位：
- `type = error`
- `message`
- `code?`
- `fatal = false`（若事件仍持續則視為 warning）

> 注意：**非致命 `error` event ≠ run 失敗**。run 是否失敗以 process exit code 與 `result` 事件為準。

### 6. Result（Terminal）
用途：成功結束的最終聚合結果與統計資料。

最低保留欄位：
- `type = result`
- `response`：聚合後的最終自然語言答案（與 `json` 模式的 `response` 對齊）。
- `stats.models[*]`：per-model token & API latency 統計（`api.totalRequests / totalErrors / totalLatencyMs`、`tokens.prompt / candidates / cached / thoughts / tool / total`）。
- `stats.tools`：`totalCalls / totalSuccess / totalFail / totalDurationMs / totalDecisions / byName[*]`。
- `stats.files`：`totalLinesAdded / totalLinesRemoved`。
- `session_id?`（若提供）。

## Tool Call 契約

Gemini 的 `tool_use` / `tool_result` 採用 `name + arguments` 的結構，沒有像 Cursor 那樣為特定工具（`readToolCall`、`writeToolCall`）定義專用 schema。
因此你的工具事件正規化應採 **多型泛型 schema**，以 `name` 為派發鍵，把 `arguments` 與 `content` 當成原始 JSON 交給上層決定如何顯示。

### Tool 事件規格
- `tool_use.name`：內建或 extension / MCP 工具名稱。
- `tool_use.arguments`：原始 JSON，不要預先規格化。
- `tool_result.content`：工具輸出，可能為文字、結構化 payload 或混合。
- `tool_result.ok` / `tool_result.error`：以 boolean / error object 表達成功或失敗。

### Tool correlation 規格
- `call_id` 是 `tool_use` → `tool_result` 的對應鍵，應保存於你的 internal trace graph。
- 若你的系統已有 tool lifecycle 事件，Gemini 的 `call_id` 可直接映射為外部 tool invocation id。
- `stats.tools.totalDecisions` 反映人為介入狀態（`accept / reject / modify / auto_accept`），對 audit 與 approval flow UI 有幫助。

## Completion 契約

若使用 `json` 模式，成功時 stdout 只會輸出一個 JSON 物件，內容是聚合後的 `response`、`stats`、以及（失敗時）`error`；不會有 delta、tool events 或 `init`。
因此對你的 agent framework 而言，`json` 模式只適合「一次性 completion provider」，不適合作為 full-fidelity agent stream provider，也不適合需要 session resume 的 workflow（因為 session ID 不一定保證暴露於頂層）。

### `json` 模式成功物件最低欄位
- `response`：最終自然語言輸出。
- `stats.models[*]` / `stats.tools` / `stats.files`：與 `stream-json` 的 `result` 統計對齊。
- `error?`：失敗時的 `{ type, message, code? }`，僅在失敗時出現。

### `json` 模式用途
- batch job
- 非互動式 pipeline
- 只需最終自然語言答案
- 不需要工具中間狀態、live UI、或 session resume。

## Error 契約

官方文件明示 headless 模式使用標準 exit code 指示結果：失敗時 process 以 non-zero exit code 結束，錯誤輸出到 stderr，而且不保證產出合法 JSON terminal object。
因此你的 provider error 規格必須以 **process semantics 優先於 JSON semantics**，換句話說，`stdout parse success != run success`。

### Exit code 對照
- `0`：成功。
- `1`：一般錯誤或 API 失敗。
- `42`：輸入錯誤（prompt 或參數不合法）。
- `53`：超過 turn 上限。

### 成功判定標準
一個 run 應同時滿足：
- process exit code = 0；
- 若為 `stream-json`，理想上收到 terminal `result` event；
- 若為 `json`，stdout 可 parse 成單一合法 object 且 **無** `error` 欄位。

### 失敗判定標準
任一成立即可視為 run failure：
- process exit code != 0（含 `1 / 42 / 53` 等）；
- stdout NDJSON 中斷且無 terminal `result`；
- `json` 模式輸出包含 `error` 欄位；
- stdout 非法 JSON 且 process 結束異常；
- stderr 有錯且 process 失敗。

### Error object 最少應含
- `provider = gemini`
- `phase = spawn|stream|parse|wait|provider`
- `exit_code?`（特別注意區分 `1 / 42 / 53`）
- `stderr`
- `session_id?`
- `partial_text?`
- `last_event_type?`
- `gemini_error?`（若是 `json` 模式，帶上 CLI 回傳的 `{ type, message, code? }`）

## Lifecycle 契約

由於 Gemini 是 subprocess provider，你的 agent runtime 需要完整管理：
- spawn
- stdout stream read
- stderr stream read
- cancellation
- wait/reap child process。

### Cancellation 規格
- 以 host-side context cancel / process kill 為主。
- 被取消的 run 應視為 `aborted` 而非 `failed`，除非 Gemini 回報明確 provider error（exit code != 0 與 cancel 無關的 stderr）。
- 若 cancel 發生在 session 已建立後（已收到 `init`），應保留該 `session_id`，因為後續可能要 resume 繼續。

## Capability 契約

Gemini CLI 具備檔案讀寫、shell 執行、web 搜尋、MCP / extension 工具等能力；可透過 `--approval-mode` 控制工具執行的放行策略（`default` 需逐項確認、`auto_edit` 自動放行編輯、`yolo` 全自動、`plan` 僅規劃不執行）。
所以你應把 Gemini provider 標記為 **agentic provider with filesystem + shell side effects**，不要把它當成純文字 completion engine。

### 建議 capability metadata
- `supports_session_resume = true`（`--resume`）。
- `supports_streaming = true`（`stream-json`）。
- `supports_final_json = true`（`json`）。
- `supports_tool_events = true`（`tool_use` / `tool_result`）。
- `supports_model_selection = true`（`--model`，含別名）。
- `supports_approval_mode = true`（`--approval-mode`；這是 Gemini 對應 Claude permission mode 的最接近概念）。
- `supports_sandbox = true`（`--sandbox`）。
- `supports_plan_mode = true`（`--approval-mode plan`）。
- `has_side_effects = true`，因為可寫檔與執行 shell。

### 與 Claude permission mode 對應
| Claude permission mode | Gemini 對應 |
| --- | --- |
| `default` | `--approval-mode default` |
| `acceptEdits` | `--approval-mode auto_edit` |
| `bypassPermissions` | `--approval-mode yolo`（或 `-y`，後者已 deprecated） |
| `plan` | `--approval-mode plan` |
| `auto` / `dontAsk` | 無 1:1 對應，使用 `auto_edit` 或 `yolo` 依安全需求選擇 |

## Thinking / Reasoning 契約

Gemini 的 `stats.models[*].tokens.thoughts` 會在結束時以聚合數字呈現思考 token 量，但 **thinking 內容本身在 headless 模式不會以串流事件暴露**。
所以你的統一 agent schema 若有 reasoning stream 欄位，對 Gemini provider 應標記為 **unsupported stream, aggregated-only**：無法取得中間推理文字，僅能取得 thoughts token 總量。

## Forward Compatibility 契約

官方明示 `stats` 結構與 `init` metadata 可能隨版本擴張（per-model 細分、extension / skill metadata、MCP 狀態等）。
因此你的解析策略應是：
- 外層 event envelope 鬆綁；
- 只解析所需核心欄位（`type`, `session_id`, `role`, `call_id`, `response`, `stats.*`）；
- 原始 JSON 保留一份作 raw payload，以利除錯與後續升級；
- 忽略 unknown 事件 type，不要因出現未知 type 而 abort stream。

## 認證契約

Gemini CLI 支援 Google 登入（`gemini` 首次啟動會引導），以及透過環境變數 `GEMINI_API_KEY` / `GOOGLE_API_KEY` 等提供 API 金鑰；headless 情境通常使用環境變數為主。
對系統整合而言，建議把認證抽象成 provider credential source，優先順序可設計為：runtime env（由 agent framework 注入子行程 env）> host 登入狀態。由於 CLI 沒有 `-a/--api-key` 旗標，**金鑰只能經由環境變數傳入**，切勿在 argv 上明文帶入。

## MCP / Extensions 契約

Gemini CLI 原生支援 MCP 管理（`gemini mcp add/remove/list`）與 Extensions / Skills 管理（`gemini extensions …`、`gemini skills …`）。
如果你的 agent framework 已有 tool registry，應把 Gemini 的 MCP / Extensions / Skills 視為 **provider-side external tool substrate**，不是你 framework 直接可觀測的 tool registry，除非你額外同步兩邊定義。

啟動時可透過下列旗標控制：
- `--allowed-mcp-server-names`：白名單 MCP server。
- `--extensions` / `-e`：指定要啟用的 extension 子集（預設全部啟用）。
- `--allowed-tools`：已 deprecated，建議改走 Policy Engine（gemini settings）或 `--approval-mode`。

## 你的框架需要補的抽象

如果你要在既有 `internal/agent` 套件中新增 Gemini 支援（對應 `agent-integration.md` 的 Phase E.2），最實際要補的不是範例，而是以下抽象：

### Provider Registration
- provider id: `gemini`（對應 `agent.TypeGemini`）
- transport: `subprocess`
- session model: `provider-owned`
- stream format: `ndjson`（`stream-json`）
- completion mode: `json` / `stream-json`
- reasoning visibility: `aggregated-only`（僅 thoughts token 計數）
- side effects: `filesystem`, `shell`, `web`（視啟用的工具與 MCP 而定）

### Internal Normalized Events 映射
| Gemini event | 你的 `agent.Event` |
| --- | --- |
| `init`（含 `session_id`） | `EventSessionInit` |
| `message`（assistant chunk） | `EventDelta`（串接 text） |
| `message`（user） | （可選）audit event，預設不轉發 |
| `tool_use` | `EventToolStarted`（若尚未定義，可先忽略或以 log 記錄） |
| `tool_result` | `EventToolCompleted`（同上） |
| `error`（非致命） | 可落入 log，不必映射為 `EventError` |
| `result` | `EventDone`（帶 `session_id` 與最終文字） |
| process exit != 0 / stream 中斷 | `EventError` |

> 注意：現有 `agent.Event` 沒有 tool lifecycle 事件型別；新增 Gemini 時建議先把 `tool_use` / `tool_result` 視為 diagnostic log 處理，等 Phase E 完成後再擴充 `EventType`。

### Permission Flow
- Gemini **沒有** Claude 式的 `permission_denials` → `allow_once` 交互流程。
- 授權策略在啟動時以 `--approval-mode` 決定，一旦 run 開始就不能中途切換。
- `agent.Runner.Run` 不會觸發 `EventPermDenied`，WS handler 的 `allow_once` / `set_mode` 對 Gemini session 應直接 no-op 或回傳「此工具不支援互動授權」。

### Persistence Domain
- `provider = "gemini"`
- `thread_key`
- `session_id`（來自 `init` 事件）
- `workspace`（= `cwd`，與 `--include-directories` 一起記錄）
- `model`（別名解析後的實際模型名，由 `init` 或 `result.stats.models` 推出）
- `approval_mode`
- `started_at` / `completed_at`
- `exit_code`
- `stderr`
- `partial_output`

## 建議的最終技術規格

你可以把 Gemini provider 寫成下面這個規格：

| 面向 | 規格 |
|---|---|
| Provider 類型 | Local subprocess agent provider。 |
| 啟動方式 | `gemini -p <prompt> [--output-format <format>] [--resume <session|latest>] [-m <model>] [--approval-mode <mode>] [-s] [--include-directories <dirs>]`。 |
| Prompt 傳遞 | `-p/--prompt` 旗標（或 stdin pipe；兩者可併用，`-p` 附加在 stdin 之後）。headless 模式**不**使用 positional。 |
| Streaming | `stream-json`，stdout NDJSON，一行一事件。 |
| Final-only | `json`，成功時單一 JSON object（含 `response` 與 `stats`）。 |
| Session continue | `--resume <id|latest>`，session_id 由 provider 在 `init` 事件產生。 |
| Tool telemetry | `tool_use` / `tool_result` + `call_id`；彙總見 `result.stats.tools`。 |
| Final result | `type=result, response, stats.{models,tools,files}, session_id?`。 |
| Failure source | exit code（`0` / `1` / `42` / `53`）+ stderr；`json` 模式的 `error` 欄位亦為失敗訊號。 |
| Unknown fields | 必須忽略，以保持前向相容。 |
| Reasoning stream | 不提供中間 thinking 文字，僅 `stats.models[*].tokens.thoughts` 聚合計數。 |
| Side effects | 可讀檔、寫檔、執行 shell、web 搜尋；由 `--approval-mode` 控制放行策略。 |
| 認證 | 環境變數（`GEMINI_API_KEY` 等）或 `gemini` 登入狀態，**無 `--api-key` 旗標**。 |
