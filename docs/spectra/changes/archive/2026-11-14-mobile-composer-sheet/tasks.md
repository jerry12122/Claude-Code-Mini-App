## 1. 工具內容與手機入口

- [x] 1.1 依「單一工具內容與常駐選檔器」先整理 ChatView 的既有工具 JSX 與 handlers：只掛載單份有效工具、fileInputRef 對應 input 移至工具容器之外並持續掛載，桌面順序、select id、設定草稿及附件按鈕行為保持原樣。在 scripts/composer-attachment.test.py 以本地 React、ReactDOM、go.mod 已有 esbuild 的 Go CLI（go run github.com/evanw/esbuild/cmd/esbuild，產物只存暫存目錄）與 stub 的 hooks／網路／無關子元件載入實際 ChatView，讓測試執行產品 handlers，取代只檢查複製 markup 的 fixture。驗證：python scripts/composer-attachment.test.py 通過桌面選檔、input／select 唯一性、取消後重選同檔及原有焦點回歸。
- [x] 1.2 [after: 1.1] 完成「手機工具面板與焦點分離」、Explicit mobile tools entry、Composer action row hosts model, effort, and permission controls 及 Chat header no longer duplicates the controls：手機 Plus 打開獨立 toolsOpen 的原生 dialog；依「面板內單選清單取代手機原生下拉」及 Settings use in-sheet single-choice pages 呈現標題、Close、遮罩與整列設定／單選頁按 design.md 呈現；控制項沿用 Agent／guest／shell／busy／斷線等限制，保留 danger 權限色及下次送出套用，關閉返回 Plus。保持手機 textarea 依內容增高、取消失焦 44px 上限，桌面仍內嵌。驗證：python scripts/composer-attachment.test.py 覆蓋 Plus、focus／blur 不自動開關、Close／遮罩、Claude／Codex／Cursor／訪客／shell／唯讀矩陣、單選返回／當前值勾選、載入失敗保留當前值、busy 禁用及送出前 WS 不新增設定訊息。

## 2. 選檔與返回行為

- [x] 2.1 [after: 1.2] 完成「選檔取消保留面板與成功返回草稿」及 Native picker lifecycle preserves attachment drafts：同步 click 開啟常駐 input、cancel／空清單維持面板、非空且允許的結果先建 chips 再關面板與串行上傳；手機此路徑不自動 focus，桌面貼上／拖放行為維持。過期 session 或失去權限的選檔結果不得上傳到新會話，錯誤可重試移除、文字保留。驗證：python scripts/composer-attachment.test.py 使用實際 handleFilesPicked／uploadFiles、受控的 upload 回應及 file chooser，覆蓋取消再選、重選同檔、多檔順序、第二檔失敗／重試、上傳前關閉、選檔期間切 session 與變唯讀、以及不誤送訊息／不彈鍵盤。
- [x] 2.2 [after: 2.1] 完成「關閉返回與生命週期清理」的返回部分及 Accessible dismissal consumes only the tools interaction：dialog 的命名、背景 inert、焦點約束與返回 Plus；Escape 消費當次事件，避免 app.js 連按 Escape 關閉分頁；Browser Back 只消耗面板自己的 history 標記，重複 Plus 不堆疊，Forward 不重開，Telegram BackButton 綁定與可見狀態可還原；原生 picker 取消或單選頁返回不誤關面板。驗證：python scripts/composer-attachment.test.py 覆蓋 Tab／Shift+Tab、Escape 事件不抵達全域關頁 handler、返回不換頁、重複開關與 Forward、Telegram stub 訂閱清理與單選頁先返回首頁、選單操作不觸發遮罩及關閉後焦點／草稿保留。

## 3. 版面與完整驗收

- [x] 3.1 [after: 2.2] 完成 Sheet state survives focus and viewport changes、Sheet lifecycle retains existing control restrictions、Compact controls and zoom retain access to controls 及「關閉返回與生命週期清理」的切換／卸載部分：手機 query 開啟期間鎖定、鍵盤改高度不關面板、手機旋轉仍可操作、轉桌面與session／唯讀／interrupt-only／卸載正確收尾並還原捲動鎖及 handlers，保留各會話草稿；按 Design Source 套用雙主題、觸控區、安全區、內容捲動與固定可見 Close。驗證：python scripts/composer-attachment.test.py 覆蓋 390×844 直向、844×390 粗指標橫向、1280×900 桌面（測試 fixture 尺寸），高度改變、query 邊界、200% 等效重排／文字放大、模態背景無捲動、無橫向溢出、附件 chip 不隱藏及生命週期清理。
- [x] 3.2 [after: 3.1] 對全部 delta spec 場景建立任務／測試對照並完成回歸，保持附件驗證、草稿恢復、下次送出設定及桌面操作；執行 python scripts/composer-attachment.test.py、node --test scripts/remaining-ux.test.cjs scripts/console-usability.test.cjs scripts/theme.test.cjs、go test ./internal/static、go test -tags production ./internal/static。以 iOS Safari、Android Chrome 與 Telegram Mini App 實際驗證鍵盤後 Plus、單選設定、附件取消／成功／失敗及返回，在本 change 的 verification.md 記錄設備／版本、執行結果與未驗證項；無實機時明確留下實機驗收缺口，不以桌面模擬宣稱實機通過。

