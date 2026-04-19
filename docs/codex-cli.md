## Provider 定位

Codex 的非互動模式是 `codex exec`，官方明確說這是給 script、CI、pipeline 用的，並且 `--json` 會把 stdout 變成 JSON Lines 事件流。 
因此在你的系統裡，Codex 至少應拆成兩種可選 transport：**CLI subprocess transport**（先做這個）與 **app-server protocol transport**（後續若你要更原生的雙向通訊再做）。 

## 啟動契約

非互動執行的主命令是 `codex exec`，prompt 可以是 positional argument，也可以用 `-` 表示從 stdin 讀完整 prompt。 
如果你要 machine-readable integration，正式規格應固定用 `codex exec --json`，因為不加 `--json` 時，stdout 預設只印 final message，而 progress 走 stderr，不適合程式解析。 

### 必備 request 欄位
- `provider = codex`
- `cwd`，對應 `--cd, -C`。 
- `prompt`，對應 `PROMPT` positional arg，或 `-` 走 stdin。 
- `session_id?`，給 `codex exec resume [SESSION_ID]`。 
- `model?`，對應 `--model, -m`。 
- `sandbox_policy?`，對應 `--sandbox, -s`。 
- `approval_policy?`，對應 `--ask-for-approval, -a`。 
- `profile?`，對應 `--profile, -p`。 
- `json_stream = true`，對應 `--json`。 
- `ephemeral?`，對應 `--ephemeral`。 
- `skip_git_repo_check?`，對應 `--skip-git-repo-check`。 
- `output_schema?`，對應 `--output-schema`。 
- `output_last_message_path?`，對應 `--output-last-message`。 

## Session 契約

Codex 支援 non-interactive session continuation，方式是 `codex exec resume [SESSION_ID]`，或 `--last` 續接目前工作目錄下最近一次 session；也可用 `--all` 跨目錄找最近 session。 
所以和 Cursor 一樣，你的 framework 應把 Codex session 當成 **provider-owned session state**，不要每次自己重播完整 transcript。 

### Session 規格
- session key 名稱：`thread_id`，事件流中會先出現 `thread.started` 並提供 `thread_id`。 
- resume 方式：`codex exec resume <SESSION_ID>`。 
- 最近 session 快速續接：`codex exec resume --last`，預設限定 current working directory。 
- 跨目錄搜尋最近 session：加 `--all`。 

### Session 管理規則
- `thread_id` 是你應保存的主要 provider session id。 
- 若你要 deterministic resume，請用明確 `SESSION_ID`，不要依賴 `--last`，因為 `--last` 綁 cwd 範圍且帶有環境語意。 
- 若 run 使用 `--ephemeral`，官方說不會把 session rollout files 寫到磁碟，因此你的系統不應假設本機 session files 一定存在。 

## Stream 契約

`codex exec --json` 時，stdout 會變成 **JSONL stream**，每個 state change 一行一個 JSON 物件。 
官方列出的高層事件型別包含 `thread.started`、`turn.started`、`turn.completed`、`turn.failed`、`item.*`、`error`，這表示你應採 **event-driven state machine** 整合，而不是只看最後一條 message。 

### Stream 基本規格
- transport：stdout。 
- encoding：JSONL / NDJSON，一行一個事件。 
- success terminal：`turn.completed`。 
- failure terminal：`turn.failed` 或 `error`，以及 process 非 0 結束。 
- 非 JSON 模式：stdout 只印 final message，stderr 才有進度資訊。 

## Event 類型契約

官方至少公開了這些事件類別，你的統一 event bus 應支援它們。 

### 1. Thread Started
- `type = "thread.started"`
- `thread_id`。 

這是 session 建立事件，等價於 Cursor 的 `system/init.session_id`。 

### 2. Turn Started
- `type = "turn.started"`。 

這是一次 request/turn 開始，可對應你的 `run.turn_started` 或 `request.started`。 

### 3. Turn Completed
- `type = "turn.completed"`
- `usage.input_tokens`
- `usage.cached_input_tokens`
- `usage.output_tokens`。 

這是成功結束事件，也是 token usage 統計的主要來源。 

