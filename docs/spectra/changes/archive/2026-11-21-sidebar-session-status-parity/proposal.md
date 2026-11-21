## Why

使用者截圖指出：上方會話分頁顯示閃動的執行中提示，左側收合欄同一會話卻沒有對應提示，必須逐一切換才能掌握進度。希望左側也一起顯示，列為第二個後續版本提案。追加截圖要求每筆會話右側的「釘選」文字改成釘選圖示，減少文字佔位。

## What Changes

- 左側收合欄依同一份 session 狀態，顯示與上方分頁一致的執行中提示。
- 沿用既有狀態類別及動畫，狀態結束立即移除；待授權／待回答仍呈現原有提示。
- 保留選取框、代理圖示、未讀數及點擊／選單操作，提示不互相遮蔽。
- 使用者啟用減少動態效果時，兩處皆採靜態狀態提示，仍可辨識執行中。
- 展開側欄的會話列以既有 📌 圖示取代「釘選」文字，保留「已釘選」提示與可存取名稱，不新增切換按鈕。
- 僅建立計畫，安排於 v0.22.0 之後；不加入本次發布實作。

## Capabilities

### New Capabilities

無。

### Modified Capabilities

- `session-tabs`: 增加分頁與收合側欄對同一會話的狀態提示一致性，以及展開側欄的釘選圖示要求。

## Impact

- internal/static/js/session/SessionView.js：收合欄目前僅渲染待處理提示，補足 running 提示。
- internal/static/js/ui/SessionTabs.js 與 internal/static/js/ui/uiAtoms.js：沿用既有 sessionStatusDotClass／sessionStatusLabel，不另存一份執行狀態。
- internal/static/index.html：檢查共用狀態動畫與 reduced-motion 規則，僅在缺少時補足。
- 瀏覽器驗證兩處狀態轉換、背景會話、未讀並存與減少動態效果；無 API／DB／runner 改動。
