## Context

ChatView 目前在 .ra-composer-tools 放置 ModeToggleBtn、附件按鈕及原生 select 元件；手機 CSS 固定顯示第二排工具，textarea 在失焦時仍受 44px max-height 限制。handleFilesPicked 清空 file input 再交給 uploadFiles；附件草稿、串行上傳、錯誤與重試由 ChatView 管理。uploadFiles 目前會呼叫 focusChatInput，手機面板選檔路徑須避免這個自動聚焦副作用，桌面貼上及拖放則維持原有行為。

相關規格：composer-model-controls 定義控制項位置、Agent／訪客範圍及設定送出時機；console-usability 定義可及性與緊湊尺寸。本提案修改其中手機位置及尺寸契約，其餘沿用。app.js 有連按 Escape 關閉分頁的快捷鍵，面板必須優先處理自己的關閉事件。

## Design Source

來源：使用者於 2026-11-13 提供的 Claude App 截圖 IMG_5423.png、IMG_5424.png；原始路徑為 workspace/uploads/82be2299-4d6a-464b-aac0-7a8ce06f36fb/ 下的 c3ffa606b24d6e6be756845050a75813.png、79c3071ba0e46fa40a464de364336266.png。

快取：.spectra/design-cache/mobile-composer-sheet/IMG_5423.png 與 IMG_5424.png。目前核准的互動來源為 docs/prototypes/mobile-composer-sheet.dc.html（commit 8cc71b3），已快取到 .spectra/design-cache/mobile-composer-sheet/mobile-composer-sheet.dc.html；PNG 保留作早期參考。截圖只提供「＋入口、遮罩、底部圓角面板、分組動作」互動參考，沒有可讀的 CSS 尺寸或色碼；以下為本專案指定設計值，不宣稱是截圖量測值。快取不進版控，交接時本文的互動及數值表足以實作。

| 項目 | 指定值與來源 |
| --- | --- |
| 手機判斷 | 沿用現有 CSS 條件：(max-width: 639px), (max-width: 1023px) and (max-height: 500px) and (pointer: coarse)；既有斷點值 |
| 輸入列 | 順序：Plus、textarea、Stop（有執行中任務才顯示）、Send；Plus／Close／送出／停止的觸控區 44×44 CSS px，既有手機動作按鈕尺寸延用至新入口 |
| 面板 | 貼齊可用視窗底部、寬 100%、上角 radius 24px、下角 0px；內容自然高度，max-height: 70dvh，另受 Telegram 可用高度上限約束；這些是設計上限，不是效能估計 |
| 間距與滾動 | 面板 padding 16px，列間距 8px，設定列最小高度 44px；底部 padding max(16px, env(safe-area-inset-bottom))；標題及關閉鈕固定於面板頂部、內容區單獨 overflow-y: auto |
| 顏色 | 面板 oklch(var(--ra-card))，文字 oklch(var(--ra-t1))，分隔線 oklch(var(--ra-line-soft))；現有深／淺主題 token，不硬轉成 hex；遮罩 #000000 / 40%（指定值） |
| 圖示 | 既有 inline SVG 風格：Plus、X（Close）、Paperclip、既有 ModeToggleBtn、Stop、ArrowUp（Send），圖示 20×20px；不加圖示套件、不畫虛假的可拖曳把手 |
| textarea | 手機 max-height 沿用 min(160px, 30svh)，取消 :not(:focus-within) 的 44px 上限；依內容增高、超出內部捲動 |

## Goals / Non-Goals

**Goals:**

- 手機輸入列維持精簡，按「＋」才開工具面板；工具列可收合且附件操作不受失焦影響。
- 系統選檔、面板內單選清單、鍵盤收放及視窗變化都不誤關閉面板；保留附件、模式及設定的既有能力與安全限制。
- 支援鍵盤、觸控、雙主題、安全區及 Telegram Mini App；桌面維持內嵌工具列。

**Non-Goals:**

- 不重製 Claude 品牌外觀、語音功能、裝置選單、專案功能或相簿縮圖瀏覽；相簿／檔案選擇沿用系統選檔器，不新增相簿權限或 API。
- 不新增拖曳關閉、滑動分段高度、動態手勢函式庫或泛用 modal 框架；關閉由按鈕、遮罩及返回操作提供。
- 不改聊天標頭、其他彈窗、新會話表單、上傳協定、後端、草稿持久化格式或發版流程。

## Decisions

### 手機工具面板與焦點分離

