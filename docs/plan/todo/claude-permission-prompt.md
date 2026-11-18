# 功能計劃：Claude 的 ask 規則改走 miniapp 授權（`--permission-prompt-tool`）

> 狀態：已實作。2026-11-18 瀏覽器驗過允許與拒絕（見 `docs/plan/todo.md`）；桌面版、Telegram、長等待與重複次數尚未驗
> 建立日期：2026-10-06
> 目標：
> - 第 0 期：指令被 Claude 權限規則（使用者 `~/.claude/settings.json` 等）擋下時，明確告訴使用者「是規則擋的、按允許無效」，不再顯示會失敗的「允許此操作」。
> - 第 1 期：session 在 `bypassPermissions` 下，命中 `permissions.ask` 規則（例如 `Bash(rm -rf *)`、`Bash(curl *)`）時，跳出 miniapp 的「允許／拒絕」，按允許後同一輪就執行。不改變未命中 ask 規則的指令行為。
> - 兩期可獨立交付；第 0 期改動小、不依賴新端點，先做。

## 1. 現況與原因

- miniapp 以 `claude -p` 執行，沒有互動介面。`deny` 規則一律擋；`ask` 規則沒人可問，被記成 denial。
- 現行「允許此操作」流程是：被拒 → 存 `pending_denials` → 使用者按允許 → 帶 `--allowedTools` 重跑。ask 的優先順序高於 allow，所以對 ask／deny 規則重跑仍被拒（已實測）。
- 官方文件：ask 規則命中時，即使 `bypassPermissions` 也會轉給 `--permission-prompt-tool` 指定的 MCP 工具決定。

## 1.5 第 0 期：失敗原因提示

### 問題
`result` 事件的 `permission_denials` 只有 `tool_name`／`tool_use_id`／`tool_input`，沒有原因；miniapp 目前把所有拒絕都當成「等使用者授權」。被 deny 規則擋下的指令同樣顯示「允許此操作」，按了只會重跑並再次被拒，使用者看不出原因（本次 `rm -rf .demo cmd/zz_seed` 即此情形，session 連續 4 次重試）。

### 訊號（實測，`claude` 2.1.286）
串流中被拒那筆 `tool_result` 帶 `tool_result_meta[].non_execution_kind`：

| 情境 | tool_result 文字 | `non_execution_kind` |
|---|---|---|
| deny 規則 | `Permission to use Bash with command … has been denied.` | `permission-rule` |
| ask 規則、無 prompt-tool | `Claude requested permissions to use Bash, but you haven't granted it yet.` | `user-rejected` |
| default 模式、無規則 | 未取得樣本 | 未知 |

`non_execution_kind` 是觀察到的欄位，官方文件未見說明 → 視為盡力而為的訊號，不可當唯一依據（見 §5）。

### 做法（三層，由簡到繁）
1. **標記 deny 規則**（必做）：`internal/claude/events.go` 解析 `tool_result_meta`；`runner.go` 依 `tool_use_id` 把 `permission-rule` 的 denial 標成 `BlockedByRule`，並在 `agent.PermissionDenial` 加對應欄位。`ws/handler.go`（約 L645–655）遇到此類 denial：
   - 不寫入 `pending_denials`、不進 `awaiting_confirm`、不廣播 `permission_request`（即不顯示允許按鈕）。
   - 改成在聊天室寫一則系統訊息＋TG 通知：「指令被 Claude 權限規則（permissions.deny）禁止，按允許也無效。請檢查 `~/.claude/settings.json`、專案 `.claude/settings.json`／`settings.local.json`。」
   - run 以一般結束處理（不當作 permDenied，佇列的暫停規則需一併確認）。
