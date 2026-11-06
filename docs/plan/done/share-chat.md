# 功能計劃：臨時共享聊天室（PIN + 連結 + 暱稱協作）

> 狀態：規劃中
> 建立日期：2026-10-02
> 目標：擁有者可臨時（預設 1 小時）把目前的聊天室分享給朋友。朋友用另一台電腦的瀏覽器，憑連結＋PIN 與暱稱加入，與擁有者對同一個聊天室對話，訊息顯示說話者名字，雙方一起協作開發。

## 1. 使用流程

**擁有者**

1. 聊天室選單按「分享」，設定三項：
   - **時長**：預設 1 小時。
   - **角色**：`viewer`（唯讀）或 `editor`（權限與擁有者相同）。
   - **範圍**：`snapshot`（只分享到目前這則訊息為止）或 `live`（後續訊息也持續串流）。
2. 取得 `https://<host>/share/<token>` 與 6 位數 PIN，可一鍵複製。
3. 分享面板顯示剩餘時間、在線名單與「結束分享」，結束即刻踢出所有訪客。

**設定組合**

| 角色 | 範圍 | 訪客體驗 |
|---|---|---|
| viewer | snapshot | 靜態唯讀頁，只有分享當下為止的歷史，不連 WS |
| viewer | live | 唯讀，持續看到新訊息與 agent 串流，不能送任何操作 |
| editor | live | 與擁有者相同權限，可送訊息、回應授權；**必須是 live** |

- `editor` + `snapshot` 不成立：協作者要看到 agent 的回覆才能操作。UI 選 `editor` 時範圍鎖定為 `live`，後端也驗證，不符回 400。

**訪客**

1. 開連結，輸入 PIN 與暱稱。
2. 進入聊天室，依角色與範圍呈現（見上表）。
3. editor 的訊息與擁有者的訊息都標示說話者名字。

## 2. 現有基礎

- `ws/broadcast.go` 的 `hub` 已支援同一 session 多條 WS 訂閱，串流、狀態、授權事件自動同步到訪客。
- `queued_messages` 已處理忙碌時多則訊息排隊。
- 前端驗證集中在 `core.js` 的 `apiFetch` / `wsBaseURL`，新增一種驗證方式改動面小。
- `auth/session.go` 的記憶體 session store 可參考，但分享需持久化到期與撤銷，故另建表。

## 3. 資料

- `shares(id, session_id, token, pin, role, mode, snapshot_msg_id, expires_at, revoked, failed_attempts, created_at)`
  - `pin`：6 位數**明文**存放（臨時碼，僅擁有者可由列表讀取），讓設定的分享列表可重新顯示連結與 PIN
  - `role`：`viewer` | `editor`
  - `mode`：`snapshot` | `live`
  - `snapshot_msg_id`：建立時該 session 最大的 `messages.id`；`mode=snapshot` 時只能讀 `id <= snapshot_msg_id`，`live` 時為 NULL
- `share_guests(id, share_id, nickname, token, created_at)`
- `messages` 新增 `author TEXT NOT NULL DEFAULT ''`；空字串視為擁有者，舊資料不需搬移。

## 4. API 與驗證

```text
POST   /sessions/:id/shares          建立分享（擁有者驗證）body {ttl, role, mode} → {url, pin, expires_at}
                                     role=editor 且 mode=snapshot → 400
DELETE /shares/:id                   結束分享（擁有者驗證）→ 踢出所有訪客
DELETE /shares                       結束全部有效分享（擁有者驗證）
DELETE /shares/ended                 清除全部已撤銷／到期的分享紀錄 → {cleared: 數量}
DELETE /shares/ended/:id             清除單筆已撤銷／到期的分享紀錄 → {cleared: 0 或 1}
GET    /shares                       全部分享（含 session 名稱、url、pin、在線訪客；擁有者，供設定頁列表）
GET    /sessions/:id/shares          該聊天室的分享與在線名單（擁有者）
GET    /share/:token                靜態頁（PIN + 暱稱表單）
POST   /share/:token/join           {pin, nickname} → {guest_token, session_id, nickname}
```

- PIN 連續錯誤 5 次即鎖定該分享（`failed_attempts`）。
- 清除只供擁有者使用；有效分享（包含 PIN 鎖定者）回 409，不允許清除。無效或非正整數 id 回 400；重複清除回 `{cleared:0}`。以交易刪除分享與其訪客紀錄，保留聊天室及訊息，舊連結與訪客 token 持續失效。
- 暱稱在同一分享內重複時自動加數字後綴。
- `authMiddleware`：在 TG 驗證之後、內網 IP 檢查之前，加入 guest token 分支（`X-Share-Token` 或 `?share=`）。通過後寫入 `share_session_id`，並限制只能存取：
  - `/sessions/:id/ws`
  - `/sessions/:id/messages`
  - `/sessions/:id/uploads*`
  - 且 `:id` 必須等於 token 綁定的 session，其餘路由回 403。
