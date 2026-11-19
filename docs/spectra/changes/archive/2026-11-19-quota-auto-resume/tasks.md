## 1. 取樣（阻擋項）

- [x] 1.1 真的撞一次 5 小時上限（或在 `poc/` 建立等價的重現），記錄失敗時 Claude `result` 的原文，以及同一時刻 `quota.Snapshot` 中 `session` 視窗的 `percent` 與 `resets_at` 實際內容，寫成 `poc/quota-exhaustion/` 下的簡短紀錄（格式比照 `poc/agent-handoff/agent-handoff-poc.md`）。驗證：紀錄能回答三件事——撞限時 `percent` 是否 ≥ 100、`resets_at` 是否存在且可被 `usage.ParseResetsAt` 解析、Claude 是否已記錄失敗那一輪（決定重跑原 prompt 或改送「請繼續」）。若 `percent` 不到 100 或沒有 `resets_at`，**停止後續 3.1 起的自動復原任務**並回頭更新 design 的「以額度快照判定額度用盡」與 spec 的 Detect quota exhaustion from the quota snapshot，再繼續（額度守門相關的 2.3、3.6、4.1 不受此阻擋）。

## 2. 資料層與設定

- [x] 2.1 「自動復原狀態機存進 DB」落實 Persisted auto-resume state machine 的資料層：`sessions` 新增 `quota_resume_state`（預設 `none`）、`quota_resume_at`、`quota_resume_attempts` 三欄；單一條件式轉移函式（`UPDATE … WHERE quota_resume_state IN (…)`，回傳是否成功，Go 端白名單拒絕表外轉移，`attempts` 只在 `waiting → resuming` +1、回 `none` 歸零）；列出到期 `waiting`、批次轉 `none`、啟動復原（`resuming` → `waiting` 且時間為啟動當下）；以及武裝時的前插（實作於 `ArmQuotaResume` 的交易內，沒有獨立的 `EnqueueFront`；遵循「失敗訊息排回佇列最前面」：id 取 `MIN(既有最小 id, 0) - 1` 使其非零且排在最前，且不會像 `EnqueueMessage` 在空佇列時解除暫停）。驗證：`internal/db` 單元測試逐列涵蓋 spec 轉移表（含表外轉移回傳 false、兩個並行轉移只有一個成功、投遞失敗還原次數）、前插排序與保持暫停（`TestQueueFrontRowOrdering` 與 ws 武裝測試）、`PromoteQueuedMessage` 能以負數 id 正確刪除佇列列、重開 DB 後狀態與時間仍在、啟動復原只動 `resuming`。
- [x] 2.2 「開關存在 GeneralSettings」：`GeneralSettings` 新增 `AutoResumeOnQuota`（JSON `autoResumeOnQuota`，預設 false），Global auto-resume switch 在 `PUT /settings/general` 由 true 改為 false 時同一動作把所有非 `none` 的 session 轉 `none`（Cancelling a pending auto-resume 的「取消與關閉開關」之一）。驗證：`internal/db/general_settings_test.go` 補「無列時為 false」「舊 JSON 缺欄位為 false」；`internal/api` 測試補「true→false 後所有狀態被清、false→true 不動」。
- [x] 2.3 「額度守門設定改存 DB 並於執行期讀取」的資料與 API 層，落實 Quota guard settings stored on the server 與 Guard settings validation：`GeneralSettings` 新增 `QuotaGuardEnabled`、`QuotaGuardPercent`；有效值解析（未儲存過時由 `config.yaml` 的 `quota_guard_percent` 決定：>0 啟用該值、0 停用且百分比 90）；`GET` 回傳有效值，`PUT` 驗證百分比在 (0, 100] 否則 400 且不寫入。驗證：測試涵蓋 spec 的三個有效值情境、驗證邊界表（0、-5、100.5、100、0.5、90）、缺欄位 PUT 因百分比 0 被拒、被拒時既有設定不變。

## 3. 後端行為

