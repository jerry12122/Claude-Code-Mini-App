# Multi-Agent 整合計劃 (已完成大部分)

> 目標：將現有 Claude 專屬架構抽象化，支援 Codex、Gemini CLI 等多種 AI 工具。
> 建立 Session 時可指定工具，同一 Session 全程使用同一工具。

---

## 總覽

```
Phase A — 定義共用介面與事件模型 (Done)
Phase B — DB Migration + Session 加入 agent_type 欄位 (Done)
Phase C — Claude Runner 重構為介面實作 (Done)
Phase D — WS Handler 改用 Runner 介面注入 (Done)
Phase E — 新增其他工具的 Runner 實作 (Gemini/Cursor Done, Codex Todo)
Phase F — 前端 UI 支援建立 Session 時選擇工具 (Done)
```

---

## 狀態摘要

- [x] **Phase A — 定義共用介面與事件模型**
- [x] **Phase B — DB Migration**
- [x] **Phase C — Claude Runner 重構**
- [x] **Phase D — WS Handler 重構**
- [x] **Phase E.1 — Cursor Runner 整合**
- [x] **Phase E.2 — Gemini Runner 整合**
- [ ] **Phase E.3 — Codex Runner 整合** (待執行，詳見 `docs/plan/todo/codex-integration.md`)
- [x] **Phase F — 前端 UI 支援**

---

## Phase A — 定義共用介面與事件模型

### A.1 建立 `internal/agent` 套件

- [ ] 新增 `internal/agent/runner.go`，定義 `Runner` 介面：

```go
type RunOptions struct {
    Prompt     string
    SessionID  string
    WorkDir    string
    ExtraArgs  map[string]string // 工具專屬參數（permission_mode 等）
}

type EventType string

const (
    EventDelta           EventType = "delta"
    EventDone            EventType = "done"
    EventError           EventType = "error"
    EventPermDenied      EventType = "permission_denied"
    EventSessionInit     EventType = "session_init"
)

type Event struct {
    Type      EventType
    Text      string              // delta 文字
    SessionID string              // session_init / done 時帶入
    Denials   []PermissionDenial  // 僅 Claude 有
    Err       error               // error 時帶入
}

type PermissionDenial struct {
    ToolName  string
    ToolUseID string
    ToolInput json.RawMessage
}

type EventCallback func(e Event)

type Runner interface {
    Run(ctx context.Context, opts RunOptions, cb EventCallback) error
    Name() string // "claude" | "codex" | "gemini"
}
```

- [ ] 新增 `internal/agent/factory.go`，實作 `NewRunner(agentType string) Runner`

### A.2 確認介面覆蓋現有功能

- [ ] 確認 `Event` 結構能對應現有 WS `serverMsg` 的所有 type
- [ ] 確認 `RunOptions.ExtraArgs` 足夠傳遞 `permission_mode`、`allowed_tools`

---

## Phase B — DB Migration

### B.1 Schema 變更

- [ ] 在 `internal/db/db.go` 的 schema 加入欄位：

```sql
ALTER TABLE sessions ADD COLUMN agent_type TEXT NOT NULL DEFAULT 'claude';
```

- [ ] 將 `sessions.claude_id` 改名為 `sessions.agent_session_id`
  - 舊欄位保留相容，或直接 migration（視資料是否需要保留）

### B.2 Session struct 更新（`internal/db/session.go`）

- [ ] `ClaudeID` → `AgentSessionID string`
- [ ] 新增 `AgentType string`（`"claude"` | `"codex"` | `"gemini"`）
- [ ] 更新 `scanSession`、`CreateSession`、`GetSession`、`ListSessions` 對應欄位
- [ ] 更新 `UpdateClaudeID` → `UpdateAgentSessionID`

### B.3 API 層更新（`internal/api/session.go`）

- [ ] `Create` handler 接收 `agent_type` 欄位
- [ ] `agent_type` 為空時預設 `"claude"`
- [ ] 回傳 Session JSON 包含 `agent_type`

---

## Phase C — Claude Runner 重構

### C.1 重構 `internal/claude/runner.go`

- [ ] 將 `Run` 函式封裝進 `ClaudeRunner` struct，實作 `agent.Runner` 介面
- [ ] `RunOptions` 從 `agent.RunOptions` 取出 `ExtraArgs["permission_mode"]`、`ExtraArgs["allowed_tools"]`
- [ ] `EventCallback` 改用 `agent.EventCallback`，在內部將 `StreamEvent` 轉換為 `agent.Event`：

