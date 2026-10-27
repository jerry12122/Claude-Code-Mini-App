## Context

目前聊天室採用前端管線處理 Agent 輸出的 Markdown 訊息：
1. Agent 透過 WebSocket 傳送內容增量（`delta`）。
2. 前端即時呼叫 `parseMarkdown(text)`（基於 Marked.js 與 highlight.js）產出 HTML 字串。
3. `ChatView.js` 透過 `dangerouslySetInnerHTML={{ __html: m.html }}` 將內容渲染進 DOM。

當 Agent 輸出包含 ````mermaid` 的流程圖、時序圖或架構圖時，現有系統僅能將其作為普通代碼區塊呈現，無法直觀瀏覽結構關聯。為了支援圖表原生渲染，同時解決 LLM 逐字串流（Streaming）過程中代碼未閉合導致頻繁語法錯誤的問題，本設計提出基於 Web Component 的渲染管線架構。

## Goals / Non-Goals

**Goals:**
- 在聊天室中自動識別 ````mermaid` 程式碼區塊，並非同步渲染為高畫質向量 SVG 圖表。
- 提供「圖表 / 原始碼」雙向檢視切換，保留一鍵複製原始語法功能。
- 實作串流輸出保護：在訊息串流中維持代碼高亮並顯示繪製指示，待串流完畢後才執行圖表渲染，避免畫面劇烈重繪與解析錯誤。
- 實作優雅的語法錯誤容錯（Graceful fallback）：當語法非法時自動切回原始碼區塊並顯示警告標籤，避免破壞聊天室版面。
- 完美契合系統暗色主題（Dark theme），適配 OKLCH 色彩空間。
- 整合現有的燈箱機制（Lightbox），支援圖表點擊放大檢視。

**Non-Goals:**
- 不支援在聊天室內部進行節點拖曳排版或視覺化在線編輯。
- 不引入後端伺服器端圖表渲染（SSR），所有解析與 SVG 生成均在前端瀏覽器端完成。

## Decisions

### Web Component encapsulation for Mermaid blocks

使用自定義 Web Component（`<ra-mermaid>`）封裝圖表渲染邏輯。

- **理由**：現有架構採用 `dangerouslySetInnerHTML` 注入 HTML。若使用 React `useEffect` 在整棵 DOM 樹中遞迴尋找未渲染節點，會增加虛擬 DOM 與實體 DOM 同步的複雜度；改採 Web Component，瀏覽器會在標籤進入 DOM 時自動觸發 `connectedCallback`，具備獨立生命週期、內部狀態管理與按需重繪能力。
- **替代方案比較**：
  - *React AST 管道重構（如 react-markdown）*：改動幅度過大，破壞現有已穩定運作的引言選字、代碼複製與圖片放大邏輯。
  - *全域 DOM 輪詢/MutationObserver*：效能開銷較大，容易在訊息滾動與長列表更新時產生競爭條件。

### Streaming protection and deferred rendering

在訊息串流接收期間實施防護延遲渲染。

- **理由**：LLM 串流輸出（`streaming: true`）時，每秒傳送多次 chunk。Mermaid 語法在未閉合前呼叫解析必定丟出語法異常，且頻繁執行龐大的 SVG 排版計算會造成 CPU 飆高與畫面閃爍。
- **作法**：
  - 在 `parseMarkdown` 輸出 `<ra-mermaid>` 時標註 `data-streaming="true"`。
  - 當元件偵測到串流中時，內部維持代碼檢視並顯示「圖表生成中...」骨架提示。
  - 當串流結束（`streaming: false`）或載入既有歷史訊息時，自動排程非同步呼叫 `mermaid.render()`。

### Graceful fallback on syntax error

實作語法錯誤攔截與自動降級退回。

