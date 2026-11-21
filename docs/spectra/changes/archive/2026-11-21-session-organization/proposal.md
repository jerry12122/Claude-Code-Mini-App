## Why

使用者希望在既有工作目錄群組下自行整理會話，也能把已讀會話標記為待回看，避免大量會話只能依目錄與時間尋找。需求來自 2026-11-19 使用者回饋，排入 v0.22.0 之後的功能候選；本計畫不實作、不改動本次發布範圍。

## What Changes

- 在側欄的實體工作目錄群組下，提供單層自訂分類與會話歸類；分類可新增、改名、刪除，刪除只解除歸類。
- 分類及歸屬儲存在伺服器，重整與其他裝置可見；既有會話維持未分類。
- 在會話選單新增手動標記未讀／已讀，另存待回看標記，不改寫歷史訊息已讀狀態或偽造未讀數。
- 保留釘選、搜尋、真實未讀數與執行中佇列行為，處理多視窗延遲已讀回報，提供鍵盤與手機入口。

## Capabilities

### New Capabilities

- `session-folder-categories`: 工作目錄內的單層分類管理、會話歸屬、持久化與隔離。
- `session-manual-unread`: 與訊息未讀數分離的手動待回看標記，以及抗延遲回報的清除規則。

### Modified Capabilities

無。既有精確訊息數與 Telegram 零計數契約維持；新能力僅增加獨立提醒狀態。

## Impact

- 資料層：internal/db/ 的 schema、migration 與 session 查詢／更新。
- API 與同步：internal/api/session.go、internal/server/ 路由及權限、既有 session 變更廣播。
- 前端：internal/static/js/session/SessionView.js、會話選單與讀取回報；以現有實際來源定位，不引入分類框架。
- 驗證：DB/API 回歸、瀏覽器重整／多視窗／手機／鍵盤流程；沿用現有測試工具。
- 發布：新增資料需向後相容升級；安排於 v0.22.0 之後，具體版號由後續發布規劃決定。

