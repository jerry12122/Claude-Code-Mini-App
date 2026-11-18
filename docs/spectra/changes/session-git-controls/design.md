## Context

- `internal/gitinfo`（L1，已在 `arch_test.go` 的 `execAllowed` 內）目前只回傳分支名稱：先讀 `.git/HEAD`，失敗才 spawn `git rev-parse`，結果以絕對路徑快取 15 秒。`api` 在 `/sessions` 列表與 `enrichGitBranch` 呼叫它，前端 `GitBranchBadge` 顯示 `session.git_branch`。
- `api/open.go` 的 `sessionWorkDir` 已能由 session id 取得並驗證 `work_dir`（不存在、未設定都有明確錯誤）。
- 訪客路由是「預設拒絕」：`guestRouteAllowed` 只放行明列的路徑，新路由自動不開放給訪客，不需改動。
- session 狀態（`idle`／`running`／`awaiting_confirm`／`awaiting_input`／`awaiting_shell_confirm`）存在 DB，`api` 可直接讀。
- 使用者主要用手機，沒有終端機可處理合併衝突或輸入帳密，所以操作要保守：寧可明確拒絕，也不要留下需要人工收拾的半完成狀態。

## Goals / Non-Goals

**Goals:**

- 在 session 的聊天室內看得到：目前分支、與上游的領先／落後提交數、未提交增刪行數。
- 能切換到既有本機分支、`pull`（僅快轉）、`push`（僅既有上游）。
- 任何失敗都回傳 git 自己的訊息，不卡住、不留下半完成狀態。

**Non-Goals:**

- 不做 commit、stage／unstage、stash、merge、rebase、reset、建立或刪除分支、force push、`push --set-upstream`、tag、diff 檢視、log 瀏覽。
- 不切換到只存在於遠端的分支（不自動建立追蹤分支）。
- 不做 `fetch` 單獨按鈕：領先／落後數字反映本機已知的遠端狀態；`pull` 會順帶更新。使用者若要看最新遠端狀態，請先 `pull`。
- 不處理帳密：沿用伺服器主機上 git 既有的憑證設定，不提供輸入帳密的介面。
- 不在 `/sessions` 列表加入同步狀況或未提交數（維持現有只有分支名稱，避免對每個目錄多跑昂貴的 `git status`）。
- 不開放訪客使用。
- 不做背景輪詢或檔案監聽；狀態只在聊天室掛載、Git 選單展開、agent 回到閒置與操作成功後取得。

## Decisions

### 狀態查詢用單一 `git status --porcelain=v2 --branch -z`

一次指令同時取得 `branch.head`、`branch.upstream`、`branch.ab`（領先／落後）與所有檔案項目，輸出格式不隨語系變動，`-z` 避開路徑引號問題。替代方案：分開跑 `rev-parse`、`rev-list --left-right --count`、`status --short` ——多 spawn 兩次，且三次之間狀態可能不一致。

- 設定環境變數 `GIT_OPTIONAL_LOCKS=0`，讓唯讀查詢不要為了刷新索引而取得 `index.lock`，避免與 agent 正在執行的 `git` 互相干擾。
- 檔案清單最多回傳 200 筆，超過時 `truncated=true` 並回報 `total`，避免未被忽略的大型目錄（如 `node_modules`）讓回應膨脹。
- 一筆檔案以布林欄位表示類別：`staged`、`unstaged`、`untracked`、`conflict`；同一檔案可同時 `staged` 與 `unstaged`。前端不需要認識 `XY` 兩字元代碼。

### 寫入操作用固定指令，分支名稱先比對再使用

| 操作 | 實際指令 | 備註 |
|---|---|---|
| 切分支 | `git switch -- <name>` | `<name>` 必須與 `git for-each-ref refs/heads` 的某筆完全相同才執行，否則 400；不接受 `-` 開頭 |
| pull | `git pull --ff-only` | 無法快轉就由 git 拒絕，不產生 merge commit |
| push | `git -c remote.<remote>.mirror=false push --no-follow-tags -- <remote> HEAD:<upstream-ref>` | 先以 `for-each-ref` 解析目前分支的上游 remote 與完整 ref；明確指定目的地與 refspec，禁止 mirror 及額外 tag |

