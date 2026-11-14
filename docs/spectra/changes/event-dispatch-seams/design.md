## Context

回合的核心接在 `NewHandler` 裡：`runner.Run` 的回呼同時決定要不要收下 `agent.Event`，並在同一個 `switch` 裡改狀態、寫庫、呼叫 `broadcast`。非 `delta` 的內容沖洗發生在 `switch` 之前。`runner.Run` 返回之後，錯誤、交接與逗留通知另行 `broadcast`。前端 `useChatSocket` 的 `ws.onmessage` 依 `msg.type` 更新畫面；`sync` 與 `status` 會先共用一段耗時校正，`error` 只有訪客會跳出提示。標題列元件是 `ChatSessionHeader`，桌面操作列與手機展開列都是直接寫死的按鈕。分割畫面會同時有兩個標題列。

這次只搬家，讓現有行為各自有一個看得懂的入口。插件還沒有第一個呼叫端，現在若把攔截點、全域註冊表或公開槽位名定死，下一版會被迫沿用放錯的接縫。

## Goals / Non-Goals

Goals:

- 後端用 `applyRunEvent` 接收一則已決定要處理的 `agent.Event`，回呼只做沖洗與中止過濾。
- 既有 `broadcast` 繼續負責這次回合的送訊，包含 `runner.Run` 返回之後的那幾則。
- 前端用連線內部的分派表處理既有訊息，`sync`／`status` 的共用耗時校正與訪客才提示的 `error` 維持現狀。
- 標題列兩處排版各有一個固定回傳 `null` 的位置，不產生節點。

Non-Goals:

- 不實作插件介面、Base、設定檔啟用清單、事件匯流排、`plugin_event` 或 `plugin_command`。
- 不新增攔截清單，也不把 `applyRunEvent` 訂成之後唯一的插件入口。
- 不把分派表做成對外註冊表。
- 不做槽位註冊、註銷、渲染參數、優先權或例外隔離。
- 不拆分 `serverMsg`，不改 `agent.Runner` 與各 CLI runner。
- 權限、佇列、shell、交接與寫庫仍留在套用函式內。
- 不把 Mermaid、HTML 預覽、訊息操作或分享改成插件。
- 不改變 capability-level observable behavior。

## Decisions

- 套用函式放在 `internal/ws`，名稱為 `applyRunEvent`。它接收這次回合已經在用的狀態與依賴，不新增第二種送訊函式，也不接受攔截清單。`runner.Run` 回呼保留兩段閘門：非 `delta` 先沖洗內容緩衝；`ctx` 已取消時只放行 `delta`、`done`、`error`、`permission_denied`、`session_init`。通過閘門後才呼叫 `applyRunEvent`。
- Supersedes: 「之後的同步攔截只包 `applyRunEvent`」以及「插件訊息只在分派表加鍵」。`applyRunEvent` 只是把 `switch` 搬出來。沖洗在它之前，回合結束後的錯誤、交接與逗留通知在 `runner.Run` 返回之後直接 `broadcast`。之後若要觀察，入口是每一次 `broadcast`；若要改寫事件，入口要在沖洗和寫庫之前。這次兩個入口都不做。分派表留在 `useChatSocket.js` 的 `ws.onmessage` 裡，只保住現有訊息行為；`scripts/telegram-chat.test.cjs` 只載入這個檔。它不是插件登記表。
- `onmessage` 仍先做「非 delta 且有待沖洗的 delta 計時器就先沖洗」。接著 `sync` 與 `status` 都先跑耗時校正，再跑各自的處理。其餘類型一則訊息只進一個處理函式。訪客的 `error` 仍只提示並結束；擁有者的 `error` 在這個分派裡仍然不提示。
- Supersedes: 具名槽位 `chat.header.actions`、註冊順序與例外隔離。改由 `internal/static/js/ui/header-slot.js` 提供 `HeaderSlot`，固定回傳 `null`，模組不提供註冊函式。`ChatSessionHeader` 在桌面操作列與手機展開後的操作列各放一個。空結果不產生包裹元素，所以兩個標題列不會因為它多出按鈕或間距。腳本在 `index.html` 裡排在 `chat-header.js` 之前。註冊、註銷、渲染參數與例外隔離等第一個標題列插件出現再定，避免分割畫面共用一份全域清單。

## Implementation Contract

capability-level observable behavior 維持不變：既有 WebSocket 訊息種類、聊天狀態、授權、串流、shell 與標題列外觀都不因這次重排而改變。`HeaderSlot` 不增加節點，也不改變現有按鈕的順序與可見性。

完成時必須同時成立：

- `go test ./internal/ws/` 通過。這包現有測試涵蓋佇列、授權、交接、逗留程序與會話指示等回合行為。
- `node --test scripts/telegram-chat.test.cjs` 通過。該檔以真的 `useChatSocket` 檢查 sync、時間線與待傳訊息。
- `node --test scripts/header-slot.test.cjs` 通過：`HeaderSlot()` 的結果是 `null`；同一模組沒有註冊函式；`chat-header.js` 的桌面操作列與手機展開列各出現一次 `HeaderSlot`；`index.html` 在 `chat-header.js` 之前載入 `header-slot.js`。

失敗時的界線：套用函式若漏掉某種 `agent.Event`，既有 `internal/ws` 測試會看到狀態或廣播與現在不同。分派表若把 `sync` 的耗時校正拆掉，或讓擁有者的 `error` 開始提示，即視為行為改變，必須改回。`HeaderSlot` 若回傳包裹元素，或出現註冊函式，即不符合契約。

## Risks / Trade-offs

- [套用函式搬移時抓錯閉包裡的 `permDenied`、`succeeded` 等旗標] -> 旗標仍由回呼與 `applyRunEvent` 共用同一組變數或同一個回合物件，並以 `go test ./internal/ws/` 守住。
- [分派表把現在會先執行的共用段落收成互斥分支] -> `sync` 與 `status` 明確先做耗時校正；`error` 維持訪客才提示。
- [空槽多一個永遠存在的包裹元素，桌面操作列間距改變] -> `HeaderSlot` 回傳 `null`，不渲染包裹元素。
- [新腳本放錯順序，`ChatSessionHeader` 看不到 `HeaderSlot`] -> `index.html` 將 `header-slot.js` 放在 `chat-header.js` 之前，並由 `scripts/header-slot.test.cjs` 檢查順序與兩處掛載。
- [這次順手做成全域註冊表，分割畫面的兩個標題列黏在同一份清單上] -> 模組不提供註冊函式，測試要能看出沒有註冊入口。

## Migration Plan

沒有資料遷移或設定遷移。部署就是換成包含這次重排的二進位與靜態檔。若要回到現況，還原這次的程式變更即可，因為沒有新的持久化欄位或新的對外訊息。

## Open Questions

沒有會擋住這次實作的問題。第一個標題列插件的註冊、註銷與渲染參數，以及觀察型入口要包哪些 `broadcast`、改寫型入口要放在沖洗前的哪一層，都留到那次變更再定。
