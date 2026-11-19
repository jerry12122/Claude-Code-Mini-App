## Context

來源計劃：`docs/plan/todo/quota-auto-resume.md`（含現況核對）。目前行為：

- 任務因額度用盡失敗時，Claude runner 把 `result.is_error` 轉成一般 `EventError`（文字只有 `claude: <result>`），沒有結構化的「額度用盡」類別；`internal/ws/handler.go` 的收尾對失敗／中斷呼叫 `pauseQueue()`，而它底層的 `PauseQueueIfPending` **只在佇列非空時才暫停**。
- 失敗的那則訊息已經從 `queued_messages` 取出並晉升成 `messages` 的 user 列（`PromoteQueuedMessage` → `saveUserMessage`），所以單純「解除暫停」只會跑佇列剩下的，被中斷的那則會被丟掉。
- `EnqueueMessage` 在佇列原本為空時會順便解除暫停；`ListQueuedMessages`／`PopQueuedMessage` 都以 `ORDER BY id` 排序，`queued_messages.id` 是 `AUTOINCREMENT`。
- 額度服務 `internal/quota` 的 `Snapshot.Windows[]` 有 `Key`（`session`、`weekly`…）、`Percent`、`ResetsAt`（RFC3339，無法解析時原樣字串）；`Service.RefreshAfterRun`／`RefreshManual` 可刷新。
- `internal/server/room.go` 已有「ticker ＋ loopback 與 token 連 WS 送 `room_drain`」的伺服器端喚醒模式，handler 只在 `room_wake` 為真時處理。
- 一般設定 `GeneralSettings` 以單列 JSON 存在 `settings` 表；`PUT /settings/general` 是**整包覆寫**（`PutGeneralSettings` 對全新結構體 `json.Unmarshal`），前端以 `{...general, 欄位}` 回寫。因此「讀到的值」必須就是有效值，否則舊預設會被整包寫回而悄悄改變行為。
- 額度守門 `quota_guard_percent` 目前由 `config.yaml`（預設 90，0 停用）經 `ws.WithQuotaGuardPercent` 在**每條 WS 連線建立時**固定成 `quotaGuardFor(...)` 的閉包，改設定要重啟。
- 啟動時 `ResetRunningSessions` 會把 `running`／`awaiting_input` 等 session 設為 idle 並**暫停佇列**，記錄「程式重啟，上一個任務已中斷」。

## Goals / Non-Goals

**Goals:**

- 5 小時（`session`）視窗用盡後，伺服器在視窗重置後自動接著跑，且被中斷的訊息不遺失。
- 自動復原的完整生命週期是一個明確、持久化的狀態機；任何時刻重啟都不遺失、不重複觸發。
- 單一全域開關，預設關；關閉時行為與現在完全相同。
- 額度守門的開關與門檻可在設定頁調整、執行期生效。

**Non-Goals:**

- 週額度（`weekly`）、額度守門暫停都不自動復原。
- 不推 Telegram 通知；不做每個 session 各自的開關。
- 不以錯誤文字比對判定額度用盡。
- 不新增額度重置時間的推測邏輯：沒有可解析的 `ResetsAt` 就不武裝。
- 非使用者訊息觸發的執行（授權重試的內部提示、Telegram 型 session）不武裝，維持現狀。
- 不重新設計 session 的 `THINKING`／`STREAMING`／`IDLE` 狀態；這裡的「狀態機」只指自動復原。

## Decisions

### 開關存在 GeneralSettings
`GeneralSettings` 加 `AutoResumeOnQuota bool`（JSON `autoResumeOnQuota`），預設 `false`。選它而不是 `config.yaml` 或前端 localStorage：輪詢在伺服器、需要執行期切換、且前端沒開也要生效；缺欄位就是 `false`，不需遷移。

### 自動復原狀態機存進 DB
`sessions` 新增三欄（以既有 `tryAlter` 加欄）：`quota_resume_state TEXT NOT NULL DEFAULT 'none'`、`quota_resume_at TEXT NOT NULL DEFAULT ''`（RFC3339 UTC，只在 `waiting` 有意義）、`quota_resume_attempts INTEGER NOT NULL DEFAULT 0`。四個狀態與轉移以 spec 的表為準：

```
none ──偵測且次數<3──▶ waiting ──到期且額度回復，搶到更新──▶ resuming ──成功／其他失敗／取消──▶ none
 │                      ▲  │                                   │
 │                      │  └─到期但仍滿：改寫時間，維持 waiting │
 │                      └──── 再次用盡且次數<3；重啟；投遞失敗 ──┘
 └─偵測且次數=3──▶ exhausted ◀──再次用盡且次數=3── resuming
任何非 none ──取消／手動繼續／直接送訊息／清空佇列／刪 session／關開關──▶ none（次數歸零）
```

