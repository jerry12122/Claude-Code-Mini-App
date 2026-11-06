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
| Reduced motion keeps state understandable | 未覆蓋缺口 | 已模擬偏好並確認不用動畫等待；尚無跨完整操作狀態的執行測試。後續驗證可在 reduced-motion 下執行刷新／失敗／完成並斷言文字與可用控制。 |
| Navigate a tab group with wraparound | 有測試 | tab-groups 對應表格，瀏覽器實際方向鍵選取與焦點。 |
| Navigation target selection（全部五列） | 有測試 | tab-groups 逐列 A/B/C 首尾、Home／End、單頁的獨立預期值。 |
| Leave a tab group using Tab | 有測試 | 瀏覽器 Tab 到作用中關閉按鈕；inactive tab／close 的 tabindex 為 -1。 |
| Close an active tab with siblings | 有測試 | 正式元件 B 關閉後 C 聚焦；console-usability 測同群組與群組卸載 fallback。 |
| Close the final open tab | 有測試 | 瀏覽器最後回導覽；console-usability 測無剩餘群組及卸載時無目的地不拋錯。 |

外觀排除不列為缺口；另有一項 reduced-motion 執行測試缺口，未阻擋這次試用建置，也未宣稱已完整驗證。

## 試用限制

未啟動試用 EXE 或第二個正式服務；未修改使用者 DB／config。實體手機、Telegram WebView、讀屏、實際瀏覽器 200% 縮放及桌面啟動相容性待使用者試用。瀏覽器僅有既有 Tailwind CDN 與 Telegram swipe 版本警告，未見 JavaScript 執行錯誤。

試用前需從系統匣退出原程式，避免兩個程式同時使用現有服務埠。試用 EXE 與現有設定放在相同 repo 根目錄；直接啟動 `claude-miniapp-desktop-ux.exe`。
