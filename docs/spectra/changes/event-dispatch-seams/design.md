## Context

回合的核心接在 `NewHandler` 裡：`runner.Run` 的回呼同時決定要不要收下 `agent.Event`，並在同一個 `switch` 裡改狀態、寫庫、呼叫 `broadcast`。非 `delta` 的內容沖洗發生在 `switch` 之前。`runner.Run` 返回之後，錯誤、交接與逗留通知另行 `broadcast`。前端 `useChatSocket` 的 `ws.onmessage` 依 `msg.type` 更新畫面；`sync` 與 `status` 會先共用一段耗時校正，`error` 只有訪客會跳出提示。標題列元件是 `ChatSessionHeader`，桌面操作列與手機展開列都是直接寫死的按鈕。分割畫面會同時有兩個標題列。

這次只搬家，讓現有行為各自有一個看得懂的入口。插件還沒有第一個呼叫端，現在若把攔截點、全域註冊表或公開槽位名定死，下一版會被迫沿用放錯的接縫。

本變更自 2026-11-13 建立後未開始實作（程式中沒有 `applyRunEvent`，沒有 `internal/static/js/ui/headerSlot.js`，沒有 `scripts/header-slot.test.cjs`，`ChatHeader.js` 內 0 處 `HeaderSlot`）。期間倉庫出現三項與本計畫相關的變動，已逐一確認：

1. `docs/spec/frontend-architecture.md` 的 R5 禁止 `internal/static/js/` 下使用 kebab-case 檔名，且 `scripts/frontend-layering.test.cjs` 的 `KNOWN_VIOLATIONS.R5_kebab` 現為空清單，規範明訂新增檔案不得寫入白名單。
2. `internal/static/js/chat/chat-header.js` 已不存在，改名為 `internal/static/js/chat/ChatHeader.js`，`internal/static/index.html` 於第 608 行載入；`ui/` 的腳本區塊（第 587 至 598 行）整體早於 `chat/` 區塊（第 605 行起）。
3. 新增了 `ws-message-dispatch-split` 變更，目標是 `NewHandler` 裡的 `switch msg.Type`。它與本變更改同一個檔案，但區塊不重疊。

另已確認仍成立的現狀：`internal/static/js/hooks/useChatSocket.js` 共 900 行、只有 1 處 `onmessage`；`scripts/telegram-chat.test.cjs` 有 5 處引用 `useChatSocket`；`ChatSessionHeader` 內確實有手機 `<details>` 展開後的操作列與桌面的 `justify-end` 操作列各一處。

## Goals / Non-Goals

### Goals

- 後端用 `applyRunEvent` 接收一則已決定要處理的 `agent.Event`，回呼只做沖洗與中止過濾。
- 既有 `broadcast` 繼續負責這次回合的送訊，包含 `runner.Run` 返回之後的那幾則。
- 前端用連線內部的分派表處理既有訊息，`sync`／`status` 的共用耗時校正與訪客才提示的 `error` 維持現狀。
- 標題列兩處排版各有一個固定回傳 `null` 的位置，不產生節點。
- 新增與修改的前端檔案都通過 `docs/spec/frontend-architecture.md` 的既有規則，不新增任何白名單條目。

### Non-Goals

- 不實作插件介面、Base、設定檔啟用清單、事件匯流排、`plugin_event` 或 `plugin_command`。
- 不新增攔截清單，也不把 `applyRunEvent` 訂成之後唯一的插件入口。
- 不把分派表做成對外註冊表。
- 不做槽位註冊、註銷、渲染參數、優先權或例外隔離。
- 不拆分 `serverMsg`，不改 `agent.Runner` 與各 CLI runner。
- 權限、佇列、shell、交接與寫庫仍留在套用函式內。
- 不把 Mermaid、HTML 預覽、訊息操作或分享改成插件。
- 不改變 capability-level observable behavior。
- 不碰 `NewHandler` 的 `switch msg.Type`，那是 `ws-message-dispatch-split` 的範圍。
- 不承諾讓 `NewHandler` 符合 `docs/spec/backend-architecture.md` 的 R8（150 行）。

## Decisions

