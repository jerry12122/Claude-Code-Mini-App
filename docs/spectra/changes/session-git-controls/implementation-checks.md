# 本次實作與預覽紀錄（2026-11-17）

## 行內控制修訂

最終進度 8/9：本次新增 task 5.1／5.2 已完成。最終 `go test ./cmd/... ./internal/...` 與 `go vet ./internal/gitinfo/ ./internal/api/` 通過；4.1 仍保留原本全目錄 POC 編譯阻擋。預覽已更新至 `preview-inline-v2.exe`，bundle HTTP 200。

依使用者六點回饋更新：不再渲染 Git dialog／檔案清單／同步說明；標題列改成原生分支 select、`↑N ↓N` 同步徽章、綠 `+N`／紅 `-N` 文字增刪徽章，以及獨立 Git 按鈕的 Pull／Push popover。歷史 task 3.1／3.2 保留，補償任務 5.1／5.2 追蹤本版。

- `go test ./internal/gitinfo/ ./internal/api/`：通過，涵蓋相對 HEAD 的已追蹤文字淨差異、SHA-1／SHA-256 unborn、特殊路徑／rename、binary／untracked 排除與統計失敗錯誤。
- `node scripts/git-panel.test.cjs`：通過，新增訪客不查詢、agent 轉 IDLE 刷新、換 session 不重複刷新、45 秒查詢預算。
- `python scripts/git-panel-ui.test.py`：通過，直接渲染實際 ChatSessionHeader／GitControls／hook，驗證 `main`、`↑1 ↓1`、`+2043`、`-3`、無 dialog／同步說明、分支直選、Pull 成功刷新、Push 錯誤保留、忙碌／guest、Enter／Tab／Escape／外部點擊與手機定位；另外強制不支援 anchor 的定位退路並驗證選單緊貼按鈕。移除行內控制的暫時變異確實使測試失敗，還原後通過。
- `node scripts/frontend-layering.test.cjs`、`go test ./internal/arch/ ./internal/static/`、`go vet ./internal/gitinfo/ ./internal/api/`：通過，未新增例外。
- 預覽實測 `↑1 ↓0`、`+3 -1` 與 `git diff HEAD --numstat` 一致；Git 按鈕展開 Pull／Push；native select 選 dev 遇到修改時顯示 Git 原始錯誤、保持 main；DOM 中沒有 dialog。

上述兩個新增規格的所有情境都有自動測試；舊有遠端憑證／未登入與全目錄 POC 阻擋限制仍見下方第一版紀錄。增刪數按已追蹤文字 diff 計算，未追蹤與二進位檔不納入行數。

## 第一版紀錄

實作任務 1.1～3.2 已完成。4.1 的文件已更新，完整目錄測試仍受既有 POC 編譯錯誤阻擋，故保持未勾選；本次沒有修改該 POC。

## 已執行的檢查

- `go test ./cmd/... ./internal/...`：通過，涵蓋全部正式套件與後端架構檢查。
- `go vet ./cmd/... ./internal/...`：通過，無輸出。
- `go test ./internal/static/`：最終前端版本通過。
- `node scripts/git-panel.test.cjs`：查詢、錯誤保留、寫入、成功刷新、前端逾時預算、重疊操作及跨 session 過期回應通過。
- `python scripts/git-panel-ui.test.py`：Edge 實際渲染 390px 與 1280px，開啟面板、檔案分類、切分支、Pull 成功、Push 失敗、忙碌停用、關閉與焦點還原通過。另暫時移除忙碌防護，確認測試失敗，再還原原始檔並確認通過。
- `node scripts/frontend-layering.test.cjs`、`node scripts/dialog-focus.test.cjs`、`git diff --check`：通過；未增加架構例外。
- `spectra analyze session-git-controls --json`：無 Critical／Warning，五項補充範例 Suggestion。
- `spectra validate session-git-controls`：通過。
- `go test ./...`、`go vet ./...`：未通過。既有忽略目錄 `poc/session-instructions/main.go` 與 `browser-fixture.go` 重複宣告 `main`；正式套件結果均通過。

## 規格情境與測試對應