2. **允許後仍被拒的保險**（必做）：`runAgent(..., allowedOnce)` 的 retry 中若再出現任何 denial，表示 `--allowedTools` 已帶入仍被擋（ask／deny 規則或 managed 政策），顯示「已允許但仍被拒，通常是 settings 內的 ask／deny 規則」，**不得再進入等授權**，避免無限的允許→失敗迴圈。此判斷不依賴 `non_execution_kind`，是第 1 層失效時的後備。
3. **指出規則與檔案**（選做）：讀 `~/.claude/settings.json`、`<work_dir>/.claude/settings.json`、`settings.local.json` 的 `permissions.deny`／`ask`，用「`*` 前的字面值吻合」做前綴比對找命中的規則，訊息改為「被 `<檔案>` 的 `Bash(rm -rf *)` 擋下」。比不出（複合指令、managed settings）就退回第 1 層通用文字。

### 完成標準
- 以暫存 `CLAUDE_CONFIG_DIR` 設 `deny: ["Bash(rm -rf *)"]`，`bypassPermissions` 下要求 `rm -rf x`：聊天室出現規則說明、**沒有**允許按鈕、session 回到 idle、無 `pending_denials`。
- 同情境改為 `default` 模式、無規則：行為與現在完全相同（仍顯示允許按鈕，按了可執行）。
- 模擬 retry 仍被拒：出現第 2 層訊息，且不再進入等授權。
- 單元測試：`tool_result_meta` 解析（含欄位缺漏）、denial 與 `tool_use_id` 對應、retry 分支。

## 2. 已驗證（2026-10-06，暫存 `CLAUDE_CONFIG_DIR`，`claude` 2.1.286）

| 情境 | 結果 |
|---|---|
| ask 規則 + prompt-tool 回 `allow` | 指令執行，無 denial（2 次成功；首次一次未觸發，原因不明，疑為 MCP 首次連線時間差） |
| ask 規則 + prompt-tool 回 `deny` | 不執行，denial 帶原因 |
| 未命中 ask 規則的 `rm -r` | 不詢問，直接執行 |
| prompt-tool 延遲 130 秒才回 `allow`（stdio） | 正常等待並執行，共 142 秒 |
| ask 規則 + 無 prompt-tool + `--allowedTools` 重跑 | 仍被拒 |

未驗證：HTTP transport 下的長時間等待（見 §5）。

## 3. 設計

### 3.1 整體流程

```
runAgent(Claude, pm=bypassPermissions)
  ├─ 註冊 run token → 回呼 askFn（取代 kiroacp 的 RequestPermission closure 本體）
  └─ claude -p ... --mcp-config <inline JSON: type=http, url=http://127.0.0.1:<port>/claude-perm/<token>>
                   --permission-prompt-tool mcp__miniapp_perm__approve
命中 ask 規則
  → claude 呼叫 approve{tool_name, input, tool_use_id}
  → HTTP handler 以 token 找到 askFn → 阻塞
  → askFn：pendingPermEntry + status=awaiting_confirm + broadcast permission_request + TG 通知（同 kiroacp）
  → 使用者 allow_once / deny_once → permResolve（既有）→ askFn 回傳
  → 回 {"behavior":"allow","updatedInput":<原 input>} 或 {"behavior":"deny","message":"..."}
```

### 3.2 為什麼另開端點，不重用 `/mcp`
- `/mcp` 預設停用（`mcp_token` 空），且 token 等於完整 session 控制權（含 shell）。為了這個功能開 `/mcp` 會擴大攻擊面。
- 新端點只有一個工具，認證用「每次 run 隨機 token」：128 bit、run 結束即失效、僅接受 loopback 來源。token 不同於 `mcp_token`，外洩的影響僅限該次 run 的授權詢問。

### 3.3 範圍限制（刻意最小）
- 只對 `agent_type=claude` 且 `permission_mode=bypassPermissions` 的 run 啟用。`default` 模式維持現行「拒絕後重跑」流程，行為不變。
- 不寫新設定開關：使用者沒設 ask 規則就不會有任何詢問，等同不啟用。
- 只支援「允許一次／拒絕」，不做「允許並記住」。