- 套用函式放在 `internal/ws`，名稱為 `applyRunEvent`。它接收這次回合已經在用的狀態與依賴，不新增第二種送訊函式，也不接受攔截清單。`runner.Run` 回呼保留兩段閘門：非 `delta` 先沖洗內容緩衝；`ctx` 已取消時只放行 `delta`、`done`、`error`、`permission_denied`、`session_init`。通過閘門後才呼叫 `applyRunEvent`。
- Supersedes: 「之後的同步攔截只包 `applyRunEvent`」以及「插件訊息只在分派表加鍵」。`applyRunEvent` 只是把 `switch` 搬出來。沖洗在它之前，回合結束後的錯誤、交接與逗留通知在 `runner.Run` 返回之後直接 `broadcast`。之後若要觀察，入口是每一次 `broadcast`；若要改寫事件，入口要在沖洗和寫庫之前。這次兩個入口都不做。分派表留在 `useChatSocket.js` 的 `ws.onmessage` 裡，只保住現有訊息行為；`scripts/telegram-chat.test.cjs` 只載入這個檔。它不是插件登記表。
- `onmessage` 仍先做「非 delta 且有待沖洗的 delta 計時器就先沖洗」。接著 `sync` 與 `status` 都先跑耗時校正，再跑各自的處理。其餘類型一則訊息只進一個處理函式。訪客的 `error` 仍只提示並結束；擁有者的 `error` 在這個分派裡仍然不提示。
- Supersedes: 具名槽位 `chat.header.actions`、註冊順序與例外隔離。改由 `internal/static/js/ui/headerSlot.js` 提供 `HeaderSlot`，固定回傳 `null`，模組不提供註冊函式。`ChatSessionHeader` 在桌面操作列與手機展開後的操作列各放一個。空結果不產生包裹元素，所以兩個標題列不會因為它多出按鈕或間距。註冊、註銷、渲染參數與例外隔離等第一個標題列插件出現再定，避免分割畫面共用一份全域清單。
- Supersedes: 檔名 `internal/static/js/ui/header-slot.js`。改為 `internal/static/js/ui/headerSlot.js`。理由是 `docs/spec/frontend-architecture.md` R5 禁止 `js/` 下的 kebab-case 檔名，`scripts/frontend-layering.test.cjs` 的 `KNOWN_VIOLATIONS.R5_kebab` 已清空，新增檔案不得補進白名單；沿用舊檔名會讓 `node scripts/frontend-layering.test.cjs` 失敗。
- Supersedes: 「腳本在 `index.html` 裡排在 `chat-header.js` 之前」。該檔已改名為 `chat/ChatHeader.js`。`headerSlot.js` 放進 `index.html` 既有的 `ui/` 腳本區塊即滿足順序要求，因為該區塊整體早於 `chat/` 區塊；驗證改為檢查 `headerSlot.js` 的載入行號小於 `ChatHeader.js` 的載入行號，而不是比對特定行數。
- 與 `ws-message-dispatch-split` 的邊界：本變更只動 `runner.Run` 的事件回呼，對方只動 `switch msg.Type`，兩者不重疊。但兩者都編輯 `internal/ws/handler.go`，所以先合併的一方不需調整，後合併的一方必須重跑 `go test ./internal/ws/` 與 `go test ./internal/arch/`。實作順序由使用者決定。

## Implementation Contract

capability-level observable behavior 維持不變：既有 WebSocket 訊息種類、聊天狀態、授權、串流、shell 與標題列外觀都不因這次重排而改變。`HeaderSlot` 不增加節點，也不改變現有按鈕的順序與可見性。

完成時必須同時成立：

- `go test ./internal/ws/` 通過。這包現有測試涵蓋佇列、授權、交接、逗留程序與會話指示等回合行為。
- `go test ./internal/arch/` 通過，且 `internal/arch/arch_test.go` 的 `knownViolations` 沒有新增條目。
- `node --test scripts/telegram-chat.test.cjs` 通過。該檔以真的 `useChatSocket` 檢查 sync、時間線與待傳訊息。
- `node scripts/frontend-layering.test.cjs` 通過，且 `KNOWN_VIOLATIONS` 沒有新增條目。
- `node --test scripts/header-slot.test.cjs` 通過：`HeaderSlot()` 的結果是 `null`；同一模組沒有註冊函式；`chat/ChatHeader.js` 的桌面操作列與手機展開列各出現一次 `HeaderSlot`；`internal/static/index.html` 載入 `ui/headerSlot.js` 的行號小於載入 `chat/ChatHeader.js` 的行號。

失敗時的界線：套用函式若漏掉某種 `agent.Event`，既有 `internal/ws` 測試會看到狀態或廣播與現在不同。分派表若把 `sync` 的耗時校正拆掉，或讓擁有者的 `error` 開始提示，即視為行為改變，必須改回。`HeaderSlot` 若回傳包裹元素，或出現註冊函式，即不符合契約。若需要放寬既有測試的斷言或補白名單才能通過，表示行為或分層已改變，不符合本契約。

## Risks / Trade-offs

- [套用函式搬移時抓錯閉包裡的 `permDenied`、`succeeded` 等旗標] -> 旗標仍由回呼與 `applyRunEvent` 共用同一組變數或同一個回合物件，並以 `go test ./internal/ws/` 守住。
- [分派表把現在會先執行的共用段落收成互斥分支] -> `sync` 與 `status` 明確先做耗時校正；`error` 維持訪客才提示。
- [空槽多一個永遠存在的包裹元素，桌面操作列間距改變] -> `HeaderSlot` 回傳 `null`，不渲染包裹元素。
- [新腳本放錯順序，`ChatSessionHeader` 看不到 `HeaderSlot`] -> `headerSlot.js` 放入 `index.html` 既有的 `ui/` 區塊，並由 `scripts/header-slot.test.cjs` 以行號比較檢查順序與兩處掛載。
- [這次順手做成全域註冊表，分割畫面的兩個標題列黏在同一份清單上] -> 模組不提供註冊函式，測試要能看出沒有註冊入口。
- [與 `ws-message-dispatch-split` 同期進行，`internal/ws/handler.go` 出現合併衝突] -> 兩者區塊不重疊；後合併者重跑 `go test ./internal/ws/` 與 `go test ./internal/arch/`。

## Migration Plan

沒有資料遷移或設定遷移。部署就是換成包含這次重排的二進位與靜態檔。若要回到現況，還原這次的程式變更即可，因為沒有新的持久化欄位或新的對外訊息。

## Open Questions

沒有會擋住這次實作的問題。第一個標題列插件的註冊、註銷與渲染參數，以及觀察型入口要包哪些 `broadcast`、改寫型入口要放在沖洗前的哪一層，都留到那次變更再定。

