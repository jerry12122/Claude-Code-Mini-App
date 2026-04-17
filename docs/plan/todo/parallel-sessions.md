# 並行多 Session 背景執行計劃

> **目標**：使用者可同時對多個 Session 發送任務，任務在背景持續執行；切換回某 Session 時能看到執行結果或正在進行的串流回應。

---

## 總覽

```
Phase A — DB 擴充（任務狀態持久化）
Phase B — 後端任務生命週期解耦（不綁 WS 連線）
Phase C — WS 廣播機制（同一 Session 多連線）
Phase D — WS 連線時狀態恢復（sync 事件）
Phase E — 前端 Session 狀態角標
Phase F — 前端切換 Session 恢復狀態
```

---

## Phase A — DB 擴充

### A.1 `sessions` 表新增 `status` 欄位

- [ ] 在 `internal/db/db.go` schema 加入：

```sql
ALTER TABLE sessions ADD COLUMN status TEXT NOT NULL DEFAULT 'idle';
-- 合法值：'idle' | 'running' | 'awaiting_confirm'
```

- [ ] 在 `internal/db/session.go` 新增：
  - `UpdateSessionStatus(id, status string) error`
  - `Session` struct 加入 `Status string`
  - `scanSession` 讀取新欄位

### A.2 `messages` 表新增 `status` 欄位

- [ ] 在 schema 加入：

```sql
ALTER TABLE messages ADD COLUMN status TEXT NOT NULL DEFAULT 'done';
-- 合法值：'pending'（串流進行中）| 'done'（完整）
```

- [ ] 串流開始時先 `INSERT` 一筆 `status=pending` 的 assistant 訊息，`content` 為空
- [ ] 每個 `delta` 事件以 `UPDATE ... SET content = content || ?` 累積文字
- [ ] 任務結束（`EventDone`）時將該訊息 `status` 改為 `done`
- [ ] `internal/db/message.go` 新增：
  - `CreatePendingMessage(sessionID string) (msgID int64, err error)`
  - `AppendMessageContent(msgID int64, delta string) error`
  - `FinalizeMessage(msgID int64) error`

---

## Phase B — 後端任務生命週期解耦

> 核心改動：任務執行不再綁定 WebSocket 連線。WS 斷線不中斷任務。

### B.1 建立 Session 級別的任務管理器

- [ ] 新增 `internal/ws/taskmanager.go`，定義全域 `taskManager`：

```go
type taskEntry struct {
    cancel  context.CancelFunc
    deltas  chan string  // 即時廣播用
    msgID   int64       // 當前 pending message 的 DB id
}

var taskManager struct {
    sync.Mutex
    tasks map[string]*taskEntry // key: sessionID
}
```

- [ ] `StartTask(sessionID string, entry *taskEntry)`：登記任務
- [ ] `CancelTask(sessionID string)`：取消並清除
- [ ] `GetTask(sessionID string) (*taskEntry, bool)`：查詢是否有執行中任務

### B.2 修改 `internal/ws/handler.go` 任務啟動邏輯

- [ ] 收到 `input` 訊息時，在新 goroutine 啟動任務（**不等待結束**）
- [ ] 任務 goroutine 流程：
  1. `db.UpdateSessionStatus(sessionID, "running")`
  2. `db.CreatePendingMessage(sessionID)` 取得 `msgID`
  3. 呼叫 `runner.Run(ctx, opts, cb)`
  4. 每個 `EventDelta` → `db.AppendMessageContent(msgID, text)` + 廣播至 `taskEntry.deltas`
  5. `EventDone` → `db.FinalizeMessage(msgID)` + `db.UpdateSessionStatus(sessionID, "idle")`
  6. `EventPermDenied` → 存 `pending_denials` + `db.UpdateSessionStatus(sessionID, "awaiting_confirm")`
  7. 任何結束情況都從 `taskManager` 移除該 entry
- [ ] WS 連線斷開時**不再 cancel** 任務的 context（任務繼續）
  - 僅取消「將 delta 推送至此 WS」的訂閱

---

## Phase C — WS 廣播機制

### C.1 建立 Session 級別的訂閱器

- [ ] 新增 `internal/ws/broadcast.go`：