## 4. 修改清單

| # | 檔案 | 內容 |
|---|---|---|
| 1 | `internal/ws/`（新檔，如 `permbroker.go`） | 全域 registry：`RegisterRunPerm(sessionID, askFn) (token, unregister)`；`askFn(ctx, toolName, input, toolUseID) (allow bool, msg string)`。token 用 `crypto/rand`。 |
| 2 | `internal/ws/handler.go` | 把 kiroacp 的 `RequestPermission` closure 本體抽成共用函式（`pendingPermEntry`、status、`permission_request` 廣播、TG 通知、等待與收尾），kiroacp 與 Claude 共用。Claude 分支在 `runAgent` 內註冊 token，並以 `defer unregister()` 收尾。廣播的 `tools` 用 `[{tool_name, tool_input}]`，與現有 Claude denial 同形，前端可顯示完整指令。 |
| 3 | `internal/ws/taskmanager.go` | 同一 session 的詢問序列化（加 per-session mutex 或佇列）：Claude 可能並行呼叫多個 Bash，而 `pendingPerms` 每個 session 只有一格。 |
| 3b | `internal/ws/handler.go`、`internal/claude/runner.go` | **過濾已回答過的 denial**：實測 prompt-tool 回 `deny` 時，`result.permission_denials` 仍會帶出該筆（§2）。若不處理，使用者剛按拒絕，run 結束後 `EventPermDenied` 又把它寫進 `pending_denials` 並再跳一次「允許」，而這次重跑必被 ask 規則擋下。做法：askFn 記錄本 run 已決定的 `tool_use_id`，`EventPermDenied` 處理時剔除；全部剔除後視同無 denial。 |
| 4 | `internal/mcp/`（新檔，如 `permserver.go`） | 用現有依賴 `go-sdk/mcp` 建一個單工具 server（stateless Streamable HTTP）。工具 `approve` 的輸入 `{tool_name, input, tool_use_id}`，輸出為 JSON 文字。handler 以 URL token 查 registry，ctx 取消（claude 被中斷／結束）即視為拒絕並清掉 pending。 |
| 5 | `internal/server/server.go` | 掛 `POST /claude-perm/:token`，**不經** `authMiddleware`，自行檢查 token。不能只靠 loopback 判斷：同機的 `cloudflared` 轉進來的外部請求來源也是 127.0.0.1。因此額外拒絕帶 `CF-Connecting-IP`／`X-Forwarded-For`／`Forwarded` 標頭的請求，真正的防線是 128 bit 隨機 token（run 結束即失效）。 |
| 6 | `internal/claude/runner.go` `buildClaudeArgs` | 有 token 時加 `--mcp-config <inline JSON>` 與 `--permission-prompt-tool`。經由 `agent.ExtraArgs` 傳入（新增 key，例如 `ArgPermPromptURL`）。 |
| 7 | `internal/claude/runner.go` log | 「執行指令」那行 `slog` 會印出完整 args（也會出現在 Settings → Logs 與 `logs/server.log`）→ 需遮蔽 token。替代方案：改用環境變數（`cmd.Env` 已有自訂）搭配 `--mcp-config` 的 `${VAR}` 展開，args 就不含 token；需先驗證 `claude` 支援。 |
| 8 | 前端 `ChatView.js` / `useChatSocket.js` | `permission_request` 由 `setPermTools` 收下，按鈕送 `allow_once`／`deny_once`，後端 `permResolve` 已先於 Claude 重跑路徑處理，第 1 期不需新按鈕。但 grep 全部前端 JS 找不到 `tool_input` 的使用（`ChatView.js:490` 只送 `tool_name`），需確認授權面板是否真的顯示完整指令——詢問對象是 `rm -rf` 這類危險指令時，使用者必須看得到要刪什麼才能決定；若沒有，第 1 期要補顯示 `tool_input.command`。 |
| 9 | 設定（使用者手動，非 repo） | `~/.claude/settings.json`：`Bash(rm -rf *)`、`Bash(curl *)` 由 `deny` 移到 `ask`。需使用者確認後才改。 |
| 10 | 文件 | README 的 Permissions 一行補充；`docs/spec/` 補流程；CHANGELOG 於發版時寫入。 |

