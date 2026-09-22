# `internal/static/` 前端開發指南

本目錄從單一 3500 行 `index.html` 拆成 `index.html` + `js/` 下依職責分資料夾的多個檔案（core / hooks / session / chat / ui）。這裡是純瀏覽器 no-build React，**沒有任何編譯步驟**，行為跟一般 npm 專案不同，動手改之前先看完 TL;DR。

## TL;DR

1. **完全沒有 build**：瀏覽器用 `@babel/standalone` 即時轉譯 JSX，沒有 webpack/vite/npm。改完存檔、重新整理瀏覽器即可看到結果。
2. **`<script>` 的順序即依賴順序**：各檔 top-level 的 `function`/`const`/`let` 共用同一個全域作用域，前面的檔案定義、後面的檔案才能用。順序錯了會直接 `ReferenceError`。資料夾只是分類，**不影響**共用作用域——載入順序完全由 `index.html` 裡 `<script>` 標籤的排列決定，跟檔案實際放在哪個資料夾無關。
3. `js/app.js` **必須排最後**——它含 `ReactDOM.createRoot(...).render(<App/>)`，且依賴前面所有檔案定義好的元件。
4. 任何 `<script>` **都不能加 `async`**，否則載入順序不保證，會打破上面的共用作用域假設。
5. 沒有模組系統：**不要寫 `import`/`export`**，新增的東西直接宣告在全域即可被其他檔案用。
6. 新增 `.js` 檔不用改 Go 也不用重編譯後端——Go 用 `app.Static` 直接從磁碟提供整個目錄。

## 檔案地圖

`index.html` 現在只剩 `<head>`（CDN script 標籤、Babel 註冊、`<style>`）+ `<div id="root">` + 依序載入的 `<script type="text/babel" data-presets="react-classic" src="./js/xxx.js">`。實際載入順序（見 `index.html` 底部）：

| # | 檔案 | 內容 |
|---|------|------|
| 1 | `js/core/core.js` | 第一行 `const { useState, ... } = React;`（全域 hook 別名，所有元件都靠這行）。Telegram WebApp 初始化、localStorage 偏好、API base 推算（`resolveApiUrl`）、session 過期/401 處理、`wsURL`、`parseMarkdown`、基礎 hooks（`useMediaQuery`/`useChatHeaderCollapsed`）、label 工具。 |
| 2 | `js/ui/ui-atoms.js` | 小型展示元件與相鄰工具：`SessionStateChip`、各種 Badge/Chip、`PermMode`/`Model`/`Effort` Select、`QuotaBadge`、`ShellOutput`、`InputModeTab`、`ModeToggleBtn`、`MessageCopyButton`、cli-arg 解析、input-mode 儲存等。 |
| 3 | `js/hooks/useChatSocket.js` | `ChatView` 的 WebSocket 連線/重連、串流解析、訊息與設定狀態，抽出 `{ messages, state, send, flushPendingModes, ... }`。 |
| 4 | `js/hooks/useNewSessionForm.js` | `SessionView` 建立表單與 `ForwardModal` 新建會話共用的表單狀態＋payload 組裝邏輯。 |
| 5 | `js/chat/ForwardModal.js` | `ForwardModal` 元件。 |
| 6 | `js/session/SessionView.js` | `SessionView`（session 列表畫面）。 |
| 7 | `js/chat/chat-header.js` | `SlashCommandMenu` + `ChatSessionHeader`。 |
| 8 | `js/chat/MentionMenu.js` | `@mention` 選單與 token／`[miniapp]` 展開（詢問／討論，不是 Forward）。 |
| 9 | `js/chat/ChatView.js` | `ChatView`（聊天主畫面，render + 操作 handler；WS 邏輯已移至 `useChatSocket`）。 |
| 10 | `js/app.js` | `PasswordView`、`DebugBanner`、`App`，以及 `ReactDOM.createRoot(document.getElementById('root')).render(<App />)`。**必須最後載入**。 |

後端：`cmd/server/main.go:136` 的 `app.Static("/", "./internal/static")`（Fiber）直接把整個目錄當靜態檔案伺服，**沒有 `go:embed`**。這代表：
- 新增/修改 `js/*.js` 立刻生效，不用重啟、不用重編譯 Go binary。
- 只有改 Go 程式碼本身（路由、中介層等）才需要重編譯。

