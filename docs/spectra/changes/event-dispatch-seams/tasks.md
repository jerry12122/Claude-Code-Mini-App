## 1. 後端回合入口

- [ ] 1.1 通過閘門的 `agent.Event` 改由 `internal/ws` 的 `applyRunEvent` 套用，且不新增攔截清單或第二種送訊函式：非 `delta` 仍先沖洗內容緩衝，`ctx` 已取消時只放行 `delta`、`done`、`error`、`permission_denied`、`session_init`，權限、佇列、shell、交接與寫庫仍在這個套用路徑裡，`runner.Run` 返回後的錯誤、交接與逗留通知仍走既有 `broadcast`。不碰 `NewHandler` 的 `switch msg.Type`（屬 `ws-message-dispatch-split` 範圍）。驗證：`go test ./internal/ws/` 通過，且 `go test ./internal/arch/` 通過而 `internal/arch/arch_test.go` 的 `knownViolations` 沒有新增條目。

## 2. 前端訊息分派

- [ ] 2.1 `useChatSocket` 的 `ws.onmessage` 改以連線內部的分派表處理既有訊息，分派表留在 `internal/static/js/hooks/useChatSocket.js`，且不提供對外註冊入口。非 `delta` 仍先沖洗待送出的 delta；`sync` 與 `status` 仍先做耗時校正再進入各自處理；訪客的 `error` 仍只提示並結束，擁有者的 `error` 在這條路徑仍不提示。驗證：`node --test scripts/telegram-chat.test.cjs` 通過（該檔以真的 `useChatSocket` 檢查 sync、時間線與待傳訊息），且 `node scripts/frontend-layering.test.cjs` 通過而 `KNOWN_VIOLATIONS` 沒有新增條目。

## 3. 標題列空位

- [ ] 3.1 `HeaderSlot` 固定回傳 `null`、不產生包裹節點，且提供它的模組不含註冊函式。模組檔名為 `internal/static/js/ui/headerSlot.js`（camelCase，符合 `docs/spec/frontend-architecture.md` R5 對 `js/` 下檔名的限制；`scripts/frontend-layering.test.cjs` 的 `KNOWN_VIOLATIONS.R5_kebab` 為空清單且不得新增）。`ChatSessionHeader` 在桌面操作列與手機展開後的操作列各掛一個，該元件位於 `internal/static/js/chat/ChatHeader.js`。`internal/static/index.html` 載入 `ui/headerSlot.js` 的行號小於載入 `chat/ChatHeader.js` 的行號。驗證：`node --test scripts/header-slot.test.cjs` 通過，涵蓋上述四項；且 `node scripts/frontend-layering.test.cjs` 通過而 `KNOWN_VIOLATIONS` 沒有新增條目。

## 變更紀錄（非執行項）

- 2026-11-16：原任務 3.1 指定的新檔名 `internal/static/js/ui/header-slot.js` 被 `docs/spec/frontend-architecture.md` 的 R5 禁止，已取代為 `internal/static/js/ui/headerSlot.js`。
- 2026-11-16：原任務 3.1 的掛載目標與載入順序基準 `internal/static/js/chat/chat-header.js` 已不存在（改名為 `ChatHeader.js`），已取代為 `internal/static/js/chat/ChatHeader.js`，順序驗證改以行號比較而非特定行數。
- 2026-11-16：任務 1.1 補上與 `ws-message-dispatch-split` 的邊界宣告與 `go test ./internal/arch/` 驗證。兩個變更都編輯 `internal/ws/handler.go` 但區塊不重疊；後合併的一方須重跑這兩項測試。實作順序由使用者決定。