ChatView 增加僅存在該會話記憶體的 toolsOpen 布林值，開啟的唯一使用者入口為「＋」。使用原生 dialog.showModal() 提供頂層顯示、背景 inert 與焦點約束；面板標題「聊天工具」以 aria-labelledby 關聯，Plus 具 aria-haspopup="dialog" 與 aria-expanded，開啟時聚焦關閉按鈕，允許 textarea 自然失焦。dialog 的 class 不得讓未開啟的 dialog 意外顯示；開啟不綁定 :focus-within。不在 blur 時強制 focus，不用全域 preventDefault 攔下按鈕及 select。

手機判斷使用上述既有 media query 並監聽變化；面板開啟時鎖定當次手機工具呈現，鍵盤／高度變化僅更新可用高度，不卸載面板。寬度進入至少 1024px 或 pointer 改為 fine 且寬度至少 640px 時，視為切換桌面並關閉面板、恢復內嵌工具列；其他 media query 變化待面板關閉後套用。旋轉後仍為手機版時面板維持開啟。

替代方案：原生 details 可提供開關，但缺少模態焦點與返回管理；繼續用 :focus-within 無法區分選檔與結束編輯，均不採用。

### 單一工具內容與常駐選檔器

控制項值及 handlers 留在 ChatView，工具 JSX 集中於同檔的局部 render 區塊；新增 ComposerTools.js 僅管理手機 dialog、返回及焦點，ui-atoms 內既有 selectors 增加可選 sheet 呈現參數，沿用同一份選項來源與 onChange，依版面只掛載一份：桌面在原 action row，手機在 dialog。fileInputRef 對應的唯一 file input 常駐於兩種工具容器外，面板開關、換模式及重排都不卸載它。工具順序為加入圖片／檔案、輸入模式、模型、思考強度、權限；隱藏不支援項目，桌面保持原順序。

重用 ModelSelect、EffortSelect、PermModeSelect、ModeToggleBtn 與既有 draft handlers；手機面板以整列 button 顯示標籤、當前值及向右箭頭。點進後在同一 dialog 顯示 fieldset／原生 radio 單選清單與勾選標記；選好後返回工具首頁並聚焦原入口，點當前值亦返回。返回按鈕、Escape、系統 cancel 與 Telegram BackButton 在清單頁先返回首頁，再次操作才關面板。瀏覽器 Back 在清單頁返回首頁並恢復一個面板歷史標記。候選項載入失敗時保留「預設」及目前值，不清掉草稿。danger 權限沿用琥珀色提示。Shell 模式保留模式切換，隱藏附件及 Agent 選項；可寫訪客只保留原本允許的附件動作。唯讀／分享結束／不支援的舊版 Kiro 等原本沒有 composer 的路徑不新增入口。忙碌、斷線、sending、draftRestoring 與 pending-send 防護仍由原條件決定；面板入口不繞過控制項停用，仍保留可關閉能力。

替代方案：複製一整組手機 selectors 會增加重複 id、請求與狀態差異，因此採用單份有效工具內容。僅新增 ComposerTools.js 專用元件檔案，無第三方相依。

### 選檔取消保留面板與成功返回草稿

附件按鈕直接於使用者 click handler 呼叫常駐 file input.click()，不經 await、計時器或關閉面板，以保留選檔所需的使用者操作資格。cancel（含重選同一清單所觸發的取消事件）及空清單不關面板、不清草稿，不靠 window focus 推測選檔結束。

change 收到非空清單且通過既有 uploadFiles 入口限制時，先同步建立附件佔位 chip，再關閉面板，持續既有串行上傳；不等待全部上傳完成才關。檔案 input value 仍於讀取清單後清空，允許再次選同一檔案。面板選檔路徑略過 uploadFiles 內的 focusChatInput，自動聚焦規則對桌面貼上／拖放維持原樣。上傳失敗保留 chip、錯誤與重試；取消、失敗、重試都不送出訊息或清除文字。

若選檔期間改變 session 或變唯讀，舊結果不能加入新會話或繞過權限；以發起選檔時的 session id 驗證歸屬，過期結果捨棄、不上傳。切換可用模式後返回工具首頁，草稿依既有模式切換規則保留。

### 關閉返回與生命週期清理

Close、點真正的 backdrop 統一關閉當前面板；Escape、browser Back 與可用的 Telegram BackButton 先返回單選清單的工具首頁，已在首頁時才關閉面板。點面板內控制項或操作單選清單／系統選檔器不得被 backdrop/cancel 誤判；系統選檔器的取消先交由選檔器處理。Escape 在面板／dialog 消費並停止冒泡，避免觸發 app.js 的連按 Escape 關閉會話分頁。

