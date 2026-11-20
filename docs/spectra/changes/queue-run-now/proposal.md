## Why

Session 忙碌時送出的訊息只會排在佇列尾端，等當前回合跑完才執行；使用者發現 agent 走錯方向時，只能按「停止」（會暫停整個佇列、丟掉後續排隊內容）再重新輸入。各家 TUI（Codex、Kiro、Cursor）都提供「先排好訊息、再一鍵打斷並改跑這則」的動作，本專案缺少，且因「每則訊息 spawn 一個子進程、`-p`、不用 PTY」的架構決策，無法做 steer（回合中途注入），只能走「打斷後以 `--resume` 送新訊息」。

## What Changes

- 佇列中的每一則訊息新增「立刻執行」動作：在 session 有進行中的 agent 回合時，打斷該回合，並把被指定的那一則當成新 prompt 以 `--resume` 送出。
- 新增 WebSocket 訊息 `queue_run_now`（帶佇列項目 `id`），與現有 `queue_remove`、`queue_resume` 並列；入口訊息 `input` 不變，預設行為仍是排隊。
- 「立刻執行」與「停止」不同：不暫停佇列、不發「已取消」的 Telegram 通知，被指定那則之外的佇列項目保持原順序，新回合成功後照常接著執行。
- 後端必須等舊子進程確實退出後才啟動新進程，避免兩個 `claude --resume` 同時寫同一份 transcript。
- 被打斷的回合其 partial 輸出已在 DB 但不在 agent 的 transcript，新 prompt 前綴一行說明（上一輪回覆被使用者打斷），讓模型知道發生過什麼。
- 範圍限定：僅白名單 spawn 型 runner（claude、codex、cursor）；已停用的 kiro、等授權、等使用者回答問題、直連 Shell 任務進行中、ACP 型（kiroacp、antigravity）一律拒絕並回傳明確錯誤。
- 前端佇列項目加「立刻執行」按鈕，與一般送出明確分開（不與送出鈕共用、不依狀態猜意圖），預設維持排隊。

## Capabilities

### New Capabilities

- `queue-run-now`: 在 session 忙碌時，對已排隊的訊息執行「立刻執行」——打斷當前回合、等舊進程退出、以該則訊息 `--resume` 續跑，並保持佇列不暫停、順序不變；含不支援情境（等授權／等問題／Shell 任務／ACP 型 runner）的拒絕規則。

### Modified Capabilities

（無。既有的佇列恢復與失敗暫停規格，需求都不變。）

## Impact

- 後端：`internal/ws/taskmanager.go`（任務登記加完成通知與「被插嘴」標記）、`internal/ws/dispatch.go`（新增 run-now 路徑，須在 `dispatchLock` 內）、`internal/ws/handler.go`（新 WS 訊息 `queue_run_now`；`runAgent` 收尾的暫停佇列與取消通知分支需略過被插嘴的任務）。
- 資料庫：`internal/db/queue.go` 預期不需改 schema；啟動指定項目沿用 `PromoteQueuedMessage`（以項目 `id` 而非佇列頭），需確認不依賴 FIFO 順序。
- 前端：`internal/static/js/chat/ChatView.js` 佇列列表加按鈕與 WS 送出。
- 測試：`internal/ws/queue_test.go` 旁新增 fake runner 測試（阻塞在 `ctx.Done()`），斷言舊 `Run` 先於新 `Run` 返回、佇列未暫停、訊息順序為 user A → assistant(partial) → user B。
- 未驗證風險：Claude 在 tool_use 之後、tool_result 之前被 KillTree 時，`--resume` 是否因 transcript 內有未配對的 tool_use 而失敗，實作前須先手動實測；結果會決定是否需要在 resume 失敗時退回既有的交接（handoff）流程。
