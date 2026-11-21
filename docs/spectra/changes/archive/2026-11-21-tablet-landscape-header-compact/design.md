## Context

使用者確認新增第四項，並以最新「工作樹開起來 可以派了」授權四項於隔離工作樹開工，逐項驗收後整合 main；目前發布修補與功能工作分離。

已讀來源：ChatSessionHeader 的外層含 env(safe-area-inset-top) 與 --tg-content-safe-top；SessionView 的列表標題亦使用安全區。SessionTabs 為獨立分頁列。core.js 監聽 Telegram contentSafeAreaChanged，且目前 tgDesktopFullscreen 只對指定桌面平台生效；index.html 對該狀態的特殊 CSS 限定 fine pointer 與 hover。這些是定位線索，不是已證明的平板根因，不能只把 iOS 加入桌面平台清單。

## Design Source

- 來源：本輪使用者附件 IMG_0164.png，2026-11-21 取得；沒有 .dc.html 或目標設計稿。
- 原始附件：workspace/uploads/98e28d24-7cb4-4277-9703-61a6b7a93075/05218462923e869be2af86dc14c4dc24.png。
- 本機快取：.spectra/design-cache/tablet-landscape-header-compact/reference.png；只作問題參考，不納入發布封裝。
- 圖中可辨：系統狀態列、Telegram 關閉／選單、分頁列、標題與副標。圖片像素不直接換算 CSS px；無指定色碼、圓角或圖示更換。
- 已讀實作的精確值：ChatSessionHeader 基底頂距 0.5rem；列表標題基底頂距 18px；特殊桌面 fullscreen 聊天內層上下距 12px。這些不是平板實測高度。

## Goals / Non-Goals

**Goals:**
- 平板橫向標題區縮高，聊天可視高度增加，保留控制與真正安全區。
- 在相同條件下留下修改前後 DOM 數值及圖像證據，支援方向、指標、宿主安全區事件與鍵盤變化。

**Non-Goals:**
- 不重設手機／桌面全域斷點、不改字體大小或隱藏功能來湊高度，不以裁切或負 margin 蓋住問題。
- 不改 Telegram 宿主控制、不宣稱可縮短系統本身區域；不改 API、DB、runner、Room 佇列密度或目前發布候選。

## Decisions

### 隔離開工依最新授權

**Supersedes**: tablet-landscape-header-compact / Context 原「目前版本公開發布與 PM 下載產物驗收通過才開始」順序決定。

以使用者後續「工作樹開起來 可以派了」取代舊發布 gate。可先量測與實作，仍須逐項驗收後整合 main，不能改寫 v0.22.0 公開資產或混入 v0.22.1 修補候選。共用檔案必須由 PM 指定單一修改者，既有 Sidebar 與 Room 已驗收來源保持凍結。

### 先量測安全區責任再縮高

先在橫向平板量取 shell、分頁列、標題、訊息區與 composer 的 bounding box、computed padding 與 Telegram／CSS insets。判定宿主保留區、應用 base spacing、可能重複偏移分別屬於誰；只移除或縮減證實可避免的部分。排除直接將所有安全區設零，以及只憑 screenshot 推斷裝置 CSS 尺寸。

### 保留觸控與響應式狀態

優先現有 CSS 與事件來源，按實際布局與輸入能力限定規則，不把寬螢幕等同滑鼠。若需安全區責任調整，使用既有變數並於當前受影響容器中處理，避免 shell、tab、header 重複消耗同一保留區。沒有必要就不新增 JS、polling 或依賴。

## Implementation Contract

- 行為：Compact landscape header preserves conversation space；平板橫向可看到更多聊天，必要標題資訊與既有操作保持可達。
- 介面：沿用 ChatSessionHeader、session-tab-bar、ra-desktop-shell 及 --tg-content-safe-top。若改既有 Telegram 事件處理，保持非 Telegram／缺少 API 的 fallback，不改既有桌面旗標定義冒充 iPad。
- 範圍：只處理這份附件中的頂部標題多餘高度。普通會話為必驗；共用 shell 若牽動 Room，必驗其頂部不受遮擋，不順便改 Room 版面。
- 選定驗收條件（尚非裝置量測值）：1194×834 與 1024×768 CSS px 橫向、root font 16px、coarse pointer；在相同宿主 inset 與相同固定會話內容下，可避免的頂部應用區塊比原版至少減少 16 CSS px，訊息可視區至少增加相同高度，composer 可操作。度量為 shell 起點到訊息區起點的總佔位，不只把 padding 移到另一容器。
- 必要安全區不算可避免空白；若基線證明上述縮減與必要安全區／操作相衝突，保留基線證據回報 PM 修訂計畫，不交付遮擋版。不能以減小宿主回報值或字級取得通過。
- Touch targets and host clearance remain usable：觸控控制的可點範圍至少 44×44 CSS px、不互相覆蓋；Telegram 關閉／選單及系統區不遮應用互動控制；長標題、Git／quota／分享仍能操作。
- fallback／錯誤：無 Telegram、缺 contentSafeAreaInset、回報零 inset 時維持正常布局；方向或安全區更新、退出 fullscreen、鍵盤開關後不得殘留舊頂距，禁止全域清零安全區。
- 回歸：平板直向 834×1194、手機 390×844、桌面 1280×800（均為選定測試尺寸）及 fine pointer、不支援新 API、放大文字 200%、分割窗格、背景會話切換；操作可達、無新增頁面橫捲及裁切。
- 驗證：使用既有隔離瀏覽器 fixture 與假 runner；同條件前後截圖／DOM JSON、node scripts/frontend-layering.test.cjs、python scripts/build-css.py --check、既有 production build。如改 core.js 事件邏輯，執行 node --test scripts/telegram-fullscreen.test.cjs 並補實際改變行為的檢查；純 CSS 不加鏡像單元測試。真 Telegram 平板宿主驗證單獨記錄，模擬 DOM 或 BrowserStack Safari 不冒稱真 Telegram WebView；缺該環境時列為未驗，不宣稱完整通過。

## Risks / Trade-offs

- 截圖無法辨識實際 inset、DPR 或配件 → 開工先取可重複 DOM 基線，不能先把原因定成重複 padding。
- 縮高可能遮住 Telegram 宿主按鈕 → 保留必要保留區與觸控驗證，測非零／零值與方向切換。
- 與 sidebar-session-status-parity、session-organization 同碰 SessionView → 每個檔單 owner，依序交接；修改共用檔案前須由 PM 明確交接單一 owner，避免覆寫已驗收功能；發布狀態不是開工 gate。
- 不改資料格式，可用程式回退；未通過實機邊界前不承諾裝置全面支援。
