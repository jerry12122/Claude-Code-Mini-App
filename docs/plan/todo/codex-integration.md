# 功能計畫書：Codex CLI 整合

> 狀態：草稿  
> 建立日期：2026-04-19  
> 關聯規格：`docs/codex-cli.md`、`docs/plan/todo/agent-integration.md`（Phase E.1）

---

## 1. 功能概述

將 OpenAI Codex CLI（`codex exec --json`）整合為第二個 agent provider，實作 `agent.Runner` 介面，讓使用者在建立 Session 時可選擇使用 Codex 而非 Claude。

Codex 的事件模型與 Claude 有本質差異：Claude 用 `stream_event` + `result`，Codex 用 `thread.*` / `turn.*` / `item.*` 三層結構。本計畫的核心工作是將 Codex 的 JSONL 事件流正確映射為 `agent.Event`。

**前置條件**：`docs/plan/todo/agent-integration.md` Phase A～D 已完成（`agent.Runner` 介面已存在）。

---

## 2. Codex 啟動契約

```bash
# 首次執行（新 thread）
codex exec --json --skip-git-repo-check --cd <work_dir> [--model <model>] \
  [--sandbox <policy>] [--ask-for-approval <policy>] "<prompt>"

# Resume 已有 thread
codex exec resume <thread_id> --json --skip-git-repo-check --cd <work_dir> "<prompt>"
```

> `--skip-git-repo-check` **預設帶入**。Codex 原本要求 `work_dir` 必須是 Git repo，不符合時直接拒絕執行。本系統的 Session 工作目錄不強制為 Git repo，因此一律略過此檢查。

### 關鍵差異對照

| 面向 | Claude | Codex |
|:---|:---|:---|
| 非互動命令 | `claude -p` | `codex exec` |
| JSON 串流旗標 | `--output-format stream-json` | `--json` |
| Session ID 名稱 | `session_id` | `thread_id` |
| Resume 語法 | `--resume <id>` | `codex exec resume <id>` |
| Session ID 來源 | `system/init` 事件 | `thread.started` 事件 |
| 中間事件模型 | `assistant` + `tool_call` | `item.*` typed items |
| Permission 系統 | `permission_denials` | `--ask-for-approval` 旗標 |

---

## 3. 事件流映射

### 3.1 Codex 原始事件

```
thread.started        → { thread_id }
turn.started          → { }
item.started          → { item: { id, type, status, ... } }
item.completed        → { item: { id, type, status, ... } }
turn.completed        → { usage: { input_tokens, cached_input_tokens, output_tokens } }
turn.failed           → { }
error                 → { }
```

### 3.2 映射至 `agent.Event`

| Codex 事件 | → `agent.Event` | 說明 |
|:---|:---|:---|
| `thread.started` | `EventSessionInit { SessionID: thread_id }` | 等同 Claude `system/init` |
| `turn.started` | *(無對應，忽略或 log)* | |
| `item.started` where `type = "command_execution"` | `EventActivity { Label: "執行指令中…" }` | 前端顯示活動提示 |
| `item.started` where `type = "web_search"` | `EventActivity { Label: "搜尋中…" }` | |
| `item.started` where `type = "file_change"` | `EventActivity { Label: "修改檔案中…" }` | |
| `item.started` where `type = "mcp_tool_call"` | `EventActivity { Label: "呼叫工具中…" }` | |
| `item.started` where `type = "reasoning"` | *(忽略，不推前端)* | |
| `item.started` where `type = "plan_update"` | *(忽略，不推前端)* | |
| `item.completed` where `type = "agent_message"` | `EventDelta { Text: item.content }` | 完整訊息一次送出（非逐字增量） |
| `turn.completed` | `EventDone { Usage: ... }` | 含 token 統計 |
| `turn.failed` | `EventError { Err: ... }` | |
| `error` | `EventError { Err: ... }` | |
| process exit code != 0 且無 `turn.completed` | `EventError` (補強機制) | 含斷流偵測 |

> **注意**：Codex 沒有 `permission_denials`，`EventPermDenied` 永不觸發。  
> Approval 機制透過啟動旗標控制（`--ask-for-approval`），不走 WS 授權流程。

---

## 4. Session 管理

### 4.1 thread_id 保存

- 收到 `thread.started` 時，呼叫 `db.UpdateAgentSessionID(sessionID, threadID)`
- Resume 時使用 `codex exec resume <thread_id>`，而非 `--resume <id>` 旗標
- `--last` / `--all` 快捷僅供 debug，程式整合一律使用明確 `thread_id`