```
stream_event content_block_delta  →  agent.Event{Type: EventDelta, Text: ...}
result (session_id)               →  agent.Event{Type: EventDone, SessionID: ...}
result (permission_denials)       →  agent.Event{Type: EventPermDenied, Denials: ...}
system init (session_id)          →  agent.Event{Type: EventSessionInit, SessionID: ...}
```

- [ ] `Name()` 回傳 `"claude"`
- [ ] `internal/claude/events.go` 保留（仍用於內部解析），對外不暴露

### C.2 更新 Factory

- [ ] `internal/agent/factory.go` 中 `NewRunner("claude")` 回傳 `&claude.ClaudeRunner{}`

---

## Phase D — WS Handler 改用介面

### D.1 更新 `internal/ws/handler.go`

- [ ] `NewHandler` 參數加入 `runners map[string]agent.Runner`（或透過 factory）
- [ ] 將 `runClaude` 函式重命名為 `runAgent`
- [ ] 將 `claude.Run(ctx, opts, cb)` 替換為：

```go
runner := agent.NewRunner(sess.AgentType)
runner.Run(ctx, agentOpts, func(e agent.Event) {
    switch e.Type {
    case agent.EventDelta:       // ...
    case agent.EventDone:        // ...
    case agent.EventPermDenied:  // ...
    }
})
```

- [ ] `allow_once` / `set_mode` 邏輯：檢查 `sess.AgentType == "claude"` 才處理 permission flow
  - 其他工具收到 `allow_once` 可直接 no-op 或回傳提示

### D.2 更新 `cmd/server/main.go`

- [ ] 初始化時不再直接依賴 `claude` 套件
- [ ] 改用 `agent.NewRunner` factory

---

## Phase E — 新增其他工具 Runner（未來）

> 此階段實作新工具，每個工具獨立一個套件，實作 `agent.Runner` 介面。

### E.1 Codex Runner（`internal/codex/runner.go`）

- [ ] 研究 `codex` CLI 的輸出格式（stream-json 或其他）
- [ ] 實作 `CodexRunner.Run`，將輸出轉換為 `agent.Event`
- [ ] 確認 session resume 機制（有無 `--resume` 等效旗標）
- [ ] 無 permission 機制，`EventPermDenied` 永不觸發

### E.2 Gemini CLI Runner（`internal/gemini/runner.go`）

- [ ] 研究 `gemini` CLI 輸出格式
- [ ] 實作 `GeminiRunner.Run`
- [ ] 確認 session 持久化方式
- [ ] 更新 factory 支援 `"gemini"`

---

## Phase F — 前端 UI

### F.1 建立 Session 表單（`internal/static/index.html`）

- [ ] 新增「AI 工具」下拉選單：

```
[ Claude ▾ ]
  > Claude
  > Codex
  > Gemini CLI
```

- [ ] 送出 `POST /sessions` 時帶入 `agent_type`
- [ ] Session 列表顯示每個 session 使用的工具（小標籤）

### F.2 UI 邏輯條件化

- [ ] 只有 `agent_type === "claude"` 的 session 才顯示：
  - Permission Mode 切換按鈕
  - 授權確認對話框（`AWAITING_CONFIRM` 狀態）
- [ ] 其他工具只有 `IDLE` / `THINKING` / `STREAMING` 三種狀態

---

## 相依關係圖

```
Phase A（介面定義）
    ↓
Phase B（DB）   Phase C（Claude Runner）
    ↓                   ↓
         Phase D（WS Handler）
                ↓
    Phase E（其他 Runner）
                ↓
         Phase F（前端 UI）
```

Phase A 完成後，B、C 可並行進行。

---

## 注意事項

- **向下相容**：既有 session（無 `agent_type` 欄位）migrate 後預設為 `"claude"`，行為不變。
- **Permission Flow 隔離**：`AWAITING_CONFIRM` 狀態與 `allow_once`/`set_mode` 訊息僅對 `agent_type=claude` 生效。
- **Session ID 格式差異**：各工具的 session resume 機制不同，`agent_session_id` 欄位存各工具原生 ID。
- **ExtraArgs 設計**：避免在 `agent.RunOptions` 放 Claude 專屬欄位，一律用 `ExtraArgs map[string]string` 傳遞。
