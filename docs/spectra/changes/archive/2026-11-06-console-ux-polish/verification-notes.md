# 試用版驗證紀錄

日期：2026-11-06。分支：`ux/console-polish`。保留既有元件、composer 排列與 32px 控制，不搬用 prototype。

## 已執行

- `node --test scripts/*.test.cjs`：68/68 通過；輸出保存在本機 `.spectra/test-builds/console-ux-polish/node-tests.txt`。
- `go test ./internal/static`：通過。
- `go test -tags production ./internal/static`：通過。
- 新增測試確認分頁方向鍵、Home／End、首尾循環、空群組與關閉焦點 fallback；授權送出失敗保留操作面板，成功才清除。授權保護以記憶體中的反向條件變異確認會失敗，未將變異寫入來源。
- 瀏覽器走查使用當前正式 SessionTabBar、ChatSessionHeader、ui-atoms、core 與 index CSS 的隔離 React 頁面；未連線後端。實際 ChatView 的失敗與草稿行為由 Node 測試及原碼檢查確認，隔離頁沒有掛載整個 ChatView。
- 桌面實際鍵盤操作：A → ArrowRight → B；Tab 到關閉；Enter 關閉後焦點到 C；關閉全部後回會話導覽。標題以 Enter 展開，完整路徑可見。空名稱為「未命名」，HTML 字串維持文字，訪客不新增完整絕對路徑曝光。
- 390×844 觸控模擬、680px 窄欄、720×480 等效重排：無頁面橫向溢出，code 局部橫捲，select／附件／送出仍為 32px。既有 ModeToggleBtn 為 28px，未修改。720×480 是 1440×960 的 200% 等效 viewport，沒有實際操作瀏覽器 200% 縮放。
- 雙主題 canvas 色彩量測：用量文字最低 5.02:1；狀態標籤最低 4.797:1。保留背景，只修正不足文字色階。
- reduced-motion 模擬與來源檢查：新增的用量刷新動畫可停用，分頁切換及操作結果不等待動畫。尚未完整測試所有操作狀態的 reduced-motion 流程。
- 對照本次修改前基準：selectCompactClass、ra-cmd-bar 尺寸與 script 載入順序未改；沒有新增依賴，也沒有本次後端／API／WS 修改。工作樹含其他未提交開發，未將其當成本次修改。

## 每個 scenario／example 的分類

「有測試」包含已執行的 Node 斷言或上述隔離頁的瀏覽器操作檢查；瀏覽器檢查的覆蓋範圍僅為已載入的正式元件。

| Scenario / Example | 分類 | 證據或排除理由 |
| --- | --- | --- |
| Reach a shared icon action without a mouse | 有測試 | 瀏覽器鍵盤聚焦標頭／分頁關閉動作，檢查名稱與 outline；共享 CSS 覆蓋兩主題。 |
| Composer receives keyboard focus | 有測試 | 真實 ChatView textarea 名稱斷言；瀏覽器隔離 composer 聚焦後外框可見。 |
| Use supported selectors on a phone | 排除 | 本次保留單一 composer 的尺寸、間距與排列，屬單畫面外觀；已比對來源與 390px 渲染。模型／角色可用性流程沒有改動，未以隔離頁宣稱完整 ChatView 整合測試通過。 |
| Enlarge a narrow interface | 排除 | 單畫面縮放及重排外觀；檢查 viewport 已解除限制、720px 等效重排可見輸入／送出。實際瀏覽器縮放待試用。 |
| Read a long work directory | 有測試 | 正式標題元件透過 Enter 展開並檢查完整路徑；另檢查空名稱、文字注入與訪客分支。 |
| Read a long code line in a split group | 排除 | 未修改的單畫面捲動／溢出外觀；680px 隔離欄實測局部 code 橫捲、頁面無溢出，未掛載完整 SplitLayout。 |
| Read usage and state across themes | 有測試 | console-usability 測共享狀態／用量分支；瀏覽器以實際雙主題前景背景量測對比均達 4.5:1。 |
| Edit a draft while disconnected | 有測試 | ux-reliability 保護既有草稿／離線來源契約；原碼確認 canQueue 與 sendOrToast。未在本次隔離頁重現真實斷線。 |
| An authorization action cannot be delivered | 有測試 | console-usability 執行正式 allow／deny handler，失敗保留 pending tools，成功清除；變異檢查通過。 |
| Reduced motion keeps state understandable | 有測試 | 收尾時在 reduced-motion 下執行正式 QuotaBadge 的刷新／失敗／完成 props 狀態；斷言動畫為 none、刷新時停用／失敗與完成可重試、可讀名稱與用量文字保留。失敗提示包含重試說明。這是正式元件測試，未連真實用量服務。 |
| Navigate a tab group with wraparound | 有測試 | tab-groups 對應表格，瀏覽器實際方向鍵選取與焦點。 |
| Navigation target selection（全部五列） | 有測試 | tab-groups 逐列 A/B/C 首尾、Home／End、單頁的獨立預期值。 |
| Leave a tab group using Tab | 有測試 | 瀏覽器 Tab 到作用中關閉按鈕；inactive tab／close 的 tabindex 為 -1。 |
| Close an active tab with siblings | 有測試 | 正式元件 B 關閉後 C 聚焦；console-usability 測同群組與群組卸載 fallback。 |
| Close the final open tab | 有測試 | 瀏覽器最後回導覽；console-usability 測無剩餘群組及卸載時無目的地不拋錯。 |

外觀排除不列為缺口；初版記錄的 reduced-motion 元件執行測試缺口已在下列收尾檢查補齊。全部 scenario 與 example 有測試或具體排除理由，實機平台限制另列。

