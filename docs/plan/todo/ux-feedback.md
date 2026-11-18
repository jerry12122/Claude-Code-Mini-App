# 功能計劃：主要使用情境的即時回饋與可靠性

> 狀態：2.1–2.6 已實作（2026-10-05），2.6 已實測；2.1–2.5 需登入後手動驗證（見各項完成標準）
> 建立日期：2026-10-05
> 目標：手機遠端操控的核心流程（送訊息、授權、中斷、看用量、切換聊天室、登入）在延遲或失敗時，使用者一定看得到回饋，且不會因為靜默失敗卡住或產生重複資料。

## 1. 範圍

主要使用情境：用手機（Telegram WebView／瀏覽器）遠端指揮 agent，以及在 session 之間切換。
本計劃只處理這條路徑上「按了沒回饋」「失敗被吞掉」「卡死」的問題，共 6 項（P0）。其餘次要項目列在第 5 節，不在本輪。

## 2. P0 項目（依實作順序）

### 2.1 授權／Shell 確認／中斷：send 失敗不能被吞掉

現況（`ChatView.js`）：
- `handleAllowOnce`／`handleDenyOnce`／`handleShellApprove`／`handleShellCancel` 不檢查 `send()` 回傳值，先 `setPermTools([])`／`setShellPendingCmd(null)`。WS 斷線時面板消失，狀態卻停在 `AWAITING_CONFIRM`，畫面沒有入口可再按。
- Shell 的三顆允許鈕（`shell_allow_once`／`shell_allow_remember_workdir`／`shell_deny`）按下後不 disabled，可連點。
- 「中斷」按下後沒有任何狀態變化。
- `/reset`、`shell_exec` 不檢查 `send` 就清空輸入框。

做法：
- 在 `ChatView` 加一個小 helper：`send` 失敗時 `showToast('連線中斷，操作未送出，請稍後再試', { error: true })` 並回傳 `false`；上述各處只在回傳 `true` 時才清面板／清輸入。
- 授權與 Shell 確認面板加 `actionPending`：點擊後按鈕 disabled，`state` 離開該等待狀態時重設；10 秒仍未變化則重設並提示。
- 中斷鈕加 `interrupting`：顯示「中斷中…」並 disabled，`state` 離開執行中狀態或 10 秒後重設。

完成標準：
- DevTools 設為 Offline 後按「允許」，面板仍在並出現錯誤 toast；恢復連線後可再按。
- 快速連點「允許一次」只送出一次。
- 中斷按下後立即有「中斷中…」。
- Offline 下送 `/reset`，輸入框內容保留。

### 2.2 WS 斷線指示

現況：`useChatSocket.js` 的 `onclose` 靜默每 2 秒重連，使用者只在送出時才知道斷線。

做法：
- `useChatSocket` 新增 `connected` state：`onopen` 設 true、`onclose` 設 false、session 切換時重設。回傳給 `ChatView`。
- 斷線超過約 1.5 秒才顯示（避免切換 session 初次連線時閃一下）：頂欄下方細橫幅「連線中…（訊息暫時無法送出）」（文案中性，初次連線與斷線重連都適用），樣式比照 `ShareStatusBar`。重新連線後後端會送 `sync`，狀態自動恢復，橫幅消失。
- snapshot 訪客沒有 WS，不顯示。
- 斷線時不 disable 輸入框（草稿本來就會保留）；送出失敗已有 toast。

完成標準：Offline 約 2 秒內出現橫幅；恢復後橫幅消失且 `state` 與後端一致；切換 session 不閃橫幅。

### 2.3 用量刷新

現況：
- 桌面 `ChatHeader.js` 的 `{quotaRefreshing ? '↻' : '↻'}` 兩個分支相同，沒有旋轉。
- 手機頂欄傳 `compact`，`uiAtoms.js:475` 的 `!compact && onRefresh` 讓手機完全沒有刷新鍵。
- `useChatSocket.js:556` 的 10 秒 timeout 沒清除，逾時無提示；刷新成功與否使用者看不出來。

