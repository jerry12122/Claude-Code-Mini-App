# 中斷功能修正計劃

> 目標：修正 Cursor / Gemini 在 Windows 上無法正常中斷的問題，並補上伺服器重啟後的狀態修復機制。
>
> 背景：`cursor-agent` 與 `gemini` 在 Windows 是 `.cmd` → `node.exe` 的 wrapper 鏈。
> Go 的 `exec.CommandContext` 只殺直接子進程（`cmd.exe`），`node.exe` 成為孤兒進程繼續執行，
> 持有 stdout pipe 的寫入端，導致 scanner goroutine 永久卡住並持續廣播過期事件。
> 同時，伺服器 crash 後 DB 殘留 `status=running`，重啟後前端卡在 THINKING 狀態。

---

## 問題清單

| # | 問題 | 嚴重度 |
|---|---|---|
| P1 | Windows 孤兒進程：`cmd.exe` 死掉但 `node.exe` 繼續跑，pipe 不關閉 | 高 |
| P2 | 孤兒 goroutine 廣播：context 取消後 callback 仍廣播 STREAMING/IDLE，污染前端狀態 | 高 |
| P3 | 伺服器 crash 後 `status=running` 殘留，重啟無清理，前端永久卡在 THINKING | 中 |
| P4 | 強制終止 ≠ Ctrl+C：force kill 不讓 CLI 優雅儲存 session 狀態，影響 `--resume` | 中 |

---

## 總覽

```
Phase A — Callback 加 context 保護（治 P2，最快見效）
Phase B — 伺服器啟動狀態修復（治 P3）
Phase C — Windows 進程樹終止（治 P1）
Phase D — 優雅停止：先 Ctrl+C 再 force kill（治 P4）
```

---

## Phase A — Callback 加 context 保護

> 最快見效：即使孤兒進程還活著，也阻止它的輸出污染前端狀態。

### A.1 修改 `internal/ws/handler.go`

- [ ] 在 runner goroutine 的 `cb` 最前面加 `ctx.Err()` 檢查：

```go
err := runner.Run(ctx, opts, func(e agent.Event) {
    if ctx.Err() != nil {
        return  // context 已取消，丟棄所有事件
    }
    switch e.Type {
    // ...現有邏輯不變
    }
})
```

### A.2 確認效果

- [ ] 中斷後前端不再出現 STREAMING 回跳現象
- [ ] 孤兒進程的輸出靜默丟棄，不寫入 DB，不廣播 WS

---

## Phase B — 伺服器啟動狀態修復

> 修正 crash 後重啟，前端卡在 THINKING 的問題。

### B.1 新增啟動清理函式（`internal/db/session.go`）

- [ ] 新增 `ResetRunningSessions() error`：

```go
func (db *DB) ResetRunningSessions() error {
    _, err := db.Exec(
        `UPDATE sessions SET status = ? WHERE status = ?`,
        SessionStatusIdle, SessionStatusRunning,
    )
    return err
}
```

### B.2 於伺服器啟動時呼叫（`cmd/server/main.go`）

- [ ] 在 DB 初始化後、WebSocket handler 掛載前呼叫：

```go
if err := database.ResetRunningSessions(); err != nil {
    log.Printf("[startup] ResetRunningSessions 失敗: %v", err)
}
```

### B.3 同步修復 pending 訊息

- [ ] 新增 `ResetPendingMessages() error`，將所有 `status=pending` 的訊息標為 `done`：

```go
func (db *DB) ResetPendingMessages() error {
    _, err := db.Exec(
        `UPDATE messages SET status = ? WHERE status = ?`,
        MessageStatusDone, MessageStatusPending,
    )
    return err
}
```

- [ ] 啟動時同樣呼叫此函式

---

## Phase C — Windows 進程樹終止

> 確保 `node.exe`（孫進程）也被殺掉，pipe 真正關閉。

### C.1 建立跨平台 kill 工具（`internal/proc/kill.go`）

- [ ] 新增 `KillTree(pid int) error`：
  - **Windows**：執行 `taskkill /F /T /PID <pid>`
  - **Linux/macOS**：`syscall.Kill(-pid, syscall.SIGKILL)`（kill process group）

```go
//go:build windows

func KillTree(pid int) error {
    cmd := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid))
    return cmd.Run()
}
```

```go
//go:build !windows

func KillTree(pid int) error {
    return syscall.Kill(-pid, syscall.SIGKILL)
}
```

- [ ] Linux 版需在啟動子進程時設 `cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}`，讓子進程獨立 process group

### C.2 在各 Runner 加入 process group 設定（`internal/cursor/runner.go`、`internal/gemini/runner.go`）

- [ ] Linux 版加入 `SysProcAttr`：

```go
// go:build !windows
cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
```

### C.3 修改 `exec.CommandContext` 的取消行為

- [ ] 使用 `cmd.Cancel`（Go 1.20+）替換預設行為，改為呼叫 `KillTree`：

```go
cmd.Cancel = func() error {
    if cmd.Process != nil {
        return proc.KillTree(cmd.Process.Pid)
    }
    return nil
}
```

---

## Phase D — 優雅停止（先 Ctrl+C 再 force kill）

> 讓 CLI 有機會儲存 session 狀態，確保 `--resume` 可正常接續。

### D.1 建立優雅終止流程（`internal/proc/kill.go`）

- [ ] 新增 `GracefulStop(pid int, timeout time.Duration) error`：

```go
// Windows：GenerateConsoleCtrlEvent → 等待 timeout → taskkill /F /T
// Linux：SIGINT → 等待 timeout → SIGKILL（process group）
```

- [ ] Windows 注意事項：`GenerateConsoleCtrlEvent` 需要目標進程在同一 console group，
  否則直接退回 `taskkill /F /T`

### D.2 更新 Runner 的 Cancel 邏輯

- [ ] `cmd.Cancel` 改為呼叫 `proc.GracefulStop(pid, 3*time.Second)`

### D.3 確認各 CLI 的 session 狀態儲存時機

- [ ] 實測 Claude Code：Ctrl+C 後 `--resume` 是否能正常接續
- [ ] 實測 Cursor：Ctrl+C 後 `--resume` 是否保留上下文
- [ ] 實測 Gemini：Ctrl+C 後 `--resume` 是否保留上下文

---

## 相依關係圖

```
Phase A（callback 保護）   ← 可立即獨立執行，不依賴其他 Phase
Phase B（啟動清理）         ← 可立即獨立執行
Phase C（進程樹終止）       ← 需先確認各平台行為
    ↓
Phase D（優雅停止）         ← 依賴 Phase C 的基礎建設
```

A、B 可並行，C 完成後再做 D。

---

## 注意事項

- **Phase A 是最低成本的緊急修補**，即使 C、D 尚未完成也能阻止前端狀態污染。
- **Phase C 的 `taskkill /F /T` 仍是 force kill**，session 狀態可能不完整；Phase D 才是真正的優雅停止。
- **Gemini 走 stdin**，stdin 在父進程死後會關閉，`node.exe` 可能比 Cursor 更快自然退出，但不能保證。
- **`--resume` 的可靠性**最終取決於各 CLI 工具自身的 session flush 邏輯，超出本 app 控制範圍；Phase D 只是盡力提供機會。