## 試用限制

未啟動試用 EXE 或第二個正式服務；未修改使用者 DB／config。實體手機、Telegram WebView、讀屏、實際瀏覽器 200% 縮放及桌面啟動相容性待使用者試用。瀏覽器僅有既有 Tailwind CDN 與 Telegram swipe 版本警告，未見 JavaScript 執行錯誤。

試用前需從系統匣退出原程式，避免兩個程式同時使用現有服務埠。試用 EXE 與現有設定放在相同 repo 根目錄；直接啟動 `claude-miniapp-desktop-ux.exe`。

## 收尾驗證與程式審查（2026-11-06）

使用者已更新修正版並要求收尾。修正版為 `claude-miniapp-desktop-ux-header.exe`；桌面狀態與分支緊接標題。瀏覽器正式標頭測得短標題至狀態間距 8px、狀態至分支間距 8px；680px 下長標題寬 505.45px、內容寬 1680px，確實截斷，分支右緣 652px，無頁面溢出。此單畫面排列依測試範圍排除單元斷言，以實際渲染量測驗證。

### 檢查範圍與限制

Spectra snapshot `0815eaf088bfd318941ba9fa5039041b41e68174`，來源 `touched_tracking`、狀態 resolved。基準 `6b87ddb7531e95b15f3d19921084182f6d40267d` → `60a6b31607f7937261c5631ebeb478522ca6e6f1`，另含尚未提交的對齊修正。已執行 check-snapshot 確認當時來源未變。

已檢查 8 個正式來源：index.html、ChatView.js、chat-header.js、core.js、tabGroups.js、SessionView.js、SessionTabs.js、ui-atoms.js；及 console-usability、tab-groups 兩個測試檔。ChatView、chat-header、SessionView、ui-atoms 在任務前已有修改，檔案追蹤不能代表整檔歸屬；以保存的任務前基準與實際提交差異區分。其他即時活動、釘選與傳輸修改不納入本次提交或品質結論。

### 規格對應

| Requirement | 實作位置 | Scenario 測試 | Example 值 | 執行證據 |
| --- | --- | --- | --- | --- |
| Keyboard-visible and named shared controls | index.html:59、ChatView.js:1427、chat-header.js:119 | 上表前兩項；標題、分頁與 composer 焦點檢查 | 無規格表格 | Node 與正式元件瀏覽器操作 |
| Compact controls and zoom retain access to controls | index viewport 與原有 composer 樣式 | 單畫面尺寸／重排排除，已量測 390／680／720px | 無規格表格 | 保留來源值及瀏覽器量測；實際 200% 縮放 not-run |
| Long content remains readable in both themes | SessionIdentity、core 狀態色、ui-atoms 用量色 | 長路徑展開、空值／訪客／純文字、雙主題對比；code 外觀排除 | 無規格表格 | 正式元件瀏覽器操作、Node 分支斷言、雙主題最低對比 4.797:1 |
| Failure feedback preserves recoverable input | ChatView 原有 sendOrToast、allow／deny；ui-atoms reduced-motion | 草稿與失敗回復 Node、授權 handler、刷新三狀態瀏覽器檢查 | 無規格表格 | 測試通過，保留既有傳輸邏輯 |
| Keyboard navigation and focus within a tab group | tabGroups.js:666、SessionTabs.js:208 | 循環／Home／End、Tab 至關閉 | A/B/C 的五列表格逐列對應 tab-groups.test.cjs | Node 與正式元件鍵盤操作 |
| Closing a tab preserves a valid keyboard destination | SessionTabs.js:4、SessionView 導覽入口 | 同群組、卸載後其他群組、最後一頁與無目標 | 無規格表格 | console-usability 斷言及瀏覽器關閉操作 |

### 本次執行結果

- **passed-current**：`node --test scripts/*.test.cjs`，當前工作區 68/68；輸出 `.spectra/test-builds/console-ux-polish/node-close-tests.txt`。工作區包含其他開發，故另於僅有本次提交內容的快照執行測試，結果另列於收尾提交證據。
- **passed-current**：`go test ./internal/static` 與 `go test -tags production ./internal/static`，均通過。
- **passed-current**：Chrome 隔離頁正式 QuotaBadge，`prefers-reduced-motion: reduce`。刷新：42%、disabled=true；失敗：42%、disabled=false、tooltip「讀取失敗，請重試」；完成：35%、disabled=false。三者 animationName=none 且皆有刷新可讀名稱。
- 先前正式元件的鍵盤、雙主題、390px 與等效縮放為同一會話的執行紀錄；對齊修改後已重新量測 1440px 及 680px 標頭。未將原型或靜態閱讀當作執行測試。
- **not-run**：實體手機、Telegram WebView、讀屏與實際瀏覽器 200% 縮放。使用者表示已更新，未將這句話解讀為所有平台驗證通過。

### 結論

完整度：6/6 任務、6 項需求皆找到實作；所有 scenario／example 已分類。正確性、效率、簡化與重用、專案慣例四個面向皆已審查，未發現本次範圍內可重現的程式缺陷；無 Critical 或 Warning。視覺與平台證據的限制如上，不擴張為整個應用或其他未提交功能的保證。可依預覽結果歸檔，將兩份增量規格各套用一次。

收尾提交快照（僅含本次修改）：`node --test scripts/*.test.cjs` 66/66 通過，兩種 static Go 測試通過；另存本機 `.spectra/test-builds/console-ux-polish/node-close-isolated-tests.txt`。歸檔成功且無 cleanup_warnings。核心產生的 @trace 原先誤列其他 dirty 檔案，已將本次新增需求的追蹤註解限定到實際 UX 來源與測試，需求內容未改。
