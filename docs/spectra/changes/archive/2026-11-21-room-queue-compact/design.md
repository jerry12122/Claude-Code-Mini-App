## Context

使用者已確認 screenshot b2da53... 指的是工作室佇列區塊本身偏高。RoomQueuePanel 的標題按鈕目前使用 min-h-11、一般列 minHeight 28、區塊 py-2，maxHeight 40vh 且 overflowY auto；不需新增折疊狀態或另一套 queue 元件。

## Design Source

- 來源：workspace/uploads/98e28d24-7cb4-4277-9703-61a6b7a93075/b2da53d88bf1b590950bcdce9e9749e7.png，使用者已補充「佇列區塊本身太高」。
- 快取：.spectra/design-cache/room-queue-compact/reference.png，僅本地參考，不打包。
- 截圖是問題現況，未提供新視覺稿；圖片像素不能當作 CSS pixel，驗收以瀏覽器 boundingClientRect 量測。

| 項目 | 已讀現有來源的值 |
| --- | --- |
| 標題按鈕 | min-h-11（預設 root font 16px 時 44 CSS px） |
| 普通預覽列 | minHeight 28 CSS px |
| 區塊上下內距 | py-2（預設字級時每側 8 CSS px） |
| 上限與捲動 | maxHeight 40vh，overflowY auto |
| 編輯按鈕圖示 | 既有筆形 SVG，14 × 14；不另換圖示 |

## Goals / Non-Goals

**Goals:** 桌面單筆預覽更矮、保留聊天與 composer 空間；手機觸控、附件、編輯與錯誤資訊仍可達。

**Non-Goals:** 不改聊天留白／輸入框高度，不加折疊開關或密度設定，不重構共用 queue，不改普通會話隊列，不更動來源錯誤、API、DB、FIFO、批次 ACK／鎖或 runner。錯誤「此來源不可一起送」另行查核，不以 CSS 掩蓋。

## Decisions

### 只壓縮一般預覽並保留操作空間

使用既有 flex／padding／media query 調整 RoomQueuePanel。桌面標題與預覽列縮減垂直佔位；維持多筆內捲動上限。編輯、錯誤或 local pending 可依內容增高，不能強行塞進單筆高度。對 coarse pointer 保留至少 44 × 44 CSS px 的觸控目標，必要時增加列高，不拿桌面高度限制套用手機。

不採新增收合控制或全域密度架構，因為需求只是區塊偏高；不縮小文字或隱藏附件／錯誤來達成高度。

## Implementation Contract

- 範圍為 RoomQueuePanel 的一般預覽排版及必要的既有 CSS；沿用 RoomQueueEditor，不能更動資料流或鎖。
- 選定驗收門檻（非既有量測結果）：1280 × 800 CSS px、root font 16px、pointer fine、正常文字縮放、單筆短訊息、無編輯／錯誤／批次提示／local pending 時，區塊總高不超過 72 CSS px，且小於同環境改前基線。實作時保存前後 boundingClientRect 實測；若門檻與可操作性衝突，回報具體反例，不刪除操作硬湊數字。
- 多筆仍能捲動至最後一筆；一般短摘要與附件標識不能讓頁面橫向溢出。保留 maxHeight 40vh 語意，焦點與編輯動作可滾動到可見位置。
- 390 × 844 CSS px 的 coarse pointer 驗收保留至少 44 × 44 CSS px 觸控目標；同時驗鍵盤及放大文字，不能重疊或截掉儲存／取消／重新同步／批次入口。
- 單筆／多筆／空佇列、附件、正在編輯、保存失敗、已出列保留草稿、等待 ACK／批次提示，逐一真 DOM 檢查。既有停用條件與錯誤訊息不變。

## Risks / Trade-offs

- [桌面縮小使觸控按鈕難點] → 使用 pointer 條件與真 DOM 量測，觸控不套桌面壓縮上限。
- [短高度裁掉編輯器或錯誤] → 高度門檻只用一般預覽，內容狀態可增高並保留容器捲動。
- [截圖縮放導致數值誤判] → 同瀏覽器 CSS viewport 與字級前後量測，不拿影像實體像素宣稱改善比例。
