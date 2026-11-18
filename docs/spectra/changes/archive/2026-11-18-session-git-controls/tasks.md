## 1. 唯讀：Git status report 與 Local branch listing

- [x] 1.1 `internal/gitinfo` 新增 `gitops.go`，提供狀態查詢與本機分支清單兩個函式（Requirement: Git status report、Local branch listing）。狀態查詢依設計的「狀態查詢用單一 `git status --porcelain=v2 --branch -z`」只 spawn 一次 git，並設 `GIT_OPTIONAL_LOCKS=0`、`GIT_TERMINAL_PROMPT=0`、`GCM_INTERACTIVE=never`、`proc.SysProcAttr()` 與 10 秒逾時（`cmd.WaitDelay`，對應「非互動執行與逾時」）；回傳分支或 detached 短 hash、上游、領先／落後數、最多 200 筆檔案（每筆含 `staged`／`unstaged`／`untracked`／`conflict`）、`total`、`truncated`；非 Git 目錄回報 `is_repo=false`；分支清單只含 `refs/heads` 並依名稱排序。驗證：新增 `internal/gitinfo/gitops_test.go`，以 `t.TempDir()` 建真實 repo 與 bare 遠端（找不到 `git` 時 `t.Skip`），涵蓋 spec 的「file categories」表格五列、領先 2 落後 1、無上游、detached HEAD、非 repo、超過 200 檔；`go test ./internal/gitinfo/` 通過。
- [x] 1.2 新增 `internal/api/git.go` 的 `GitHandler`，實作 `GET /sessions/:id/git/status` 與 `GET /sessions/:id/git/branches`（回應形狀依設計「Implementation Contract」），以 `sessionWorkDir` 取得並驗證目錄（未設定或不存在回 400、session 不存在回 404），並在 `internal/server/server.go` 以 `authMiddleware` 註冊這兩條路由（對應設計「路由與認證」與 Requirement「Access restricted to signed-in non-guest users」）。訪客請求這兩條路徑回 403（`guestRouteAllowed` 對未列出的路徑預設拒絕，不需修改 `internal/server/guest.go`）。驗證：新增 `internal/api/git_test.go` 涵蓋 200／400／404 與非 repo 的 `is_repo=false`；在 `internal/server/guest_test.go` 新增一則「訪客 GET `/sessions/:id/git/status` 回 403」；`go test ./internal/api/ ./internal/server/` 通過。[after: 1.1]

## 2. 寫入：Switch、Fast-forward-only pull、Push

- [x] 2.1 `internal/gitinfo/gitops.go` 新增切分支、pull、push 三個函式，依設計「寫入操作用固定指令，分支名稱先比對再使用」：切分支前須與本機分支清單完全相同、不接受空字串或 `-` 開頭、執行 `git switch -- <name>`（Requirement: Switch to an existing local branch）；pull 執行 `git pull --ff-only`（Requirement: Fast-forward-only pull）；push 解析目前分支的既有上游 remote 與完整 ref，執行 `git -c remote.<remote>.mirror=false push --no-follow-tags -- <remote> HEAD:<upstream-ref>` 且不帶任何 force 旗標（Requirement: Push to the existing upstream）。依設計「pull、push 先檢查上游」，沒有上游或 detached 時在 spawn 前回傳可辨識的錯誤。逾時分別為 30／90／90 秒，輸出最多保留 4096 位元組；以每個絕對路徑一把互斥鎖搭配 `TryLock` 序列化寫入，取不到鎖回傳可辨識的「進行中」錯誤；切分支成功後依設計「切分支成功後讓分支快取失效」清除該路徑的 `Branch` 快取（Requirement: Branch lookup cache is invalidated by a switch）。驗證：`internal/gitinfo/gitops_test.go` 新增案例——切到存在與不存在／空字串／`-x` 分支、未提交修改阻擋切換且分支不變、pull 快轉成功與分歧被拒（無 merge commit）、push 成功與遠端拒絕、`push.default=matching` 且另一分支有未推送提交時只推目前分支、`remote.origin.push`／mirror／followTags 不能擴大範圍或強制覆寫、無上游兩者皆不 spawn 網路指令、兩個同路徑寫入第二個立即回「進行中」、切分支後立刻呼叫 `Branch` 得到新分支；`go test ./internal/gitinfo/` 通過。[after: 1.1]
- [x] 2.2 `internal/api/git.go` 新增 `POST /sessions/:id/git/checkout`（body `{"branch": ...}`）、`/git/pull`、`/git/push` 並在 `internal/server/server.go` 註冊。依設計「忙碌時拒絕寫入，且以工作目錄為單位」：只要有任一 session 的 `work_dir` 解析後與目標相同且狀態不是 `idle`，或 2.1 回報「進行中」，就回 409 且不執行 git（Requirement: Write operations are blocked while the working directory is busy）；錯誤對應依設計表格：分支名稱不合法 400、沒有上游或 git 非零結束 422（`error` 取 git 輸出最後一段並附 `output`）、逾時 504 並說明秒數、找不到 `git` 執行檔 500（Requirement: Non-interactive and time-bounded Git execution）；成功回 `{"ok": true, "output": ...}`。驗證：`internal/api/git_test.go` 新增案例——目標 session `running` 回 409、同目錄另一 session `awaiting_confirm` 回 409、`running` 時 status 與 branches 仍回 200、422／400／504 各一例、成功路徑之後 `GET /sessions` 的 `git_branch` 為新分支；`go test ./internal/api/ ./internal/server/` 通過。[after: 1.2, 2.1]

## 3. 前端：Branch badge opens the Git panel

