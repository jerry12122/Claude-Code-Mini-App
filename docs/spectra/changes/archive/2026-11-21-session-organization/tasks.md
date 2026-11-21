## 1. 工作目錄分類

- [x] 1.1 實作 Folder category management 與「工作目錄內單層分類」的伺服器 CRUD／持久化，舊 DB 升級預設未分類；以 DB/API 測試驗證空白／80 與 81 code points／重名／跨目錄同名、改名 id 不變、刪除不丟歷史及 queue，並以 SQLite 備份副本重啟檢查遷移。
- [x] 1.2 [after: 1.1] 實作 Directory scoped session membership 的歸類／解除與交易不變量；以 DB/API 測試驗證跨目錄與刪除 id 拒絕、分類刪除和指派競爭無懸空歸屬、既有支援的 work_dir 更新清除不相符歸屬，以及 last_active／已讀狀態／runner／queue 不變。
- [x] 1.3 [after: 1.2] 實作 Category presentation and owner isolation 與「沿用群組與會話選單」的分類入口、排序、未分類、搜尋及釘選呈現；以 owner/guest API 測試及瀏覽器測試驗證 guest 無讀寫分類、跨裝置重整一致、空分類可管理、搜尋揭露結果、釘選往返不重複、手機與鍵盤可操作。

## 2. 手動標記未讀

- [x] 2.1 [after: 1.3] 實作 Independent manual unread marker 與「獨立提醒與版本比對」的儲存及 owner API，沿用 session 更新通知；以 DB/API 測試驗證空會話／已讀會話／Telegram marker 重整持久、真 unread_count 不變、guest 不取得 marker 也不能修改、訊息收據／last_active／runner／queue 不變。
- [x] 2.2 [after: 2.1] 實作 Revision guarded manual marker clearing 與新版 mark-read／Read All；以交易競爭測試驗證舊 message-ID ACK 不清 marker、revision 4 清除不能蓋掉 revision 5、Read All 不清後來標記或 pending 後來完成的結果，並驗證舊 API 仍可確認訊息。
- [x] 2.3 [after: 2.2] 實作 Accessible manual unread presentation 及離開再進入的自動清除條件；以兩視窗瀏覽器測試驗證目前開啟標未讀／重整仍保留、真正重訪最新完成內容才清、空會話靠顯式已讀、背景不清、鍵盤／手機與錯誤復原，以及 0／3／100 真計數的提醒／3／99+ 與標題總數 103。

## 3. 獨立驗收與交接

- [x] 3.1 [after: 2.3] 逐項核對兩份 delta spec 與 Implementation Contract，跑受影響 DB/API/server 回歸與競爭 race、現有前端分層／production build、分類及未讀瀏覽器流程；保存精確來源版本、命令、結果與截圖，確認無歷史／queue 遺失及 guest 資料暴露，驗收通過後才標記本計畫任務完成。
