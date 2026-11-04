<!--
實作任務清單：session-failure-and-queue-resume
本 repo 啟用 tdd：每個任務先寫會失敗的測試，再實作到通過。
-->

## 1. 失敗紀錄持久化

- [x] 1.1 實作 Persist the latest run failure on the session row 的資料層：sessions 表以 `tryAlter` 新增 `last_error_text`、`last_error_at`，提供寫入（截斷 4000 字元、覆寫舊值）與清除的方法，`Session` 結構帶出欄位，落實 Persist the latest run failure、Clear the failure after a successful run 與 Failure state survives restart。先在 `internal/db/session_failure_test.go` 寫失敗測試（寫入、覆寫、截斷、清除、重開資料庫後仍在），再實作；以 `go test ./internal/db/...` 驗證。
- [x] 1.2 於 `internal/ws/handler.go` 接上寫入與清除：run 失敗且非取消時寫入 `errText` 與時間，成功路徑清除，取消不寫也不清。先在 `internal/ws/failure_record_test.go` 寫失敗測試（失敗寫入、成功清除、取消保持原狀），再實作；以 `go test ./internal/ws/...` 驗證。

## 2. list_sessions 暴露與過濾

- [x] 2.1 實作 Expose failure and queue state through list_sessions，落實 Report failure and queue state in list_sessions：每個 session 輸出 `last_error`、`last_error_at`（無失敗省略）、`queue_paused`、`queue_length`。先在 `internal/mcp/list_sessions_test.go` 寫失敗測試（有失敗且佇列暫停、無失敗兩種），再實作；以 `go test ./internal/mcp/...` 驗證。
- [x] 2.2 實作 `failed_after` 參數，落實 Filter sessions by failure time：只回傳 `last_error_at` 晚於給定時間者，與 `since` 取交集，格式錯誤回傳錯誤。先在 `internal/mcp/list_sessions_test.go` 補失敗測試（單獨、與 since 併用、無失敗者排除、非法時間），再實作；以 `go test ./internal/mcp/...` 驗證。

## 3. resume_queue 工具

- [x] 3.1 於 `internal/mcp/registry.go` 實作 Resume the queue through the websocket path without resetting turn state 的 `ResumeQueue`：`ensure` 連線後只送 `queue_resume`，不呼叫 `begin()`，落實 Resume does not alter turn state。先在 `internal/mcp/resume_queue_test.go` 寫失敗測試（空佇列後 `get_status` 仍為 idle、不清 `lastErr`），再實作；以 `go test ./internal/mcp/...` 驗證。
- [x] 3.2 於 `internal/mcp/tools.go` 註冊 `resume_queue` 工具，落實 Resume queue tool 與 Respect maintenance gating：必填 `session_id`、空白或不存在回錯誤，成功回 `{ok:true}`；暫停且閒置時清除暫停並開始第一則，忙碌時只清除暫停，空或未暫停無副作用，維護中不取出佇列。先在 `internal/mcp/resume_queue_test.go` 補失敗測試涵蓋這些情境，再實作；以 `go test ./internal/mcp/... ./internal/ws/...` 驗證。

## 4. 文件與整合驗證

- [x] 4.1 更新 `docs/features.md` 與 `docs/features.zh-TW.md` 的 MCP 說明：`list_sessions` 新欄位與 `failed_after`、`resume_queue` 工具，並註明 `get_queue` 的 `queue_paused` 可用 `resume_queue` 解除。以內容檢閱確認與 spec 一致。
- [x] 4.2 端到端驗證：以 codex 額度用盡的真實失敗（或測試替身）確認 `list_sessions` 回報錯誤與暫停佇列，呼叫 `resume_queue` 後佇列依序執行；重啟 miniapp 後失敗紀錄仍在。以手動驗收紀錄於本 change 的驗證備註，並執行 `go test ./...`。

## 驗證備註（2026-11-04）

- `TestQueueResumeEndToEndAfterRestart` 使用真實 MCP HTTP、WebSocket handler 與 SQLite，只替換 Agent runner：模擬額度失敗後，`list_sessions` 回報原始錯誤、時間與兩則暫停訊息；關閉並重建 MCP／WS server 與資料庫連線後狀態不變；呼叫 `resume_queue` 後依序完成兩則訊息，成功後清除失敗紀錄。
- 暫時將失敗寫入改為清除，新增測試確實因「failure persisted and queue paused」逾時而失敗；立即還原後測試通過。
- `go test ./...` 與 `go test -tags production ./internal/...` 均通過。執行時僅對測試程序設定不含本機 Agent CLI 的 PATH，保留 Go、Git、Windows PowerShell 與 Node；原始 PATH 會讓既有 server 啟動測試呼叫真實 CLI，造成背景同步逾時與 Windows 暫存目錄被占用。
- `node scripts/theme.test.cjs`、`node scripts/mermaid-render.test.cjs` 通過；MCP 佇列工具探索檢查已保留並加入 `resume_queue`。
- 規格對應：失敗寫入／覆寫／截斷／清除／重啟由 `internal/db/session_failure_test.go` 與 `internal/ws/failure_record_test.go` 涵蓋；查詢欄位與篩選由 `internal/mcp/list_sessions_test.go` 涵蓋；工具參數與狀態保持由 `internal/mcp/resume_queue_test.go` 涵蓋；恢復順序與維護保護另沿用 `internal/ws/queue_test.go`、`internal/ws/admission_test.go`。
- 範例限制：測試保留 cloudserver、額度錯誤、指定失敗時間與兩則 FIFO 的語意；佇列 ID 由測試資料庫自動產生，未固定為範例的 244／248。未消耗真實 provider 額度，也未驗證 EVE 呼叫端。
- 檢閱未發現阻擋發佈的正確性、效率、重用或慣例問題；初始檔案已存在未提交修改，Spectra 的變更歸屬為近似判定，檢閱以工作區實際差異為限。Postman CLI 因本機 Private Cloud manifest 設定無法啟動，MCP 契約以專案 Go SDK 測試驗證。