- [x] 3.1 新增 `internal/static/js/hooks/useGitStatus.js`（只含狀態、取得與操作呼叫，無 JSX，命名 camelCase）與 `internal/static/js/chat/GitPanel.js`（原生 `<dialog>`，比照 `ForwardModal.js` 以 `showModal()`／`close()` 管理，不加 `role="dialog"`、不自訂焦點陷阱）；`internal/static/index.html` 依序載入 hook、`GitPanel.js`、再載入 `ChatHeader.js`。（Requirement: Branch badge opens the Git panel）`ChatHeader.js` 讓非訪客且有 `session.git_branch` 的 `GitBranchBadge` 變成按鈕、點擊開啟面板；訪客仍是純文字。面板顯示分支、`↑領先 ↓落後`、依類別分組的未提交檔案（超過上限顯示「還有 N 個」）與重新整理按鈕；非 repo 時顯示說明。驗證：`node scripts/frontend-layering.test.cjs` 通過且未新增例外；手動以有未提交修改且領先遠端 1 個提交的 repo 開啟面板，數字與 `git status -sb` 一致，訪客分享連結中徽章不可點。[after: 1.2]
- [x] 3.2 面板加入本機分支下拉、Pull、Push 控制並呼叫 2.2 的路由：進行中時該按鈕停用；失敗時在面板內顯示 git 的訊息並保留先前狀態；成功後重新取得狀態；session 狀態不是 `idle` 時三個控制停用並顯示「session 閒置後才能執行」的說明。驗證：手動——session 執行中時控制為停用；對有未提交修改的 repo 切分支顯示 git 拒絕訊息且分支不變；對落後的分支 Pull 成功後 `↓` 變 0；`node scripts/frontend-layering.test.cjs` 通過。[after: 2.2, 3.1]

## 4. 文件與總驗證

本次驗證紀錄見 `implementation-checks.md`。`go test ./...`／`go vet ./...` 會被 git 忽略的本機目錄 `poc/session-instructions`（重複 `main`，不在版本庫內）阻擋，因此 4.1 的總驗證範圍改為排除 `poc/` 的 tracked 套件。

- [x] 4.1 更新 `docs/spec/backend-architecture.md` 的 L1 `gitinfo` 描述（由「分支查詢」改為「分支、狀態與簡單寫入操作」），並在 `README.md` 與 `README.zh-TW.md` 的功能列表加入 Git 行內控制與一句安全說明（使用伺服器主機既有的 git 憑證，僅快轉 pull、無 force push）。驗證：排除 `poc/` 後 `go test` 全數通過（含 `go test ./internal/arch/`，且 `internal/arch/arch_test.go` 的 `knownViolations` 與 `execAllowed` 沒有新增條目）、`node scripts/frontend-layering.test.cjs` 通過、排除 `poc/` 後 `go vet` 無輸出。[after: 5.1, 5.2]

## 5. 使用者修訂：行內 Git 控制

task 3.1、3.2 是已完成的前一版歷史；其彈窗與清單行為由「行內 Git 控制與增刪徽章」決策取代，新增以下補償任務，不回退原完成紀錄。

- [x] 5.1 新增 Uncommitted line counts：`gitinfo.ChangeStats` 與 `GET .../git/status` 的 additions/deletions，依「行內 Git 控制與增刪徽章」計算已追蹤文字檔相對 HEAD 的淨增刪，含 unborn、rename／特殊路徑與 binary 跳過，沿用錯誤處理。驗證：真實 repo 測試 staged＋unstaged 不重複、未提交新增／刪除、unborn、binary、未追蹤不計，以及 API JSON 欄位；`go test ./internal/gitinfo/ ./internal/api/` 通過。[after: 1.2]
- [x] 5.2 實作 Inline Git controls：刪除 GitPanel.js 與載入／測試依賴，ChatHeader.js 使用 GitControls.js 與共用 hook；顯示 main 原生分支 select、↑1 ↓0 與綠 +2043／紅 -3 徽章，獨立 Git 按鈕的 Pull／Push popover、無 modal／檔案清單／同步說明。保留忙碌、訪客、錯誤保留、操作成功刷新與分支快取契約。驗證：Node hook 測試、Edge 桌面／手機原生 select 與 popover／錯誤／忙碌／guest 測試、`node scripts/frontend-layering.test.cjs` 與 `go test ./internal/static/` 通過；重建並重啟獨立 8081 預覽。[after: 1.2, 2.2]

## 6. 視覺修訂：沿用原有徽章與深色選單

5.2 的原生 select 是完成歷史，由下列補償任務取代。徽章比例與單一畫面間距屬本畫面視覺驗證，不增加像素斷言。

- [x] 6.1 依「沿用原有徽章與深色選單」修訂 Inline Git controls：分支重用 GitBranchBadge 並開啟深色 popover、標示目前分支；分支與 Git 選單樣式統一、保留外點／Escape／Tab、忙碌與失敗處理。驗證：`python scripts/git-panel-ui.test.py`、`node scripts/git-panel.test.cjs`、`node scripts/frontend-layering.test.cjs`、`go test ./internal/static/` 通過；實際查看 8081 桌面與手機開啟選單的截圖並重建預覽。[after: 5.2]

## 7. 補計未追蹤文字檔

- [x] 7.1 未追蹤文字檔計入新增行數（Uncommitted line counts）。Git 狀態 API 與標題列加總未忽略的新文字檔行數，保留已追蹤的淨差異、二進位判斷和既有錯誤，並更新徽章提示。驗證 `go test ./internal/gitinfo/ ./internal/api/`、`node scripts/frontend-layering.test.cjs` 與徽章提示文案。[after: 5.1, 6.1]