- **理由**：LLM 可能輸出帶有未轉義字元或語法結構不合法的 Mermaid 代碼。Mermaid 預設行為會在 DOM 底部注入顯眼的紅底錯誤區塊。
- **作法**：
  - 配置 `mermaid.initialize` 抑制全域報錯注入（使用 `suppressErrorRendering: true`）。
  - 使用 `try/catch` 包裹 `mermaid.render()`，一旦解析失敗，元件標記為錯誤狀態，自動切換至原始碼視圖，並在頂部顯示警告標記（如「⚠️ Mermaid 語法解析失敗」）。

### CDN on-demand script loading and dark theme initialization

採用 CDN 按需載入 Mermaid 函式庫，並適配暗色主題。

- **理由**：Mermaid.js bundle 體積約 700KB (gzipped)，若使用者瀏覽無圖表的對話，提前載入會造成不必要的網路開銷。
- **作法**：
  - 當畫面上首次出現 `<ra-mermaid>` 時，動態載入 Mermaid 腳本。
  - 初始化全域配置：設定 `securityLevel: 'strict'`（防止 XSS）、`theme: 'dark'` 以及貼合聊天室色系的 `themeVariables`（主色、邊框色、背景色）。

### Interactive toolbar with code toggle and lightbox zoom

為圖表區塊配備完整的互動工具列與燈箱放大功能。

- **作法**：
  - 工具列提供「圖表 / 代碼」切換標籤按鈕。
  - 複製按鈕複用既有的 `copyTextExecCommand` 機制，支援點擊一鍵複製原始 Mermaid 原始碼。
  - 點擊放大按鈕或點擊圖表本體，將 SVG 內容傳遞給 `ChatView.js` 現有的燈箱放大系統進行檢視。

## Implementation Contract

- **Behavior**:
  - 當 Agent 回覆包含 ````mermaid 代碼時，對話框內預設展示渲染完成的向量圖表。
  - 使用者可點擊「代碼」按鈕查看原本的 Mermaid 語法，並可隨時點擊「圖表」切回。
  - 點擊圖表或放大按鈕可開啟放大檢視視窗。
  - 點擊複製按鈕可複製原始代碼並顯示「已複製」動畫反饋。
- **Interface / Data Shape**:
  - `parseMarkdown` 針對 `language-mermaid` 代碼區塊輸出：
    `<ra-mermaid data-code="..." data-streaming="true|false"></ra-mermaid>`
  - `<ra-mermaid>` 提供標準 DOM API 與內部狀態管理。
- **Failure Modes**:
  - 語法錯誤：自動退回顯示代碼區塊，並顯示黃色/紅色錯誤指示，不中斷對話渲染。
  - CDN 載入失敗：顯示載入失敗提示與重試按鈕，代碼區塊保持完整可讀。
- **Acceptance Criteria**:
  - 包含有效 Mermaid 語法的歷史訊息載入時能正確渲染為 SVG。
  - 串流訊息接收過程中不發生解析崩潰與畫面跳動，輸出結束後正確展開圖表。
  - 惡意包含腳本的 Mermaid 語法不會觸發腳本執行（嚴格模式防禦）。
  - 語法錯誤的代碼能完整顯示原始文字且不報紅底全域錯誤。
- **Scope Boundaries**:
  - **In Scope**: 前端 HTML 載入、Markdown 解析攔截、`<ra-mermaid>` 元件開發、暗色主題適配、工具列與燈箱放大。
  - **Out of Scope**: 後端 Go 程式碼修改、WebSocket 協定變更、圖表在線可視化編輯器。

## Risks / Trade-offs

- **[Risk] CDN 網路受阻或延遲** → **Mitigation**: 實作載入超時保護與重試機制，若載入失敗維持語法高亮代碼展示，確保文字內容始終可讀。
- **[Risk] 複雜大型圖表導致行動端排版溢出** → **Mitigation**: 圖表容器設定 `overflow-x: auto` 橫向滑動，並提供全螢幕燈箱放大檢視。
- **[Risk] Mermaid 內建 `click` 回呼造成 XSS 隱患** → **Mitigation**: 強制啟用 `securityLevel: 'strict'`，阻止圖表內部任意腳本執行。
