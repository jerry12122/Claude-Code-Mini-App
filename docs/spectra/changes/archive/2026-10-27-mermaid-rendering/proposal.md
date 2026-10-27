## Why

使用者在透過 Coding Agent 進行架構規劃、系統分析與流程溝通時，Agent 經常產生 Mermaid 格式的圖表語法（如 flowchart, sequence diagram, class diagram 等）。目前聊天室僅能將其作為一般程式碼區塊展示，使用者無法直觀瀏覽結構關係，降低了架構理解與溝通的效率。

## What Changes

- 支援 Markdown ````mermaid` 程式碼區塊自動識別與非同步 SVG 圖表渲染。
- 提供圖表與原始碼的雙向切換模式（Tabs），並保留一鍵複製 Mermaid 原始碼功能。
- 支援串流輸出保護：在 Agent 串流生成代碼中途保持代碼高亮與繪圖中提示，串流完成後才非同步執行圖表渲染，避免語法不完整導致的報錯與畫面卡頓。
- 具備優雅的錯誤退回機制（Graceful fallback）：當 Mermaid 語法解析失敗時，攔截原生報錯並自動退回原始碼檢視，並標註友善錯誤提示。
- 適配聊天室深色主題（Dark theme），確保節點、線條與文字在 OKLCH 色彩空間下維持一致的高對比與美觀。
- 整合圖表燈箱放大檢視（Lightbox integration），支援大尺寸與多節點複雜圖表的點擊放大檢視。

## Non-Goals (optional)

- 本次不支援在聊天室內部進行視覺化拖曳編輯或即時修改 Mermaid 圖表節點。
- 本次不支援後端伺服器（SSR）渲染或圖表二進位圖片輸出，所有解析與 SVG 繪製皆在前端瀏覽器環境完成。

## Capabilities

### New Capabilities

- `mermaid-rendering`: 在聊天室中自動識別並非同步渲染 Mermaid 圖表，提供圖表與原始碼雙向切換、串流保護、錯誤退回與放大檢視。

### Modified Capabilities

## Impact

- 前端公開資源：
  - `internal/static/index.html`：載入 Mermaid 庫並設定圖表樣式變數。
  - `internal/static/js/core/core.js`：擴充 `parseMarkdown` 識別 mermaid 代碼區塊並輸出 `<ra-mermaid>` 元件。
  - `internal/static/js/chat/MermaidBlock.js`：新增 Web Component 處理非同步渲染、主題配置、代碼切換與錯誤攔截。
  - `internal/static/js/chat/ChatView.js`：整合圖表燈箱放大與互動事件委派。
- 測試套件：
  - `scripts/mermaid-render.test.cjs`：驗證 Markdown 解析、狀態轉換與錯誤降級。
- 後端服務：
  - 無需修改 Go 後端代碼或 WebSocket 通訊協定。
