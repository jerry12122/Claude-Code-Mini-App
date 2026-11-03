## Context

整個前端（`internal/static`，Tailwind CDN + React UMD + Babel standalone，無建置步驟）只有深色：

- 灰階用 Tailwind 任意值寫死（`bg-[oklch(0.19_0.02_264)]`、`text-[oklch(0.85_0.01_264)]`、`border-[oklch(0.28_0.02_264)]`，含 `hover:`/`active:`/`placeholder-`/`divide-`/`ring-` 變體），約 190 行、17 個 JS 檔。
- 另有約 240 處 `gray-*`/`slate-*` 色階 class，以及淡色強調字（`violet/red/amber/yellow/orange/sky/emerald` 的 100–400）。
- `index.html` 內建 `<style>` 約 40 處寫死 oklch；`app.js`、`SetupWizard.js`、`JoinView.js` 各有一個 inline `style={{color:'oklch(0.94 0.01 264)'}}`；`MermaidBlock.js` 用 innerHTML 拼 class 字串，且 Mermaid 設定寫死 `theme: 'dark'`；highlight.js 載入 `github-dark.min.css`。
- 專案沒有 `tailwind.config`、沒有使用 `dark:` 變體。
- 外觀設定（Markdown 顏色、自訂 CSS）存在伺服器（SQLite）並以 `cc_appearance_v1` 快取；對話字級 `cc_chat_fs_v1` 則是只存本機的每裝置偏好。
- 既有測試風格：`scripts/*.test.cjs` 用 `node:vm` 載入並切出 `core.js` 的函式，`scripts/mermaid-render.test.cjs` 用沒有 `document.documentElement` 的 mock 環境呼叫 `window.getMermaidConfig()` 並斷言 `theme === 'dark'`。

## Goals / Non-Goals

**Goals:**

- 三態主題（跟隨系統 / 深色 / 淺色），預設深色，每裝置獨立，切換即時、載入無閃白。
- 淺色模式涵蓋所有畫面，特別是容易漏掉的浮層：Ctrl+P 快速切換、各種下拉選單（含原生 `<select>` 展開清單）、`@` 提及選單、訊息動作選單、選字引用 popover、Lightbox、彈窗。
- 新增程式碼時只要使用 token 就自動支援兩種主題，不需 `!important` 或屬性選擇器覆蓋表。
- 使用者仍可用自訂 CSS 局部微調（以 `html[data-theme="light"]` 限定淺色，或覆寫 token 變數）。

**Non-Goals:**

- 不把主題存進伺服器、不改 Go 後端、`settings.go`、`/settings/appearance`。
- 不處理 Wails 桌面視窗標題列、不呼叫 Telegram `setHeaderColor`／`setBackgroundColor`。
- 不保證深色像素級完全一致（灰階合併為約 14 個 token，允許 ≤0.04 L 的細微差異（範圍較寬的 token 取中間值，捲軸滑桿另設 --ra-thumb 保持原色））。
- 不改使用者既有的自訂 CSS 內容。
- 不重新設計版面或調整強調色（accent 紫色、使用者泡泡漸層在兩種主題相同）。

## Decisions

### 語意 token 取代覆蓋表

以 CSS 變數＋`tailwind.config` 色名（`ra-*`）取代「依屬性選擇器逐一覆蓋 Tailwind class」的做法。

- 理由：新程式碼照 token 寫就自動支援兩種主題；不需 `!important`；使用者可直接覆寫變數微調淺色。
- 替代方案：把覆蓋用 CSS 放進 `html[data-theme="light"]` 底下（改動小，但約 190 行寫死值之後新增的元件會悄悄漏成深色，且依賴 `!important`）。已否決。

### 灰階任意值合併為 14 個 token

變數以「通道」寫法定義（例如 `--ra-card: 0.19 0.02 264`），Tailwind 色名為 `oklch(var(--ra-card) / <alpha-value>)`，使 `/80` 透明度修飾詞可用。深色值與對照：

| Token（`bg-/text-/border-` 後綴） | 取代的深色 L 值 |
|---|---|
| `ra-bg` | 0.15 |
| `ra-sunken` | 0.12、0.13、0.14 |
| `ra-card` | 0.16、0.17、0.18、0.19、0.2 |
| `ra-raised` | 0.22、0.24、0.25、0.26、0.27、0.28（chroma 0.02–0.03） |
| `ra-active` | 0.31、0.32 |
| `ra-line-soft` | 0.24、0.25（divide）、0.26 |
| `ra-line` | 0.28、0.3 |
| `ra-line-strong` | 0.32、0.34、0.36、0.4 |
| `ra-t1` | 0.9、0.92、0.93、0.94、0.95 |
| `ra-t2` | 0.8、0.85 |
| `ra-t3` | 0.7、0.72、0.75 |
| `ra-t4` | 0.6、0.62、0.65 |
| `ra-t5` | 0.45、0.48、0.5、0.55（含 `placeholder-`） |