實作約束：所有轉移都走單一函式，以 `UPDATE … WHERE id = ? AND quota_resume_state IN (<允許的來源>)` 條件式更新並回傳是否成功（`RowsAffected`），在 Go 端另以白名單拒絕表外的轉移；不允許先讀後寫。`attempts` 只在 `waiting → resuming` 時 +1，凡回到 `none` 一律歸零；`resuming → waiting` 因「投遞失敗」退回時還原到進入前的值，因「再次用盡」或「重啟」退回時保留。
實作補充：`quota_resume_generation INTEGER NOT NULL DEFAULT 0` 是私有的單調世代識別；每次 `waiting → resuming` 同道條件式更新加一，回 `none`、關開關與啟動復原不歸零。收尾及重武裝以世代條件防止取消後重新計次的舊回合誤改新回合；`attempts` 只負責三次上限。
理由：先前只用「`quota_resume_at` 有值」與計數隱含狀態，無法區分「等待中」「已觸發、執行中」「已放棄」，也無法擋住兩個 tick 同時觸發，重啟後更不知道該不該再試。替代方案是把狀態放記憶體，重啟即遺失，否決。

### 以額度快照判定額度用盡
失敗收尾（開關開 ∧ 失敗 ∧ 非取消 ∧ 非等授權 ∧ 該次執行由使用者訊息觸發）時呼叫 `RefreshAfterRun`，只在 `session` 視窗 `Percent >= 100` 且 `ResetsAt` 能解析成時間時武裝。替代方案是比對錯誤文字（各 CLI 字串不同且會隨版本改變），否決；代價是少數情況（快照拿不到、剛好 99%）不會自動復原，這是刻意的「寧可不動」。

### 失敗訊息排回佇列最前面
武裝時由失敗那則 `messages` user 列（以該次執行的 `userMsgID` 取得）重建內容、署名與附件 ID，新增 `EnqueueFront`：id 取 `MIN(既有最小 id, 0) - 1`，保證是非零且排在最前（`saveUserMessage` 以 `queueID != 0` 判斷是否為佇列項目，所以不能用 0）；它**不可**像 `EnqueueMessage` 那樣在空佇列時解除暫停，武裝流程在入列後無條件 `SetQueuePaused(true)`。自動重跑的那一輪失敗時，訊息已在啟動時離開佇列，所以同樣再前插一次。
取捨：重跑時該訊息會再晉升成一列新的 user 訊息，歷史裡會看到同一句出現兩次（第一次失敗、第二次重跑）。接受這個重複，換取不改動歷史／晉升流程。

### 輪詢經內部 WebSocket 通道恢復
新檔 `internal/server/quota_resume.go` 仿 `roomRuntime.watch`：每 30 秒查 `state = 'waiting' AND quota_resume_at <= now` 的 session；對每個先 `RefreshManual`，`session` 視窗 < 100% 才做條件式轉移 `waiting → resuming`，搶到的才送 `quota_resume`；仍滿則改寫成新的 `ResetsAt + 60 秒`；刷新失敗則保留、下個 tick 再試。handler 內新增 `case "quota_resume"`，與 `room_drain` 一樣只在 `room_wake` 為真時處理，呼叫既有 `dispatcher.Resume()`（清除暫停、`drain(false)` 不過額度守門），並回覆 `quota_resumed`，帶 `started`（執行後該 session 是否真的有任務在跑）。`started` 為假時（維護閘門擋下、佇列已空）：佇列空則 `resuming → none`，其餘 `resuming → waiting` 並還原次數，下個 tick 重試。不直接在輪詢裡呼叫 runner：dispatcher 與 `runAgent` 是每條 WS 連線建立的閉包，只有經 WS 才能取得同一套準入、鎖與廣播。60 秒緩衝吸收時鐘差，到期後再確認一次額度是最終保險。

### 啟動時復原狀態
伺服器啟動時，在 `ResetRunningSessions` 之後執行一次 `RecoverQuotaResume`：`resuming` 的 session 一律轉 `waiting`，`quota_resume_at` 設為啟動當下，次數保留（重啟中斷的那一輪算一次）。這樣下個 tick 會重新確認額度並繼續，不會停在沒人推進的 `resuming`；`waiting` 與 `exhausted` 原樣保留。

