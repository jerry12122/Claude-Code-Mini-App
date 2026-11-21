## Context

截圖中，上方 Cursor 會話分頁的執行提示與左側收合欄圖示被框選，使用者希望旁邊也一起提示。已核對：SessionTabs 使用 sessionStatusDotClass／sessionStatusLabel；SessionView 的收合欄目前只在回傳 ra-status-ask 時畫狀態，running 未畫出。共用 CSS 已提供 running 動畫與 reduced-motion 分支，不需重造動畫。

## Design Source

- 來源：使用者本輪附件 workspace/uploads/98e28d24-7cb4-4277-9703-61a6b7a93075/8ba936fe1638350395b8b9ff79149eeb.png。
- 追加釘選來源：workspace/uploads/98e28d24-7cb4-4277-9703-61a6b7a93075/320da8d455d2e175021e65e4dd67e753.png；快取 .spectra/design-cache/sidebar-session-status-parity/pin-reference.png，框選每筆會話右側的「釘選」文字。
- 本地參考快取：.spectra/design-cache/sidebar-session-status-parity/reference.png；這是現況與需求標記，不是新視覺稿，不進產品包。
- 以下為已核對 internal/static/index.html 的既有設計值；沿用它們，不把截圖當新配色規格。

| 項目 | 既有值 |
| --- | --- |
| 執行狀態類別 | ra-status-dot running |
| 圓點大小 | 7px × 7px |
| 執行顏色 | oklch(0.75 0.15 70) |
| 動畫 | pulse-dot 1.2s infinite，僅 no-preference |
| 待處理類別 | ra-status-ask，16px × 16px，問號 |

## Goals / Non-Goals

**Goals:** 同一會話在上方分頁與左側收合欄具有一致執行／待處理提示；保留未讀、選取、鍵盤及 reduced-motion；展開側欄以既有釘選圖示取代文字標記。

**Non-Goals:** 不改 runner 狀態、API、資料庫、未讀計數或分類；不讓整個代理圖示閃爍；不新增通知、輪詢、計時器、動畫框架或逐影格相位同步。安排於 v0.22.0 之後，本輪只規劃。

## Decisions

### 重用共用狀態與動畫

收合欄使用既有 sessionStatusDotClass／sessionStatusLabel，將其已支援的 running 類別也渲染出來。狀態角標沿既有左上待處理位置，未讀保持右上，代理圖示及選取框保留。分頁與收合欄使用同一 CSS，對相同 session 資料立即呈現同義狀態；元件掛載時間不同不要求逐影格同相。

不採獨立推測忙碌的 timeout 或整個圖示閃動，避免延遲失真與干擾辨識。共用 CSS 已有減少動態效果處理，驗證有效即可，不另加鏡像規則。

### 釘選文字改為既有圖示

SessionView 的展開會話列目前在 s.pinned 時顯示「釘選」文字；改用同檔釘選群組已使用的 📌 glyph，避免新增圖示資產或套件。保留 title「已釘選」，以可存取名稱或描述辨識；僅替換顯示，不新增獨立按鈕，也不把點擊改為切換釘選。既有選單取消釘選及群組行為不變。替代方案是加入自製 SVG 或可點擊切換；本次無此需求，沿用既有圖示即可。

## Implementation Contract

- 入口：SessionView 的收合會話圖示；參照 SessionTabs 的狀態呈現。讀取同一 session 的 status，不新增本地執行旗標或網路請求。
- running 顯示既有圓點；三種 awaiting 狀態顯示原對應提示；idle 或未知值不顯示忙碌／待處理。連線中斷不自行猜測狀態，與分頁維持相同既有資料語意。
- 狀態角標不接收點擊、不蓋住未讀數，按鈕的可存取描述提供狀態；保留會話名稱及選單／鍵盤／拖放行為。
- 驗收：真 DOM 同時觀測分頁與收合欄，覆蓋 idle→running→awaiting→running→idle、另一背景會話獨立狀態、選取與未讀並存、reduced-motion 靜態可見；檢查沒有狀態動畫時仍能讀懂標籤。
- 展開側欄只替換每列釘選標記；保留群組標題、既有釘選 API、選單與排序，驗證圖示／未讀／執行／選取並存及 hover/可存取名稱。
- 未知狀態沿既有 helper 無提示，不偽裝執行中。無新增 API 與錯誤回應契約；載入錯誤處理不變。

## Risks / Trade-offs

- [左上角標與右上未讀擠在一起] → 檢查選取／未讀／running 同時出現的實際截圖及點擊區，不改掉未讀數。
- [測試只比對 class 卻沒顯示動畫] → 真 DOM 核對 computed animation 與 reduced-motion，附前後截圖；不為純 CSS 加鏡像單元測試。