## 跨檔共用作用域的原理

`@babel/standalone` 對每個 `type="text/babel"` 的 `<script>` 做轉譯後，是透過「注入一個沒有 `type` 屬性的 classic `<script>`」來執行轉譯結果的。這意味著：

- 每個檔案 top-level 宣告的 `function`、`const`、`let` 會進入**同一個全域詞法環境**，不是各自獨立的模組作用域。
- A 檔定義的東西，B 檔可以直接用——**前提是 A 的 `<script>` 標籤排在 B 前面**，因為 script 是依序注入、依序執行的。
- 一旦某個 `<script>` 加了 `async`，瀏覽器不保證依原順序執行，會打破「前面先定義」的假設，導致間歇性的 `ReferenceError`。**所以每個 script 標籤都不能加 `async`**（目前也確實都沒有）。

有一個重要的例外：**元件互相參照（發生在 render body / JSX 裡）不受定義先後影響**，因為所有 JSX 只有在 `App` 被 `render()` 呼叫、實際渲染時才會執行，那時所有檔案早已全部載入完畢。例如 `chat/ChatView.js`（第 9 個）裡的 JSX 可以引用 `app.js`（第 10 個）定義的東西也不會報錯，只要不是在 `ChatView.js` 的 top-level 直接執行。

真正需要在意順序的，是**檔案載入時就會立即執行的 top-level 程式碼**（例如 `core.js` 開頭那行 `const { useState, ... } = React` 的解構賦值、模組層級的初始化邏輯、`window.Telegram?.WebApp` 檢查等）。這類程式碼依賴的東西，必須在更前面的檔案已經定義好。

每個 `<script>` 標籤都帶 `data-presets="react-classic"`，代表**各檔案是獨立轉譯的**，Babel 不會把所有檔案合併成一份再轉譯——所以型別/語法錯誤只會出現在對應的那個檔案裡，但共用作用域仍然成立（轉譯是獨立的，執行時的全域環境是共用的）。

## 新增或修改元件的步驟

**修改既有元件**：直接找到對應檔案（依上面檔案地圖）編輯，存檔後重新整理瀏覽器。不用管順序，因為檔案本身沒動。

**新增元件時**：
1. 決定新元件屬於哪個既有檔案，或是否要開新檔。
2. 若開新檔：
   - 決定它依賴誰（用了哪些其他檔案定義的東西），把新 `<script>` 標籤插在**所有它依賴的檔案之後**。
   - 若它會被其他既有檔案在 top-level 直接呼叫/使用（少見），則要排在那些檔案**之前**；若只是被 JSX render body 參照，順序不重要（只要不排在 `app.js` 之後，因為 `app.js` 必須是最後一個）。
   - 在 `index.html` 裡插入 `<script type="text/babel" data-presets="react-classic" src="./js/<資料夾>/新檔名.js"></script>`，**不要加 `async`**。
   - `js/app.js` 永遠是最後一個 `<script>`，新檔案一律插在它前面。
3. 若加在既有檔案裡，遵照該檔案現有的匯出風格（直接宣告全域 `function`/`const`，不用 `export`）。
4. 全程不要寫 `import`/`export`——這是全域 script，不是 ES module。

## 驗證方式

- **一般開發**：改完直接重新整理瀏覽器，no-build，不需要任何編譯指令。
- **語法檢查（可選，建議大改/切檔後執行）**：因為沒有編譯期把關，`@babel/standalone` 的轉譯錯誤只會在瀏覽器 console 裡以 runtime 錯誤出現。若要在改動後快速確認每個檔案語法正確，可寫一個小 Node 腳本，用 `@babel/standalone`（或 `@babel/core` + `@babel/preset-react`）對 `js/*.js` 逐一跑 `Babel.transform(source, { presets: ['react'] })`，任何拋出的 SyntaxError 即代表該檔案有問題。這不是專案既有的自動化流程，只是建議的手動排查手段。

## 備份與還原

- 拆分前的原始單檔備份於 repo 根目錄：`backups/index.html.20260805.bak`。
- 若拆分後的多檔版本出問題需要緊急還原，可將該備份檔內容複製回 `internal/static/index.html`，並移除/忽略 `internal/static/js/` 目錄（`app.Static` 只會依 `index.html` 裡宣告的 `<script src>` 載入檔案，不會自動抓目錄下所有檔案）。