### 4. Turn Failed
- `type = "turn.failed"`。 

這是本次 turn 失敗的正式事件，但你仍應以 process exit code 做最終成功判定補強。 

### 5. Error
- `type = "error"`。 

這是 provider-level 或 runtime-level 錯誤訊號，應映射到你的標準錯誤事件。 

### 6. Item Events
- `type = "item.started"` / `item.completed"`。 

`item` 是 Codex 中最重要的中間事件抽象，官方明說 item type 包含：
- agent messages
- reasoning
- command executions
- file changes
- MCP tool calls
- web searches
- plan updates。 

這表示 Codex 比 Cursor 更明確把中間活動抽象成統一 `item` 模型。 

## Item 契約

官方範例顯示 `item.started` / `item.completed` 會帶一個 `item` 物件，內含 `id`、`type`、`status`，例如 `command_execution` 或 `agent_message`。 
所以你在內部設計上應把 Codex 的中間輸出視為 **typed work items**，而不是 Cursor 那種偏 message/tool_call 雙分流模型。 

### 最低 item schema
- `item.id`
- `item.type`
- `item.status`
- type-specific payload。 

### 已知 item.type 類別
- `agent_message`
- `reasoning`
- `command_execution`
- `file_change`
- `mcp_tool_call`
- `web_search`
- `plan_update`。 

## Completion 契約

如果不用 `--json`，`codex exec` 在執行中會把 progress 寫到 stderr，而 stdout 只輸出 final assistant message，因此它在 default mode 下比較像「final-text provider」。 
若用 `--json`，stdout 就是完整 JSONL event stream，適合做統一 agent runtime 的細粒度整合。 

### Final output 規格
- final human-readable answer：可由 `agent_message` 類 item 重建，或用 `--output-last-message` 輸出到檔案。 
- usage metrics：來自 `turn.completed.usage.*`。 
- structured final output：可透過 `--output-schema` 要求 final response 符合 JSON Schema。 

## Structured Output 契約

Codex 原生支援 `--output-schema <path>`，可以要求最終回應符合指定 JSON Schema，官方文件直接把它定位為 downstream automation 的穩定資料格式方案。 
所以如果你的 agent framework 有「structured final answer」能力，Codex provider 應標記為 **native structured-output capable**，而不是靠 prompt engineering 模擬。 

### 這代表你的框架可以增加
- `supports_output_schema = true`。 
- `final_output_validation = provider-native`。 
- `structured_result_channel = final_message`。 

## Error 契約

Codex 非 JSON 模式下，進度主要走 stderr；JSON 模式下 stdout 是 JSONL event stream，但最終成功與否仍不能只靠有沒有收到事件判斷，因為 CLI 是 subprocess，exit code 仍是 host-level truth。 
所以和 Cursor 一樣，你的成功判斷應採 **event + process dual validation**。 

### 成功判定
- process exit code = 0； 
- `turn.completed` 出現； 
- 若要求 structured output，最終 schema 驗證已通過，否則 command 本身應失敗或輸出不符預期。 

### 失敗判定
- process exit code != 0； 
- 收到 `turn.failed`； 
- 收到 `error`； 
- JSONL 中途斷流且無 terminal success event。 

### Error object 建議欄位
- `provider = codex`
- `phase = spawn|stream|parse|wait|provider`
- `exit_code?`
- `stderr`
- `thread_id?`
- `last_turn_state?`
- `last_item_id?`
- `partial_text?`

## Sandbox / Approval 契約

Codex 有比 Cursor 更明確的 sandbox 與 approval 模型；可用 `--sandbox read-only|workspace-write|danger-full-access`，也可用 `--ask-for-approval untrusted|on-request|never` 控制指令是否需要人類批准。   
這表示在你的 agent framework 中，Codex provider 應暴露 **execution-safety policy** 作為一級能力，而不是只留一個布林值 `force`。 

### 建議正規化欄位
- `sandbox_mode = read-only | workspace-write | danger-full-access`。 
- `approval_mode = untrusted | on-request | never`。 
- `full_auto = shortcut`，代表 low-friction preset，不應當成獨立能力語義。 
- `yolo = bypass approvals and sandbox`，高風險模式。 

