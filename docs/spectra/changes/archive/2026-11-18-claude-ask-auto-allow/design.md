## Context

來源與現況核對：`docs/plan/todo/claude-permission-prompt.md`，並對照程式碼：

- `bypassPermissions` 下命中 `permissions.ask` 規則的工具呼叫，Claude CLI 會經 `--permission-prompt-tool` 轉給 miniapp 的 MCP 端點，最後進到 `internal/ws/claudeperm.go` 的 `claudeAsker.ask`；它取得每 session 一格的信號量後呼叫 `askUser`（定義在 `internal/ws/handler.go` 的 `runAgent` 內），`askUser` 登記待決權限、把 session 狀態改成等授權、廣播 `permission_request`、發 Telegram 通知，然後阻塞到使用者按 `allow_once`／`deny_once` 或 run 被取消。
- 目前所有 Claude 模式都使用這個授權通道（`handler.go` 註解：「All Claude modes use the prompt bridge for both permissions and AskUserQuestion」），所以通道存在不代表是 `bypassPermissions`；本輪權限模式在 `runAgent` 內的 `pm := s.PermissionMode`。
- `AskUserQuestion` 走 `claudeAsker.askQuestion`，與工具授權是兩條獨立路徑；`deny` 規則在 Claude CLI 內直接擋下，不經授權通道。
- 一般設定 `GeneralSettings` 以單列 JSON 存在 `settings` 表，缺欄位即預設值；`/settings/general` 只經 `authMiddleware`。訪客路由由 `guestRouteAllowed` 白名單控制，白名單只列出 `/guest/me`、`/model-options/:agentType` 與綁定自己 session 的路由，沒有設定路由，所以訪客本來就無法讀寫一般設定。
- `askUser` 內已有「`rctx` 取消即視為拒絕」的處理，`claudeAsker.dropAnswered` 負責剔除已回答過的 denial，避免已答過又被重問。

## Goals / Non-Goals

**Goals:**

- 一個全域開關，開啟時 `bypassPermissions` 的 Claude run 的 ask 規則詢問由後端直接回允許。
- 每次自動允許都可稽核（聊天回覆一行＋server log）。
- 切換不需重啟；預設關。

**Non-Goals:**

- 每 session 開關、「允許並記住」、規則編輯 UI、改 `permissions.ask` 規則。
- `default`／`acceptEdits` 模式、`deny` 規則、`AskUserQuestion`、其他 agent。
- 自動允許的 Telegram 通知。

## Decisions

### 開關存在 GeneralSettings
`GeneralSettings` 加 `ClaudeAutoAllowAsk bool`（JSON `claudeAutoAllowAsk`），預設 `false`；缺欄位即 `false`，不需遷移。選一般設定而非 `config.yaml`：要能在設定頁即時切換，且啟動後無需重啟。每次詢問時讀取（單列 SQLite 查詢，成本可忽略），不快取，確保「切換立刻生效」。替代方案是每 session 一個欄位，需求明確要全域一個，否決。

### 自動允許只在 claudeAsker 內判斷
在 `claudeAsker.ask` 一進來就判斷，早於取得信號量與 `askUser`：條件成立就稽核並直接回 `(true, "")`。所以不碰 `pendingPerms`、不改 session 狀態、不廣播、不發通知——與「沒有任何待決詢問」等價，其餘流程完全不用改。`registerClaudeAsker` 新增兩個參數：`autoAllow func() bool`（`runAgent` 傳入「本輪 `pm == "bypassPermissions"` 且開關為真」的閉包）與 `audit func(toolName, summary string)`。本輪權限模式用建立時的 `pm`，不在中途重讀，避免同一輪行為前後不一致。替代方案是在 `askUser` 內判斷，會需要先登記再撤銷待決權限、且訊息順序易出錯，否決。

### 稽核紀錄不靜默放行
`audit` 由 `runAgent` 實作：寫 `slog.Info`（含 session 識別、工具名稱、摘要），並以與既有 `appendNotice` 相同的路徑把一行說明附加到本輪回覆（落 DB＋即時廣播）。摘要規則：`Bash` 取 `input.command`，其他工具取壓縮後的 input JSON，皆截斷到 200 字元並加省略標記。`appendNotice` 定義在任務 goroutine 內、晚於授權通道註冊，所以 `audit` 以後綁定的函式變數傳入（goroutine 內設定後才生效；尚未設定時只寫 log，不遺失允許行為）。取捨：摘要可能含 token 等機敏字串，但同樣內容原本就會顯示在授權面板與 Claude 的回覆裡，不新增暴露面。