淺色初始值：bg 0.97、sunken 0.95、card 1.0、raised 0.93、active 0.90；line-soft 0.90、line 0.88、line-strong 0.82；t1 0.20、t2 0.28、t3 0.36、t4 0.44、t5 0.50（皆 hue 264、低 chroma），apply 階段以實際畫面微調。既有 `--ra-bg0/bg1/surface/border/text/muted` 改為引用這些通道（`oklch(var(--ra-bg))` 等），`body.ra-body` 等不需改。

### 以 tailwind.config 重新映射 gray、slate 與淡色強調字

- `gray`、`slate`（100–950）改為 `rgb(var(--gray-N) / <alpha-value>)`；深色通道值取 Tailwind 預設色的 RGB，故深色與現在完全相同；淺色把色階反轉（例如 `gray-800` 變淺灰、`gray-200` 變深灰）。如此約 240 處 class 不必修改。
- `violet/red/amber/yellow/orange/sky/emerald` 僅映射 100–400（淡色強調字）；淺色時變為對應的深色階。500 以上（實心填色、按鈕）保持不變。狀態小圓點（`bg-amber-400`、`bg-emerald-400`）在淺色時會稍微變深，視為可接受。
- 800–950 的深色強調（`bg-red-900`、`bg-amber-900`、`bg-amber-950/45` 等）不映射，改由例外清單人工處理，因為它們同時被用作「實心深底＋白字」與「半透明深色調」兩種語意。

### 主題引擎：data-theme 加 color-scheme 加事件

- 偏好值：`system` | `dark` | `light`，存 `localStorage['cc_theme_v1']`；無值或非法值視為 `dark`。
- `<html data-theme="dark|light">` 為唯一的有效主題載體；CSS 以 `:root`（深色）與 `html[data-theme="light"]`（淺色）定義 token，並各自設定 `color-scheme`。
- `core.js` 提供純函式 `resolveTheme(pref, prefersDark)`（可被 vm 測試）、`readThemePref()`、`setThemePref(pref)`、`applyTheme()`；`system` 時掛 `matchMedia('(prefers-color-scheme: dark)')` 的 change 監聽。有效主題改變時在 `window` 派發 `ra-theme-change`（`detail: { theme }`）。
- `<head>` 內另有一小段 inline script（與 chat 字級的做法一致）在 React 前同步設定 `data-theme`，邏輯與 `resolveTheme` 相同；這是刻意的小量重複，因為 `core.js` 是 `text/babel` 延後載入，無法用於防閃白。

### highlight.js 雙樣式切換

載入 `github-dark` 與 `github` 兩份 CSS（各一個 `<link id>`），依有效主題設定另一份的 `disabled`。`.prose pre`、`.prose pre code.hljs` 的背景由 token 決定，不依賴 hljs 主題背景。

### Mermaid 依主題設定並重繪

`getMermaidConfig()` 讀取有效主題（`document.documentElement.dataset.theme`），缺少 `document.documentElement` 時回傳深色設定，使既有測試（無 `documentElement` 的 mock，斷言 `theme === 'dark'`）不需修改。淺色設定使用 `theme: 'default'`、`darkMode: false`、淺色背景。`<ra-mermaid>` 元件監聽 `ra-theme-change`：重新 `mermaid.initialize(getMermaidConfig())`，並對已渲染（且非 streaming）的圖表重設 `_rendered = false` 後呼叫 `renderDiagram()`。其 innerHTML 內的 class 也改用 token。

### 設定頁主題選擇器

`AppearanceSection` 最上方新增 `ThemeField`（版型仿 `ChatFontSizeField`）：三段式按鈕，選取即呼叫 `setThemePref`，不經外層「儲存」，說明文字註明「只影響這個裝置」。

### 浮層與下拉選單的覆蓋清單

「易漏元件」明確列為遷移與驗收對象：`ui/QuickSwitcher.js`（`bg-slate-950/95` 等）、`ui/ui-atoms.js` 的 `selectBaseClass`/`selectCompactClass`（含 danger 琥珀變體）、`chat/chat-header.js` 的下拉清單、`chat/MentionMenu.js`、`chat/MessageActions.js`、`chat/SelectionQuotePopover.js`、`chat/ForwardModal.js` 的 `<select>`、`ui/LogsSection.js` 的 `<select>`、`session/SessionView.js` 的 `<select>`、Lightbox，以及 `type="color"`/`range`/`checkbox`。原生 `<select>` 展開清單的顏色由 `color-scheme` 決定。

## Implementation Contract

**Behavior**

- 無儲存值：`<html data-theme="dark">`，畫面與現狀相同。選 `light` 後整個 UI 變淺色，重新整理維持；選 `system` 時跟隨 OS 並即時更新。
- 切換發生在設定頁，立即生效，不呼叫任何伺服器 API。

**Interface / data shape**

