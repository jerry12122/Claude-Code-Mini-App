# Prototype 審閱紀錄

2026-11-06 建立。方向尚待使用者審閱，未修改正式應用程式。

- 原型：docs/prototypes/console-ux-polish.dc.html；預覽： http://127.0.0.1:18920/docs/prototypes/console-ux-polish.dc.html 。
- 頁面內「檢查 prototype」會執行 runPrototypeChecks()，目前 10 項通過；它會暫時顯示桌面分頁測試鍵盤操作，再恢復預覽範圍。
- 以 Chrome DevTools 實際按 ArrowRight 後再按 Tab，確認啟用第二分頁並聚焦其關閉按鈕。
- 檢查三種預覽範圍 × 兩種主題 × 兩種密度 × 四個情境，共 48 種組合：預覽框無橫向溢出，切換情境保留草稿。
- 另以 390×844、touch 與 reduced-motion 的 Chromium 模擬環境檢查：頁面無橫向溢出；可見的頂欄按鈕、composer 按鈕與 selectors 點擊區寬高皆至少 44 CSS px；待授權操作可由訊息區捲動找到。
- 已目視檢查桌面深色、手機淺色待授權及 680px 窄欄淺色畫面；最後版本 console 無 error／warning。
- 原型色值計算的對比：深色弱文字／表面 7.16:1，淺色弱文字／底色 5.72:1，深色主按鈕文字 4.52:1，淺色主按鈕文字 5.77:1，使用者氣泡文字 5.15:1。這些數值只代表原型指定色值，不能代替正式介面的實測。
- Spectra analyze：Critical 0、Warning 0；12 個補充情境例子的 Suggestion 保留為建議。spectra validate 通過。

尚未測試實體手機、Telegram WebView、軟鍵盤、讀屏軟體與正式產品的 200% zoom。正式驗收依 tasks.md 執行；prototype 的最後示範分頁刻意保留，正式產品的最後分頁關閉規則仍以既有規格與本 change 的焦點要求為準。

與使用者先討論：桌面偏好緊湊或易讀、手機 selector 換兩行是否可接受、待授權與斷線的文字位置。這些是審閱選擇，不把尚未收到的回覆視為核准。
