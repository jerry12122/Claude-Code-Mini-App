## 1. 平板定位與縮高

- [x] 1.1 依「隔離開工依最新授權」在指定隔離工作樹開工，先核對共用檔案 owner 與已凍結來源，不等待發布驗收。依「先量測安全區責任再縮高」與 Compact landscape header preserves conversation space，在隔離服務／假 runner 下保存選定平板橫向尺寸、非零及零宿主 inset 的 DOM 基線與截圖，記錄 shell／分頁／標題／訊息／composer 邊界、computed padding、實際 viewport 與 CSS px，確認可避免留白來源；驗證重複執行量測可定位同一容器，不能以圖片像素冒稱 DOM 高度。
- [x] 1.2 [after: 1.1] 實作 Compact landscape header preserves conversation space，遵守「保留觸控與響應式狀態」；僅調整已證實的頂部多餘間距及必要安全區責任。依 Implementation Contract 的同條件前後 DOM 量測，總頂部應用佔位至少減少選定的 16 CSS px、聊天區同步增加，保留標題／狀態／Git／quota／分享與 composer；若必要安全區或操作使目標不可行，先交證據由 PM 修訂，不以移位、裁切、隱藏功能或縮字通過。純 CSS 無鏡像單元測試，事件邏輯變更需用既有測試驗證行為。

## 2. 獨立驗收與整合

- [x] 2.1 [after: 1.2] 獨立覆核 Touch targets and host clearance remain usable：依 Implementation Contract 的平板雙橫向／直向、手機／桌面、coarse／fine、零／非零／缺少 inset、旋轉／fullscreen 退出／鍵盤、長標題／200%文字／分割窗格矩陣保存 DOM JSON 與截圖；觸控至少44×44 CSS px、不遮宿主控制且無新增橫捲。記錄真 Telegram 平板驗證結果，缺環境則列未驗。跑受影響瀏覽器回歸、node scripts/frontend-layering.test.cjs、python scripts/build-css.py --check 與既有 production build，修改事件時加跑 node --test scripts/telegram-fullscreen.test.cjs；核對精確 diff、來源及與另三計畫的共用檔案，驗收通過才標完成並依使用者授權整合 main。
