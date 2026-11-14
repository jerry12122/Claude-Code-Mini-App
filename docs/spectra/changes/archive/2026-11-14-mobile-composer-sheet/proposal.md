## Summary

手機聊天輸入區改為常駐「＋」、輸入框與送出／停止按鈕，按「＋」開啟獨立的底部工具面板。面板參考使用者提供的 Claude App 截圖，允許輸入框失焦及鍵盤收起，同時保持附件與設定操作可用。

## Motivation

v0.18.2 曾用 :focus-within 切換手機工具列，開啟系統選檔器或收起鍵盤會使工具列隱藏並改變輸入區高度；v0.18.4 改為工具列固定顯示，解決附件操作卻持續佔用聊天空間。現在需要能省空間、又不依賴文字輸入焦點的工具入口。

## Proposed Solution

- 手機及既有窄高度觸控版面，以「＋」開啟底部模態面板；面板包含加入圖片／檔案、現有模式切換及各 Agent 支援的模型、思考強度與權限控制；設定採整列入口，於同一面板切換至有勾選標記的單選清單，選完返回工具首頁。
- 面板獨立管理開關，聚焦、失焦、鍵盤及視窗高度變化不會自行開關面板。輸入框依內容增高，取消失焦時強制縮小的規則。
- 選檔取消時保留面板；選取非空檔案清單後加入既有附件草稿流程、關閉面板，輸入區顯示附件及上傳／重試狀態。檔案 input 始終掛載於面板之外。
- 提供關閉按鈕、背景遮罩、Escape 與返回操作；保留草稿，關閉後把焦點交回「＋」，不強制彈出鍵盤。
- 桌面工具列、既有權限與停用條件、設定於下次送出套用、附件驗證與上傳流程維持原有行為。

## Impact

- Affected specs: mobile-composer-sheet（新增）、composer-model-controls（修改手機控制項位置及入口）、console-usability（修改手機尺寸與排列要求）
- Affected code:
  - Modified: internal/static/js/chat/ChatView.js；internal/static/index.html；scripts/composer-attachment.test.py；internal/static/js/ui/ui-atoms.js；internal/static/AGENT.md
  - New: internal/static/js/chat/ComposerTools.js（僅承載此面板及其單選列，不建立通用彈窗框架）
  - Removed: none
- API、WebSocket 協定、資料庫與相依套件無變更。參考截圖及設計數值記錄於 design.md；本次依核准 mock 實作正式介面，不自動發版。
