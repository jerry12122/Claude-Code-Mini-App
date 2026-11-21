## 1. 緊湊佇列預覽

- [x] 1.1 實作 Compact queue preview preserves conversation space 與「只壓縮一般預覽並保留操作空間」：只調整 RoomQueuePanel 呈現；真 DOM 保存同環境前後基線，1280×800 CSS px／fine pointer／root font 16px 的單筆正常短訊息區塊 ≤72 CSS px 且比改前矮，多筆40vh內可捲至最後、空佇列不佔位，聊天與 composer 可操作；不為純 CSS 寫鏡像單元測試。
- [x] 1.2 [after: 1.1] 驗證 Queue density preserves controls and recovery：390×844 CSS px／coarse pointer 下操作目標至少44×44 CSS px，鍵盤與放大文字不截斷；覆蓋附件、編輯、出列保留草稿、保存錯誤、等待 ACK及批次提示，必要時僅修排版且不改既有停用條件／資料流。

## 2. 獨立驗收

- [x] 2.1 [after: 1.2] 獨立核對兩項 requirement 與高度／觸控契約，跑既有受影響 Room queue 瀏覽器回歸、前端分層／production build／CSS check；保存精確受驗版本、量測 JSON、前後與手機截圖，確認無普通會話／API／DB／來源判定／批次鎖變動後才勾任務。