做法：
- 桌面：刷新時按鈕加 `animate-spin`，disabled 時有視覺差異。
- 手機：`compact` 模式下整個 quota 晶片可點擊觸發刷新（不新增元素，避免擠壓 `max-w-[4.5rem]`）；刷新中晶片用 `animate-pulse`。
- `handleQuotaRefresh` 把 timeout 存進 ref，收到 `quota_update` 或 session 切換時清除。
- 只在「使用者主動刷新」時回饋：收到 `quota_update` 顯示「用量已更新」（`quota.error` 有值則顯示該錯誤）；10 秒沒回應顯示「用量更新逾時」。

完成標準：桌面與手機都能點、點了有動畫；成功、錯誤、逾時三種結果各有 toast；背景自動推送的 `quota_update` 不跳 toast。

待確認：quota 為空時（`quotaText` 為空，刷新鍵消失）是否要保留刷新入口。Antigravity 已支援配額取得及刷新，需納入首次取得或取得失敗的情境。確認前不動。

### 2.4 建立 session 後送首則訊息失敗，重試會產生重複 session

現況：`NewSessionComposer.js:39-57` 先 POST `/sessions`，再用 `sendPromptViaEphemeralWS` 送首則訊息。後者失敗時只顯示錯誤、不呼叫 `onCreated`，session 其實已存在，使用者再按一次會再建一個。`ForwardModal.js:110-121`「新建會話」同樣問題。

做法：session 建立成功後，首則訊息失敗不再重試迴圈，改為：
- 把訊息寫進該 session 的草稿（`localStorage[draftInputStorageKey(created.id)]`，`ChatView` 切換 session 時已會讀取）。
- `showToast('Session 已建立，但首則訊息未送出，已放入輸入框', { error: true })`。
- 照常 `onCreated(created)`／`onForwarded({ ..., jump: true })` 進入該聊天室。

不新增狀態與重試邏輯，也不會丟失使用者輸入。轉發到既有 session 失敗時沒有建立動作，維持現有錯誤顯示。

完成標準：模擬 WS 失敗（例如暫時讓 `wsURL` 指向無效位址），建立後進入聊天室、輸入框有原文、`/sessions` 只多一筆；轉發新建同理。

### 2.5 切換聊天室時載入中的空白畫面

現況：`ChatView.js:677` 只處理 `histLoaded && messages.length === 0`，歷史載入中訊息區完全空白。

做法：`!histLoaded` 時訊息區置中顯示「載入對話中…」（樣式沿用 `GuestApp` 的載入中文字）。歷史請求失敗目前會被當成空列表並繼續連 WS（`sync` 會補訊息），維持不變，只補一個失敗 toast。

完成標準：DevTools 網路節流 Slow 3G 切換長歷史聊天室時，載入中有文字，載入完成後消失。

### 2.6 登入失敗時按鈕永遠停在「驗證中…」

現況：`app.js:11` 的 `fetch` 沒有 try/catch，網路失敗時 `setLoading(false)` 不會執行。

做法：用 try/catch/finally 包起來，失敗顯示「連線失敗，請重試」。

完成標準：Offline 下按「進入」，出現錯誤文字，按鈕恢復可按。

## 3. 實作順序與提交方式

建議每項一個 commit，依 2.1 → 2.2 → 2.3 → 2.4 → 2.5 → 2.6：2.1 與 2.2 影響最大且互相呼應（斷線時的操作回饋）；2.6 最小，可隨時插入。

涉及檔案：`chat/ChatView.js`、`chat/ChatHeader.js`、`hooks/useChatSocket.js`、`ui/uiAtoms.js`、`session/NewSessionComposer.js`、`chat/ForwardModal.js`、`app.js`。皆為前端，不需改後端或協議。

## 4. 驗證方式

專案沒有前端測試框架，這幾項都是互動與時序行為，不為此新增框架。每項以第 2 節的完成標準手動驗證，用 DevTools 的 Offline／Network throttling 重現，電腦與手機（TG WebView）各跑一輪。

