# 拒絕權限請求功能

## 問題描述

當 Claude 觸發 `permission_denials` 時，系統進入 `AWAITING_CONFIRM` 狀態，前端顯示授權面板。目前面板只有「允許此操作」與「允許並記住」兩個選項，**完全缺少「拒絕」按鈕**，導致使用者無法拒絕授權，系統卡死在等待狀態。

---

## 設計決策

### 拒絕後的行為

「拒絕」的語意：使用者不允許 Claude 執行被拒絕的工具操作，並讓 Claude 知道「這個操作被拒絕了」，由 Claude 自行決定如何繼續（回報無法完成、改用其他方式等）。

兩種方案比較：

| | 方案 A：靜默取消 | 方案 B：帶訊息 retry（本方案） |
|---|---|---|
| 行為 | 清除 pending + 回 IDLE，Claude 不知道發生什麼 | 帶「[user denied: <tool_name>]」prompt 重新 resume |
| 優點 | 簡單 | Claude 可繼續回應（例如說「好，我不會執行」） |
| 缺點 | 任務掛起，沒有結尾 | 需要多一次 claude 呼叫 |

**採用方案 B**：拒絕後以 `"[Permission denied by user]"` prompt 重新 resume，讓 Claude 有機會給出結尾回應，任務才有完整的收尾。

---

## 實作範圍

### 1. 後端 `internal/ws/handler.go`

新增 `deny_once` case：

```go
case "deny_once":
    if !isClaude {
        continue
    }
    clearPendingDenials(database, sessionID)
    runAgent("[Permission denied by user. Please acknowledge and stop the current operation.]", nil)
```

- 清除 `pending_denials`
- 以固定 prompt 重新 resume，讓 Claude 有機會結尾
- 不需要改 DB schema，利用現有的 `runAgent`

### 2. 前端：三個 HTML 檔案

三個主題都需要在授權面板加入「拒絕」按鈕：

#### `internal/static/index.html`（預設主題）

在 `AWAITING_CONFIRM` 面板的 `flex gap-2 mt-3` div 裡，「允許並記住」按鈕之後加入：

```jsx
<button onClick={handleDenyOnce}
  className="px-3 py-1.5 bg-red-900 hover:bg-red-800 text-white rounded-lg text-xs">
  拒絕
</button>
```

並新增 handler：
```js
const handleDenyOnce = () => {
  send({ type: 'deny_once' });
  setPermTools([]);
};
```

#### `internal/static/Nexus/index.html`（Nexus 主題）

相同邏輯，按鈕樣式配合 Nexus 風格：
```jsx
<button onClick={handleDenyOnce}
  className="px-4 py-2 bg-red-900/80 hover:bg-red-800 text-white rounded-xl text-xs font-semibold">
  拒絕
</button>
```

#### `internal/static/Focus/index.html`（Focus 主題）

相同邏輯，按鈕樣式配合 Focus 風格（與 Nexus 相同）。

---

## 實作步驟

- [ ] **後端**：`internal/ws/handler.go` 新增 `deny_once` case
- [ ] **前端 - index.html**：授權面板加「拒絕」按鈕 + `handleDenyOnce`
- [ ] **前端 - Nexus/index.html**：同上
- [ ] **前端 - Focus/index.html**：同上

---

## 測試要點

1. 觸發 permission_denials（用 `default` mode 執行寫檔操作）
2. 點「拒絕」後，Claude 應回應「無法完成操作」之類的訊息並結束
3. 狀態應從 `AWAITING_CONFIRM` → `THINKING/STREAMING` → `IDLE`
4. 再次輸入新訊息應正常運作

---

## 不在範圍內

- 拒絕後「記住拒絕」（永久封鎖某 tool）— 過度複雜，目前不需要
- 「拒絕並結束 session」— 現有 reset_context 已可處理
