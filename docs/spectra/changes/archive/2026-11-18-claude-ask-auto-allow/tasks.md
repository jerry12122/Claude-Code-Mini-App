## 1. 設定儲存

- [x] 1.1 「開關存在 GeneralSettings」：`GeneralSettings` 新增 `ClaudeAutoAllowAsk`（JSON `claudeAutoAllowAsk`，預設 false），落實 Global auto-allow switch 的儲存面，並確認訪客無法讀寫 `/settings/general`。驗證：`internal/db/general_settings_test.go` 補「無列時為 false」「舊 JSON 缺欄位為 false」「寫入後讀回 true」；`internal/server` 測試以訪客 token 讀與寫 `/settings/general` 皆被拒（Guest cannot change it）。**注意**：`quota-auto-resume` change 的 2.2／2.3 也改同一個結構體，後合併者需手動合併欄位與測試。

## 2. 後端自動允許

- [x] 2.1 「自動允許只在 claudeAsker 內判斷」落實 Automatic allow for ask-rule prompts 與 Scope limits：`registerClaudeAsker` 新增 `autoAllow` 與 `audit` 參數，`claudeAsker.ask` 在取得信號量與呼叫 `askUser` 之前，若 `autoAllow()` 為真就呼叫 `audit` 並直接回允許；`runAgent` 傳入「本輪 `pm == "bypassPermissions"` 且每次詢問時讀到的 `claudeAutoAllowAsk` 為真」的閉包，讀設定失敗視為關。驗證：`internal/ws` 單元測試涵蓋 spec 適用範圍表六列（`default`／`acceptEdits` 不自動、`AskUserQuestion` 仍走提問、開關關仍詢問、開關開且 `bypassPermissions` 自動允許）、自動允許不呼叫 `askUser`（不觸碰 `pendingPerms`、不改 session 狀態、不廣播）、讀設定失敗時走詢問、同一輪第一個詢問等待中途開啟後第二個詢問自動允許而第一個面板保持等待。[after: 1.1]
- [x] 2.2 「稽核紀錄不靜默放行」落實 Audit trail for automatic allows：`runAgent` 實作 `audit`——寫 `slog.Info`（session 識別、工具名稱、摘要）並以與 `appendNotice` 相同的路徑把一行說明附加到本輪回覆；摘要規則為 `Bash` 取命令、其他工具取壓縮 input JSON，截斷至 200 字元並加省略標記；`appendNotice` 尚未可用時只寫 log、仍允許。驗證：單元測試涵蓋「`curl --version` 的稽核行含 `Bash` 與命令」「500 字元命令被截斷為 200 字元加標記」「非 Bash 工具使用 input JSON 摘要」「附加回覆不可用時仍允許且寫 log」。[after: 2.1]

## 3. 前端

- [x] 3.1 「開啟需要二次確認」：設定頁「一般」新增「Claude 完全跳過授權」開關，由關變開時先顯示確認對話框（文案說明 ask 規則原本要先問你、開啟後會直接放行），確認才 PUT，關閉免確認；`GENERAL_DEFAULTS` 補欄位，沿用 `vscodeNoAdmin` 的即時 PUT 寫法。驗證：以臨時實例操作——取消確認時開關維持關且不送 PUT、確認後重新整理仍為開；`node scripts/frontend-layering.test.cjs` 通過。**注意**：與 `quota-auto-resume` 的 4.1 同改一般區塊與 `GENERAL_DEFAULTS`，後合併者需手動合併。[after: 1.1]

## 4. 實測與文件

- [x] 4.1 端到端實測：臨時實例搭配暫存 `CLAUDE_CONFIG_DIR`（`permissions.ask` 設 `Bash(curl *)`）、`bypassPermissions` 的 Claude session，真實 `claude -p`。驗證：開關關 → 出現授權面板；開關開 → 不出現面板、`curl --version` 照常執行、回覆含稽核行、server log 有紀錄；`AskUserQuestion` 仍跳提問；`deny` 規則仍擋；`go build ./...`、`go vet ./...`、`go test`（排除 `poc/`）、`go test ./internal/arch/` 全過，並把觀察寫進 `docs/plan/todo/claude-permission-prompt.md`。[after: 2.2, 3.1]
- [x] 4.2 文件：`docs/plan/todo/claude-permission-prompt.md` 補「完全跳過授權」開關的說明與風險、`CHANGELOG.md`、`docs/plan/todo.md`。驗證：內容審閱，確認與實作行為一致、沒有殘留未實作字樣。[after: 4.1]