- [x] 3.1 Detect quota exhaustion from the quota snapshot 並落實「以額度快照判定額度用盡」與 Arming preserves the interrupted message：失敗收尾（開關開 ∧ 失敗 ∧ 非取消 ∧ 非等授權 ∧ 由使用者訊息觸發）刷新該 provider 額度，`session` 視窗 `percent ≥ 100` 且 `resets_at` 可解析時，轉 `waiting`（或次數已 3 則轉 `exhausted`）、記 `quota_resume_at = resets_at + 60 秒`、以失敗那則 user 訊息的內容／署名／附件 ID 呼叫 `EnqueueFront`、無條件暫停佇列並廣播。Scope limits：週視窗、額度守門暫停、系統內部提示觸發的執行、Telegram 型 session 都不武裝。驗證：`internal/ws` 單元測試逐列涵蓋 spec 的 detection boundaries 表（8 列）與「失敗訊息＋既有佇列順序為 A、B、C 且暫停」「週視窗滿不武裝」「內部授權重試提示失敗不武裝」「自動重跑那輪再失敗會再前插一次」。[after: 1.1, 2.1, 2.2]
- [x] 3.2 Resume loop re-checks quota before continuing 並落實「輪詢經內部 WebSocket 通道恢復」：新增 `internal/server/quota_resume.go`，每 30 秒處理到期的 `waiting`（先 `RefreshManual`，`session` 視窗 < 100% 才做 `waiting → resuming` 條件式轉移，搶到者送 `quota_resume`；仍滿則把時間改為新的 `resets_at + 60 秒`；刷新失敗保留），handler 新增只在 `room_wake` 為真時處理的 `quota_resume`，呼叫既有 `Resume()` 並回 `quota_resumed`（`started`）；`started` 為假時佇列空轉 `none`、其餘退回 `waiting` 並還原次數；開關關著時輪詢不動作。驗證：測試涵蓋「到期且額度 40% → `resuming` 且前端訊息重跑」「到期但仍滿 → 維持 `waiting`、時間更新」「未到期 → 不刷新額度」「`resuming` 被跳過」「開關關 → 不啟動」「刷新失敗 → 保留」「兩個並行 tick 只送一次」「`started` 為假的兩種處理」。[after: 3.1]
- [x] 3.3 Bounded consecutive retries：自動重跑那一輪結束時依狀態收尾——成功、其他原因失敗、被取消都 `resuming → none`（次數歸零）；再次因額度用盡失敗則 `resuming → waiting`（次數 < 3）或 `resuming → exhausted`（次數 = 3），`exhausted` 保留手動暫停並顯示失敗原因。驗證：測試涵蓋 spec 的「第 4 次連續用盡進入 `exhausted` 且佇列暫停」「成功後狀態 `none` 且次數 0」「其他原因失敗回 `none`」。[after: 3.2]
- [x] 3.4 「啟動時復原狀態」：伺服器啟動在 `ResetRunningSessions` 之後呼叫啟動復原，落實 spec 的 Restart while resuming 與 State survives restart。驗證：整合測試以「建立 `resuming` 與 `waiting` 的 session → 重開 DB 並執行啟動流程」斷言 `resuming` 變 `waiting` 且時間為啟動時間、`waiting` 與 `exhausted` 原樣、之後一個輪詢 tick 會重新確認額度並繼續。[after: 3.2]
- [x] 3.5 「取消與關閉開關」落實 Cancelling a pending auto-resume：手動「繼續」（排除內部 `quota_resume`）、刪除／清空佇列項目、刪除 session、直接送訊息、橫幅 `quota_resume_cancel`（訪客無權）均取消自動復原（非 `none` 轉 `none` 且次數歸零）。取消排程本身保留佇列與暫停；原操作仍各自生效：手動繼續照跑、直接訊息照既有 Dispatch、刪除／清空照刪指定內容、刪 session 照刪。驗證：每個入口一條，對橫幅取消／關開關斷言原訊息仍首位且暫停；手動繼續斷言啟動一次；刪除／清空斷言僅刪指定內容且不自動恢復；刪 session 後不會被輪詢啟動；`exhausted` 可清；訪客取消被拒；取消與晚到投遞／收尾交錯不重啟。 [after: 3.3]
- [x] 3.6 Guard reads settings at evaluation time 並落實「額度守門設定改存 DB 並於執行期讀取」：`internal/ws/quota_guard.go` 的守門閉包每次 `Drain` 前讀有效設定（`ws.WithQuotaGuardPercent` 退為初始值來源），守門關閉時不因額度暫停，其餘規則不變。驗證：既有 `quota_guard_test.go` 維持通過，另補「執行期把門檻從 90 改 40 後下一次自動啟動被暫停」「執行期關閉守門後下一次自動啟動照跑」「直接送出與按「繼續」仍不受守門影響」「stale 與無 percent 視窗放行」。[after: 2.3]
- [x] 3.7 「橫幅狀態來源」：session sync 與 `queue_update` 廣播攜帶 `quota_resume_state` 與 `quota_resume_at`（`none` 時為空）。驗證：`internal/ws` 測試斷言新連線的 sync 訊息帶有 `waiting` 與時間、`exhausted` 帶狀態、轉 `none` 後的 `queue_update` 帶空值。[after: 3.5]

