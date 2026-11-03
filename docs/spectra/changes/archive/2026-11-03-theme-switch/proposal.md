## Why

目前整個 UI 只有深色：灰階以 Tailwind 任意值（`bg-[oklch(0.19_0.02_264)]` 等，約 190 行、分布在 17 個 JS 檔）與 `gray-*`/`slate-*` 色階（約 240 處）寫死，`index.html` 的內建 CSS、`MermaidBlock.js` 的 innerHTML、highlight.js 主題也都是深色。使用者想要原生的淺色模式並可在設定切換；用自訂 CSS 逐個 class 覆蓋既脆弱（新程式碼會悄悄漏成深色，且要靠 `!important`）。

## What Changes

- 新增三態主題設定：**跟隨系統 / 深色 / 淺色**，預設**深色**（現有畫面視為深色模式，不變）；只存本機 `localStorage`（`cc_theme_v1`），每個裝置各自設定，不動後端與伺服器 appearance 設定。
- 建立語意色彩 token（CSS 變數，深色為 `:root`、淺色為 `html[data-theme="light"]`），並以 `tailwind.config` 暴露成 `ra-*` 色名；同時把 `gray`/`slate`（含 900/950）與淡色強調字（`violet/red/amber/yellow/orange/sky/emerald` 的 100–400）映射到 token，讓既有 ~240 處色階 class 不必逐一修改。
- 機械式遷移：把 JS 內寫死的灰階 oklch 任意值（含 `hover:`/`active:`/`placeholder-`/`divide-`/`ring-` 變體）、inline style、`index.html` 內建 CSS、`MermaidBlock.js` 的 innerHTML class 字串，換成 token；深色外觀維持不變（允許 ≤0.04 L 的細微差異）。
- 主題引擎：`<head>` inline script 於 React 載入前套用主題（防閃白，涵蓋訪客 Join 頁與 Setup 頁）；`core.js` 提供讀/寫/套用主題與「跟隨系統」的 `matchMedia` 監聽；`color-scheme` 隨主題切換，使原生 `<select>` 下拉清單、捲軸、`type=color`/checkbox 控制項跟著變。
- highlight.js 依主題切換淺/深兩份樣式；Mermaid 依主題切換設定並讓畫面上既有圖表重繪。
- 設定彈窗「外觀」頁最上方新增主題三段式按鈕，選取即時套用，不經「儲存」。
- 淺色調色與例外處理：淺字配深底的實心按鈕、半透明紅/琥珀底、Mermaid 錯誤橫幅等；並逐一檢查容易漏掉的浮層元件（Ctrl+P 快速切換、各種下拉選單、@ 提及選單、訊息動作選單、選字引用 popover、Lightbox、Share/Forward 彈窗）。

## Non-Goals

- 不將主題存進伺服器 SQLite / `settings.go` / `/settings/appearance`（每裝置各自設定）。
- 不處理桌面版 Wails 視窗標題列顏色，也不呼叫 Telegram `setHeaderColor`／`setBackgroundColor`。
- 不改使用者既有的自訂 CSS 內容（僅在文件／設定說明中提示可用 `html[data-theme="light"]` 限定淺色）。
- 不為深色做像素級完全一致保證（灰階合併成約 14 個 token）。

## Capabilities

### New Capabilities

- `theme-switching`: 深色／淺色／跟隨系統主題的選擇、持久化、無閃白套用，以及整個 UI（含浮層、下拉選單、程式碼區塊）在兩種主題下的可讀呈現。

### Modified Capabilities

- `mermaid-rendering`: Mermaid 圖表與其工具列/容器須跟隨目前主題，主題切換時已顯示的圖表須以新主題重繪。

## Impact

- `internal/static/index.html`：token 變數、`tailwind.config`、內建 `<style>` 改用變數、防閃白 inline script、highlight.js 雙樣式連結。
- `internal/static/js/core/core.js`：主題讀寫與套用函式、`matchMedia` 監聽、主題變更事件。
- `internal/static/js/ui/SettingsModal.js`：新增 `ThemeField`。
- `internal/static/js/chat/MermaidBlock.js`：依主題的設定與重繪。
- 其餘 `internal/static/js/**`（`app.js`、`chat/*`、`session/*`、`share/*`、`ui/*`、`setup/*`）：灰階任意值換 token、例外按鈕調整，含 `ui/QuickSwitcher.js`、`ui/ui-atoms.js` 的下拉選單樣式。
- 不影響 Go 後端、資料庫與 API。
