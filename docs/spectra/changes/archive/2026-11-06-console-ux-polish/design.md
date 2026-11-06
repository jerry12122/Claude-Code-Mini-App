## Context

使用者要求以正式介面原有元件開分支試用，否決 prototype 的 44px 控制與兩排工具列。正式介面為 no-build React 全域 scripts，遵守 internal/static/AGENT.md，所有改動維持 script 順序。

現有 SessionTabBar 每個分頁均可 Tab 聚焦，只處理 Enter／Space 且 outline-none。ChatSessionHeader 的名稱與副標使用 truncate；QuotaUsage 的部分標籤使用低強調文字。ChatView 已有 sendOrToast、離線指示和送出確認的草稿保留，應先驗證而非重寫。當前工作樹含並行開發，僅局部加入本次變更。

## Design Source

- 正式介面的現有 JSX、CSS 與元件為視覺基準；prototype 只記錄已否決的探索過程，不作配色、字級、內距或版面替換來源。
- 參考檔 docs/prototypes/console-ux-polish.dc.html 已修正成 32px；快取 .spectra/design-cache/console-ux-polish/console-ux-polish.dc.html 同步最新稿，避免讀到先前被否決的版本。
- 必須保留的具體值：selectCompactClass 的 h-8（32px）、ChatView 的 w-8 h-8（32px）、現有 ra-cmd-bar padding 8px 12px／gap 6px 10px／radius 14px；字體與主題 token 沿用現值。上述為來源程式中的值，不是新視覺設計。

## Goals / Non-Goals

**Goals:** 保留原介面外觀，讓鍵盤、完整文字、縮放及必要回饋可用，產出可試用的分支及建置。

**Non-Goals:** 不搬用 prototype 元件；不放大控制、不主動拆排、不新增字體、主題、密度設定、依賴或動畫；不改後端協議、agent、佇列、草稿保存流程和其他進行中的功能。

## Decisions

### 依使用者回饋保留正式元件與尺寸

**Supersedes**: console-ux-polish / Prototype 審閱先於正式實作

使用者在多次 prototype 審閱後，明確要求保留原有元件與 32px 尺寸，並授權開分支試作精簡優化。此回覆滿足原本的實作前審閱條件；取消大型控制、密度重設、selector 換兩排與配色重做。未完成的舊實作要求已被此決策取代，不標記為已完成。

### 分頁焦點沿用既有群組狀態

SessionTabBar 的視覺外框保留，分頁主動作與關閉動作使用相鄰按鈕，避免巢狀按鈕。只有 active tab 與其 close 動作進入 Tab 序列；左右鍵循環、Home／End 選首尾，鍵盤切換後立即聚焦並捲至目標。關閉後在下一個 animation frame 找同群組 active tab，若群組消失則找仍存在的 active tab，再退回會話導覽的搜尋入口；只移交焦點，不改 reducer 的關閉選擇、分割持久化或掛載規則。修飾鍵與 IME 保留原用途。

### 原生完整資訊展開與共用焦點

以可重用的原生 details／summary 在手機及桌面標題提供完整名稱與工作目錄，預設收合維持原標題尺寸；展開時允許標頭暫時增高並自然換行。使用 React 文字節點避免注入，摘要有可讀名稱及焦點。解除 maximum-scale 限制，CSS 只增加 focus-visible 樣式與 composer focus-within，不變更控制尺寸。只在有證據的副標與用量標籤使用既有 ra-t3，不改全域色盤。

桌面標題及其左側容器不使用 flex-1 撐滿剩餘寬度，狀態與分支緊接標題，維持原本靠左的排列；長標題仍以 min-w-0 與 truncate 縮短顯示。

### 現有錯誤與傳輸行為先驗證再補缺漏

重用 ChatView 的 sendOrToast 和既有 pending-input 邏輯；不改 useChatSocket 或傳輸協議。既有失敗路徑符合就維持原碼。必要時補 toast 的語意 live status 與按鈕標籤，不逐 token 播報聊天內容，不靠動畫判定成功。

## Implementation Contract

- In scope：分頁鍵盤與關閉焦點、共享焦點、標題完整資訊、縮放、局部副標／用量對比及已存在的失敗路徑驗證。
- Out of scope：後端、API、WS、model／effort／權限選項可用性、32px 控制尺寸、既有輸入列結構、活動快照與其他並行修改。
- Interface：沿用 SessionTabBar 的 onSelectTab／onCloseTab 和 tabGroups reducer；導覽純函式對空清單與不支援按鍵回傳 null。完整資訊以原生 details 控制開合，不新增全域 state／儲存。
- Failure：關閉後即使原群組卸載，也有仍存在的 active tab 或會話搜尋作為焦點目的。普通長文字可換行，code 繼續只在局部橫向捲動。離線保留可編輯草稿，授權 send 失敗不隱藏原面板。
- Acceptance：node --test scripts/tab-groups.test.cjs scripts/console-usability.test.cjs scripts/theme.test.cjs scripts/ux-reliability.test.cjs；go test ./internal/static 與 go test -tags production ./internal/static。鍵盤測試覆蓋 A/B/C 首尾循環、Home／End、單頁／空頁、群組卸載及最後一頁焦點 fallback。
- Visual verification：用實際 JSX 和 CSS 驗證一般桌面、680px 群組窄欄及 390px 手機，雙主題與 reduced motion；200% browser zoom 的等效窄 viewport 需可操作，並明確區分等效重排與實際手機／瀏覽器縮放。確認 composer 控制仍 32px，長名稱與路徑可展開，無額外頁面橫向溢出。局部標籤的正常文字對比以 4.5:1 為驗收輸入。
- Trial build：以 production tag 建置獨立名稱的桌面 EXE，不覆寫現有 EXE、不啟動第二個正式服務、不碰使用者 DB 或 config。

## Risks / Trade-offs

- [方向鍵啟用分頁會沿用既有 remount] → 不改掛載數及草稿機制，以既有測試與實際元件走查驗證。
- [展開完整資訊會暫時增加標頭高度] → 只由使用者主動展開，預設收合不改日常排列。
- [並行修改與本 change 共用檔案] → 保存來源基準，僅加入相關小改動，不以整個 dirty tree 作歸屬或提交。
- [使用者環境的 WebView／讀屏與手機未必可完整測試] → 清楚記錄已執行的瀏覽器證據與未測平台，不以 prototype 通過取代正式驗證。
