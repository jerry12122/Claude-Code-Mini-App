## 1. 狀態一致性

- [x] 1.1 實作 Consistent status indicators across tabs and collapsed sidebar 及「重用共用狀態與動畫」：收合圖示沿用共用 helper 呈現 running／awaiting；以瀏覽器反例先證明目前 running 只有分頁顯示，再驗 idle→running→各 awaiting→running→idle 與未知值、背景 session 互不干擾，無新增 polling／timer／API。
- [x] 1.2 [after: 1.1] 驗證 Status parity preserves accessibility and navigation：真 DOM 檢查 reduced-motion 下兩處靜態可見且 computed animation 停用、標籤含狀態、選取／圖示／未讀 3 並存不遮擋，以及鍵盤／滑鼠／選單／既有拖放語意不變；有缺陷只修本提案範圍。

- [x] 1.3 [after: 1.2] 實作 Compact pinned session icon 與「釘選文字改為既有圖示」：展開側欄以既有 📌 取代每列「釘選」文字；以瀏覽器驗證 tooltip／可存取名稱為「已釘選」、取消釘選後標記消失且群組正常、選取／running／未讀 3 並存，以及點圖示維持列開啟而不切換釘選。

## 2. 獨立驗收

- [x] 2.1 [after: 1.3] 由獨立驗收者覆核三項 requirement 全情境、共用狀態來源與精確 diff，跑受影響瀏覽器案例、既有前端分層／production build；保存受驗版本、命令結果、一般及 reduced-motion 截圖，確認無 API／DB／runner／未讀行為改動後才勾任務。
