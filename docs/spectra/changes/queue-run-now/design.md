## Context

目前 session 忙碌時（`sessionDispatcher.busy`：有進行中的 agent 任務、直連 Shell 任務，或有待處理的授權）送出的訊息都會進 `queued_messages`，等回合結束才由 `drainLocked` 依 FIFO 取出執行。使用者唯一能立刻介入的是「停止」：它呼叫 `taskInterrupt`（取消 context，runner 以 `proc.KillTree` 殺掉整個進程樹），且 `runAgent` 收尾的 `default` 分支會 `pauseQueue()`，所以後面排隊的訊息全部停住。

架構決策不可更動：每則訊息 spawn 一個子進程、全程 `-p`、以 `--resume <session_id>` 續接、不使用 PTY。因此無法做 TUI 常見的 steer（回合中途注入）。可行的是「打斷後以新訊息 resume」。

已讀過、本設計依賴的現況（皆來自程式碼）：

- `taskCancel` 會立刻把任務從 `taskManager.tasks` 刪除再 cancel context，所以 `taskIsActive` 在子進程真正退出前就已回傳 false。
- claude、codex、cursor 三個 runner 都以 `exec.CommandContext` 啟動，`cmd.Cancel` 呼叫 `proc.KillTree`，`cmd.WaitDelay` 為 5 秒。
- `PromoteQueuedMessage` 以項目 `id`（加上內容與附件比對）刪除佇列列並新增 user 訊息，不依賴 FIFO 順序，可直接用於「指定某一則」。
- `runAgent` 的 goroutine 在被取消時：`cancelled=true`，Run 返回後執行 `FinalizeMessage`、把 session 狀態設為 idle 並廣播，最後在 `default` 分支 `pauseQueue()` 並發 `OutcomeCancelled` 通知。
- 已存在的測試 fake runner `fakeq`：prompt 以 `block` 開頭時會阻塞直到 `ctx.Done()` 或 `fakeGate`，可直接用來測打斷。
- `dispatchLock(sessionID)` 序列化「判斷忙碌 → 排隊或啟動」；`drainIfIdle`（即 `Drain`）也會取同一把鎖。

## Goals / Non-Goals

**Goals:**

- 使用者可對已排隊的某一則訊息按「立刻執行」，打斷目前回合並讓該則訊息成為下一個 prompt。
- 打斷不暫停佇列、不發「已取消」通知；其餘排隊項目順序不變。
- 新進程啟動前，舊進程已確實退出，且舊回合的收尾（訊息定稿、狀態寫回）已完成，聊天紀錄順序為 user A → assistant（partial）→ user B。
- 不支援的情境一律明確拒絕，佇列內容與目前任務不受影響。

**Non-Goals:**

- 不做 steer（回合中途注入訊息）：需要常駐進程與 `--input-format stream-json`，違反「每則訊息 spawn 一個子進程」的架構決策；另有已知回報指出回合中途送進 stdin 的訊息不會寫入 Claude 的歷史檔。
- 不改變「送出」的預設行為：忙碌時送出仍是排隊，不新增「送出即打斷」。
- 不處理 ACP 型 runner（kiroacp、antigravity 的 ACP 路徑）：它們有原生 `session/cancel`，可不殺進程，留待後續變更。
- 不處理等授權、等使用者回答問題、直連 Shell 任務進行中的情境（直接拒絕）。
- 不新增 schema、不改變 `queue_remove`、`queue_resume`、MCP `resume_queue` 的語意。
- 不對 resume 失敗做新的復原機制；沿用既有的 `agent.ErrHandoff` 交接流程。

## Decisions

### 入口採用獨立的 WebSocket 訊息 queue_run_now，不在 input 加旗標

先排隊再推出去，代表被執行的內容已經在 `queued_messages` 並有穩定的項目 `id`，也已通過 `AcceptInput` 的冪等與附件檢查。新增 `queue_run_now`（欄位 `id`）與現有 `queue_remove`、`queue_resume` 並列即可，不必重做請求 ID 與收據邏輯。

替代方案：在 `input` 加 `interject: true`。放棄原因：訊息要先寫入佇列尾端再取出，等於繞一圈；且 UI 上「送出」鈕會同時承擔兩種意圖，這正是 Cursor 論壇回報的預設值混淆回歸來源。

### 在 sessionDispatcher 新增 RunNow，全程持有 dispatchLock

`RunNow(id)` 與 `Dispatch`、`Drain` 同樣先 `admitWork`、再取 `dispatchLock`。流程：驗證項目存在 → 驗證 session 與 agent 類型可打斷 → 若有進行中的 agent 任務則打斷並等待 → 以 `start(clientMsg, &q)` 啟動該項目（沿用 `PromoteQueuedMessage`）→ `broadcastQueue`。

