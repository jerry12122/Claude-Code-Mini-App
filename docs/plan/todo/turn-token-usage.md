# 設計草案：回合 Token 統計

> 狀態：**待使用者決定要不要做、做哪個方案**（尚未實作）
> 建立日期：2026-11-18
> 來源：`agy-integration.md` 與 `todo.md` 的「回合 Token 統計串接待補」。原本只有一句話，沒有範圍；本文是 supervisor 先查現況再列的選項，**不是已決定的規格**。

## 1. 現況（2026-11-18 以程式碼核對）

| 位置 | 狀態 |
|---|---|
| `internal/usage/usage.go` | 有單回合用量結構 `Info`（成本、credits、input／output／cache tokens、耗時）與 `FromClaudeResult`、`FromCursorResult`、`FromKiroStderr`。**這三個解析函式與 `Info` 沒有任何呼叫者**（`internal/usage` 的其他部分——`QuotaInfo`、`QuotaWindow` 等——被 `internal/quota` 大量使用，那是帳戶額度，不是單回合用量） |
| `internal/codex/events.go`、`internal/antigravity/events.go` | 已解析 `usage`（input／output／cached／reasoning tokens），但沒有往外傳 |
| `agent.Event`（`internal/agent/runner.go`） | 沒有任何用量欄位，所以 runner 即使解析了也送不出去 |
| 資料庫、WebSocket、前端 | 沒有對應欄位或顯示 |

結論：這不是「補 agy 一條線」，而是一個跨 provider 的新資料流：runner → 事件 → WebSocket →（可選）資料庫 → 前端。

## 2. 先回答「為什麼要」

已有的頂欄用量進度條（`quota-usage-bars`）顯示的是**帳戶額度**（5 小時／週視窗）。單回合 token 能多提供的只有：
- 看單次對話吃掉多少上下文（排查「為什麼變慢／變貴」）。
- 訂閱制下成本欄位沒有意義（只有 API 計費才有金額），所以對多數使用者價值有限。

**若沒有具體的使用情境，建議不做。** 以下方案只在你確定需要時才有意義。

## 3. 方案

| 方案 | 內容 | 代價 | 缺點 |
|---|---|---|---|
| A 只顯示、不存 | `agent.Event` 加一個小的用量欄位（done 事件帶），各 runner 填，WebSocket `done` 帶出，前端在最後一則回覆下方顯示一行小字（輸入／輸出 tokens，有金額才顯示） | S～M | 重新整理後就沒了，無法加總 |
| B 存進訊息 | A 加上 `messages` 表新增一欄 `usage_json`（需 DB 遷移），歷史訊息也看得到 | M | 要處理舊資料為空、遷移與備份 |
| C 加總統計 | B 再加 session 層級的累計與設定頁圖表 | L | 範圍膨脹，容易違反 YAGNI |

分層注意：`agent` 屬較底層，不能 import `internal/usage`；要嘛在 `agent` 定義一個最小的用量結構，要嘛把 `Info` 下移。實作前先對照 `docs/spec/backend-architecture.md`，並以 `go test ./internal/arch/` 驗證。

## 4. 建議

1. **預設不做。** 額度進度條已涵蓋「用了多少」的主要需求。
2. 若要做，**只做方案 A**，且先只接 Claude、Codex、Antigravity（有現成解析），Cursor、Kiro 之後再說。
3. B、C 等 A 用過、確實需要歷史與加總再評估。

## 5. 待你決定

- 你要這個功能的具體情境是什麼（排查上下文？看成本？）？
- 要不要做、做 A 還是其他？
