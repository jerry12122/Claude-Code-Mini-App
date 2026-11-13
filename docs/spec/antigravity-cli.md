# Antigravity CLI（agy）整合規格

> **取代 legacy Gemini CLI。** Gemini CLI 已退役；本專案以 `agent_type=antigravity`、子進程 `agy` 整合。
> **應用狀態（2026-10-12）：** 已啟用 Antigravity，預設解析 `init`／`step_update`／`result` 串流；回歸測試與 Chrome 元件驗證通過，真實 CLI／Telegram WebView 本輪驗收待補。
> 實作：`internal/antigravity/` · POC 與驗證：`poc/antigravity-cli/`

## 官方文件

- https://antigravity.google/docs/cli-overview
- https://github.com/google-antigravity/antigravity-cli
- 遷移指南：CLI Overview → *Migrating from Gemini CLI*

## Provider 定位

| 項目 | 規格 |
|---|---|
| 類型 | Local subprocess（`agy`，用後即棄） |
| 通訊格式 | 空白指令沿用 `--output-format stream-json`；有專屬指令時使用 `agy acp` |
| Prompt | **stdin pipe**（避開 Windows argv 截斷，支援多行輸入） |
| Session 延續 | `--conversation <session_id>` 跨輪延續對話歷史 |
| 權限模式 | 支援 `--mode accept-edits`、`--mode plan` 與 `--dangerously-skip-permissions` |
| 模型設定 | `--model <name>`（可選用 `gemini-3.8-flash-high` 等） |
| 推理強度 | `--effort <level>`，沿用會話設定 |
| 工作目錄 | 設定子進程 `cmd.Dir`，並以 `--add-dir <work_dir>` 加入工作區 |
| 配額用量 | `agy -p /usage --output-format json`，讀取原生 bucket，分別顯示 Gemini 與 Claude/GPT 的 5 小時與每週額度 |

## 帳戶額度（2026-11-13）

正式 Go fetcher 已以 agy 1.5.8 實讀成功。檢查 `status == SUCCESS` 與 `command.name == usage`，讀取 `command.data.groups[].buckets[]`，不將單回合 `.usage` token 統計當成帳戶額度。

- 以 `gemini-5h` / `gemini-weekly` / `3p-5h` / `3p-weekly` ID 對應現有四個 window key，不依賴顯示文案。
- 已使用百分比 = `100 - remaining_fraction * 100`，保留小數；重設時間沿用 `reset_time`。
- 缺少成功 envelope、已知 buckets 或合法 remaining fraction 時回傳錯誤，既有 quota service 保留上次成功快照，不回退 TSV。
- 查詢上限 30 秒，取消時終止子進程樹；沿用 `CC_AGY_BIN` 覆寫與既有快取、前端。

## 啟動命令範本

```bash
agy --output-format stream-json \
  [--conversation <session_id>] \
  [--model <model>] \
  [--effort <level>] \
  [--mode accept-edits | plan] \
  [--dangerously-skip-permissions] \
  [--add-dir <work_dir>]
# prompt 經 stdin 管道餵入
```

## 事件協議（NDJSON）

`agy` 輸出的頂層事件欄位以 `event` 區分：

1. **`init`**：
   ```json
   {"event":"init","conversation_id":"<id>","init":{"cwd":"...","tools":[...],"permission_mode":"..."}}
   ```
2. **`step_update` (文字增量)**：
   ```json
   {"event":"step_update","step_update":{"step_type":"agent_response","state":"ACTIVE","text_delta":"..."}}
   ```
3. **`step_update` (工具呼叫與結果)**：
   ```json
   {"event":"step_update","step_update":{"step_type":"tool","state":"ACTIVE","tool_name":"...","tool_info":{"parameters":{...}}}}
   {"event":"step_update","step_update":{"step_type":"tool","state":"DONE","tool_name":"...","tool_info":{"output":"..."}}}
   ```
4. **`result`**：
   ```json
   {"event":"result","result":{"status":"SUCCESS","response":"...","usage":{"input_tokens":...,"output_tokens":...}}}
   ```

## Permission Mode 對應

| Mini App `permission_mode` | agy 命令列旗標 | 行為說明 |
|---|---|---|
| `default` | *(無額外旗標)* | 預設審核模式；無權限工具軟性拒絕 |
| `acceptEdits` | `--mode accept-edits` | 自動允許檔案讀寫與編輯 |
| `plan` | `--mode plan` | 唯讀規劃模式 |
| `bypassPermissions` / `yolo` | `--dangerously-skip-permissions` | 全自動放行所有工具 |

