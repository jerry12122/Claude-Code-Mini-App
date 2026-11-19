# 計劃：額度用盡後自動復原（含開關）

> 狀態：**計劃，尚未實作**；待使用者確認第 6 節的決定
> 建立日期：2026-11-18
> 需求原話：「因為達到 5 limit 上限停下來，loop check 自動復原，開關」

## 1. 目標與完成標準（DOD）

任務因 5 小時額度視窗用盡而停下時，伺服器自己輪詢，視窗重置後自動把該 session 接著跑；整個功能有一個開關，**預設關**。

- 開關關閉：行為與現在完全相同（佇列暫停、等使用者按「繼續」）。
- 開關開啟：
  1. 額度用盡造成的失敗被辨識出來，並記下預計恢復時間。
  2. 恢復時間到後，先再確認額度已回復，才自動繼續；沒回復就延後再查。
  3. 被中斷的那則訊息會重跑，不會遺失；佇列裡原本排隊的訊息照順序接著跑。
  4. 重啟伺服器後仍會繼續等（狀態存 DB）。
  5. 畫面看得到「額度用盡，預計 HH:MM 自動繼續」，使用者可隨時取消。
- 自動重跑最多連續 3 次，避免額度判斷錯誤造成無限迴圈。

## 2. 現況（2026-11-18 以程式碼核對）

| 項目 | 位置 | 現況 |
|---|---|---|
| 額度失敗 | `internal/claude/runner.go:373` | `result.is_error` 轉成 `EventError`，文字只有 `claude: <result>`，**沒有結構化的「額度用盡」類別** |
| 失敗收尾 | `internal/ws/handler.go:766-776` | 失敗／中斷走 `pauseQueue()`；`PauseQueueIfPending`（`internal/db/queue.go:129`）**只在佇列非空時暫停**，所以沒排隊時失敗就只是閒置、沒有暫停旗標可供恢復 |
| 失敗那則訊息 | `queued_messages` | 已被取出（Pop），失敗後**不在佇列裡**，只留在 `messages` |
| 額度資料 | `internal/quota` | `Snapshot.Windows[]` 有 `Key`（`session`＝5 小時、`weekly`）、`Percent`、`ResetsAt`（RFC3339）；`Service.RefreshManual` 可強制刷新 |
| 額度守門 | `internal/ws/quota_guard.go` | 只在**自動啟動佇列前**擋（門檻 90%），暫停後沒有任何自動解除 |
| 伺服器端輪詢範例 | `internal/server/room.go:111` | `ticker` + loopback＋token 連 WS 送 `room_drain`，handler 內 `roomWake` 才放行（`handler.go:1412`） |
| 伺服器端設定 | `internal/db/general_settings.go`、`internal/api/settings.go`、`SettingsModal.js` 的「一般」 | 已有存 DB 的整包 JSON 設定，目前只有 `vscodeNoAdmin` 一個開關 |

## 3. 假設（請確認或糾正）

1. 「5 limit」指 **5 小時額度視窗**（`session` 視窗）。週額度（`weekly`）重置要數天，不自動復原，仍走現有的手動「繼續」。
2. 「復原」＝**重跑被中斷的那則訊息**並接著跑佇列，不是只解除暫停。因為解除暫停只會跑佇列裡剩下的，被中斷的那則會被默默丟掉。
3. 開關是**全域**一個，存伺服器 DB，不是每個 session 一個，也不是前端 localStorage（輪詢在伺服器，前端沒開也要能復原）。
4. 判斷「額度用盡」用**額度快照**（視窗 `percent ≥ 100`），不靠解析錯誤文字。真實錯誤字串沒有樣本，靠正則比對一換版就壞。

## 4. 設計

### 4.1 開關
- `GeneralSettings` 加 `AutoResumeOnQuota bool`（JSON `autoResumeOnQuota`），預設 `false`。無需 DB 遷移（整包 JSON，舊資料缺欄位就是 false）。
- 設定頁「一般」加一個開關，沿用 `vscodeNoAdmin` 的即時 PUT 寫法。文案要講明白：開啟後額度重置時會**無人看管地**繼續執行，而本專案多半以 `bypassPermissions` 跑代理。