### 取消與關閉開關
清除（轉 `none`、次數歸零）的入口：手動「繼續」（`Resume()` 內，但由 `quota_resume` 觸發的那次呼叫除外）、清空佇列／刪除 session、直接送訊息（`Dispatch`）、橫幅的取消、開關被關。關閉開關時（`PutGeneral` 偵測 `autoResumeOnQuota` 由 true 變 false）一次把所有非 `none` 的 session 轉 `none`；輪詢每次 tick 也讀開關，開關關著不動作。取消自動復原的狀態轉移本身不動佇列內容與暫停狀態；橫幅取消與關開關因此保持原暫停與訊息。手動繼續仍啟動佇列、直接送訊息仍沿用 Dispatch、清空／刪除佇列項目仍刪掉使用者指定內容、刪除 session 仍刪除該 session 與佇列。這是取消排程與原操作的分工，不把使用者要求的刪除改成保留。

### 橫幅狀態來源
`quota_resume_state` 與 `quota_resume_at` 隨既有 sync／`queue_update` 廣播攜帶，前端依狀態顯示：`waiting` 顯示預計時間與取消鈕，`exhausted` 顯示「已達自動繼續上限，請手動繼續」，其餘不顯示。因此重連、重新整理與重啟後都一致。取消用新的 client 訊息 `quota_resume_cancel`。

### 額度守門設定改存 DB 並於執行期讀取
`GeneralSettings` 再加 `QuotaGuardEnabled bool` 與 `QuotaGuardPercent float64`。**有效值**規則：尚未在設定頁儲存過時，由 `config.yaml` 的 `quota_guard_percent` 決定（> 0：啟用且取該值；0：停用、百分比顯示 90）；已儲存則用儲存值。`GET /settings/general` 回傳有效值（避免整包回寫時把「沿用 config」悄悄變成明確值以外的東西——前端寫回的就是它讀到的有效值，行為不變）。`PUT` 驗證 `quotaGuardPercent` 在 (0, 100]，不合法回 400 並指出欄位，不寫入。守門閉包改成每次 `Drain` 前呼叫「讀有效設定」的函式，而不是連線建立時固定值，所以改設定不必重啟、也不必重新連線；判斷規則（stale／無資料放行、略過沒有 percent 的視窗、使用者直接送出與按「繼續」不受影響）不變。`ws.WithQuotaGuardPercent` 保留作為初始值來源。替代方案是繼續只用 `config.yaml`，需重啟且無法在手機上調整，否決。

## Implementation Contract

**Behavior**

- 自動復原開關關：任何情況行為不變（失敗暫停、手動繼續）。
- 開關開 ∧ 判定成立：狀態 `waiting`，`quota_resume_at = ResetsAt + 60 秒`；佇列 = [失敗訊息, 原有排隊…] 且暫停；所有連線的畫面顯示「額度用盡，預計 HH:MM 自動繼續」與取消鈕。
- 到期且額度回復：`waiting → resuming`，佇列恢復，最前面的訊息重跑；成功則 `none`。到期但仍滿：只改寫時間。
- 第 4 次連續用盡：`exhausted`，佇列維持暫停，畫面提示手動繼續。
- 額度守門：設定頁可開關與調整門檻，下一次自動啟動佇列時立即採用。

**Interface / data shape**

- `GeneralSettings`：`autoResumeOnQuota`（預設 false）、`quotaGuardEnabled`、`quotaGuardPercent`（有效值規則見上）。`/settings/general` 沿用整包讀寫；不合法的百分比回 400。
- DB：`sessions.quota_resume_state`、`quota_resume_at`、`quota_resume_attempts`；新函式：條件式轉移（回傳是否成功）、列出到期的 `waiting`、啟動復原、關開關時批次轉 `none`、`EnqueueFront`。
- WS 伺服器→客戶端：session sync 與 `queue_update` 帶 `quota_resume_state`／`quota_resume_at`；`quota_resumed`（`started`）僅回給內部喚醒連線。客戶端→伺服器：`quota_resume`（僅 `room_wake`）、`quota_resume_cancel`（一般連線，訪客無權）。

**Failure modes**

- 快照缺失／過期／沒有 `session` 視窗／`ResetsAt` 無法解析：不武裝，沿用現有暫停。
- 輪詢時刷新失敗：保留 `waiting`，下個 tick 再試；`admitWork()` 維護閘門擋下或佇列已空時依 `started` 處理，不停在 `resuming`。
- 重啟時處於 `resuming`：退回 `waiting` 並立即重新確認額度。
- 表外的轉移：靜默不生效（條件式更新回傳 0 列），測試覆蓋。

**Acceptance criteria**

