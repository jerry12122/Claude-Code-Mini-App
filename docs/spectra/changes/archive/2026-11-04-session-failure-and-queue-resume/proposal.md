## Why

EVE（外部秘書 agent）想在 provider 額度用盡導致 session 停擺時主動詢問 Jerry 並排程恢復，但 miniapp 目前有兩個缺口擋住它：

1. **失敗沒有持久化。** AI run 失敗時，錯誤文字只廣播給 UI、發 Telegram 通知、存進 MCP registry 的記憶體快取（`internal/ws/handler.go` 失敗路徑；只有 shell 錯誤會寫進訊息）。記憶體快取只有被 MCP 驅動過的 session 才有，重啟就消失，所以外部 agent 無法可靠得知「哪個 session 因什麼錯誤停擺」。
2. **MCP 無法解除佇列暫停。** run 失敗且佇列有待跑項目時 miniapp 會暫停佇列，解除只有 WS 訊息 `queue_resume`（UI 按鈕）。暫停期間 MCP `send_message` 送出的訊息一律排在佇列尾端而不執行，2026-11-02 16:46 的恢復排程就是這樣卡進 queue。

## What Changes

- run 失敗時把錯誤全文（有長度上限）與失敗時間寫進該 session 的資料庫欄位；下一輪成功執行時清除。
- MCP `list_sessions` 每個 session 新增 `last_error`、`last_error_at`、`queue_paused`、`queue_length`，並新增選用參數 `failed_after`（只回傳失敗時間晚於該時間的 session）。
- 新增 MCP 工具 `resume_queue(session_id)`，行為等同 UI 的 `queue_resume`。

## Non-Goals (optional)

- 不分類錯誤（額度、月額度、其他一律不判斷），只存原始文字；判斷由呼叫端負責。
- 不改既有 Telegram 失敗通知。
- 不改 `get_status` 的記憶體快取行為，也不新增獨立的「失敗清單」工具。
- 不新增排除已知失敗的參數；呼叫端用 `failed_after` 自行界定範圍。

## Capabilities

### New Capabilities

- `session-failure-record`: 持久化每個 session 最近一次 run 失敗的錯誤文字與時間，成功後清除，並透過 `list_sessions` 暴露與以 `failed_after` 過濾。
- `mcp-resume-queue`: 提供 MCP 工具解除 session 的佇列暫停並續跑。

### Modified Capabilities

<!-- 無既有 spec 的需求變更 -->

## Impact

- **Affected Code**:
  - `internal/db/db.go`、`internal/db/session.go`: sessions 表新增欄位與讀寫方法；`Session` 結構新增欄位。
  - `internal/ws/handler.go`: run 失敗時寫入、成功時清除。
  - `internal/mcp/tools.go`: `list_sessions` 輸出欄位與 `failed_after` 參數、新增 `resume_queue` 工具。
  - `internal/mcp/registry.go`: 新增只送 WS 訊息、不重置本輪狀態的 `ResumeQueue`。
  - `docs/features.md`、`docs/features.zh-TW.md`: MCP 工具說明。
- **相依方**: EVE 的 `quota-stall-resume` 變更（`C:\Users\user\Documents\EVE`）依賴本變更。
- `Session` 新欄位同時出現在 REST 的 session 回應中，屬向後相容的新增欄位。
