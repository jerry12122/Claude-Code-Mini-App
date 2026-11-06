## 1. 審閱結果與範圍

- [x] 1.1 完成「依使用者回饋保留正式元件與尺寸」：將開分支試作、32px 與保留現有排列的決定同步至 artifacts，取消先前大型元件與換排要求；以 spectra analyze 和 spectra validate console-ux-polish 確認一致，並保存本次修改前的共用檔基準。

## 2. 分頁鍵盤與焦點

- [x] 2.1 [after: 1.1] 完成「分頁焦點沿用既有群組狀態」、Keyboard navigation and focus within a tab group 與 Closing a tab preserves a valid keyboard destination：單一 active tab 入口、左右循環與 Home／End、原有 close-selection 的焦點移交與最後一頁回導覽；用 scripts/tab-groups.test.cjs、scripts/console-usability.test.cjs 及實際元件鍵盤操作驗證，保留現有拖曳和群組快捷鍵。

## 3. 局部可用性

- [x] 3.1 [after: 2.1] 完成「原生完整資訊展開與共用焦點」、Keyboard-visible and named shared controls、Compact controls and zoom retain access to controls 及 Long content remains readable in both themes：補齊 focus-visible、原生標題資訊展開與允許縮放，只修有證據的弱文字對比，保留 32px 與現有排列；以實際 JSX／CSS 的窄欄、手機、雙主題、長路徑、局部 code 捲動走查及必要 scripts/console-usability.test.cjs 檢查驗證。
- [x] 3.2 [after: 3.1] 完成「現有錯誤與傳輸行為先驗證再補缺漏」及 Failure feedback preserves recoverable input：驗證離線可編輯草稿、授權失敗保留面板、提示可讀且不逐 token 播報；用 scripts/ux-reliability.test.cjs 和必要 toast 語意檢查確認，符合項目保留原碼，不修改傳輸流程。

## 4. 試用與交接

- [x] 4.1 [after: 3.2] 執行 Implementation Contract 中四個 Node 測試與兩種 static Go 測試，完成正式元件桌面／手機／窄欄和焦點走查；記錄每個新增 scenario 的測試或單一畫面外觀排除理由，確認沒有控制尺寸、API、依賴與 script 順序的非預期變更。
- [x] 4.2 [after: 4.1] 建置獨立名稱的 production 桌面 EXE 供使用者試用，不覆寫現有程式或啟動正式服務；以建置成功、檔案存在、git 分支名稱與 spectra validate 驗證，交付試用位置及尚未測試的平台限制。

先前的 prototype 換排／大型觸控控制／密度和配色重做要求已取消，替代決策為「依使用者回饋保留正式元件與尺寸」。舊 pending 項目沒有被當成已完成工作。