若呼叫時沒有進行中的任務（剛好跑完）：不打斷，直接啟動該項目；佇列若處於暫停，則只對這一則放行，不改變暫停旗標。

替代方案：把被指定項目移到佇列最前面再 `drainLocked`。放棄原因：佇列以 `ORDER BY id` 為序，沒有重排欄位，要加欄位或改 id，而 `PromoteQueuedMessage` 已能指定 `id`。

### 打斷後等待「舊回合完整收尾」，以 done 通道通知

`taskEntry` 新增 `done chan struct{}`。`runAgent` 的 goroutine 在函式本體（含取消路徑的 `FinalizeMessage`、狀態寫回 idle 與廣播）結束後、佇列決策 defer 之前關閉它（登記為最後一個 defer，使其最先執行）。`taskInterruptForRunNow` 在 `taskCancel` 刪除登記**之前**先取走 `done`，回傳給呼叫者等待。

為什麼不是「子進程退出」就夠：若新回合在舊回合收尾前啟動，舊 goroutine 之後把 session 狀態寫回 idle 會蓋掉新回合的 running 狀態，且新的 user 訊息可能插在舊回合 partial 輸出之前。

為什麼 done 要在佇列決策 defer 之前關閉：該 defer 在「剛好成功完成又被打斷」的競爭下會呼叫 `drainIfIdle`，而 `Drain` 要取 `dispatchLock`；`RunNow` 正持有該鎖等待 done，若 done 晚於 `drainIfIdle` 關閉就會互鎖到逾時。

等待上限 15 秒（估計值：claude 等三個 runner 的 `WaitDelay` 皆為 5 秒，取其三倍；以 fake runner 測試與實測進程樹終止時間確認或調整）。逾時：回報錯誤，不啟動新進程，該項目留在佇列。

### 被打斷的任務以 interjected 標記收尾，不暫停佇列、不發取消通知

以套件層級 `sync.Map`（key 為 `msgID`）記錄「這個任務是被 run-now 打斷的」，`taskInterruptForRunNow` 在 cancel 前寫入。`runAgent` 收尾時以 `LoadAndDelete(msgID)` 判斷：命中則佇列決策 defer 什麼都不做（不 `pauseQueue()`），並略過 `OutcomeCancelled` 的 Telegram 通知。

替代方案：給 `taskEntry` 加欄位。放棄原因：`taskCancel` 會先刪除登記，goroutine 沒有持有 entry 指標，要改 `taskStart` 簽名與所有呼叫處；`sync.Map` 不動既有簽名。

### 新 prompt 前綴說明，讓模型知道上一輪被打斷

被打斷回合的 partial 輸出存在 miniapp 的 DB，但不在 agent 的 transcript，模型 resume 後並不知道。`RunNow` 啟動時在 `attachmentPrompt` 結果前加一行固定文字（說明上一輪回覆已被使用者中途打斷，以下為新指示），聊天紀錄中顯示的 user 訊息內容維持原文，不含前綴。

### 可打斷的 agent 類型採白名單

僅 `claude`、`codex`、`cursor`。其餘（`kiro` 已停止支援、`kiroacp`、`antigravity`、`gemini`、`telegram`）一律拒絕。理由：白名單在新增 agent 類型時預設安全；ACP 型之後以 `session/cancel` 另案實作。

### 前端：佇列項目加獨立按鈕

`ChatView.js` 的 `QueuedMessages` 每列在「移除」旁加「立刻執行」，僅在「有進行中的 agent 任務且 agent 類型在白名單內」時顯示或啟用，點擊送 `{ type: 'queue_run_now', id }`。送出鈕與預設行為不變。

## Implementation Contract

**觀察得到的行為**

- 收到 `queue_run_now` 且 session 有進行中的 claude／codex／cursor 任務、項目存在：舊任務被終止；舊任務的聊天訊息被定稿並保留 partial 內容；之後才出現該項目對應的 user 訊息與新回合；其餘排隊項目保持原順序；佇列暫停旗標不變（原本未暫停則仍未暫停）；不發「已取消」通知。
- 新回合成功結束後，照常由 `drainIfIdle` 接著執行剩餘佇列。
- 新回合以舊回合已記錄的 agent session id 呼叫 `--resume`；prompt 開頭帶有被打斷的說明行。
- 沒有進行中任務時：該項目直接啟動，行為等同一般取出。

**拒絕情境（回傳 `error` 型伺服器訊息，佇列與任務皆不變）**