瀏覽器開啟面板時在同一 URL 放入一個具 session／面板識別的 history.state 標記；保留原 history.state，反覆點 Plus 不重複新增。關閉流程必須可重複呼叫而不重複回退；popstate 離開本面板標記時只關面板，保持當前聊天；其他關閉方式僅在當前頂端仍為自己的標記時回退一次。清理過程不對非自己持有的歷史紀錄呼叫 back，Forward 不重開面板，也不留下需多按一次返回的空標記。Telegram 容器支援 BackButton 時暫時 show/onClick，關閉／卸載時 offClick，還原原先可見狀態；不攔截宿主的關閉 App 行為。測試須證明每次開關只消耗自己的標記。

關閉後焦點回 Plus 並 preventScroll，不主動聚焦 textarea。session 切換、失去可寫權限、composer 改為只有停止按鈕、轉桌面及卸載時都清理 dialog、history／Telegram handlers、背景捲動鎖；不移交焦點到已卸載的節點。開啟模態期間鎖背景捲動，關閉還原先前樣式與聊天捲動位置。高度變化只限制面板可用區，不自動關閉或重建。

### 面板內單選清單取代手機原生下拉

**Supersedes**: mobile-composer-sheet / 單一工具內容與常駐選檔器

依使用者於 mock 階段確認的互動，手機設定改為單選子頁；上節的單一資料來源及常駐選檔器保留。桌面原生 select 維持原樣。

## Implementation Contract

- In scope：ChatView、ComposerTools.js、ui-atoms 的手機工具入口、模態生命週期、file input 歸屬、手機 textarea 高度策略及對應 CSS／測試。Out of scope：後端與協定、其他彈窗、桌面工具位置、相簿預覽、手勢拖曳及發版。
- 介面：toolsOpen 為 ChatView 的本地布林狀態；既有 handleFilesPicked、uploadFiles、handleInputModeChange 及設定 draft handlers 保留原安全檢查與送出時機。無新增 HTTP／WS payload、儲存欄位或永久設定。
- 外觀：遵循 Design Source 數值表，設定列具名稱及當前值；沒有任何依賴 blur 的工具顯示或 textarea 高度限制。面板內容只在內容區捲動，關閉按鈕可一直操作，無頁面橫向溢出。
- 失敗：原生 picker 取消保持開啟；上傳失敗顯示既有附件錯誤／重試；失去權限立刻關面板並保留既有可恢復草稿；不提供假成功提示。必測選檔期間切 session 的歸屬限制。
- 自動驗收：延伸 scripts/composer-attachment.test.py，從 CSS-only fixture 升級為載入實際 ChatView／本地 React；以 go.mod 已有的 esbuild Go CLI 轉譯 JSX 並 stub 網路與無關子元件的互動測試，實際執行產品的 open／close／cancel／change handlers；不能只測複製的示範 markup。執行 python scripts/composer-attachment.test.py，測試情境與任務對應見 tasks.md。
- 回歸指令：node --test scripts/remaining-ux.test.cjs scripts/console-usability.test.cjs scripts/theme.test.cjs；go test ./internal/static；go test -tags production ./internal/static。以上列為實作後應執行的指令，實作須依實際結果記錄測試。
- 實機驗收：iOS Safari、Android Chrome、Telegram Mini App 實際開鍵盤後按 Plus，開／取消／選取系統附件、面板內選擇設定 及返回；記錄版本、步驟與結果，不把桌面模擬等同於真機通過。無設備時明確記錄未驗證項目。
- 規格同步：修改 composer-model-controls 的 action-row 及 header 要求，讓手機入口改為 Plus；修改 console-usability 的手機排列與點擊尺寸要求，保留桌面 32px、雙主題與 zoom 可用性。未涉及的 Agent／role／busy／next-send 契約維持原意。

## Risks / Trade-offs

- [原生 dialog 的 cancel、選檔返回及手機鍵盤在不同 WebView 有差異] → 核心開關只看明確動作，實機驗收與 file chooser 自動測試分開記錄；不以失焦推斷選檔取消。
- [history 或 Telegram 返回監聽殘留會誤離開聊天] → 單一標記、有界 ownership、開關／卸載／Forward 及多次循環測試，恢復既有 BackButton 可見狀態。
- [工具內容遷移造成重複 file input／select 或設定立即送出] → 選檔器常駐且唯一，工具只掛載一份，測試 id 唯一性、草稿值與送出前 WS 無設定訊息。
- [橫向或鍵盤擠壓面板] → 採可用視窗上限、安全區與內容內捲動；44px 觸控區為指定目標，窄高 fixture 及實機確認關閉鈕、所有選項皆可觸及。
- [既有工作目錄存在其他未提交變更] → apply 僅整合本提案相關行為，不覆蓋其他工作；以任務 baseline 保存既有未提交變更，僅歸屬本次差異。

