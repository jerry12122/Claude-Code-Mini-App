## 1. 基礎架構與載入

- [x] 1.1 實作 CDN on-demand script loading and dark theme initialization：於前端動態非同步載入 Mermaid 11 函式庫，並配置 `securityLevel: 'strict'` 與深色系主題參數，滿足 Dark theme visual consistency；驗證方式：執行 `node scripts/mermaid-render.test.cjs` 驗證全域設定與主題配置屬性。
- [x] 1.2 實作 Web Component encapsulation for Mermaid blocks：定義自定義元素 `<ra-mermaid>` 處理元件掛載、DOM 隔離與 SVG 插入，實現 Automatic Mermaid diagram detection and rendering；驗證方式：在瀏覽器中掛載元件並手動驗證自定義元素成功註冊與渲染。

## 2. Markdown 解析與串流管線

- [x] 2.1 擴充 `internal/static/js/core/core.js` 的 `parseMarkdown` 函式，識別 `language-mermaid` 代碼區塊並產生 `<ra-mermaid>` 標籤，攜帶原始代碼與串流狀態屬性；驗證方式：執行 `node scripts/mermaid-render.test.cjs` 斷言 Markdown 轉換輸出正確的元件標籤與屬性。
- [x] 2.2 實作 Streaming protection and deferred rendering：在 `streaming: true` 期間維持語法高亮程式碼區塊並顯示「圖表生成中...」指示，訊息結束時才啟動非同步圖表繪製；驗證方式：於測試中模擬串流由 pending 切換至 completed，確認非同步渲染僅在完成後觸發。

## 3. 錯誤容錯與互動介面

- [x] 3.1 實作 Graceful fallback on syntax error：配置錯誤攔截以抑制全域錯誤注入，在語法非法時自動切回原始碼區塊並展示醒目警告標籤；驗證方式：於測試中注入非法 Mermaid 語法，斷言介面未崩潰且正確退回代碼檢視。
- [x] 3.2 實作 Interactive toolbar with code toggle and lightbox zoom：提供頂部操作工具列，實現 Diagram and code view switching 以及 Code copy and lightbox inspection，支援圖表與代碼雙向切換、一鍵代碼複製與點擊放大檢視；驗證方式：在聊天室介面中手動點擊切換、複製與燈箱放大功能，確認操作正常且複製回饋正常顯示。

## 4. 整合驗證

- [x] 4.1 建立 `scripts/mermaid-render.test.cjs` 測試腳本，完整覆蓋圖表解析、串流狀態過渡與異常語法降級之自動化斷言；驗證方式：執行 `node scripts/mermaid-render.test.cjs` 確保全部測試綠燈通過。
