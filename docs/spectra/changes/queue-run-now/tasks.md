## 1. 先驗證風險

- [x] 1.1 手動實測「Claude 在 tool_use 之後、tool_result 之前被 KillTree，再 `--resume` 是否成功」：開一個 claude session，讓它執行長時間 Bash，執行中終止進程，再以同一 session id 送新 prompt。可觀察的結論是「resume 成功／失敗＋錯誤訊息」，結果寫進 `docs/spectra/changes/queue-run-now/design.md` 的 Risks 區段；若常態失敗，停下並回報，本變更改採「僅無工具執行中才允許」另案評估。

## 2. 打斷與等待機制（可打斷的 agent 類型採白名單）

- [x] 2.1 `internal/ws/taskmanager.go`：`taskEntry` 新增 `done` 通道，並提供打斷後等待舊回合完整收尾，以 done 通道通知的函式，行為是「回傳 done 通道給呼叫者、再取消任務並從登記中移除」，且不改 `taskStart` 簽名。驗證：新增單元測試，登記一個任務、呼叫該函式後，關閉 done 前等待會阻塞、關閉後解除；任務已不在時回傳 false。
- [x] 2.2 [after: 2.1] `internal/ws/taskmanager.go`：新增「被打斷的任務以 interjected 標記收尾」所需的套件層級標記（以 `msgID` 為 key，`LoadAndDelete` 語意）。驗證：單元測試確認標記一次寫入、只能被讀取消耗一次。
- [x] 2.3 [after: 2.2] `internal/ws/handler.go` 的 `runAgent`：goroutine 在本體結束（含取消路徑的 `FinalizeMessage`、狀態寫回 idle 與廣播）後、佇列決策 defer 之前關閉 done；被標記為 interjected 時，佇列決策 defer 不 `pauseQueue()` 且略過 `OutcomeCancelled` 通知，實作 Run-now does not pause the queue。一般「停止」仍然暫停佇列並發通知。驗證：`internal/ws/queue_test.go` 旁的測試——以 `fakeq`（`block` 開頭）按「停止」後佇列為暫停（回歸）；被標記打斷後佇列未暫停、無取消通知。
- [x] 2.4 `internal/ws/dispatch.go`：新增可打斷的 agent 類型白名單判斷（僅 `claude`、`codex`、`cursor`）。驗證：表格式單元測試，`claude`/`codex`/`cursor` 為真，`kiro`、`kiroacp`、`antigravity`、`gemini`、`telegram` 為假。

## 3. 後端 RunNow 流程

- [x] 3.1 [after: 2.3, 2.4] `internal/ws/dispatch.go`：在 sessionDispatcher 新增 RunNow，全程持有 dispatchLock，實作 Run a queued message immediately：指定項目存在時打斷進行中任務、等待 done（上限 15 秒）、再用 `start(clientMsg, &q)` 啟動該項目，其餘項目順序不變。驗證：fake runner 測試——舊 `Run` 先於新 `Run` 返回、佇列未暫停、訊息順序為 user A → assistant（partial）→ user B、另一筆排隊項目仍在原位。
- [x] 3.2 [after: 3.1] `internal/ws/dispatch.go`：實作 Old turn finishes before the new turn starts 的逾時路徑與 Run-now when no task is active。逾時時不啟動新進程、回報錯誤、項目留在佇列；無進行中任務（含剛好跑完、lingering 狀態）時直接啟動該項目且只執行一次。驗證：測試（逾時以注入較短上限模擬）、「剛好跑完」競爭測試不互鎖且只執行一次、lingering 案例會終止殘留進程並啟動新回合。
- [x] 3.3 [after: 3.1] `internal/ws/dispatch.go`：實作 Unsupported situations are rejected without side effects：非白名單 agent、有待處理授權或問題、Shell 任務進行中或待確認、項目 `id` 不存在、維護中，皆回傳 `error` 且不終止任務、不改變佇列。驗證：各情境一個測試案例，斷言進行中的 `fakeq` 任務仍在跑且佇列不變。
- [x] 3.4 [after: 3.1] `internal/ws/dispatch.go`：新 prompt 前綴說明，讓模型知道上一輪被打斷——RunNow 啟動時在傳給 runner 的 prompt 開頭加固定一行，實作 Interrupted turn is disclosed to the agent；聊天紀錄中的 user 訊息內容維持原文。驗證：測試斷言 runner 收到的 `opts.Prompt` 以前綴開頭，且資料庫中 user 訊息內容等於原始排隊內容。
- [x] 3.5 [after: 3.2, 3.3] `internal/ws/handler.go`：新增入口採用獨立的 WebSocket 訊息 `queue_run_now`（欄位 `id`），與 `queue_remove`、`queue_resume` 並列，呼叫 RunNow。驗證：經 WebSocket 送出 `queue_run_now` 的整合測試（沿用 `queue_test.go` 既有的連線測試輔助），收到 `queue_update` 與 `user_message`；訪客連線（`share_test.go` 既有慣例）行為與一般連線一致，不另開權限。

## 4. 前端

- [x] 4.1 [after: 3.5] `internal/static/js/chat/ChatView.js` 的 `QueuedMessages`：前端：佇列項目加獨立按鈕「立刻執行」，僅在有進行中的白名單 agent 任務時顯示，點擊送出 `{ type: 'queue_run_now', id }`，並說明會中斷目前執行中的動作；送出鈕仍然只排隊，實作 Queue view offers an explicit run-now action。驗證：手動——claude session 執行中排入兩則訊息，點第二則的「立刻執行」，確認目前回合被中斷、第二則先執行、第一則仍在佇列；並確認一般送出仍只排隊。

## 5. 收尾

- [x] 5.1 [after: 4.1] 執行 `go test ./internal/ws/... ./internal/db/...` 並手動走一遍端到端（Windows 本機）：對真實 claude 回合按「立刻執行」，確認進程樹被終止、新回合以 `--resume` 成功、`~/.claude` 無殘留互相覆寫的 transcript 錯誤。驗證：測試全數通過，手動結果記錄於變更說明。
