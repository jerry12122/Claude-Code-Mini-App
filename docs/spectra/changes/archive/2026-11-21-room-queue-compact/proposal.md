## Why

使用者指出工作室佇列只有一筆時仍佔太多垂直空間，並已明確確認是「佇列區塊本身太高」，不是聊天與佇列之間的留白或輸入框。需要縮減預覽狀態佔位，讓聊天與輸入區保有空間。

## What Changes

- 壓縮桌面工作室佇列的標題列、上下留白與一般預覽列，保留數量、訊息摘要、附件及操作。
- 多筆保留區塊內捲動；空佇列且無草稿／錯誤時不佔位，編輯與錯誤狀態能完整操作。
- 桌面可緊湊，觸控裝置仍保留可操作的按鈕範圍；不以隱藏功能換取高度。
- 僅建立第三個後續提案，安排於 v0.22.0 之後，不變更本次發布候選。

## Capabilities

### New Capabilities

- `room-queue-density`: 工作室佇列的緊湊預覽、聊天空間保留及操作可達性。

### Modified Capabilities

無。

## Impact

- internal/static/js/session/RoomControls.js 的 RoomQueuePanel：目前標題操作採 min-h-11，佇列預覽列 minHeight 28，區塊有 py-2；調整呈現密度。
- internal/static/js/session/RoomMessageBubble.js 的 RoomQueueEditor：沿用現有編輯與草稿保護，只驗證在緊湊容器中可達，不重寫編輯器。
- 如需 CSS，限定既有樣式位置並使用原生 responsive／pointer media query；不新增依賴。
- 驗證桌面高度、手機／觸控操作、編輯與錯誤、多筆捲動；無 API、DB、來源判定、批次或 runner 邏輯變更。