- agent 類型不在白名單。
- 有待處理的授權（`PendingDenials` 非空、或 `permPending`／`questionPending` 成立）。
- 直連 Shell 任務進行中，或有待確認的 Shell 指令。
- 指定的 `id` 不存在於該 session 的佇列（含已被別的連線取走）。
- 等待舊回合收尾逾時（15 秒）。
- 維護中（`admitWork` 拒絕）：沿用 `updateInProgressMsg`。

**介面**

- 客戶端 → 伺服器：`{ "type": "queue_run_now", "id": <queued_messages.id> }`。
- 伺服器廣播沿用既有 `user_message`、`status`、`queue_update`；不新增伺服器訊息型別。

**驗收**

- 使用既有 `fakeq` runner（prompt 以 `block` 開頭會阻塞到 `ctx.Done()`）的 ws 測試，斷言：(1) 舊 `Run` 在新 `Run` 開始之前已返回；(2) `QueuePaused` 為 false；(3) 訊息順序為 user A → assistant（含 partial）→ user B；(4) 另一筆排隊項目仍在佇列且順序不變。
- 拒絕情境各一個案例：非白名單 agent、等授權中、Shell 任務中、不存在的 `id`。
- 競爭案例：任務剛好成功結束時收到 `queue_run_now`，不得互鎖，且該項目只執行一次。
- 手動實測（實作前先做）：Claude 在 tool_use 之後、tool_result 之前被 KillTree，再 `--resume` 是否成功。

**範圍**

- 範圍內：`internal/ws/taskmanager.go`、`internal/ws/dispatch.go`、`internal/ws/handler.go`、`internal/static/js/chat/ChatView.js`、`internal/ws/queue_test.go` 旁的新測試。
- 範圍外：`internal/db` schema 與 `queue.go` 的既有函式（預期不需修改，若實作時發現必須改再回頭更新本變更）、MCP 工具、ACP 型 runner、直連 Shell。

## Risks / Trade-offs

- [Claude 在 tool_use 後 tool_result 前被殺，resume 可能因 transcript 有未配對的 tool_use 而失敗] → 實作前先手動實測；若失敗，`runAgent` 既有的 `agent.ErrHandoff` 交接流程會接手，並在此變更的任務中補一個失敗案例的記錄；若實測顯示常態失敗，則本變更需改為「僅在無工具執行中才允許」（另案評估）。
- [Edit／Bash 做到一半被殺，工作區可能留下半成品] → 按鈕提示文字說明會中斷目前執行中的動作；不在後端做回復。
- [等待舊回合收尾期間持有 `dispatchLock`，最多 15 秒內該 session 的其他 `Dispatch` 會被擋住] → 可接受（同一 session 本來就序列化）；逾時上限為估計值，需以測試與實測確認。
- [使用者在 lingering 狀態（回合已成功，進程仍活著等待喚醒）按下] → 同樣視為有進行中任務而打斷；因為回合已完成，不丟失工作，視為預期行為並納入測試。
- [多條連線同時對同一項目按下] → 第一個取得 `dispatchLock` 的處理；後者因 `PromoteQueuedMessage` 找不到該列而得到「項目不存在」拒絕。

### 2026-11-19 前置實測記錄（第一輪，task 1.1 未驗證）

- 隔離工作樹 `D:/Temp/cma-queue-run-now-20261119`，HEAD `2e6bad70a3b3d270c96f7632075f776b2b6894d3`；Claude Code `2.1.327`，獨立 session `2b21d4c7-35f7-46b2-b947-74d441e6ed60`，自有 PID `132116`。使用 safe mode、僅提供 Bash，未接正式 miniapp DB／服務或 MCP。
- 測試要求 `sleep 120`，但測試 prompt 額外指定了當前工具 schema 不接受的 `run_in_background`，Claude 呼叫時帶入字串 `"false"`。transcript 在 `07:59:53.958Z` 記錄 tool_use，`07:59:53.967Z` 已回傳配對錯誤：InputValidationError: Bash failed due to the following issue: An unexpected parameter `run_in_background` was provided。這是測試輸入不相容，不能推論正常 Bash 或 resume 不支援。
- 未觀察到等待進程；工具在約 9 ms 內就已返回錯誤，因此沒有符合「工具執行中、tool_result 尚未返回」的中斷窗口。未執行 KillTree，未送第二回合 `--resume`，未重試。Claude 自行退出（exit code 0），後續自有 PID 與子程序掃描為空。
- 結論：本輪只取得工具參數錯誤證據；中斷後 resume 成功／失敗、舊進程退出到新進程啟動的時序與 KillTree 耗時均未驗證。task 1.1 維持未勾選，產品支援範圍不變，後續由 PM 決定。
- 可重現腳本、濃縮證據與 SHA256 見 `D:/Temp/miniapp-queue-run-now-20261119/worker-report.md` 及同目錄 manifest；獨立測試 transcript 保留原處供 PM 驗收。