| 情境 | 自動測試 |
|---|---|
| 乾淨、無上游、detached、非 repo、本機分支排序 | `TestStatusAndLocalBranches`、`TestGitReadRoutes` |
| 檔案分類範例五列，包含同檔已暫存與未暫存、合併衝突 | `TestStatusFileCategories`；UI 同檔跨兩組另有瀏覽器斷言 |
| 領先 2／落後 1，超過 200 檔 | `TestStatusAheadBehindAndTruncation` |
| 不可用目錄／session、狀態回應與 HTTP 錯誤碼 | `TestGitReadRoutes`、`TestGitErrorResponses` |
| 切換成功、危險／未知分支、修改阻擋、立即清分支快取 | `TestSwitchAndCacheInvalidation`、`TestGitWriteRoutes` |
| Pull 快轉與分歧拒絕，Push 成功、拒絕、其他分支不變 | `TestPullFastForwardAndRejectDivergence`、`TestPushOnlyCurrentBranchAndRemoteReject` |
| 推送設定不能擴大範圍 | `TestPushConfigCannotExpandScope`：包含 mirror、force refspec、followTags、不同推送 remote |
| Pull／Push 無上游或 detached，不啟動網路指令 | `TestNoUpstreamAndConcurrentWrites`：包含 GIT_TRACE 斷言 |
| 自己／同目錄另一 session 忙碌、唯讀仍可用、同路徑寫入鎖 | `TestGitWriteRoutes`、`TestNoUpstreamAndConcurrentWrites` |
| 指令逾時、缺少 git、非互動環境、輸出上限 | `TestGitCommandErrors`、`TestGitRunnerEnvironmentAndOutputCap`、`TestGitErrorResponses` |
| 訪客 Git 路由禁止存取 | `TestGuestCannotReadGit`，五條路由均檢查 403 |
| 面板開啟、操作成功／失敗、忙碌控制、分支更新回呼 | `git-panel.test.cjs`、`git-panel-ui.test.py` |

未列為自動覆蓋的情境：未登入請求（已在預覽實測五條路由均回 401）；無已存憑證時的實際遠端認證失敗（僅驗證非互動環境）；訪客徽章純文字與完整聊天室 header／sidebar 刷新（前者已檢視條件，後者已實測切換後顯示新分支）。可補整合回歸測試；沒有將這些行為當作測試範圍排除。單一面板的間距、圓角、色彩屬局部視覺設定，依測試範圍準則排除自動斷言，已目視檢查。

## 預覽

`http://127.0.0.1:8081`，獨立設定、資料庫與本機 bare 遠端位於忽略目錄 `poc/session-git-preview/`。登入密碼 `git-preview-local`。本次啟動的程序 PID 記於該目錄 `server.pid`。

已實測：面板領先 1、三個未提交檔案與 `git status -sb` 相同；切換 dev 被 Git 拒絕且保留修改；另一 repo Pull 後落後數 1→0；成功切換後 header 與下次 session 列表取得新分支；忙碌時停用三種寫入、仍可刷新；手機無橫向溢出、關閉後焦點還原。

## 使用者視覺修訂（task 6.1）

- 原生 select 改為重用 `GitBranchBadge` 的按鈕；分支與 Git 操作使用同一組既有 ra 選單色彩、圓角與項目樣式，目前分支有勾選。徽章大小與本畫面間距依 TDD 範圍規則排除像素斷言，以實際畫面檢查。
- 先修改 `python scripts/git-panel-ui.test.py`，確認因找不到「切換分支」按鈕失敗；實作後在 390px、1280px 與強制無 anchor 支援三種環境通過，涵蓋目前分支標記、切換、Pull、Push 失敗、忙碌、訪客、外點、Escape 與 Tab。
- `node scripts/git-panel.test.cjs`、`node scripts/frontend-layering.test.cjs`、`go test ./internal/static/`、`git diff --check` 通過。沒有新增依賴或架構例外。
- 使用實際 8081 預覽目視檢查 1280×900 的分支與 Git 選單、390×844 的分支選單。手機 document.scrollWidth=390，分支清單左右界約 141／349px，位於視窗內。原先的大型金色分支框與系統白色下拉已移除。
- 已以 production 標籤建置 `preview-polish.exe`，只重啟獨立預覽程序；8081 bundle 回應 200。此次所有改動情境均有互動測試或局部視覺排除紀錄；既有整體驗證阻礙仍見 task 4.1。

## v0.20.0 發布檢查

- GitHub CI `44059510622` 在 Windows runner 的兩則錯誤案例逾時；原案例以不存在的子指令（含 5000 字元名稱）觸發 Git 額外的指令搜尋。改用 `rev-parse --verify` 的不存在 ref 與固定輸出 5000 字元後退出的 alias，避免測到指令建議機制。
- 保留真正的奈秒逾時案例、非互動環境檢查與 4096 位元組上限斷言，並明確要求失敗類型為 `failed`。正式 Git 操作與逾時設定未改動。
- 修正後 `go test ./internal/gitinfo/ -count=1` 通過。
