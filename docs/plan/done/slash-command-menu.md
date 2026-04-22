# Slash 命令選單

## 目標
在聊天輸入框輸入 `/` 時，彈出可鍵盤／點擊操作的命令選單，類似 Telegram Bot 命令列表。
命令清單**可配置**，集中定義在一個 `SLASH_COMMANDS` 陣列，方便日後新增或移除。

## 範圍
- **純前端**，不需要異動後端
- 修改檔案：`internal/static/index.html`

---

## 資料結構

在檔案頂部（常數區）定義可配置的命令清單：

```js
const SLASH_COMMANDS = [
  {
    command: '/reset',
    description: '清除對話 context（等同 /clear）',
    // 可選：限定只在特定 inputMode 顯示
    // modes: ['agent'],
  },
  {
    command: '/clear',
    description: '清除對話 context（等同 /reset）',
  },
];
```

每個項目欄位：

| 欄位 | 型別 | 說明 |
|---|---|---|
| `command` | `string` | 命令文字，含 `/` 前綴 |
| `description` | `string` | 顯示在選單右側的說明文字 |
| `modes` | `string[]`（可選） | 限定顯示的 inputMode（`'agent'`、`'shell'`），省略表示全部 |

---

## UI 行為

### 觸發條件
- 輸入框內容以 `/` 開頭時顯示選單
- 繼續輸入會即時過濾符合命令（不分大小寫）
- 輸入不符合任何命令、或內容不以 `/` 開頭時，選單消失

### 選單互動
| 操作 | 行為 |
|---|---|
| ↑ / ↓ | 移動高亮項目 |
| Enter（選單開啟中） | 選擇高亮命令，填入輸入框並**立即送出** |
| Esc | 關閉選單，游標回到輸入框 |
| 點擊某項 | 同 Enter：填入並立即送出 |
| 點擊選單外 | 關閉選單 |

### 位置與樣式
- 絕對定位，緊貼輸入框**上方**
- 最多顯示 6 項（超過可捲動）
- 每列：左側 `command`（indigo 色 monospace）、右側 `description`（gray-400）
- 高亮列用 `bg-gray-700` 標示

---

## 實作步驟

### Step 1：新增 `SLASH_COMMANDS` 常數
在 `index.html` 頂部常數區（`INPUT_MODE_STORAGE_PREFIX` 附近）加入命令陣列。

### Step 2：新增 `SlashCommandMenu` 元件
```jsx
function SlashCommandMenu({ items, activeIndex, onSelect }) { ... }
```
- `items`：過濾後的命令清單
- `activeIndex`：當前高亮索引
- `onSelect(command)`：選中時的 callback

### Step 3：在 `ChatView` 中加入 slash 選單狀態
```js
const [slashMenuItems, setSlashMenuItems] = useState([]);  // 過濾後清單
const [slashActiveIdx, setSlashActiveIdx] = useState(0);   // 高亮索引
const slashMenuOpen = slashMenuItems.length > 0;
```

### Step 4：修改 `input` 的 onChange
當 `value` 以 `/` 開頭時，依當前 `inputMode` 過濾 `SLASH_COMMANDS`：
```js
const q = value.toLowerCase();
const filtered = SLASH_COMMANDS
  .filter(c => (!c.modes || c.modes.includes(inputMode)) && c.command.startsWith(q));
setSlashMenuItems(filtered);
setSlashActiveIdx(0);
```
否則清空 `slashMenuItems`。

### Step 5：修改 `handleKeyDown`
```js
if (slashMenuOpen) {
  if (e.key === 'ArrowDown') { e.preventDefault(); setSlashActiveIdx(i => Math.min(i+1, slashMenuItems.length-1)); return; }
  if (e.key === 'ArrowUp')   { e.preventDefault(); setSlashActiveIdx(i => Math.max(i-1, 0)); return; }
  if (e.key === 'Enter')     { e.preventDefault(); handleSlashSelect(slashMenuItems[slashActiveIdx].command); return; }
  if (e.key === 'Escape')    { setSlashMenuItems([]); return; }
}
// 原本 Enter 邏輯不動
```

### Step 6：新增 `handleSlashSelect`
```js
const handleSlashSelect = (command) => {
  setInput(command);
  setSlashMenuItems([]);
  // 直接觸發送出（等同使用者按 Enter）
  // 注意：setInput 是非同步，需直接用 command 傳給 handleSend
  handleSendText(command);
};
```
將原本 `handleSend` 重構為接受可選參數 `handleSend(overrideText?)` 以支援直接傳入命令文字。

### Step 7：在輸入框上方插入 `<SlashCommandMenu>`
在 `<textarea>` 的外層 `div` 改為 `relative`，在 textarea 前插入：
```jsx
{slashMenuOpen && (
  <SlashCommandMenu
    items={slashMenuItems}
    activeIndex={slashActiveIdx}
    onSelect={handleSlashSelect}
  />
)}
```

---

## 後續擴充（不在本次範圍）

- 從後端 API 動態拉取命令清單（支援 per-session 客製化）
- 命令參數補全（如 `/mode <tab>` 展開子選項）
- 命令執行後顯示 toast 確認訊息