### 4.2 Ephemeral 模式

- 若未來支援 `ephemeral: true`，不保存 `thread_id`，每次視為全新 thread
- DB 的 `agent_session_id` 欄位留空

---

## 5. Safety Policy 映射

Codex 的 safety 由兩個獨立旗標控制：

| `ExtraArgs` key | Codex 旗標 | 可選值 |
|:---|:---|:---|
| `sandbox_mode` | `--sandbox` | `read-only` / `workspace-write` / `danger-full-access` |
| `approval_mode` | `--ask-for-approval` | `untrusted` / `on-request` / `never` |

WS Handler 對 Codex session 收到 `allow_once` / `set_mode` 訊息時：
- 回傳 `{ type: "error", message: "Codex 不支援即時 Permission 授權" }`
- 不重新 spawn 進程

---

## 6. 認證

```bash
# 自動化/CI 環境（建議）
CODEX_API_KEY=<key> codex exec --json ...

# 互動式登入（本機開發）
codex login
```

`CodexRunner` 啟動前應檢查環境變數 `CODEX_API_KEY` 是否存在；若不存在，以 `EventError` 提早失敗並附上說明文字。

---

## 7. 架構設計

### 7.1 新增套件 `internal/codex/`

```
internal/codex/
  runner.go      — CodexRunner struct，實作 agent.Runner
  events.go      — JSONL 解析與 agent.Event 轉換
  runner_test.go
```

### 7.2 `runner.go` 核心結構

```go
type CodexRunner struct{}

func (r *CodexRunner) Name() string { return "codex" }

func (r *CodexRunner) Run(ctx context.Context, opts agent.RunOptions, cb agent.EventCallback) error {
    args := buildArgs(opts)
    cmd := exec.CommandContext(ctx, "codex", args...)
    cmd.Dir = opts.WorkDir
    cmd.Env = append(os.Environ(), "CODEX_API_KEY="+getAPIKey())

    stdout, _ := cmd.StdoutPipe()
    if err := cmd.Start(); err != nil {
        return err
    }

    scanner := bufio.NewScanner(stdout)
    for scanner.Scan() {
        parseEvent(scanner.Text(), cb)
    }

    if err := cmd.Wait(); err != nil {
        cb(agent.Event{Type: agent.EventError, Err: err})
    }
    return nil
}
```

### 7.3 `buildArgs` 邏輯

```go
func buildArgs(opts agent.RunOptions) []string {
    if opts.SessionID != "" {
        // Resume 模式：子命令結構不同
        return []string{"exec", "resume", opts.SessionID, "--json",
            "--skip-git-repo-check", "--cd", opts.WorkDir, opts.Prompt}
    }
    // 首次執行
    args := []string{"exec", "--json", "--skip-git-repo-check", "--cd", opts.WorkDir}
    if m := opts.ExtraArgs["sandbox_mode"]; m != "" {
        args = append(args, "--sandbox", m)
    }
    if m := opts.ExtraArgs["approval_mode"]; m != "" {
        args = append(args, "--ask-for-approval", m)
    }
    if m := opts.ExtraArgs["model"]; m != "" {
        args = append(args, "--model", m)
    }
    return append(args, opts.Prompt)
}
```

---

## 8. `agent.Event` 擴充說明

### 8.1 狀態機差異

Codex 與 Claude 的狀態機**根本不同**：

| | Claude | Codex |
|:---|:---|:---|
| 有逐字 delta | ✅ `content_block_delta` 逐字推送 | ❌ 無，文字在 `item.completed` 一次送出 |
| STREAMING 狀態 | 有意義（邊收邊渲染） | **無意義**（文字瞬間出現） |
| AWAITING_CONFIRM | 有（permission_denials） | **不存在** |
| 狀態機 | IDLE → THINKING → STREAMING → IDLE / AWAITING_CONFIRM | **IDLE → THINKING → IDLE** |

Codex 的 THINKING 期間以 `item.started` 的 type 動態更新活動提示文字（「執行指令中…」、「搜尋中…」），收到 `item.completed (agent_message)` 後一次渲染完整文字並切回 IDLE。

### 8.2 `agent.Event` 新增 `EventActivity`

需在 `internal/agent/runner.go` 新增一個 event type：

