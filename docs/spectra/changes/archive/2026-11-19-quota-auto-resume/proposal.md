## Why

5 小時額度視窗用盡時，任務失敗、佇列暫停，使用者必須回來手動按「繼續」；被中斷的那則訊息還已從佇列取出，單純解除暫停會把它默默丟掉。使用者希望額度重置後由伺服器自動接著跑，並且可以開關。

另外，額度守門（自動啟動佇列前的 90% 暫停）目前只能改 `config.yaml` 且要重啟，使用者要求它能在設定頁調整與開關。自動復原牽涉多個階段與重啟，狀態必須完整、明確地存進 DB，不能只靠幾個隱含欄位。

## What Changes

- 新增全域開關 `autoResumeOnQuota`（存伺服器 DB 的一般設定，**預設關**），設定頁「一般」新增對應開關。
- 自動復原的狀態機明確存進 DB（`quota_resume_state`：`none`／`waiting`／`resuming`／`exhausted`），每個轉移在 spec 與 design 列表；輪詢以條件式更新搶佔 `waiting → resuming`，避免重複觸發；重啟時卡在 `resuming` 的 session 退回 `waiting` 並立即重新確認額度，不會遺失。
- 任務失敗收尾時，若開關開啟且額度快照顯示 `session` 視窗已滿（`percent ≥ 100`）且有可解析的重置時間，進入 `waiting`、記錄預計恢復時間、把失敗的那則訊息排回佇列最前面並暫停佇列，聊天畫面顯示「額度用盡，預計 HH:MM 自動繼續」橫幅（可取消）。
- 伺服器新增 30 秒輪詢：到期後先重新確認額度已回復，才透過內部 WebSocket 通道送出 `quota_resume`，沿用現有 `Resume()` 繼續執行；額度仍滿則延後到新的重置時間。
- 同一個停滯最多連續自動重跑 3 次，之後進入 `exhausted` 並交還手動處理，避免誤判造成無限迴圈。
- 取消條件：使用者按「繼續」、清空／刪除佇列、刪除 session、直接送新訊息、按橫幅取消、關閉開關；一律回到 `none`。
- 額度守門改為可調整：新增一般設定 `quotaGuardEnabled`（開關）與 `quotaGuardPercent`（門檻），設定頁可調，執行期每次自動啟動前讀取、不需重啟；`config.yaml` 的 `quota_guard_percent` 只作為尚未在設定頁儲存過時的初始值。守門的判斷規則（fail-open、略過無 percent 視窗、使用者直接送出與按「繼續」不受影響）不變。
- 任務清單第一項為阻擋項：先真的撞一次 5 小時上限取樣，確認判定條件成立才寫判定邏輯。

## Non-Goals

- 不處理週額度（`weekly`）：重置要數天，仍走手動「繼續」。
- 第一版不讓「額度守門」暫停自動復原；它是使用者自己設的停手點，意圖與撞牆不同。
- 第一版不加 Telegram 通知，橫幅即可。
- 不做每個 session 各自的開關，只有全域一個（自動復原與額度守門皆然）。
- 不靠解析錯誤文字判定額度用盡（真實字串會隨 CLI 版本變動）。
- 這裡的「狀態機」只指自動復原的狀態，不重新設計 session 的 `THINKING`／`STREAMING`／`IDLE` 狀態。

## Capabilities

### New Capabilities

- `quota-auto-resume`: 額度用盡後自動復原的開關、判定、持久化狀態機、輪詢恢復、取消與防迴圈行為。
- `quota-guard-settings`: 額度守門的開關與門檻存於伺服器設定、設定頁調整、執行期生效。

### Modified Capabilities

（無；既有規格的需求都不變。）

## Impact

- 後端：`internal/ws/handler.go`（失敗收尾判定、`quota_resume` 訊息）、`internal/ws/dispatch.go`（沿用 `Resume()`）、`internal/ws/quota_guard.go`（改為執行期讀設定）、`internal/db/`（`sessions` 新增 `quota_resume_state`、`quota_resume_at`、`quota_resume_attempts`，條件式轉移函式，`EnqueueFront`，啟動時復原，`GeneralSettings` 新欄位）、`internal/api/settings.go`（驗證與回傳有效值）、`internal/server/quota_resume.go`（新輪詢，仿 `internal/server/room.go`）、`internal/config/config.go`（`quota_guard_percent` 退為初始值）。
- 前端：`internal/static/js/ui/SettingsModal.js`、`internal/static/js/core/core.js`（一般設定開關與守門控制）、聊天佇列橫幅與取消按鈕。
- 文件：`docs/plan/todo/quota-auto-resume.md`、`docs/plan/todo.md`、`config.example.yaml`、`CHANGELOG.md`。
- 風險：開啟自動復原後會在使用者不在場時自動執行，而專案多以 `bypassPermissions` 跑代理，因此預設關。
