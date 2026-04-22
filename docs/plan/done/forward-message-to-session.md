# Forward 訊息到其他會話

## 目標
在聊天室的 assistant 訊息上新增「Forward」按鈕，讓使用者可以將規劃內容（含備註）轉發到指定的其他會話執行。

## 範圍
- **純前端**，後端不需要修改
- 修改檔案：`internal/static/Nexus/index.html`

---

## 實作步驟

### Step 1：訊息列 hover 按鈕列
- assistant 訊息 hover 時，在右上角顯示操作按鈕列
- 現有「複製」按鈕保留，新增「Forward →」按鈕
- icon 使用 `lucide-react` 的 `Forward` 或 `Share2`

### Step 2：Forward Modal 狀態管理
新增以下 state：

```js
const [forwardModal, setForwardModal] = useState(null)
// null = 關閉
// { messageContent: string } = 開啟，帶入要轉發的訊息內容
```

### Step 3：Forward Modal UI

Modal 內容分三區：

**① 目標會話選擇**
- 列出所有現有 sessions（顯示 agent type + work_dir）
- 最後一項為「+ 新建會話」
- 選中時以 cyan 高亮顯示

**② 新建會話展開區（僅在選「+ 新建會話」時顯示）**
```
複製自：[下拉選單，選現有 session] ← 自動帶入以下欄位
Agent：  [claude / cursor / shell ▼]
工作目錄：[text input]
```
- 選「複製自」後，自動帶入對應 session 的 agent_type、work_dir、model、extra_args
- 欄位可手動覆蓋
- 不複製對話歷史，新 session 全新開始

**③ 備註欄（可選）**
```
備註（可選）：
┌─────────────────────────┐
│ 請依照以下規劃執行...    │
└─────────────────────────┘
```

**④ 底部按鈕**
- `[取消]` `[Forward]`

### Step 4：組合送出內容

Forward 實際送出的 prompt 格式：

```
{備註內容}

---
（以下為規劃內容）

{原訊息內容}
```

若備註為空，則只送原訊息內容。

### Step 5：Forward 執行邏輯

1. 若選擇**現有 session**：
   - 直接透過 WebSocket 連線到目標 session
   - 送出組合後的 prompt
   - 關閉 Modal，可選擇是否跳轉到目標 session

2. 若選擇**新建 session**：
   - 呼叫現有 `POST /api/sessions` 建立 session（帶入複製的設定）
   - 建立後透過 WebSocket 連線並送出 prompt
   - 關閉 Modal，跳轉到新 session

### Step 6：跳轉提示

Forward 成功後，訊息列顯示小提示：
```
已 Forward 到「cursor / /repo/hermes」[前往查看 →]
```

---

## UI 細節

| 元素 | 描述 |
|---|---|
| Forward 按鈕 | hover 才顯示，icon + 文字「Forward」，`text-slate-400 hover:text-cyan-400` |
| Modal 寬度 | `max-w-md`，置中 |
| Session 選項 | `text-sm`，左側顯示 agent badge（顏色與現有 session 列表一致），右側顯示 work_dir 最後兩段 |
| 新建會話區 | `mt-2 p-3 rounded-lg bg-slate-800/50 border border-slate-700` 內嵌展開，不跳頁 |
| 備註欄 | `textarea rows=3`，placeholder `加上備註給目標會話（可選）` |

---

## 不需要做的事
- 後端 API 不動
- 不需要持久化 Forward 紀錄
- 不需要支援一次 Forward 多個訊息
