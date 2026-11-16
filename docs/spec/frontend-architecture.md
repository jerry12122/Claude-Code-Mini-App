# 前端架構規範 (Frontend Architecture)

本文件是 `internal/static/` 的分層與命名約定。規則以 `scripts/frontend-layering.test.cjs` 自動驗證，
已知的既存違規列在該腳本的例外清單與本文第 7 節，**例外只能減少，不得新增**。

---

## 0. 前提（不得更動）

這些是 `CLAUDE.md`「架構決策」的延伸，改動需要先改架構決策：

* **無前端 build step**：repo 裡沒有 npm、`package.json` 或打包設定。JSX 由 Go 伺服器在執行時轉換：
  `internal/static/bundle.go` 依 `index.html` `<body>` 內 `<script src="./js/...">` 的順序，用 esbuild
  逐檔轉 JSX（`react-classic`，等同 `React.createElement`），再串接成單一虛擬檔 `js/app.bundle.js`，
  並把 `index.html` 改寫成只載入這一支。開發建置在檔案變動時重新編譯，production 只編一次。
  `<script type="text/babel">` 標籤是歷史寫法，實際不經過瀏覽器端 Babel。
* **無模組系統**：esbuild 只做 transform、不做 bundle，不解析 `import`／`export`。
  所有符號都在**同一個全域作用域**。
  `window.*` 不作為匯出手段（目前應用層 `window.X =` 匯出為 0 處，vendor 不算）。
* **載入順序就是依賴圖**：`internal/static/index.html` 的 `<script>` 順序決定可見性。
  被依賴者必須排在前面。
* **單一二進位**：靜態檔由 Go `embed` 提供，不分離前後端部署。

**後果**：新增檔案時忘記在 `index.html` 註冊、或註冊位置排錯，錯誤只會在執行時以
`X is not defined` 出現。任何一支檔案有語法錯誤，整個 `js/app.bundle.js` 會回 500，整頁空白。
這是上述取捨的既定代價，由第 6 節的檢查守住。

---

## 1. 分層與依賴方向

```
vendor/        ← 第三方，不修改
  ↑
core/          ← 平台與基礎設施
  ↑
hooks/         ← 狀態與邏輯（無 JSX）
  ↑
ui/ chat/ session/ share/ setup/   ← 呈現層
  ↑
app.js         ← 組裝與路由
```

**依賴只能往下。** 下層不得引用上層定義的任何符號（含型別、常數、元件）。
平行的呈現層目錄之間可以互相引用（`chat/` 用 `ui/` 的元件是正常的），但不得形成
「`ui/` 反過來依賴 `chat/`」這種回邊。

`index.html` 的 `<head>` 只放與 React 無關的獨立腳本（`wails-runtime-ready.js`、
`devtool-easter-egg.js`）與 vendor；應用層腳本全部在 `<body>`。

---

## 2. 各層職責

### `core/` — 平台與基礎設施

唯一允許封裝「與外界打交道」的那一層。放這裡：

* 執行環境判定（Telegram WebApp、訪客分享模式、桌面版）
* 認證與 token 保管（web session、guest token、TG initData）
* HTTP 與 WS 的位址解析與包裝：`resolveApiUrl`、`apiFetch`、`fetchWithTimeout`、
  `wsBaseURL`、`wsURL`、`eventsWsURL`
* Markdown 解析與消毒（`parseMarkdown`、`renderMarkdownHTML`）
* 主題、外觀、字級、側欄等**全域**偏好的讀寫與套用
* 跨層共用的顯示字串常數（`PERM_MODE_LABEL`、`AGENT_LABEL`、`SESSION_STATE_LABEL`）
* 不含 React 的純 DOM 工具：`showToast`、`copyText`、`hapticTap`（在 `core/toast.js`）

目前有兩支檔案：`core/core.js`（平台、認證、API／WS、Markdown、偏好）與 `core/toast.js`。

**不放**：任何 JSX、任何只有單一功能用得到的邏輯。

裸 `fetch(` 只允許出現在 `core/`，其他層一律走 `apiFetch` / `fetchWithTimeout`。

### `hooks/` — 狀態與邏輯

**此層不得含 JSX**（目前 7 支檔案皆為 0 處，這條是硬規則）。

兩種檔案都放這裡，用命名區分：

| 種類 | 命名 | 例 |
|---|---|---|
| React hook | `use` + PascalCase | `useChatSocket.js`、`useChatComposer.js` |
| 純邏輯／reducer／純函式 | camelCase 名詞 | `tabGroups.js`、`composerFiles.js`、`telegramTimeline.js` |

> 為什麼不為純邏輯另開 `state/` 目錄：這層的共同性質是「無 UI 的邏輯」，
> 再切一層只會多一個目錄和 7 份 spec 的路徑要改，命名前綴已經足夠區分。

`new WebSocket(...)` 的使用限於**擁有該連線生命週期**的檔案：
對話連線在 `hooks/useChatSocket.js`，轉送／第一則訊息的短命連線在 `hooks/forwardPrompt.js`，
全域事件連線在 `session/SessionView.js`，日誌連線在 `ui/LogsSection.js`。
呈現層的通用元件不得自行開連線送訊息。

### `ui/` — 通用呈現元件

跨功能複用的元件：按鈕、下拉、分頁、分割版面、對話框、設定面板。

**不放**：

* 領域邏輯（CLI 參數解析、quota 百分比計算、訊息列轉換）→ `hooks/`
* 送出對話訊息、開 session WebSocket → `hooks/useChatSocket.js`
* 草稿／待送輸入的持久化 → `hooks/`

元件自己的 UI 偏好讀寫 `localStorage` 是允許的（例如展開狀態）；
但一旦某個 key 有多於一處讀寫，就移到 `core/`。storage key 沿用
`cc_` 或 `miniapp_` 前綴，且以頂層常數定義，不寫字面量在使用處。