**Supersedes**: `session-git-controls` 原先以 `push.default=upstream` 限制推送的決策。該設定會被 `remote.<name>.push` 蓋過，且不能阻止 mirror 或 followTags；改以明確上游 remote/refspec 加覆寫 mirror、停用 followTags。驗收須以真實 bare 遠端確認其他分支、tag 與較新遠端提交不被覆寫。依據：[git-push](https://git-scm.com/docs/git-push)。

替代方案：開放任意 `git` 子指令或用現有 `shell` allowlist——攻擊面大且與「簡單」目標不符。`git switch` 需 git ≥ 2.23（2019），目前不為更舊版本寫退路。

### pull、push 先檢查上游

沒有上游（含 detached HEAD）時，後端在 spawn 前就回 422 與明確訊息，不倚賴 git 錯誤文字，也避免 `push.autoSetupRemote` 之類的使用者設定讓 push 悄悄建立遠端分支。

### 非互動執行與逾時

所有 git 子進程設 `GIT_TERMINAL_PROMPT=0`、`GCM_INTERACTIVE=never`，缺憑證時直接失敗而不是等輸入；沿用 `proc.SysProcAttr()` 避免桌面版跳黑窗；用 `exec.CommandContext` 加 `cmd.WaitDelay`，逾時後不被子進程（ssh、credential helper）握著管線而卡住。逾時：查詢 10 秒、切分支 30 秒、pull／push 90 秒。逾時訊息要與一般失敗區分。

替代方案：沿用 `gitinfo` 現有 2 秒逾時——`status` 在大型 repo 與 Windows 上會誤判逾時，且 pull／push 本來就是網路操作。

### 忙碌時拒絕寫入，且以工作目錄為單位

寫入操作前，若「任何 `work_dir` 解析後與此 session 相同的 session」狀態不是 `idle`，回 409。只看自己這個 session 不夠，因為同一個 repo 常有多個 session。這是查完即用（check-then-act）的檢查，不是鎖：極短的視窗內 agent 仍可能在檢查後啟動，這是已知取捨（見風險）。另外對每個絕對路徑用互斥鎖序列化寫入操作，`TryLock` 失敗回 409「已有 git 操作進行中」，避免兩個請求同時搶 `index.lock`。

### 切分支成功後讓分支快取失效

`gitinfo.Branch` 快取 15 秒；切分支成功後主動清掉該路徑的快取，標題列徽章與側欄才不會顯示舊分支。前端面板自己用剛取得的狀態更新，不依賴快取過期。

### 路由與認證

五條路由掛在既有 `authMiddleware` 之後，不限本機位址（與 `open-vscode` 不同：那是在伺服器桌面開視窗，這裡是回傳資料）：

- `GET  /sessions/:id/git/status`
- `GET  /sessions/:id/git/branches`
- `POST /sessions/:id/git/checkout`　body `{"branch": "<name>"}`
- `POST /sessions/:id/git/pull`
- `POST /sessions/:id/git/push`

### 行內 Git 控制與增刪徽章

**Supersedes**: `session-git-controls` / 原 Implementation Contract「前端行為」與已完成 task 3.1、3.2 的 Git 面板決策。使用者取消彈窗與檔案清單，改由分支徽章按鈕與深色分支 popover 與 Pull／Push popover；歷史任務保留，新增 5.1、5.2 補償實作。

`GET .../git/status` 在原回應上加入 `additions`、`deletions` 非負整數。保留單一 porcelain 查詢函式供寫入前置檢查；HTTP 狀態查詢另呼叫 `gitinfo.ChangeStats` 使用 `git diff --numstat -z --no-ext-diff --no-textconv HEAD --` 計算已追蹤檔案相對 HEAD 的淨差異。未建立第一個提交時與空 tree 比較。二進位 `-` 記錄不計行數。另依「未追蹤文字檔計入新增行數」將未追蹤文字檔納入徽章。正確解析包含 tab、換行、rename 的路徑；錯誤沿用 422／504／500，不偽造零值。不增加 `/sessions` 列表成本。

### 未追蹤文字檔計入新增行數

**Supersedes**: session-git-controls / 行內 Git 控制與增刪徽章（task 5.1 中排除未追蹤檔案的決定）。

Git 從工作樹根目錄列舉未追蹤且未忽略的檔案，使用對空檔的 numstat 判定文字行數與二進位，與已追蹤檔案的淨差異合計。行數查詢共用既有查詢期限，逾時或失敗沿用 Git 錯誤。此查詢不修改工作樹或暫存區。

### 沿用原有徽章與深色選單

**Supersedes**: `session-git-controls` /「行內 Git 控制與增刪徽章」的原生 select 決策及已完成 task 5.2。使用者指出原生選單與過大的分支控制破壞既有設計語言；分支按鈕沿用 `GitBranchBadge`，同步、增刪與 Git 按鈕採相同的小型徽章比例。分支與 Git popover 沿用 `ContextMenu` 的 ra-raised、ra-line-strong、ra-active 配色、圓角與項目排版；目前分支有勾選記號，長名稱截斷、清單可捲動。原生 popover 保留 Escape、外點關閉及 Tab 操作，不引入依賴。

## Implementation Contract

**回應形狀**

`GET .../git/status` → 200：

```json
{
  "is_repo": true,
  "branch": "main",
  "detached": false,
  "upstream": "origin/main",
  "ahead": 2,
  "behind": 0,
  "additions": 12,
  "deletions": 3,
  "files": [{"path": "a.go", "staged": false, "unstaged": true, "untracked": false, "conflict": false}],
  "total": 1,
  "truncated": false
}
```

- `work_dir` 不是 Git 工作樹 → 200 `{"is_repo": false}`；沒有上游時 `upstream` 為空字串且 `ahead`、`behind` 為 0；detached HEAD 時 `detached=true`、`branch` 為短 hash。
- session 不存在 404；未設定或不存在的 `work_dir` 400（沿用 `sessionWorkDir`）。

`GET .../git/branches` → 200 `{"current": "main", "branches": ["dev", "main"]}`（僅本機分支，依名稱排序）。

`POST .../git/{checkout,pull,push}` → 成功 200 `{"ok": true, "output": "<git 輸出，最多 4096 位元組>"}`。

**錯誤**

| 情況 | 狀態 | `error` 內容 |
|---|---|---|
| session 忙碌，或同目錄已有 git 寫入進行中 | 409 | 說明原因的中文訊息 |
| 分支名稱為空或不在本機分支清單 | 400 | 同上 |
| 沒有上游（pull、push） | 422 | 同上 |
| git 以非零代碼結束（衝突、無法快轉、遠端拒絕、缺憑證…） | 422 | git 輸出的最後一段，另附完整 `output`（最多 4096 位元組） |
| git 執行逾時 | 504 | 說明逾時秒數 |
| 找不到 `git` 執行檔 | 500 | 說明需在伺服器 PATH 安裝 git |

**前端行為**

- 以 `GitControls.js` 取代 `GitPanel.js`。聊天標題列顯示沿用 GitBranchBadge 的分支按鈕、同步徽章 `↑N ↓N`、變更徽章綠色 `+N`／紅色 `-N`，以及獨立 Git 按鈕。手機與桌面皆能使用，訪客維持唯讀分支徽章且不查詢 Git API。
- 不再渲染 Git dialog、檔案清單、上游說明或「同步數字依本機已知的上游狀態顯示。」；增刪徽章的 title 說明以 HEAD 為基準，並計入未追蹤文字檔。
- Git 按鈕展開非 modal 的原生 `popover="auto"`，只放 Pull／Push 兩個操作；使用原生按鈕群組與 Tab，不假裝 ARIA menu。Popover 使用 CSS anchor positioning，缺少支援時以不超過 20 行的定位邏輯緊貼按鈕並限制於視窗內，不新增 polyfill。
- 分支選單選中另一個既有本機分支就呼叫 checkout；分支名與同步、變更數在成功後更新。失敗保留原值並以標題列內的 `role="alert"` 顯示錯誤。
- 非 `idle`／即時狀態非 `IDLE` 或寫入中，分支切換、Pull、Push 停用並提供原因；無上游／detached 時 Pull、Push 停用。操作中避免重複送出；完成／錯誤訊息不使用彈窗。
- 每個 header 共用一份 `useGitStatus`，避免手機與桌面兩份 CSS 版面各發查詢。掛載、Git 選單展開、agent 從忙碌轉 `IDLE` 與操作成功後刷新，沒有輪詢。

**驗收**

- `go test ./internal/gitinfo/ ./internal/api/` 通過；測試用 `t.TempDir()` 建立真實 repo 與 bare 遠端，找不到 `git` 時 `t.Skip`。
- `go test ./internal/arch/` 與 `node scripts/frontend-layering.test.cjs` 通過，且未新增例外條目。
- 手動：開啟有未提交修改、領先遠端 1 個提交的 repo，同步徽章與 `git status -sb` 一致，增刪徽章等於 `git diff HEAD --numstat` 加上未追蹤、未忽略文字檔的新增行數；切到另一分支後側欄徽章在下一次列表更新內顯示新分支。

**範圍邊界**

- 範圍內：上列五條路由、`gitinfo` 的讀寫函式、行內徽章、分支選單與 Git 操作 popover、`gitinfo` 快取失效、文件更新。
- 範圍外：Non-Goals 所列各項；`/sessions` 列表欄位不變；WebSocket 協議不變；資料庫 schema 不變。

## Risks / Trade-offs

- [忙碌檢查與 agent 啟動之間有空窗，agent 可能在切分支途中開始改檔] → 視窗為毫秒級；寫入操作持有每目錄互斥鎖；切分支遇到未提交修改時 git 本身會拒絕，不會覆蓋檔案。
- [同一個 `work_dir` 的 agent 若在 CLI 內自行跑 git，不經過本服務，檢查不到] → 只能以 session 狀態為準，文件註明。
- [pull 成功後工作樹內容改變，agent 的既有對話上下文可能已過時] → 屬使用者決定，面板不自動通知 agent。
- [push／pull 以登入使用者身分代用主機上的 git 憑證] → 已限定為固定指令、無 force、無任意引數；若使用者的登入 token 外洩，攻擊者最多能對已設上游的目前分支做快轉式 push。這與既有「能對 agent 下指令」的權限相比不更大，但須在 README 說明。
- [`git status` 在超大型 repo 可能超過 10 秒] → 回 504 並提示；不做背景快取。
- [Windows 上 `git` 子進程樹無法被 `CommandContext` 完全終止] → 以 `WaitDelay` 保證請求能返回；殘留的 ssh 進程由其自身逾時結束。

## Migration Plan

無資料遷移。新增路由與前端檔案，回滾即還原這些檔案；既有 API 與資料格式不變。
