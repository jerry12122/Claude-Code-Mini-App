## Summary

在獨立分支試作 Agent Console 的局部可用性改善，保留現有元件、32px 輸入列控制、單列排列、字體、主題及多會話結構。

## Motivation

使用者否決 prototype 的大型元件與兩排工具列，確認現有產品較適合日常使用。仍需補齊可見鍵盤焦點、分頁方向鍵及關閉焦點、長名稱與路徑閱讀、瀏覽器放大，以及實測有缺漏的對比與失敗提示。

## Proposed Solution

- 使用者已明確要求開分支修改試用；此次以既有正式介面為基準，不將 prototype 外觀搬入產品。
- 分頁提供單一 Tab 入口、左右鍵循環、Home／End 和有效的關閉後焦點；不新增切換動畫。
- 以現有樣式補齊共用控制的 focus-visible 與標籤，解除 viewport 放大限制。
- 會話標題提供原生可展開完整名稱及工作目錄的入口；預設收合保留原版面。
- 僅將實測過淡的工作目錄與用量標籤改用既有較清楚的語意文字色；深淺色分別驗證。
- 驗證既有離線、授權失敗與草稿保留；已符合的功能不重寫，只補缺少的可讀回饋。

## Alternatives Considered (optional)

放大元件、工具列自動換排、重畫配色及新增密度設定已由使用者否決，不納入此試作。

## Impact

- Affected specs: console-usability（新增）、session-tabs（新增鍵盤與焦點要求）
- Affected code:
  - Modified: internal/static/index.html、internal/static/js/core/core.js、internal/static/js/hooks/tabGroups.js、internal/static/js/ui/SessionTabs.js、internal/static/js/ui/ui-atoms.js、internal/static/js/chat/chat-header.js、internal/static/js/chat/ChatView.js、internal/static/js/session/SessionView.js、scripts/tab-groups.test.cjs
  - New: scripts/console-usability.test.cjs
  - Removed: none
- API、WebSocket、儲存格式、agent runner、佇列與依賴不變；工作樹內其他修改保留。