### `chat/`、`session/`、`share/`、`setup/` — 功能呈現層

各自功能專屬的元件。一個檔案一個主要元件；同檔內的輔助元件僅限
「只被此檔使用、且不超過 50 行」的小片段，否則獨立成檔。

### `app.js` — 組裝

路由（密碼頁 / 訪客頁 / 主控台 / 設定精靈）、頂層狀態組裝、版面骨架。
不放可被複用的元件。

---

## 3. 檔案命名

| 檔案內容 | 命名 | 例 |
|---|---|---|
| 匯出 React 元件 | PascalCase | `ChatView.js`、`SessionTabs.js` |
| 邏輯、hook、工具 | camelCase | `tabGroups.js`、`setupForm.js` |

**不使用 kebab-case。** 例外只有 `<head>` 載入的兩支獨立腳本
（`wails-runtime-ready.js`、`devtool-easter-egg.js`），它們不屬於 React 應用。

目錄名全小寫。`vendor/` 內檔名維持上游原樣。

---

## 4. 檔案大小

**單檔上限 850 行。** 超過就拆：先把獨立元件搬成同目錄新檔，再抽邏輯到 `hooks/`。

850 不是美學數字。它取在現況第四大檔（`session/SessionView.js`，816 行）之上一點，
目的是讓規則上線時只留下三個真正需要處理的例外，而不是把一半的大檔都寫進白名單——
白名單一長，規則就沒有約束力了。實務意義是「一個檔案還能一次讀完」，也讓
`index.html` 的依賴順序維持在可人工追蹤的規模。

---

## 5. 新增檔案的步驟

1. 依第 1、2 節決定目錄，依第 3 節命名。
2. 在 `index.html` 的 `<body>` 加 `<script type="text/babel" data-presets="react-classic" src="./js/<dir>/<file>.js"></script>`。
   **位置**：排在它引用的所有檔案之後、引用它的所有檔案之前。
   `type` 屬性不影響實際行為（bundler 只看 `src="./js/..."`），沿用既有寫法保持一致即可。
   現有 `chat/HTMLPreview.js`、`chat/MermaidBlock.js` 用普通 `<script>`，一樣會被編進 bundle。
3. 非 trivial 的邏輯在 `scripts/` 留一支 `*.test.cjs`，沿用既有慣例：
   `node:assert/strict` + `node:fs` + `node:vm` 讀原始檔執行，無框架、無 `package.json`。
4. 跑 `node scripts/frontend-layering.test.cjs`。

---

## 6. 驗證

```bash
node scripts/frontend-layering.test.cjs
```

檢查項目：

| 編號 | 規則 | 來源 |
|---|---|---|
| R1 | 每支應用層 `js` 都在 `index.html` 註冊；註冊的路徑都存在 | 第 0 節 |
| R2 | `core/` 不引用上層符號 | 第 1 節 |
| R3 | `hooks/` 不含 JSX | 第 2 節 |
| R4 | 裸 `fetch(` 只在 `core/`；`new WebSocket` 只在連線擁有者 | 第 2 節 |
| R5 | `js/` 下無 kebab-case 檔名 | 第 3 節 |
| R6 | 單檔 ≤ 850 行 | 第 4 節 |

腳本內的 `KNOWN_VIOLATIONS` 是既存違規白名單。**新增檔案不得寫進去**；
處理掉一項就從清單刪一項。

---

## 7. 現存例外（待遷移）

| 檔案 | 違反 | 處理方向 |
|---|---|---|
| `chat/ChatView.js`（1843 行） | R6 | 拆出 `LiveActivity`、`QuestionCard`、`PermToolDetail`、`QueuedMessages`、`ChatImageLightbox`、`TelegramOlderControl` 成同目錄獨立檔 |
| `hooks/useChatSocket.js`（901 行） | R6 | 僅略超，低優先 |

改名需同步更新 `index.html` 與 `docs/spectra/specs/*/spec.md` 內的路徑記錄。

### 已處理

`ui/ui-atoms.js`（1352 行，同時違反 R4／R5／R6）已依第 2 節拆分，84 個頂層符號分佈如下：

| 目的地 | 符號數 | 內容 |
|---|---|---|
| `ui/uiAtoms.js` | 41 | 真正的 UI 原子與元件（下拉、晶片、badge、`ShellOutput`、`useDialogFocus` 等） |
| `hooks/composerDrafts.js` | 15 | 草稿與待送輸入的持久化、每個 session 的輸入模式 |
| `hooks/quotaMetrics.js` | 10 | quota 版型表與數值計算（顏色函式留在 ui） |
| `hooks/messageRows.js` | 6 | `mapMessageRow` 與 SQLite datetime 解析／時間格式 |
| `hooks/cliArgs.js` | 4 | 自訂 CLI 引數解析與 `--model` 取出／合併 |
| `hooks/forwardPrompt.js` | 4 | 轉送、第一則訊息的短命 WS 送出（連線擁有者）、接手草稿 |
| `core/toast.js` | 4 | `showToast`／`copyText`／`hapticTap` 等純 DOM 工具 |

`showToast` 等工具移到 `core/` 的原因：`hooks/composerDrafts.js` 需要它，若留在 `ui/`
會形成 hooks → ui 的反向依賴，違反第 1 節。這些函式不含 React，只操作 DOM，屬於基礎設施。

同批改名：`chat/chat-header.js` → `chat/ChatHeader.js`、`ui/desktop-update.js` → `ui/desktopUpdate.js`。
Go 端的 `desktop-update:check`／`desktop-update-state` 是訊息前綴與事件名（見
`internal/desktopupdate/script.go`），與檔名無關，未改動。