### 開啟需要二次確認
前端在開關由關變開時顯示確認對話框（沿用專案既有確認元件／原生 `confirm` 的寫法，實作時對照現有慣例），文案說明「ask 規則原本是要先問你的指令，開啟後會直接放行」，確認後才 PUT。關閉不需確認。這是唯一可以讓 ask 規則形同虛設的設定，因此多一步摩擦是刻意的。

## Implementation Contract

**Behavior**

- 開關關：行為與現在完全相同。
- 開關開 ∧ 本輪 `bypassPermissions` ∧ 收到 ask 規則詢問：不出現面板、不阻塞、指令照常執行；本輪回覆多一行「已自動允許 `<工具>`：`<摘要>`」，server log 有對應紀錄。
- 其他情況（非 `bypassPermissions`、`AskUserQuestion`、`deny` 規則、其他 agent）完全不變。

**Interface / data shape**

- `GeneralSettings.ClaudeAutoAllowAsk`，JSON `claudeAutoAllowAsk`，經既有 `/settings/general` 整包讀寫。
- `registerClaudeAsker(sessionID, runCtx, askUser, askQuestion, autoAllow, audit)`（新增後兩個參數；現有呼叫端與測試一併更新）。
- 稽核行文字與 log 欄位：工具名稱、摘要（≤ 200 字元＋省略標記）、session 識別。

**Failure modes**

- 讀設定失敗：視為關（走原本的詢問），並寫 log；寧可多問，不可誤放行。
- `audit` 尚未可用或附加回覆失敗：仍允許（使用者已選擇跳過授權），但 log 一定要寫。
- 開關開啟瞬間已在等待的面板：保持等待，不被自動答覆。

**Acceptance criteria**

- 單元測試（`internal/ws`，沿用 `claudeperm_test.go` 風格）：spec 的適用範圍表每一列（含 `default`／`acceptEdits` 不自動、`AskUserQuestion` 仍走提問、開關關仍詢問）、自動允許不觸碰 `pendingPerms` 與 session 狀態、稽核回呼收到工具名稱與截斷後摘要（500 字元輸入）、讀設定失敗時走詢問、同一輪中途開啟後第二個詢問自動允許而第一個仍等待。
- `internal/db` 測試：`ClaudeAutoAllowAsk` 缺欄位為 false、往返不變；`internal/server` 測試：訪客 token 讀寫 `/settings/general` 被拒。
- `go build`、`go vet`、`go test`（排除 `poc/`）、`go test ./internal/arch/`、`node scripts/frontend-layering.test.cjs` 全過。
- 實測（臨時實例、暫存 `CLAUDE_CONFIG_DIR` 設 `ask: ["Bash(curl *)"]`、`bypassPermissions`）：開關關 → 出現面板；開關開 → 不出現面板、`curl --version` 照常執行、回覆含稽核行；`AskUserQuestion` 仍跳提問；`deny` 規則仍擋。

**Scope boundaries**

- 範圍內：開關儲存與設定頁、確認對話框、`claudeAsker` 自動允許與稽核、測試與文件。
- 範圍外：每 session 開關、「允許並記住」、`default`／`acceptEdits` 授權、`deny` 規則、`AskUserQuestion`、其他 agent、Telegram 通知。

## Risks / Trade-offs

- [開啟後 ask 規則形同虛設，`rm -rf`、`curl` 這類原本要先問的指令會直接執行] → 預設關、二次確認、每次自動允許都有稽核行與 log；使用者仍可用 `deny` 規則做硬性禁止。
- [稽核行寫進回覆會被當成模型輸出的一部分，歷史內容混入系統說明] → 沿用既有 `appendNotice` 的呈現方式，與其他說明一致。
- [讀設定失敗時退回詢問，可能在資料庫異常時多跳面板] → 刻意選擇安全方向。
- [與 `quota-auto-resume` change 同改 `GeneralSettings` 與設定頁「一般」區塊] → 欄位互不相干；後合併者需手動合併 `GENERAL_DEFAULTS` 與一般區塊的新增控制，tasks 內已標註。

## Migration Plan

純加設定鍵：缺欄位即預設 `false`，部署後行為不變；回滾只需關開關（或回舊版，設定鍵被忽略）。

## Open Questions

以下於提案階段採用的假設，使用者可在實作前推翻：

1. 只作用於 `bypassPermissions` 的 Claude run；若也要涵蓋 `default`／`acceptEdits`（等於把所有授權自動允許），需另行確認，語意已接近 `bypassPermissions`，不建議。
2. 全域一個開關，不做每 session。
3. 稽核行附加在回覆內，而非獨立的系統訊息類型。
4. 訪客不能改設定是既有路由白名單的結果，不需新增規則；本 change 只補測試。
