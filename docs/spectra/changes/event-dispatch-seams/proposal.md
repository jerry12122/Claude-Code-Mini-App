## Why

回合進行中，後端把「要不要收下這則 agent 事件」和「收到之後怎麼改狀態、寫庫、廣播」寫在同一個 `runner.Run` 回呼裡；前端也把每一種 WebSocket 訊息的處理排成一長串 `msg.type` 判斷。這次只把兩段搬開，讓現有行為有明確入口。插件的攔截點、註冊表和槽位參數還沒有第一個實際插件，現在定死會把下一版卡在錯誤的接縫上。

## What Changes

- 將 `runner.Run` 回呼裡的 `switch e.Type` 搬成 `applyRunEvent`。回呼仍負責非 `delta` 的內容沖洗，以及中止後只放行既有種類。此函式是搬家，不新增攔截清單，也不成為之後唯一的插件入口。
- 這次回合既有的 `broadcast` 維持原樣，包含 `runner.Run` 返回之後的錯誤、交接與逗留通知。不新增第二種送訊函式。
- 將 `useChatSocket` 裡依 `msg.type` 更新畫面狀態的判斷收成連線內部的分派表。`sync` 與 `status` 仍先做耗時校正；只有訪客的 `error` 會提示。這張表不對外註冊。
- 在 `ChatSessionHeader` 的桌面操作列與手機展開後的操作列各放一個 `HeaderSlot`。它固定回傳 `null`，不提供註冊函式，也不產生節點。

## Non-Goals

- 不實作插件介面、Base、設定檔啟用清單、事件匯流排、`plugin_event` 或 `plugin_command`。
- 不把 `applyRunEvent` 訂成之後的同步攔截點。觀察要涵蓋每一次 `broadcast`；改寫要站在內容沖洗和寫庫之前。這兩個入口這次都不做。
- 不把前端分派表做成插件可登記的訊息表。
- 不做槽位註冊、註銷、渲染參數、優先權或例外隔離，也不定公開槽位名。
- 不拆分 `serverMsg`，不改 `agent.Runner`，也不改各 CLI runner。
- 權限、佇列、shell、交接與寫庫仍留在 `applyRunEvent` 裡。
- 不把 Mermaid、HTML 預覽、訊息操作或分享改成插件。
- 不改變使用者看得到的聊天、授權、串流或標題列行為。

## Impact

- Affected specs: none
- Affected code:
  - New: internal/static/js/ui/header-slot.js、scripts/header-slot.test.cjs
  - Modified: internal/ws/handler.go、internal/static/js/hooks/useChatSocket.js、internal/static/js/chat/chat-header.js、internal/static/index.html
  - Removed: (none)
- Compatibility: no capability-level observable behavior changes