```go
EventActivity EventType = "activity"
// Event.Text 存放活動描述文字，供前端在 THINKING 狀態顯示
```

- 收到 `item.started` 非 agent_message 類型 → `EventActivity { Text: "執行指令中…" }`
- 收到 `item.completed (agent_message)` → `EventDelta { Text: content }`
- 收到 `turn.completed` → `EventDone`
- 不影響 Claude runner，Claude 不送出 `EventActivity`

---

## 9. DB / Session 變更

Phase A～D（agent-integration.md）若已完成，本計畫**不需新增任何 DB 欄位**。

僅需確認：
- `sessions.agent_type` 接受 `"codex"` 值（Factory 已處理）
- `sessions.agent_session_id` 欄位用於存放 `thread_id`

---

## 10. 前端調整

### 10.1 狀態機差異

| | Claude | Codex |
|:---|:---|:---|
| Permission Mode 切換按鈕 | 顯示 | **隱藏** |
| `AWAITING_CONFIRM` 狀態 | 有 | **不存在** |
| `STREAMING` 狀態 | 有（逐字渲染） | **不存在**（文字瞬間出現） |
| 狀態機 | IDLE → THINKING → STREAMING → IDLE / AWAITING_CONFIRM | **IDLE → THINKING → IDLE** |
| Session 標籤 | `Claude` | `Codex` |

### 10.2 THINKING 狀態的活動提示

Codex 在 THINKING 期間收到 `EventActivity` 時，前端動態更新提示文字：

```
⏳ 思考中…          ← 預設
⏳ 執行指令中…      ← item.started command_execution
⏳ 搜尋中…          ← item.started web_search
⏳ 修改檔案中…      ← item.started file_change
⏳ 呼叫工具中…      ← item.started mcp_tool_call
```

收到 `EventDelta` 後一次渲染完整文字，切回 IDLE。Claude 的 THINKING → STREAMING 逐字動畫在 Codex 中不需實作。

---

## 11. 實作任務

### Phase E1 — CodexRunner 後端

- [ ] `internal/agent/runner.go` 新增 `EventActivity EventType`，`Event.Text` 存放活動描述文字
- [ ] 建立 `internal/codex/events.go`：Codex JSONL 結構體定義，實作 `parseEvent` 轉換邏輯（含 item.started → EventActivity、斷流偵測）
- [ ] 建立 `internal/codex/runner.go`：實作 `CodexRunner.Run`，`buildArgs` 預設帶 `--skip-git-repo-check`，stdout 解析、process wait、`CODEX_API_KEY` 檢查
- [ ] `internal/agent/factory.go` 新增 `"codex"` 分支，回傳 `&codex.CodexRunner{}`
- [ ] `ws/handler.go` 處理 `EventActivity`，推送活動提示至前端；對 Codex session 的 `allow_once`/`set_mode` 回傳不支援提示

### Phase E2 — 測試

- [ ] `internal/codex/runner_test.go`：mock stdout 驗證事件解析
  - `thread.started` → `EventSessionInit`
  - `item.started (command_execution)` → `EventActivity`
  - `item.completed (agent_message)` → `EventDelta`
  - `turn.completed` → `EventDone`
  - `turn.failed` → `EventError`
  - process exit code != 0 且無 `turn.completed` → `EventError`（斷流）
- [ ] 手動整合測試：建立 Codex session、送出 prompt、驗證活動提示與完整文字出現、resume

### Phase E3 — 設定文件

- [ ] 補充 Codex 使用前置條件（安裝 `codex` CLI、設定 `CODEX_API_KEY`）

---

## 12. 不在本次範圍內

- Codex `app-server` transport（WebSocket/stdio 持久連線）：優先穩定 `codex exec --json`
- Structured output（`--output-schema`）支援：可於本計畫完成後作為獨立 feature
- Codex 作為 MCP tool provider（`codex mcp-server`）：另立計畫

---

## 13. 成功判定

| 條件 | 驗證方式 |
|:---|:---|
| Codex session 可建立並送出 prompt | 手動測試 |
| `thread_id` 正確寫入 DB 並在下次送訊時 resume | 手動測試 |
| THINKING 期間顯示正確活動提示文字 | 手動測試 |
| 文字完整出現（非逐字，一次渲染） | 手動測試 |
| process 失敗時前端顯示錯誤訊息 | 單元測試 + 手動 |
| Claude session 行為不受影響（regression） | 現有測試 |