## agent_type 遷移

| 舊值 | 新值 |
|---|---|
| `gemini` | `antigravity`（factory 自動別名導向 Antigravity runner） |

舊 `gemini` Session 可繼續執行，但不可用此舊值新建 Session。新建使用 `antigravity`。

## 模型與用量顯示

- 啟動及設定頁「重新抓取」會透過 `agy models` 同步模型選單（30 秒逾時，支援 `CC_AGY_BIN`）；抓取失敗、格式錯誤或空清單時保留既有選項並回報失敗。重新開啟對話即可載入更新的選單。
- 模型選擇優先序：會話 `model` → `cli_extra_args` 的 `--model` → 標籤 fallback `gemini-3.8-flash-high`。fallback 是應用推論值；沒有指定 `--model` 時，CLI 仍使用自己的預設設定。
- 切換模型時立即發送 `model_update`，執行前更新持久化資訊；重新連線與清除選擇後重新解析，避免沿用過期標籤。
- 配額是帳戶級快取，將 CLI 回報的剩餘比例換算為已使用比例（限制在 0–100%），以 Gemini／Claude/GPT 標籤區分兩組窗口；5 小時窗口附重置倒數。
- 手機精簡徽章取兩組窗口的最高使用率，展開頂欄及 tooltip 保留各組明細；100% 使用率沿用紅色警示，支援手動刷新。

## 錯誤收尾與驗證

- `CC_AGY_FORCE_PRINT_TEXT=1` 純文字後備模式若 exit 0、未取消且 stdout 為空白，發送 `EventError` 並保留 stderr，交由 WebSocket 既有流程收尾。
- `result.usage` 已可解析，但目前沒有透過 `agent.Event` 傳遞回合 Token 統計，帳戶配額顯示另由 `/usage` 取得。
- 驗證範圍與命令見 [審查修復紀錄](../plan/done/agy-review-fixes.md)。

## 環境變數

| 變數 | 用途 |
|---|---|
| `CC_AGY_BIN` | 覆寫 agy 執行檔路徑 |
| `CC_AGY_FORCE_PRINT_TEXT=1` | 強制降級為純文字 print 模式（後備方案） |

舊 `CC_AGY_STREAM_JSON` 不再控制 runner；空白指令且未強制純文字時預設使用串流。

## 建立時的會話專屬指令（2026-11-12）

建立 Antigravity session 時可填「專屬指令（選填）」，建立後固定，調整請另建會話。`system_instruction` 與 user prompt 分開儲存；API 修剪外圍空白，最多 16384 UTF-8 bytes，不接受 U+0000。省略或空白沿用既有 CLI／print 選路；非空值優先使用 `agy acp`，即使設定 `CC_AGY_FORCE_PRINT_TEXT=1` 也不降級丟掉指令。

ACP 使用已快取的本機登入，不開瀏覽器認證、不建立自訂 agent 或規則檔。`initialize` 必須宣告 `agentCapabilities._meta.antigravity.systemPrompt.append=true`，續接還需要 `loadSession=true`。新建與續接的 `session/new`／`session/load` params 皆包含：

```json
{"cwd":"<absolute-work-dir>","mcpServers":[],"_meta":{"antigravity":{"systemPrompt":{"append":"<saved-instruction>"}}}}
```

`session/load` 另帶既有 `sessionId`；`session/prompt` 僅傳本輪 user 文字。保留原生 MCP 與預設客製設定；載入歷史不重播成本輪回答。文字、思考與工具事件轉成共用 runner 事件，成功回應與程序回收後只送一次完成事件。

模型優先使用宣告的 config option，否則使用宣告的 model ID；effort 對應 `thought_level`。`default`／`acceptEdits`／`plan` 對應宣告的 `default`／`accept-edits`／`plan` 模式；只有明確的 bypass 設定才加 `--dangerously-skip-permissions`。工具授權走既有使用者決策回呼，無回呼時取消。缺能力、不支援設定、登入／RPC／EOF／逾時或子程序錯誤均回報失敗，沒有 legacy 後備。取消先送 `session/cancel` 再回收程序樹；設定請求上限 15 秒，本輪 prompt 上限 30 分鐘，程序回收有有限等待。

ACP 不記錄原始 payload 或 stderr，錯誤保留 RPC code 與登入／更新提示。2026-11-12 本機 agy 1.5.6 經正式 Go runner 的 new／跨程序 load marker 測試通過；marker 只在專屬指令內，未使用自訂 agent 檔。設定 RPC 的 plan、model、thought level 也經本機確認。