風險：`sendPromptViaEphemeralWS` 的「成功」只代表 WS 已開啟並送出，後端沒有確認訊息被接受（`hooks/forwardPrompt.js` 的 `sendPromptViaEphemeralWS`），所以 2.4 只能處理「連線失敗」這類可偵測的失敗。是否補上確認機制另案評估。

## 5. 不在本輪（次要，之後再議）

- Session 列表首次載入／失敗時顯示「尚無 Session」（`SessionView.js:481`）。
- 重新命名無樂觀更新、無錯誤處理；刪除失敗靜默、5 秒內關頁不會真的刪除。
- 分享彈窗建立後新卡片在下方看不到；結束分享無樂觀更新。
- Debug 日誌 checkbox 要等伺服器回應才變化。
- 程式碼區塊複製失敗無 toast（`ChatView.js:247`）。
- 送出鈕 `sending` 時無 spinner、textarea 被 disabled 導致手機鍵盤收起。

## 7. 驗證紀錄（2026-11-18，supervisor 以臨時實例實測）

方法：以 `-tags production` 建置，另起臨時實例（獨立埠、獨立設定與資料庫、自設密碼、無 `bot_token`），用 Chrome 驅動；結束後已關閉並刪除臨時目錄，未碰使用者的正式服務。

| 項目 | 結果 |
|---|---|
| 2.2 斷線橫幅 | 通過：停掉伺服器約 4 秒後出現「連線中…（訊息暫時無法送出）」。重連恢復部分只間接驗證：重啟後網頁登入狀態（記憶體）失效，回到登入頁，橫幅消失，但沒有驗到「同一登入狀態下恢復連線、`state` 與後端一致」 |
| 2.1 離線送 `/reset` | 通過：輸入框保留 `/reset`，出現「連線中斷，操作未送出，請稍後再試」 |
| 2.6 離線登入 | 通過：出現「連線失敗，請重試」，按鈕由驗證中恢復「進入」可按 |
| 方法限制 | DevTools 的 Offline 模擬不會中斷已建立的 WebSocket，所以 2.2 不能用它驗，要真的停伺服器 |
| 未驗證 | 2.1 授權／Shell 確認／中斷按鈕（需要執行中的 agent 與待授權狀態）、2.3 用量刷新（需 provider 額度資料）、2.4 建立 session 失敗重試、2.5 Slow 3G 長歷史載入、手機實機 |

## 8. 驗證紀錄（2026-11-18 續，臨時實例 8097，結束後已關閉並刪除）

同一登入狀態、沒有重啟服務：把聊天 WebSocket 先掐斷再讓重連失敗約 2.5 秒，橫幅出現；重連成功後橫幅消失，Shell 確認面板仍在，與後端待確認狀態一致。

| 項目 | 結果 |
|---|---|
| 2.1 Shell 確認斷線 | 通過：斷線時按「允許一次」，面板留著，按鈕可再按，出現「連線中斷，操作未送出，請稍後再試」。恢復後單次允許，`echo` 得到 exit 0 |
| 2.1 連點允許 | 未通過：同步連點兩次會送出兩次 `shell_allow_once`。第二次進入允許處理時會取消剛啟動的指令，結果是「已中止」，不是只執行一次 |
| 2.1 中斷 | 通過：`ping` 執行中按下中斷，按鈕立刻改成「再按一次強制停止」並脈動；指令以 interrupted／exit -1 結束。文案與 2026-10 計劃寫的「中斷中…」不同，以現行程式為準 |
| 2.3 用量刷新 | 部分通過：桌面刷新鈕有旋轉，接著出現「用量已更新」（真實 5 小時視窗）。錯誤與逾時兩種提示這次沒有觸發 |
| 2.4 首則訊息連線失敗 | 通過：新建只多一筆 session，聊天室顯示「上一則訊息尚未確認，已保留原內容」與「重試確認」，原文在 localStorage 的 pending 紀錄 |
| 2.5 Slow 3G | 通過：切到約 80 則歷史時出現「載入對話中…」，載完後文字消失並看得到歷史 |
| 手機實機 | 未做。Chrome 390px 手機版面看得到「附加圖片或檔案」，點了會點到檔案輸入；不是 Telegram WebView，也沒有排出佇列 |