- guest token 的有效期不超過分享的 `expires_at`，每次請求都檢查 `revoked` 與到期。
- guest token 帶 `role`、`mode`、`snapshot_msg_id`，範圍檢查依此收斂：
  - `snapshot`：只開放 `GET /sessions/:id/messages`（強制附加 `id <= snapshot_msg_id`）與屬於這些訊息的附件；不開放 `/ws`。
  - `viewer` + `live`：開放 `/ws` 與 `messages`，但 WS 為單向訂閱。
  - `editor` + `live`：開放上述全部範圍與 `uploads` 上傳。
- `join` 回傳 `role` 與 `mode`，前端據此決定要渲染哪種介面。

## 5. WebSocket 與協作行為

- `ws/handler.go` 依 Locals 判斷身分與 `author`（擁有者／訪客暱稱）。訪客無 `tg_id`，不會觸發 Telegram 通知。
- 角色在**後端**強制，不依賴前端隱藏按鈕：`viewer` 的 WS 讀迴圈只處理 ping，其餘 client 訊息一律丟棄並回錯誤事件；`editor` 才進入原本的訊息處理。
- 訊息寫入 `author`；送給 agent 的 prompt 對訪客訊息加 `[暱稱]` 前綴，讓 agent 分辨說話者。
- 忙碌時任何人送的訊息都進 `queued_messages`，排隊清單顯示送出者。
- 授權對話框兩端同時顯示，先回應者生效，另一端自動關閉；需確認 `clearPendingDenials` 對重複回應冪等。
- 新增 `presence` 廣播（加入／離開／在線名單）。
- 到期或撤銷時，`hub` 提供 `KickShare(shareID)` 關閉該分享所有訪客連線；每條訪客 WS 另有 timer 於 `expires_at` 主動關閉。

## 6. 前端

- `app.js`：路徑為 `/share/<token>` 時跳過登入，顯示 PIN／暱稱表單；加入後進入訪客模式。
- 訪客模式：只渲染該 session 的 `ChatView`，隱藏側欄、設定與 session 管理。guest token 存 `sessionStorage`，重新整理可續連。
- 依角色與範圍切換介面：
  - `snapshot`：載入歷史後不建立 WS，頂部標示「唯讀快照（截至 HH:MM）」。
  - `viewer`：隱藏輸入框、附件、授權對話框操作與模型／權限控制，標示「唯讀」；`live` 時照常顯示串流。
  - `editor`：與擁有者介面一致（仍隱藏側欄與設定）。
- 設定頁新增「分享」分頁（`SettingsModal.js` 加 `shares` section → `share/SharesSection.js`）：
  - 列表欄位：聊天室（點擊跳轉）、角色／範圍標籤、剩餘時間、在線訪客、操作（複製連結、顯示／複製 PIN、結束分享）。
  - 頂部「全部結束」與「有效中／已結束」篩選。
  - 已過期或已撤銷者灰掉並標示狀態。
- 擁有者的分享面板：角色選 `editor` 時，範圍選項自動鎖定為 `live` 並灰掉；面板顯示每個分享的角色／範圍標籤。
- `core.js`：訪客模式下 `apiFetch` 帶 `X-Share-Token`，`wsBaseURL` 帶 `share=`。
- `ChatView`：訊息顯示 `author`；顯示在線名單；到期前 5 分鐘提示，到期後顯示「分享已結束」。
- 擁有者端：session 選單新增「分享」，面板含連結、PIN、倒數、在線名單、結束分享。

## 7. 部署備註

服務目前僅允許內網 IP。朋友從外網連入需要公開入口（例如 Cloudflare Tunnel），至少放行 `/share/*`、`/sessions/*`、靜態檔與 WebSocket。此為部署設定，不在程式範圍。

## 8. 實作順序

1. DB：`shares`、`share_guests`、`messages.author` 與測試。
2. API：建立／結束／列出分享、`join`（含 PIN 鎖定）。
3. `authMiddleware` 的 guest token 與路由範圍檢查。
4. WS：署名、prompt 前綴、presence、到期與撤銷踢線。
5. 前端：加入頁、訪客模式、署名顯示、擁有者分享面板。
6. 測試：
   - 到期／撤銷、PIN 鎖定、跨 session 存取被擋。
   - 兩端同時送訊息與授權競爭。
   - `editor` + `snapshot` 被拒絕。
   - `snapshot` 讀不到 `snapshot_msg_id` 之後的訊息與附件，且連不上 WS。
   - `viewer` 透過 WS 送訊息、授權回應、shell 都被後端丟棄。

## 9. 暫不處理

- `editor` 可執行的操作範圍（shell、切換模型、權限模式、刪除 session）：第一版沿用與擁有者相同，之後視需要在 handler 以 `guest` 旗標收斂。
- 分享建立後修改角色或範圍：第一版需結束後重建；之後再考慮升降級（例如 viewer 升為 editor）。
- 多個 session 同時分享、訪客單獨的歷史起點、密碼以外的身分驗證。

## 10. 前置條件

附件功能的未提交改動與本計劃重疊（`ws/handler.go`、`ChatView.js`、`useChatSocket.js`、`server.go`），建議先提交附件功能再開始實作。