```go
// 一個 session 可有多條 WS 連線同時訂閱
type broadcaster struct {
    sync.Mutex
    subs map[string][]func(serverMsg) bool // key: sessionID
}

var hub = &broadcaster{subs: make(map[string][]func(serverMsg) bool)}

func (b *broadcaster) Subscribe(sessionID string, send func(serverMsg) bool) (unsub func())
func (b *broadcaster) Broadcast(sessionID string, msg serverMsg)
```

- [ ] 任務 goroutine 的 `EventDelta` 改為 `hub.Broadcast(sessionID, deltaMsg)`
- [ ] WS 連線建立時 `hub.Subscribe`；斷線時呼叫 `unsub()`
- [ ] 任務執行中新連線加入 → 自動接收後續 `delta`（歷史從 DB 補）

---

## Phase D — WS 連線時狀態恢復（sync 事件）

### D.1 後端推送 `sync` 事件

- [ ] WS 連線建立後立即查詢並推送：

```json
{
  "type": "sync",
  "status": "running",
  "messages": [
    { "role": "user", "content": "...", "status": "done" },
    { "role": "assistant", "content": "目前累積的文字...", "status": "pending" }
  ],
  "pending_denials": [...]
}
```

- [ ] 若 `status=running`，前端收到 `sync` 後繼續接收後續 `delta`（銜接串流）
- [ ] 若 `status=awaiting_confirm`，`pending_denials` 帶入授權對話框資料

### D.2 `GET /sessions` 回傳 `status` 欄位

- [ ] `internal/api/session.go` List handler 確保 JSON 含 `status`
- [ ] 前端 Session 列表初始化時即可顯示各 Session 狀態

---

## Phase E — 前端 Session 狀態角標

- [ ] Session 列表每項右側顯示狀態標示：
  - `running`：脈動動畫小圓點（與 THINKING 動畫同色系）
  - `awaiting_confirm`：紅色感嘆號角標
  - `idle`：無標示
- [ ] 狀態資料來源：
  - 初始載入：`GET /sessions` 的 `status` 欄位
  - 即時更新：每 5 秒輪詢 `GET /sessions`（簡單方案）
    - 後續可升級為全域監控 WS（`/ws/monitor`），只推 `{ sessionID, status }` 變更事件
- [ ] `awaiting_confirm` 角標可點擊，直接跳至該 Session 並顯示授權對話框

---

## Phase F — 前端切換 Session 恢復狀態

- [ ] 送出指令後，UI **不鎖定** Session 切換功能（移除目前的限制）
- [ ] 切換 Session 時：
  1. 斷開舊 WS（不影響後端任務執行）
  2. 建立新 WS
  3. 等待 `sync` 事件
  4. 渲染完整歷史訊息
  5. 若有 `pending` 訊息，以「串流中」樣式顯示，繼續接收 `delta`
  6. 若 `status=awaiting_confirm`，顯示授權對話框
- [ ] `pending` 訊息的顯示：與正常 STREAMING 狀態相同，但文字來自 DB 已累積內容 + 後續新 delta

---

## 相依關係

```
Phase A（DB）
    ↓
Phase B（任務解耦）  ←── 依賴 A.2 的 pending message 操作
    ↓
Phase C（廣播機制）  ←── 依賴 B.1 的 taskEntry.deltas
    ↓
Phase D（sync 事件） ←── 依賴 A.1 status、A.2 pending message、C.1 廣播
    ↓
Phase E（前端角標）  ←── 依賴 D.2 GET /sessions status
Phase F（前端恢復）  ←── 依賴 D.1 sync 事件格式
```

A → B → C → D 必須循序；E 和 F 可在 D 完成後並行開發。

---

## 注意事項

- **任務取消**：使用者仍可在 `THINKING`/`STREAMING` 狀態按「中斷」，這會呼叫 `taskManager.CancelTask`，行為不變。
- **多 WS 同一 Session**：`allow_once`/`set_mode` 授權訊息由任一連線送出即可，後端處理完廣播結果至所有連線。
- **DB 寫入頻率**：`delta` 觸發頻繁，`AppendMessageContent` 考慮 batching（每 100ms flush 一次）以避免過多 SQLite write。
- **`pending` 訊息孤兒處理**：伺服器重啟後若有 `status=pending` 的訊息，啟動時自動標記為 `done`（內容保留已累積部分）。