### 2026-11-19 未配對 tool_use 恢復實測（第二輪中斷＋第三輪 resume）

- 獨立 session `c41d36d8-9947-4313-a9cc-da9ac1597b68` 的 Bash tool_use `toolu_01Xr6Y9RxZ6kJ2cqh7vVmyBn` 於 `08:14:07.622Z` 持久化，command 為 `sleep 120`。第二輪三次 transcript 快照皆無配對 tool_result；自有 Bash 子樹存在，root `130516` 於 `08:14:12.760889Z` 以 Windows `taskkill /F /T /PID` 清理，五個持有的 process handles 於 `08:14:14.064409Z` 全部確認退出。
- PM 核對後，第三輪只對同一 session 執行一次尚未做過的 resume。續接前 transcript SHA256 核對為 `9D4F45E581FA2377DDD2A21627D150F4F0BC420E3B1EAE92CF2D9D508438BD3C`，並在 runtime 保存原始副本。保留 safe mode／default 權限、strict MCP、停用 slash commands，使用 `--tools "" --resume <同一 session id>`，未更換模型或認證。
- resume 於 `08:21:56.183797Z` 開始，晚於舊樹完整退出；新 root PID `29496`。init 與 result 的 session id 均相同；result subtype=`success`、is_error=false、permission_denials 為空，回覆 `RESUME_OK_queue_run_now_20261119`；`08:22:00.651088Z` 確認 exit code 0。本輪未呼叫新工具，清理查詢無殘留自有進程。
- 結論：此版本 Claude Code `2.1.327` 的這一筆「未配對 Bash tool_use → 自有程序樹終止 → 同 session resume」恢復成功。第二輪未觀測到 sleep.exe，不能宣稱已證明 sleep 本體開始執行，也不代表檔案修改中斷、所有工具／版本或產品 RunNow 整合已驗證。
- 第三輪報告、原始續接前副本、事件、summary、cleanup、SHA256 見 `D:/Temp/miniapp-queue-run-now-20261119/worker-report-round3.md` 與同目錄 round3 產物。前兩輪報告與證據不改寫；task 1.1 勾選及後續派工由 PM 驗收決定，本輪沒有產品實作或結案。

## Open Questions

### 2026-11-20 產品 RunNow 整合驗收（task 5.1）

- PM 已驗收派工 #78：Windows、Claude Code 2.1.327、claude-sonnet-5-5、Edge 157.0.4341.60，經隔離 MiniApp 真 DOM／WS 點第二筆「立刻執行」，恰好兩個真回合。來源 d612d568b707b9b09abb0e8fa47455ce28d32709；PM 核對驗證樹 848 個追蹤檔精確 hash 均符合凍結基線。Git 比對發布準備 main e6901e0 僅有六個版本／說明檔不同；磁碟另有 CRLF/LF 差異，正規化後相關 runner、測試與設定內容相同。
- 真 Bash command 執行自有 Python 等待，marker 及程序父鏈確認工具仍執行且沒有配對 tool_result 才點擊。七個已持 handle 的舊程序全退出後 62.5478 ms，新 Claude 才建立；原回合定稿先於新回合開始。
- 同一 native session 的 --resume 成功、exit 0。PM 核對原始 transcript SHA256、35198-byte 前綴 hash、原生補入的同 ID 中斷結果與 parentUuid；無 orphan／未配對工具。PM 已目視前後截圖，餘佇列項目保留。
- 界線：本次只驗單一 Bash／一次 resume；餘 queue 刻意設 paused 防止額外第三回合，真 provider 的自動第三回合未測。其自動續跑及 paused 不變契約由同版 TestRunNowBasic、TestRunNowIdlePausedAndLingering 與既有 ws/db 完整 PASS 覆蓋，不能將此真測延伸宣稱所有工具／版本通過。
- 有效證據與 PM 核對紀錄：D:/Temp/miniapp-release-v0220-20261120/real-run-now-v3/。原 #76／#77 失敗前提證據保留。20 個自有持 handle 程序全退出，44703 埠已釋放；未動正式服務或個人 DB。

- 15 秒等待上限是否足夠 Windows 上的 `KillTree` 與進程樹退出？以實測決定。
- 「立刻執行」按鈕在 lingering 狀態是否顯示？預設顯示；若使用者覺得易誤觸再調整。