## 5. 風險與待驗證

1. **HTTP transport 長時間等待**：§2 只測了 stdio。使用者可能數分鐘後才按允許，須實測 Streamable HTTP 下 ≥ 5 分鐘不會被 MCP client 逾時切斷；若會，改為 stdio 子程序（本 binary 加子命令，轉呼叫 loopback API）。
2. **首次連線競態**：§2 有一次首測未觸發詢問。實作後需連續重複測試（≥ 20 次）確認 `claude` 會等 MCP 連上才跑第一輪，否則 ask 規則可能在未連線時被當成拒絕。
3. **中斷／逾時**：使用者按中斷時 ctx 取消須立即解除阻塞並清除 `pending`、把 status 改回正確值；重啟後 `awaiting_confirm` 且無 `pending_denials` 會被重設為 idle（既有行為），需確認不殘留。
4. **多人同時回應**：`ws/handler.go` 對唯讀訪客（viewer）在進入 switch 前就丟棄所有訊息（`readOnly`，L1149–1150），所以 viewer 無法送 `allow_once`；editor 訪客可以，與 README 的「editor 等同本人」一致，不另處理。
5. **規則比對不是防護**：`Bash(rm -rf *)` 是字串前綴比對，`rm -fr`、`rm -r -f` 不會命中 ask，直接執行。要涵蓋需多列規則或改用 PreToolUse hook；本計劃不處理。
6. **ask 規則是全域的**：在終端機直接用 `claude` 時會變成互動詢問，預期如此。
7. **既有行為：「允許此操作」實際放行整個工具**：`ChatView.js:490` 送的是 `tool_name`（如 `Bash`），後端以 `--allowedTools Bash` 重跑，代表 default 模式下按一次允許，重跑期間所有 Bash 指令都放行（實測 G 情境）。不在本計劃範圍，但第 0 期第 2 層的判斷（retry 仍被拒 = 規則所致）正是建立在這個行為上；若日後改成精確到指令，該判斷需同步調整。
8. **`non_execution_kind` 無文件保證**：僅在 2.1.286 觀察到。升級 `claude` 後可能改名或消失 → 第 0 期第 1 層需在欄位缺漏時靜默退回第 2 層，不得報錯；並在測試中以 fixture 固定格式。
9. **prompt-tool 無法處理的拒絕**：若 MCP 端點連不上（極少見，與 miniapp 同進程），`claude` 可能等到 `MCP_TIMEOUT`（預設 30 秒）才繼續。需實測啟動失敗時 run 的行為，確認不會卡死 session。

## 6. 驗證（完成標準）

- 單元：registry（token 註冊／失效／並行）、HTTP handler（錯 token 401、非 loopback 403、ctx 取消解除阻塞）、permission 序列化。沿用 `internal/ws/perm_test.go` 風格。
- 整合：臨時 `CLAUDE_CONFIG_DIR` 設 ask 規則，真實 `claude -p` + 真實 handler：
  - 允許 → 目錄被刪、session 回到 running／idle、無 `pending_denials`。
  - 拒絕 → 目錄仍在、agent 收到拒絕原因。
  - 未命中規則（`rm -r`）→ 不產生詢問。
  - 中斷 → 無殘留 pending。
  - 拒絕 → 使用者按拒絕後**不再**跳第二次允許（§4 3b）。
- 手動：桌面版與瀏覽器各按一次允許；TG 通知有送出。
- `go test ./...` 通過。

## 7. 排除範圍

- 不改 `default` 模式既有流程。
- 不做「允許並記住」、不做規則編輯 UI。
- 不改其他 agent（Cursor／Codex／Kiro）。
