## Why

Claude session 在 `bypassPermissions` 下，命中 `~/.claude/settings.json` 等處 `permissions.ask` 規則（例如 `Bash(rm -rf *)`、`Bash(curl *)`）的工具呼叫，仍會轉給 miniapp 跳出「允許／拒絕」面板並阻塞等人按。使用者希望有一個「完全跳過授權」的開關：開啟後，後端對這類詢問自動回允許，相當於後端自動按下允許，不再跳面板、不等人（例如手機不在身邊、長任務過夜）。

## What Changes

- 新增全域一般設定 `claudeAutoAllowAsk`（存伺服器 DB，**預設關**），設定頁「一般」新增開關；開啟時需二次確認，文案明講風險。
- 開關開啟時，`permission_mode=bypassPermissions` 的 Claude run 收到的 ask 規則詢問由後端直接回允許：不寫待決權限、不改 session 狀態為等授權、不廣播授權面板、不發 Telegram 授權通知。
- 每次自動允許都留下稽核紀錄：在該輪回覆中附一行說明（工具名稱與指令摘要）並寫 server log，不靜默放行。
- 開關在每次詢問當下讀取，切換不需重啟；不改已跳出的面板。
- 不受影響：`deny` 規則仍會擋（不經授權通道）、`AskUserQuestion` 提問仍要人回答、非 `bypassPermissions` 的模式維持現有授權流程、其他 agent（Cursor／Codex／Kiro／Antigravity）不動。

## Non-Goals

- 不做每個 session 各自的開關，只有全域一個。
- 不做「允許並記住」或規則編輯 UI；不改 `permissions.ask` 規則本身。
- 不涵蓋 `default`／`acceptEdits` 模式下的授權（那是「允許此操作」重跑流程，語意不同）。
- 不處理 `deny` 規則與 `AskUserQuestion`。
- 不為自動允許另外發 Telegram 通知。

## Capabilities

### New Capabilities

- `claude-ask-auto-allow`: 全域開關控制 bypassPermissions 下 ask 規則詢問由後端自動允許，含稽核紀錄與適用範圍限制。

### Modified Capabilities

（無；既有規格的需求都不變。）

## Impact

- 後端：`internal/ws/claudeperm.go`（`claudeAsker.ask` 增加自動允許分支與稽核回呼）、`internal/ws/handler.go`（`runAgent` 內註冊授權通道處，傳入本輪權限模式與稽核回呼）、`internal/db/general_settings.go`（`GeneralSettings` 新欄位）。
- 前端：`internal/static/js/ui/SettingsModal.js`、`internal/static/js/core/core.js`（一般設定開關與確認）。
- 文件：`docs/plan/todo/claude-permission-prompt.md`（補開關說明）、`docs/plan/todo.md`、`CHANGELOG.md`。
- 與 `quota-auto-resume` change 同樣修改 `GeneralSettings` 與設定頁「一般」區塊，合併時可能衝突；兩者互不依賴。
- 風險：開啟後 ask 規則形同虛設（那些規則原本就是使用者想「要先問我」的指令，如 `rm -rf`、`curl`），因此預設關並要求確認。
