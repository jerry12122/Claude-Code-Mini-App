## 1. 後端回合入口

- [ ] 1.1 通過閘門的 `agent.Event` 改由 `internal/ws` 的 `applyRunEvent` 套用，且不新增攔截清單或第二種送訊函式：非 `delta` 仍先沖洗內容緩衝，`ctx` 已取消時只放行 `delta`、`done`、`error`、`permission_denied`、`session_init`，權限、佇列、shell、交接與寫庫仍在這個套用路徑裡，`runner.Run` 返回後的錯誤、交接與逗留通知仍走既有 `broadcast`。驗證：`go test ./internal/ws/`。

## 2. 前端訊息分派

- [ ] 2.1 `useChatSocket` 的 `ws.onmessage` 改以連線內部的分派表處理既有訊息，分派表留在 `internal/static/js/hooks/useChatSocket.js`，且不提供對外註冊入口。非 `delta` 仍先沖洗待送出的 delta；`sync` 與 `status` 仍先做耗時校正再進入各自處理；訪客的 `error` 仍只提示並結束，擁有者的 `error` 在這條路徑仍不提示。驗證：`node --test scripts/telegram-chat.test.cjs`。

## 3. 標題列空位

- [ ] 3.1 `HeaderSlot` 固定回傳 `null`、不產生包裹節點，且 `internal/static/js/ui/header-slot.js` 不提供註冊函式。`ChatSessionHeader` 在桌面操作列與手機展開後的操作列各掛一個，`internal/static/index.html` 在 `chat-header.js` 之前載入該檔。驗證：`node --test scripts/header-slot.test.cjs`。