- `localStorage['cc_theme_v1']` ∈ `'system' | 'dark' | 'light'`。
- `<html>` 屬性 `data-theme` ∈ `'dark' | 'light'`。
- `core.js`：`resolveTheme(pref, prefersDark) → 'dark' | 'light'`（非法 pref → `'dark'`；`'system'` → 依 `prefersDark`）；`readThemePref()`、`setThemePref(pref)`、`applyTheme()`。
- `window` 事件 `ra-theme-change`，`detail.theme` 為新的有效主題。
- Tailwind 色名：`ra-bg|sunken|card|raised|active|line-soft|line|line-strong|t1..t5`；CSS 通道變數 `--ra-*`（oklch 通道）、`--gray-N`、`--slate-N`、強調字階 `--violet-N` 等（rgb 通道）。
- `getMermaidConfig()` 的回傳形狀不變，只有 `theme`、`themeVariables` 的值依主題而異。

**Failure modes**

- `localStorage` 不可用（隱私模式）：視為 `dark`，設定頁選擇在本次頁面生命週期內仍可生效，不報錯。
- `matchMedia` 不存在：`system` 視為 `dark`。
- Mermaid 重繪失敗：沿用既有的錯誤回退（顯示原始碼並提示），不新增錯誤路徑。

**Acceptance criteria**

1. 新增 `scripts/theme.test.cjs`（`node:vm` 風格）：驗證 `resolveTheme` 的 spec 解析表所有列，以及非法值回退；`node scripts/theme.test.cjs` 通過。
2. 既有 `node scripts/mermaid-render.test.cjs` 不修改即通過，並新增淺色設定斷言（`theme === 'default'`、`darkMode === false`）。
3. 全專案 grep 不再出現灰階任意值樣式（`\[oklch\(0\.[0-9]+_0\.0[0-9]+_264`）與 `index.html` 內建 CSS 的灰階 oklch 寫死值（accent 與狀態色除外）。
4. 實際瀏覽器（深色）截圖與遷移前比對無可見差異；淺色逐頁檢查：主畫面、側欄、設定、Logs、Share、Forward、Join、Setup、權限／確認彈窗、程式碼區塊、Mermaid 圖、Lightbox。
5. 浮層檢查：Ctrl+P 快速切換、每一種下拉選單（開啟展開清單）、`@` 提及選單、訊息動作選單、選字引用 popover，在淺色下文字與背景對比可讀。
6. 切換主題時 Network 無 `/settings/appearance` 請求；`system` 模式下以瀏覽器模擬 `prefers-color-scheme` 即時切換成功。

**Scope**

- In scope：`internal/static/index.html`、`internal/static/js/**`、`scripts/theme.test.cjs`、`scripts/mermaid-render.test.cjs`（僅新增斷言）、`docs/plan/todo.md` 同步。
- Out of scope：Go 後端與資料庫、Wails／Telegram 外殼顏色、使用者自訂 CSS 的內容、強調色與版面設計。

## Risks / Trade-offs

- [Tailwind CDN 的 `tailwind.config` 必須在 CDN script 之後、其他腳本之前設定，否則自訂色名不生效] → 在 `index.html` 緊接 CDN script 設定，並以瀏覽器確認 `bg-ra-card` 實際產生樣式。
- [`<alpha-value>` 與帶百分比的 oklch 通道在 Tailwind CDN 版本不支援] → 先在 P1 以單一 token 驗證 `bg-ra-card/80`；失敗則改用 `color-mix()` 寫法。
- [機械式替換誤傷：同一 L 值被當成不同語意（如 0.7 chroma 0.15 的綠色狀態字）] → 對照表只替換 hue 264 的灰階；其餘彩色任意值列入例外清單人工處理。
- [深色因 token 合併產生肉眼可見差異] → 以前後截圖比對；若有明顯差異，為該 L 值補一個專屬 token。
- [使用者既有自訂 CSS 是深色專用（深色遮罩、黑色文字陰影），切到淺色仍會生效] → 在設定頁說明與文件中提示以 `html[data-theme="light"]` 限定；不自動修改使用者的 CSS。
- [淺字配深底的實心按鈕與半透明深色調在淺色下對比不足] → 例外清單人工改寫為雙主題皆可用的半透明調色（如 `bg-amber-500/15 text-amber-…`），並於驗收逐頁確認。
- [Mermaid 在串流中被主題切換重繪] → 重繪只針對已渲染且非 streaming 的圖表。

## Migration Plan

- 無資料遷移：新增 `cc_theme_v1` 鍵，舊使用者無值即為深色，行為不變。
- 分階段交付，每階段後專案可正常運作且深色不變：先加入 token 與 config（尚未使用）→ 分批遷移檔案 → 啟用主題引擎與設定頁 → 例外與調色。
- 回滾：還原提交即可；`cc_theme_v1` 殘留值無害。

## Open Questions

- 無（淺色數值以 apply 階段實際畫面微調，不影響契約）。
