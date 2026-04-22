# Session 列表：按工作目錄分組

## 目標
當同一個工作目錄有多個 Session 時，以可折疊的目錄群組呈現，避免列表過長。

## 範圍
- **純前端**，不需要異動後端（`work_dir` 已存在於 Session 物件）
- 修改檔案：`internal/static/Nexus/index.html`

---

## 實作步驟

### Step 1：新增 `groupByDir` 狀態與切換按鈕
- 在 `SessionList` component 加入 `const [groupByDir, setGroupByDir] = useState(false)`
- 搜尋列右側加一個 icon 按鈕（資料夾圖示），切換分組模式

### Step 2：計算分組資料（useMemo）
新增 `groupedSessions` memo，在 `groupByDir === true` 時啟用：

```js
// 範例邏輯（不是最終程式碼）
const groupedSessions = useMemo(() => {
  if (!groupByDir) return null;
  const map = new Map(); // dir → sessions[]
  for (const s of displaySessions) {
    const key = s.work_dir || '（未設定工作目錄）';
    if (!map.has(key)) map.set(key, []);
    map.get(key).push(s);
  }
  // 按群組內最新 last_active 排序群組
  return [...map.entries()].sort(
    (a, b) => sessionLastActiveMs(b[1][0]) - sessionLastActiveMs(a[1][0])
  );
}, [groupByDir, displaySessions]);
```

### Step 3：折疊狀態管理
- `const [collapsedDirs, setCollapsedDirs] = useState(new Set())`
- 點擊目錄標頭時 toggle 該目錄的折疊狀態

### Step 4：渲染分組列表
當 `groupByDir` 為 true，替換原本的 `displaySessions.map(s => ...)` 為分組渲染：

```
📁 /repo/claude-miniapp  (3)          ← 可點擊折疊，顯示 session 數量
   ├─ [SessionCard]
   ├─ [SessionCard]
   └─ [SessionCard]

📁 /repo/hermes  (1)
   └─ [SessionCard]

📁 （未設定工作目錄）  (2)
   └─ [SessionCard]
```

目錄標頭樣式：
- 小字 + 灰色，顯示目錄路徑的最後兩段（避免太長）
- 右側顯示 session 數量 badge
- 點擊整列折疊 / 展開
- 右側「+ 新 Session」按鈕，預填該目錄路徑

### Step 5：搜尋與分組互動
- 搜尋有結果時自動展開所有群組（忽略 `collapsedDirs`）
- 搜尋清空後恢復折疊狀態

---

## UI 細節

| 元素 | 描述 |
|---|---|
| 分組切換按鈕 | 搜尋列右側，folder 圖示，active 時 cyan 高亮 |
| 目錄標頭 | `text-[10px] uppercase tracking-wide text-slate-400`，左側 ▶/▼ 箭頭 |
| 縮排 | SessionCard 左側加 `pl-3 border-l border-cyan-500/10` |
| 空狀態 | 無 session 時不顯示群組 |

---

## 不需要做的事
- 後端 API 不動
- 資料庫 schema 不動
- 不需要持久化 `collapsedDirs`（刷新後重置即可）