## Git / Workspace 契約

官方說 Codex 預設要求在 Git repository 內執行，以降低破壞性修改風險；若確定環境安全可用 `--skip-git-repo-check` 覆寫。 
所以你的 provider request model 應有 workspace safety precheck 概念，特別是當 agent 要寫檔或執行命令時。 

## 認證契約

Codex CLI 可透過 `codex login` 使用 ChatGPT OAuth 或 API key；在 automation / CI，官方推薦 API key，且 `CODEX_API_KEY` 僅支援 `codex exec`。 
所以你在整合層應把 Codex 認證分成：
- interactive auth state
- per-run API key auth for exec。 

### 建議 auth metadata
- `auth_mode = saved-login | api-key`
- `api_key_env = CODEX_API_KEY` for exec automation。 
- `login_status_probe = codex login status`，存在憑證時 exit code 0。 

## Remote / App-server 契約

Codex 有 `app-server`，可用 `stdio://` 或 `ws://IP:PORT` 監聽；官方文件還提供 V2 thread/turn flow 範例，代表 app-server 是比 CLI 更原生的 protocol surface，但目前標記為 Experimental。 
如果你的架構後續會想做 long-lived provider connection、遠端多工、或不想每次 spawn subprocess，Codex 比 Cursor 更適合往 app-server transport 演進。 

### App-server 對你架構的意義
- 可作為 persistent bidirectional transport。 
- protocol 本身有 `initialize`、`thread/start`、`turn/start` 這類方法。 
- 適合高吞吐、本機 agent hub、或多 client 共用一個 provider daemon 的場景。 
- 但因為屬於 Experimental，你若要穩定先上線，建議先做 `codex exec --json`。 

## MCP 契約

Codex 內建 `codex mcp` 管理 MCP server，也能用 `codex mcp-server` 把 Codex 自己暴露成 MCP server over stdio。 
所以 Codex 在你的框架裡其實可以有兩種角色：**agent provider** 或 **tool provider**，這點比 Cursor 更明顯。 

## 與 Cursor 的核心差異

| 面向 | Codex | Cursor |
|---|---|---|
| 非互動主命令 | `codex exec`。  | `cursor-agent --print`。  |
| JSON stream | `--json`，stdout JSONL。  | `--output-format stream-json`，stdout NDJSON。  |
| session id | `thread_id`，由 `thread.started` 提供。  | `session_id`，由 `system/init` 或 result 提供。  |
| resume | `codex exec resume <SESSION_ID>`。  | `--resume <session_id>`。  |
| 中間事件模型 | `item.*` typed items。  | `assistant` + `tool_call` 為主。  |
| reasoning 可見性 | 有 `reasoning` item 類型。  | print mode 不提供 thinking。  |
| 結構化輸出 | 原生 `--output-schema`。  | 文件中未見等價原生 schema 約束。  |
| safety policy | approval + sandbox 分離控制。  | 偏 `--force`/permissionMode。  |
| 進階 transport | app-server / ws / stdio。  | 主要仍是 CLI / background agent docs。  |

## 你的框架要補的抽象

如果你要讓現有 AI agent 增加 Codex 實作，我會建議新增這些 provider-agnostic 抽象：

### Transport Layer
- `subprocess-jsonl`
- `persistent-rpc-jsonl`，給未來 Codex app-server 用。 

### Session Layer
- `provider_session_id`
- `provider_session_scope = cwd-bound | global`
- `resume_strategy = explicit-id | last-in-cwd`。 

### Event Layer
- `thread.started`
- `turn.started`
- `message.delta/message.completed`
- `item.started/item.completed`
- `turn.completed`
- `turn.failed`
- `error`。 

### Safety Layer
- `sandbox_mode`
- `approval_mode`
- `unsafe_bypass`
- `workspace_policy`。 

### Output Layer
- `final_text`
- `usage`
- `structured_schema`
- `final_message_file`
- `raw_event_stream`。 

## 我給你的最終規格結論

如果只用一句話定義 Codex provider：

**Codex 應被視為「支援 session resume、typed item event stream、native structured output、可配置 sandbox/approval 的 CLI/app-server agent provider」**。 