### 4.2 辨識與記錄（失敗收尾處）
僅當 開關開啟 ∧ `runFailed` ∧ 非 `cancelled` ∧ 非 `permDenied`：
1. 刷新該 provider 額度（`RefreshAfterRun`）。
2. 若有 `session` 視窗 `percent ≥ 100` 且 `ResetsAt` 可解析 → 判定為額度用盡：
   - `sessions.quota_resume_at` ← `ResetsAt + 60 秒`（RFC3339 UTC；新欄位，`tryAlter` 加欄，預設空字串）。
   - 失敗的那則 prompt（含附件 ID）**排回佇列最前面**（需新增 `EnqueueFront`；排序若以 id 為準，改取 `MIN(id)-1`，實作時先看 `ListQueuedMessages` 的 ORDER）。
   - 無條件暫停佇列（不是 `PauseQueueIfPending`）。
   - 廣播 `quota_wait`（含 `resume_at`）讓畫面顯示橫幅。
3. 判不出來（沒資料、`stale`、沒有 `session` 視窗）→ 照舊，不武裝。**不猜。**

### 4.3 輪詢（loop check）
- 新檔 `internal/server/quota_resume.go`，仿 `room.watch`：每 30 秒一次，查 `quota_resume_at != '' AND quota_resume_at <= now` 的 session。
- 對每個到期的 session：
  1. `RefreshManual` 重新取額度；`session` 視窗 `percent < 100` → 往下。
  2. 仍 ≥ 100：把 `quota_resume_at` 改成新的 `ResetsAt + 60 秒`，`quota_resume_attempts + 1`（見 4.5）。
  3. 透過 loopback＋token 的 WS 送新訊息 `quota_resume`。handler 內 `case "quota_resume"` 與 `room_drain` 一樣只在 `roomWake` 為真時處理，呼叫現有 `dispatcher.Resume()`（`dispatch.go:173`，它已經是「解除暫停＋不過額度守門＋自動啟動」）。成功後清掉 `quota_resume_at`。
- 開關在**每次 tick 讀**，關掉就立刻停止復原（已武裝的 session 保留 `quota_resume_at` 但不動作；或關閉時一併清除，二選一，見 6-3）。

### 4.4 取消
下列任一動作清除 `quota_resume_at`：使用者按「繼續」、清空／刪除佇列、刪除 session、使用者直接送新訊息（已是明確人為動作，會走自己的啟動路徑）。橫幅上加「取消自動繼續」，等同清除。

### 4.5 防迴圈
新欄位 `quota_resume_attempts`（預設 0）。每次自動重跑前 +1；任務**成功**才歸零。到 3 次就不再武裝，改走現有手動暫停並顯示原因。

### 4.6 與既有機制的關係
- **額度守門**（90% 暫停）是另一種暫停，原因文字是「額度守門：…」。第一版**不**讓它自動復原（見 6-2）。
- **程式重啟**（`ResetRunningSessions`）會把佇列設為暫停；`quota_resume_at` 在 DB 裡，重啟後輪詢照常接手。
- 輪詢觸發的啟動走 `admitWork()` 維護閘門，更新中會被擋，下個 tick 重試。
- 分層：新欄位／查詢放 `internal/db`，輪詢放 `internal/server`，訊息處理在 `internal/ws`，不新增跨層 import；完成後跑 `go test ./internal/arch/`。

## 5. 實作步驟

1. **先取樣（阻擋項）**：真的撞一次 5 小時上限（或用 `poc/` 模擬），記下失敗時 `result` 的原文、`Windows[]` 的 `percent`／`ResetsAt` 當下長相。**若撞限時 `percent` 不到 100 或沒有 `ResetsAt`，4.2 的判定條件要改。** 沒有這個樣本不要動手寫判定。
2. DB：`sessions.quota_resume_at`、`quota_resume_attempts`；`EnqueueFront`；讀寫函式；單元測試。
3. 設定：`GeneralSettings.AutoResumeOnQuota`（含 `general_settings_test.go`）；API 不用改（整包）。
4. 判定與武裝：失敗收尾處加 4.2；以可注入的額度來源寫單元測試（涵蓋：用盡武裝／未用盡不武裝／stale 不武裝／開關關不武裝／週視窗不武裝／取消與權限拒絕不武裝）。
5. 輪詢＋`quota_resume` 訊息：測試涵蓋到期且額度回復→恢復、到期但仍滿→延後、未到期不動、開關關不動、3 次上限、重啟後仍恢復。
6. 前端：設定頁開關；聊天橫幅（`quota_wait`／取消）；`node scripts/frontend-layering.test.cjs`。
7. 文件：`config.example.yaml` 不需動（開關在 DB）；更新 `CHANGELOG.md`、本檔結果與 `docs/plan/todo.md`。

