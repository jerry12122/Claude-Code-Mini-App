## Context

現況（已讀原始碼確認）：
- AI run 失敗時（`internal/ws/handler.go`），錯誤文字 `errText` 只做 `broadcast(serverMsg{Type: "error"})`、`notifyTaskAsync(... OutcomeError ...)`，以及被 MCP registry 的 readLoop 收進記憶體 `lastErr`（`internal/mcp/registry.go`）。沒有寫進資料庫；只有 shell 的錯誤會 `AppendMessageContent`。
- `registry.begin()` 在新一輪操作開始時會清掉 `lastErr`；`get_status` 對沒被 MCP 連線過的 session 回 `connected:false`、error 空。
- 失敗且佇列有待跑項目時 `pauseQueue()` 設 `queue_paused=1`（持久化在 sessions 表）。解除只有 WS `queue_resume` → `resumeQueue()`（清除暫停並 `drainIfIdle()`）。
- `list_sessions`（`internal/mcp/tools.go`）輸出 `db.Session`，目前沒有佇列或錯誤欄位。
- MCP 工具透過 registry 的 WS client 與 session 互動（`ensure` 連線後 `writeJSON`）。

## Goals / Non-Goals

**Goals**
- 外部 agent 能不依賴 WS 連線與記憶體快取，得知每個 session 最近一次失敗的原因與時間。
- 外部 agent 能解除佇列暫停。
- 改動最小：擴充既有 `list_sessions`，不新增獨立的失敗查詢工具或排除參數；恢復佇列另提供 `resume_queue`。

**Non-Goals**
- 不分類錯誤、不改通知、不改 `get_status`。

## Decisions

### Persist the last failure on the session row

在 sessions 表新增 `last_error_text`、`last_error_at` 兩欄（沿用 `tryAlter` 加欄位的慣例）。run 失敗且非使用者取消時覆寫；下一輪成功執行（`OutcomeSuccess` 路徑）時清空。錯誤全文截斷到 4000 字元。取消不清除也不寫入。

理由：每個 session 只需最近一次失敗，不需要失敗歷史表；欄位與 `queue_paused` 一樣屬於 session 狀態。
替代方案：新增 `session_failures` 歷史表（需求只要最近一次，過度設計，否決）；把錯誤寫成訊息（會污染對話歷史並影響未讀計數，否決）。

### Expose failure and queue state through list_sessions

`list_sessions` 每個 session 新增 `last_error`、`last_error_at`（ISO8601，無失敗時省略）、`queue_paused`、`queue_length`。新增選用參數 `failed_after`（ISO8601，格式沿用 `parseQueryTime`）：給定時只保留 `last_error_at` 晚於該時間的 session，與既有 `since` 並存（兩者皆給時取交集）。

理由：呼叫端本來就用 `list_sessions` 掌握全部 session，擴充欄位不需要新工具與新權限面。`failed_after` 讓呼叫端只取新失敗。
替代方案：新增 `list_failed_sessions` 工具（多一個工具，且與 `list_sessions` 重複，否決）。

### Resume the queue through the websocket path without resetting turn state

`resume_queue` 在 registry 新增 `ResumeQueue(sessionID)`：`ensure` 連線後只送 `{"type":"queue_resume"}`，不呼叫 `begin()`。這樣沿用 ws handler 既有的 `resumeQueue()`（包含維護閘門 `admitWork` 與 `drainIfIdle`），行為與 UI 完全一致。

理由：`begin()` 會清掉 `latestText`、`lastErr` 並把狀態設為 running，而佇列為空時 resume 並不會啟動 run，狀態會被錯誤地卡在 running。不重複實作暫停解除邏輯，避免與 UI 行為分歧。
替代方案：在 MCP 層直接呼叫 `db.SetQueuePaused(false)`（不會觸發取出下一則，也繞過維護閘門，否決）。

## Implementation Contract

**資料**
- sessions 表新增 `last_error_text TEXT NOT NULL DEFAULT ''`、`last_error_at TEXT NOT NULL DEFAULT ''`。

**`list_sessions`**
- 輸入新增 `failed_after`（選用）。格式錯誤回傳與 `since` 相同的錯誤。
- 每個 session 輸出新增：`last_error`（空則省略）、`last_error_at`（空則省略）、`queue_paused`（布林）、`queue_length`（整數）。
- 給定 `failed_after` 時，沒有失敗紀錄的 session 不回傳。

**`resume_queue`**
- 輸入 `session_id`（必填）。空白或不存在的 session 回傳錯誤，行為與 `get_queue` 一致。
- 成功回傳 `{"ok": true}`。佇列為空或本來就未暫停也回成功，不產生副作用。
- 佇列暫停且 session 閒置：清除暫停並開始執行佇列第一則。session 忙碌：清除暫停，等目前任務結束後依既有流程取出下一則。

**範圍外**：失敗歷史、錯誤分類、通知內容、`get_status`。

## Risks / Trade-offs

- [`Session` 新欄位出現在 REST 回應] → 皆為新增欄位，向後相容；前端不使用則忽略。
- [錯誤全文含敏感內容] → 錯誤文字本來就會發到 Telegram 並寫進 log；僅 MCP 驗證通過者可讀，沿用現有權限。
- [`resume_queue` 與 UI 同時操作] → 沿用 ws handler 的 `dispatchLock`，與 UI 的 `queue_resume` 共用同一路徑。
- [取消後保留舊的失敗紀錄] → 取消不是成功也不是新失敗，保留是刻意的；下一次成功才清除。

## Migration Plan

啟動時以 `tryAlter` 新增兩欄，舊資料預設空字串，無需遷移。回滾：欄位保留不影響舊版。

## Open Questions

- 錯誤文字截斷長度 4000 是否足夠？目前看過的額度類樣本都在數百字元內，暫定 4000，若遇到更長的再調整。