- 單元測試：spec 轉移表的每一列（含表外轉移被拒）、兩個並行 tick 只有一個成功、`EnqueueFront` 排序與不解除暫停、判定邊界表、各取消入口、關開關清全部、重啟後 `waiting` 保留與 `resuming` 退回 `waiting`、`started` 為假的兩種處理、次數規則（3 次上限、回 `none` 歸零、投遞失敗還原）。
- 守門：有效值解析的三種情境、百分比驗證邊界表、執行期改門檻與關閉守門不需重連即生效、既有守門測試維持通過。
- `go build`、`go vet`、`go test`（排除 `poc/`）、`go test ./internal/arch/`、`node scripts/frontend-layering.test.cjs` 全過。
- 臨時實例以可注入的假額度來源把 `ResetsAt` 縮到數十秒，目視確認「用盡→橫幅→到期→自動重跑→成功」「中途重啟→仍會繼續」「關開關→橫幅消失且不動」；設定頁調整守門門檻後下一則排隊訊息的暫停行為隨之改變。
- 真實撞上限取樣（阻擋項）：記錄失敗時 `result` 原文與 `Windows[]` 的 `percent`／`ResetsAt`，證實判定條件成立。

**Scope boundaries**

- 範圍內：自動復原的開關、判定、持久化狀態機、輪詢、取消、防迴圈、橫幅、啟動復原；額度守門的設定儲存、驗證、設定頁控制與執行期讀取；文件與 CHANGELOG。
- 範圍外：`weekly`、額度守門暫停自動復原、Telegram 通知、每 session 開關、歷史重複訊息的去重、非使用者訊息觸發的執行、session 其他狀態的重新設計。

## Risks / Trade-offs

- [無人看管地繼續執行，且專案多以 `bypassPermissions` 跑代理] → 預設關，設定頁文案明講；橫幅可隨時取消。
- [撞限時快照的 `percent` 可能不到 100 或沒有 `ResetsAt`，判定落空] → 第一個任務先實測取樣；落空時不武裝，行為退回現狀，並依樣本調整判定條件再回頭更新 spec。
- [歷史出現重複的 user 訊息] → 已接受，見取捨。
- [重跑原 prompt 不保證與失敗前的半成品銜接，Claude 端是否已記錄該輪取決於 CLI] → 取樣時一併確認；必要時改為送「請繼續上一個任務」，屬 design 變更，不是靜默調整。
- [時鐘差與重置邊界] → 60 秒緩衝＋到期後再確認額度。
- [輪詢與使用者操作競爭（剛好同時按「繼續」）] → 所有轉移都是條件式更新，後到者更新 0 列即放棄；啟動走 dispatcher 的鎖與忙碌檢查。
- [`started` 判斷依賴 `Resume()` 返回時任務已登記] → 驗收測試明確覆蓋；若發現登記有延遲，改在轉移前以 `taskIsActive` 輪詢短暫確認，屬實作細節調整。
- [整包覆寫的設定被舊前端覆寫新欄位] → `GET` 回傳有效值、前端 `GENERAL_DEFAULTS` 同步加欄位；舊前端若漏欄位，`quotaGuardPercent` 缺值會是 0 而被驗證擋下（400），不會悄悄改值；`autoResumeOnQuota` 缺值則視為 false。前後端是同一個二進位，不會錯配；測試補「缺欄位的 PUT」的行為。

## Migration Plan

純加欄與加設定鍵：`tryAlter` 加三個欄位、既有 session 預設 `none`，舊版回滾時多出的欄位無害。`autoResumeOnQuota` 預設關，部署後行為不變。守門在使用者未儲存前沿用 `config.yaml`，行為不變；回滾只需關開關（或回舊版，欄位與設定鍵被忽略，守門回到 `config.yaml`）。

## Open Questions

以下於提案階段採用計劃第 6 節的建議值或對需求的解讀，視為**假設**，使用者可在實作前推翻：

1. 「狀態機要存好」解讀為「自動復原的狀態機完整存進 DB」，**不是** session 的 `THINKING`／`STREAMING`／`IDLE` 狀態；若指後者，需另開 change。
2. 自動復原預設關（建議）。
3. 額度守門暫停不自動復原（建議；要納入只需守門暫停時也進 `waiting`，狀態機已能承載）。
4. 關開關時所有非 `none` 狀態一律轉 `none`（建議，而非保留）。
5. 不通知（建議）。
6. 待取樣後確認：撞限時 `percent` 是否為 100、`ResetsAt` 是否存在，以及重跑原 prompt 還是送「請繼續」。

