## Context

SessionView 目前固定依 work_dir 分組，另外呈現釘選、Telegram 與未設定目錄的虛擬群組。internal/db/session.go 的 MarkSessionRead 以已完成的非使用者訊息收據計算真實未讀，並保留 last_active；internal/api/session.go 的 Patch 已支援 mark_read 與 read_message_ids。新需求增加整理與待回看能力，不替代原有訊息收據。

本變更安排於 v0.22.0 之後；目前僅建立可交接計畫，不實作或阻擋本次候選發布。

## Goals / Non-Goals

**Goals:** 在實體工作目錄內管理單層分類；持久化會話歸屬；手動標記待回看；維持精確未讀計數、釘選、搜尋、執行與跨裝置一致性。

**Non-Goals:** 不移動檔案、不變更 repository 路徑、不做多層分類、多標籤、顏色系統、分類拖拉排序或新的拖放框架；不對虛擬群組建立分類；不更動 runner／工作室派工或 Telegram 訊息同步；不實作多使用者個人分類。

## Decisions

### 工作目錄內單層分類

沿用既有 work_dir 群組識別規則，由伺服器保存 category(id, work_dir, name) 與 sessions.category_id。使用外鍵／交易維持目錄相符與刪除解除歸類；不能只信任前端。分類名稱裁切空白後採精確相等判重，同目錄唯一；長度 1 到 80 Unicode code points 是本計畫選定的輸入規格，非效能估計。

替代方案：只存瀏覽器成本較低，但無法跨裝置；多層樹與多標籤超出本次需求。採最小單層資料模型。

### 獨立提醒與版本比對

sessions 新增 manual_unread 布林及 manual_unread_revision 整數；預設 false／0。每次標記狀態更新遞增 revision，清除需提供觀察到的 revision，在同一交易比對。訊息收據保持獨立；舊客戶端的 mark_read/read_message_ids 不得隱式清掉新提醒。新版顯式已讀與 Read All 傳入觀察到的 revision；不匹配回傳衝突及當前狀態，允許訊息收據仍按既有規則處理，前端不得把衝突當清除成功。

替代方案：把舊訊息全部改回未讀會破壞精確計數；單純布林無法抵抗延遲 ACK。使用每會話整數比對，避免引入通用事件系統。

### 沿用群組與會話選單

在實體目錄下顯示分類，未分類最後；分類按名稱排序，內部沿用既有會話排序。無搜尋時保留空分類以便管理；搜尋先篩會話再顯示有命中的分類。釘選維持原虛擬群組，會話不重複且取消釘選恢復原分類。選單提供歸類／未分類與標未讀／已讀，鍵盤與手機均可操作，不要求拖拉。

手動標未讀後，即使會話正開啟或重整也保留。只有離開再進入、且已有完成結果顯示於前景焦點最新位置，才送該次觀察 revision 的清除；持續停留不自動清除。空會話靠顯式已讀解除。真數字優先顯示；只有提醒時顯示非數字標記，輔助標籤說明待回看。標題未讀總數仍只加總真訊息數。

替代方案：另一套清單或新導覽層增加學習與同步成本；本計畫沿用現有入口。

## Implementation Contract

- 分類 CRUD 回傳穩定 id/work_dir/name；會話查詢增加 nullable category_id、manual_unread 與 manual_unread_revision。新增分類路由掛在既有 owner 驗證邊界內；具體路由名稱在實作時沿 repo 路由慣例選定，不另增認證機制。
- PATCH 歸類支援 category_id=null；跨目錄、無效／已刪 id 拒絕。分類刪除與解除歸類原子化；任何既有支援的工作目錄更新路徑須原子清掉不相符歸屬。不新增工作目錄編輯功能。
- 手動標記與清除需明確操作；清除帶 manual_unread_revision。Read All 帶每會話觀察 revision，新增標記不受舊請求影響。舊訊息已讀 API 仍相容，不清手動標記。讀取結果包含目前狀態，跨裝置沿既有 session 變更通知更新。
- 輸入錯誤 HTTP 400、未登入沿現有認證拒絕、guest HTTP 403、找不到資料 HTTP 404、名稱重複／版本不符 HTTP 409；以既有 JSON error 格式回應，無靜默失敗。guest 不取得分類／手動提醒欄位。
- 未分類、不帶 marker 的舊會話呈現不變；改分類／提醒不改 last_active、歷史、runner 或 queue。分類與 marker 的 SQL 寫入使用最小交易與既有同步機制。
- 驗收以兩份 delta spec 的全部情境為準：DB/API 狀態斷言、升級副本與重啟、兩視窗延遲清除、搜尋／釘選、鍵盤與手機真瀏覽器。資料安全與競爭案例不能以截圖代替。
- 範圍僅本文件 Goals；Non-Goals 均不加入實作。

## Risks / Trade-offs

- [舊客戶端清不掉新提醒] → 保留舊已讀語意，提示以新版選單操作；不讓舊 ACK 覆寫新狀態。
- [群組識別或目錄大小寫不同造成分類分裂] → 實作前核對 work_dir 現有識別／正規化規則並沿用，不自行做 filesystem resolve。
- [釘選與搜尋造成會話遺失或重複] → 覆蓋分類、釘選、篩選交叉組合，保持每筆僅一位置。
- [遷移回退遺失整理資訊] → 部署前 SQLite 一致性備份，副本驗證舊資料與新預設；回退需還原匹配版本備份，不僅替換 binary。分類刪除不刪會話。