## 4. 前端

- [x] 4.1 設定頁「一般」新增兩組控制，落實 Global auto-resume switch 與 Guard controls in the settings page：「額度用盡後自動繼續」開關（文案說明重置後會在無人看管時繼續執行），以及「額度守門」開關加百分比輸入（守門關閉時輸入停用；儲存後伺服器回錯誤則顯示錯誤並還原為上次接受的值）；`GENERAL_DEFAULTS` 補新欄位，沿用 `vscodeNoAdmin` 的即時 PUT 寫法。驗證：以臨時實例切換並重新整理後仍保持；輸入 150 顯示伺服器錯誤並還原；`node scripts/frontend-layering.test.cjs` 通過。[after: 2.2, 2.3]
- [x] 4.2 Waiting banner（橫幅畫面已於 2026-11-19 在臨時實例複驗，見 plan §8；`scripts/quota-banner-fixture` 已移除，改以 e2e 測試與該紀錄為證）：聊天畫面依 `quota_resume_state` 顯示：`waiting` 為「額度用盡，預計 HH:MM 自動繼續」與取消鈕（送 `quota_resume_cancel`），`exhausted` 為「已達自動繼續上限，請手動繼續」（無取消鈕），重連／重新整理後仍顯示，回 `none` 後消失。驗證：臨時實例以假額度來源（見 5.1）把重置時間縮短，目視確認 `waiting` 出現、重新整理後仍在、按取消後消失且佇列仍暫停；`exhausted` 情境另以測試資料確認文案；`node scripts/frontend-layering.test.cjs` 通過。[after: 3.7]

## 5. 整合驗證與文件

- [x] 5.1 端到端驗證：以臨時實例與可注入的假額度來源，把 `resets_at` 縮到數十秒，走完「用盡 → 橫幅 → 到期 → 自動重跑 → 成功」「仍滿 → 延後」「`waiting` 時重啟伺服器 → 仍會繼續」「`resuming` 時重啟 → 退回 `waiting` 後繼續」「關開關 → 橫幅消失且不動」，並在設定頁調整守門門檻確認下一則排隊訊息的暫停行為改變。驗證：`go build ./...`、`go vet ./...`、`go test`（排除 `poc/`）、`go test ./internal/arch/` 全過，並把觀察結果寫進 `docs/plan/todo/quota-auto-resume.md`。[after: 3.4, 3.6, 4.1, 4.2]
- [x] 5.2 文件：更新 `CHANGELOG.md`、`config.example.yaml`（`quota_guard_percent` 改註明為初始值）、`docs/plan/todo/quota-auto-resume.md` 的狀態機與守門設定，以及已知限制（歷史會出現重複的 user 訊息、只涵蓋 `session` 視窗），並同步 `docs/plan/todo.md`。驗證：內容審閱，確認與實作行為一致、沒有殘留「尚未實作」字樣。[after: 5.1]

