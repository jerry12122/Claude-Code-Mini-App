## Why

目前標題列只有一個唯讀的 Git 分支徽章（`GitBranchBadge`）。使用者用手機遠端操作時，想知道「Claude 改完之後哪些檔案還沒提交」「本機落後遠端幾個提交」，或要切分支、pull、push，只能請 agent 代跑 `git` 指令，既慢又耗額度，也看不到結構化結果。伺服器端已經有 `internal/gitinfo`（可 spawn `git`），補上少量固定指令的查詢與操作即可。

把未追蹤文字檔納入增刪徽章，才能看見剛建立、尚未暫存的檔案新增了多少行。

## What Changes

- 聊天標題列直接顯示分支、`↑1 ↓0` 同步徽章、綠色 `+N`／紅色 `-N` 變更徽章，不顯示「領先」「落後」或同步數字的說明文字。
- 點目前分支（例如 main）以原有 GitBranchBadge 開啟與應用程式一致的深色分支選單，選擇既有本機分支即切換。
- 獨立 Git 按鈕開啟含 Pull／Push 的原生 popover 操作群組；不使用 Git 面板或 dialog，也不顯示檔案清單。
- 增刪數合計已追蹤文字檔相對 HEAD 的淨差異與 Git 未忽略的未追蹤文字檔行數。二進位檔不計行數。
- 沿用既有登入、訪客禁止、同目錄忙碌限制、固定指令、非互動執行與逾時。Pull 僅快轉，Push 僅目前分支的既有上游、不 force。
- 操作成功更新徽章，失敗在標題列附近顯示 Git 訊息。初次開啟聊天室、Git 選單展開、agent 回到閒置與操作成功後刷新；不背景輪詢。

## Capabilities

### New Capabilities

- `session-git-controls`: session 工作目錄的 Git 狀態檢視（分支、同步狀況、未提交檔案）與切換分支、pull（僅快轉）、push（僅既有上游）操作，含忙碌與錯誤時的行為。

### Modified Capabilities

(none)

## Impact

- Affected specs: new `session-git-controls`
- Affected code:
  - New: internal/api/git.go、internal/api/git_test.go、internal/gitinfo/gitops.go、internal/gitinfo/gitops_test.go、internal/static/js/chat/GitControls.js、internal/static/js/hooks/useGitStatus.js
  - Modified: internal/server/server.go（註冊路由）、internal/static/js/chat/ChatHeader.js（徽章改為可點）、internal/static/index.html（載入新檔）、docs/spec/backend-architecture.md（`gitinfo` 職責描述）、README.md、README.zh-TW.md
  - Removed: internal/static/js/chat/GitPanel.js（前一版預覽彈窗）
- Dependencies: 不新增 Go 依賴；伺服器 PATH 內需有 `git`（既有需求）。
- Security: `push`／`pull` 使用伺服器主機上既有的 git 憑證設定；經登入的使用者即可觸發，等同代為執行這兩個固定指令。