驗證：`go build`／`go vet`／`go test`（排除 `poc/`）、`go test ./internal/arch/`、前端分層檢查；再用臨時實例，把輪詢間隔與 `ResetsAt` 縮短（測試注入假額度來源），親眼看到「用盡→橫幅→到期→自動重跑→成功」與「關開關→不動」。

## 6. 需要你決定的事

1. **預設值**：我建議預設關。自動復原等於在你不在場時花額度、跑可能是 `bypassPermissions` 的指令；預設開對這個專案風險不對稱。
2. **額度守門的暫停要不要也自動復原**：它是你設的 90% 自願停手，意圖和「撞牆」不同；自動復原會把你想省的額度又花掉。建議第一版不含；要的話只是在守門暫停時也寫 `quota_resume_at`，改動很小。
3. **關開關時已武裝的 session 怎麼辦**：（a）保留旗標、只是不動作，之後重新打開又會恢復；（b）關的當下全部清掉。建議 (b)，行為較好預測。
4. **是否通知**：自動恢復時要不要沿用 Telegram `notify` 推一則？第一版建議不做（已知限制），橫幅就夠。

## 7. 風險與已知限制

- 判定依賴額度快取／即時查詢，查不到就不復原（fail-safe：寧可不動也不亂跑）。
- 時間用額度服務給的 `ResetsAt`；伺服器與供應商時鐘差由 60 秒緩衝吸收，仍可能落在邊界，由「到期後再確認一次額度」兜底。
- 只涵蓋有 `session` 視窗的 provider；沒有該視窗的（如 cursor 的金額型）不武裝。
- 重跑的是原 prompt，不保證與失敗前的半成品輸出銜接；Claude 端是否已記錄該輪取決於 CLI，步驟 1 取樣時一併確認，必要時改成送「請繼續上一個任務」。

## 8. 5.1 端到端觀察（2026-11-19）

> 下文的臨時程序 `scripts/quota-banner-fixture` 已於清理時移除；可重跑的驗證以 `internal/server/quota_e2e_test.go` 的 `TestQuotaE2E*` 為準。

臨時程序是 `go run ./scripts/quota-banner-fixture`。它不呼叫 `server.Start`，因此不同步模型、不暖機、不打真供應商；runner 與額度都在行程內，而且只聽 127.0.0.1。30 秒輪詢是正式的 `watchQuota`，測試沒有改 30／60 秒常數，也沒有把手動 `resumeDue` 當成這次已驗過。輪詢啟動或延後只對上刷新前列出的世代與預定時間。

已用假額度與真內部 WS 走完：

- 用盡 → 橫幅 → 到期 → 自動重跑 → 成功：`TestQuotaE2ERealWatchContinues`，以及 18771 的 flow。瀏覽器先出現「額度用盡，預計 00:46 自動繼續」與取消鈕，佇列暫停；約一個輪詢後橫幅消失，回覆為 `ok:請回一個字`。使用者訊息列出現兩次，這是既有的重跑限制。
- waiting 重啟與 resuming 重啟：同一支測試在啟動前種下兩種狀態。啟動後 sync 都是 waiting，同一個 30 秒輪詢把兩則都跑完並回到 none。
- 仍滿 → 延後：`TestQuotaE2ERealWatchDelays`。時間改成新的 resets_at+60 秒，佇列 `keep` 仍暫停、沒有啟動。重新連線的 sync 帶新時間。延後寫入成功後，會把該 session 最新的 queue_update（含新時間）推給當時已連線的客戶端。寫入沒對上或失敗時不推。
- 關開關：`TestQuotaE2ESwitchOffStays`。連線先看到 waiting，PUT 關閉後看到 none；再等過一個 30 秒輪詢，狀態仍是 none，佇列 `stay` 沒有被啟動。
- 守門門檻：`TestQuotaE2EGuardThreshold`。快取 session 50%，執行中把設定改成 40，下一則自動啟動被暫停。這是設定頁所用的 PUT，不是這輪親手點設定頁。

留給 PM：delay、restart、off、guard 的畫面還沒在這輪瀏覽器逐一點過。flow 的成功路徑已在 Chrome 看過。設定頁把 90 改成 40 的點擊尚未重做。
