## 狀態：已取消（非已完成）

2026-11-13：依使用者決定，本變更不再實作。以下 10 項任務全部取消，保留未勾選狀態作為歷史紀錄，不再列入待執行工作。

<!-- 本變更嚴格遵守 Non-Goals 範圍：不更動現有 Telegram Mini App 的使用者操作介面外觀、功能與 WebSocket 通訊協定，不拆解 internal/ws/handler.go 業務領域分層，不更動 Agent Runner 的呼叫模式與權限旗標。 -->

## 1. 前端建置工具：採用 Vite + React 18 + Tailwind CSS (Goals: 前端建置現代化)

- [ ] 1.1 在 web/ 目錄建立基於 Vite、React 18 與 Tailwind CSS 的前端建置設定（web/package.json, web/vite.config.js, web/tailwind.config.js），確認執行 npm --prefix web run build 能夠成功編譯並產出 web/dist 目錄。
- [ ] 1.2 將 internal/static/js 內的 19 個 JavaScript 腳本轉換並重構為標準 ES Module 架構至 web/src/，移除全域 window 依賴與解構語法，確保 npm --prefix web run build 編譯檢查無報錯。[after: 1.1]
- [ ] 1.3 遷移 internal/static/index.html 至 web/index.html，完全移除對 @babel/standalone、Tailwind Play CDN、React 18 Dev 等 6 個外部 CDN 連結，改由 Vite 入口引入，驗證離線模式下能正確載入樣式與所有對話 UI 元件。[after: 1.2]

## 2. 靜態資源內嵌：採用 Go embed.FS (Goals: 消除外部 CDN 依賴)

- [ ] 2.1 在 internal/server 引入 Go embed.FS 內嵌 web/dist 靜態產物，替換原先以實體相對路徑 ./internal/static 提供靜態檔案的機制，確保編譯出的二進位執行檔在不含外部檔案時能獨立提供網頁服務。[after: 1.3]
- [ ] 2.2 在 build.bat 與相關建置說明中增加前端預先編譯指令（npm --prefix web run build），並執行 go test ./internal/server/... 驗證路由端點能正常回傳 200 與 HTML 內容。[after: 2.1]

## 3. 資料庫遷移：基於 PRAGMA user_version 的順序版本化遷移 (Goals: 資料庫版本化)

- [ ] 3.1 於 internal/db/ 實作基於 PRAGMA user_version 的版本遷移框架（internal/db/migrations.go），提供循序執行升級交易與版本註冊介面，並以單元測試 TestVersionedMigrations 驗證空資料庫可連續升級且版本號正確更新。
- [ ] 3.2 將既有 db.migrate() 中所有 CREATE TABLE 與歷次欄位變更收斂固化為 Baseline (Version 1)，移除開機時以字串匹配忽略錯誤的 tryAlter 循環，並以 TestBaselineMigration 驗證包含現存結構的資料庫不會重覆執行已套用之遷移。[after: 3.1]

## 4. 執行期資料隔離：規範 data/ 目錄與平滑向下相容 (Goals: 產物目錄隔離與環境清理)

- [ ] 4.1 調整 config.example.yaml 與 internal/config 的資料庫預設路徑至 data/claude-miniapp.db，並在 internal/db/db.go 加入向後相容檢測：若 data/ 不存在 DB 但根目錄存在舊 DB，則自動載入既有檔案並輸出日誌警示。
- [ ] 4.2 調整 internal/api/upload.go 的檔案上傳儲存路徑至 data/uploads/，取代原本存於 internal/static/uploads 的模式，並透過單元測試 TestUploadPathIsolation 驗證上傳檔案落地於獨立目錄。
- [ ] 4.3 更新 .gitignore 忽略 data/、bin/、本機執行產物與 NVIDIA Corporation/ GPU 日誌目錄，並刪除已無程式碼的 internal/gemini/ 空資料夾，使用 git status --porcelain 驗證工作目錄維持乾淨。
