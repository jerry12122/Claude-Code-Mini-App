# 功能計劃：檔案總管（唯讀瀏覽 work_dir）

> 狀態：規劃中（尚未實作）
> 建立日期：2026-11-18
> 來源：`todo.md`「檔案總管功能 (瀏覽 work_dir 檔案)」原本只有一行，沒有規格；以下為 supervisor 補的最小規格，**假設都標在第 4 節，使用者可推翻**。
> 目標：在手機上不必請 agent 幫忙，就能看某個 session 的工作目錄有哪些檔案、讀一個文字檔內容。

## 1. 範圍（最小、唯讀）

- `GET /sessions/:id/files?path=<相對路徑>`：列出該 session `work_dir` 底下某目錄的項目（`name`、`is_dir`、`size`、`mtime`），目錄在前、名稱排序；`path` 空值代表根。
- `GET /sessions/:id/files/content?path=<相對路徑>`：回傳單一檔案內容。僅文字檔、上限 512 KB；二進位回 415、過大回 413（附實際大小）。
- 前端：session 選單加「瀏覽檔案」，開唯讀對話框（麵包屑 + 清單 + 文字預覽）。**不提供**上傳、刪除、重新命名、編輯、下載。

## 2. 安全約束（本功能最大風險，必須有對抗性測試）

- 路徑必須限制在該 session 的 `work_dir` 內：拒絕絕對路徑、含 `..`、Windows 磁碟機代號與 UNC、含 NUL；解析後用 `filepath.EvalSymlinks` 再比對一次前綴，擋住 symlink／junction 跳出 `work_dir`。
- 授權：僅已登入的擁有者可用。**分享聊天室訪客與 snapshot 訪客一律 403**（訪客畫面本來就刻意不顯示 `work_dir`，見 `internal/api/share.go`）。
- 不跟隨指向 `work_dir` 外的 symlink，也不在清單中洩漏其目標路徑。
- 預設隱藏規則：不特別隱藏 dotfile，但**永不回傳**內容的檔名樣式：`.env*`、`*.pem`、`*.key`、`id_rsa*`、`config.yaml`（本專案設定檔含 `bot_token`、密碼）。清單可顯示其名稱但標記為受保護，內容請求回 403。
- 回應不得含 `work_dir` 的絕對路徑，只用相對路徑。
- 大目錄分頁：單次最多 500 項，超過回傳 `truncated: true`。

## 3. 完成標準

- 單元／整合測試（RED 先行）：正常列目錄與讀檔；`..`、絕對路徑、`C:`、UNC、NUL、符號連結跳出、junction 跳出皆被拒；受保護檔名 403；二進位 415；過大 413；訪客 403；未登入 401；不存在 404；目錄當檔案讀回 400。
- `go build`、`go vet`、`go test`（排除 `poc/`）、`go test ./internal/arch/`、`node scripts/frontend-layering.test.cjs` 全過。
- 對抗性複核：supervisor 親自嘗試繞過（URL 編碼 `..%2f`、雙重編碼、大小寫混用、尾端點與空白、8.3 短檔名、符號連結），並記錄結果。
- 用臨時實例實測：登入後能瀏覽、能預覽、`config.yaml` 預覽被拒。

## 4. 假設（待使用者確認）

1. 唯讀，不做寫入。要寫入屬另一個、風險高得多的功能。
2. 範圍限單一 session 的 `work_dir`，不能瀏覽整機磁碟。
3. 只預覽文字檔；圖片預覽、下載另案。
4. 受保護檔名清單如上，不做可設定。

## 5. 不做

編輯、上傳、刪除、下載、搜尋、Git 狀態、跨 session 瀏覽、整機磁碟。

## 6. 風險

- 瀏覽功能讓「能登入」的人讀到 `work_dir` 內所有文字檔；它的權限不超過現有的 shell 模式，但比 agent 對話更容易被快速掃過，所以受保護檔名與訪客 403 不能省。
- Windows 路徑特性多（短檔名、保留名稱 `CON`／`NUL`、備用資料流 `file.txt:stream`），需在測試與複核中涵蓋。
