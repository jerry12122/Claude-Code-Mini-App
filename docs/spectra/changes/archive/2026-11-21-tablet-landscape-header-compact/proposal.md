## Summary

縮減平板橫向使用時，會話標題黑色區塊的多餘高度，增加聊天可視空間。依使用者最新「工作樹開起來 可以派了」授權，與另外三項在隔離工作樹開工，逐項驗收後整合 main；不以發布驗收作為開工前提，不混入 v0.22.1 修補候選。

## Motivation

使用者提供 IMG_0164.png，平板橫向的分頁下方至會話標題之間有明顯留白。圖中有 Telegram 宿主控制；必須區分必要安全區與應用自身間距，不能直接把頂部 padding 歸零。

## Proposed Solution

- 新能力 tablet-landscape-header-density：橫向平板的標題區更緊湊，聊天與輸入空間增加；標題、狀態、Git、額度與分享仍可操作。
- 先在相同 viewport／Telegram inset／內容下保存 DOM 高度與安全區基線，再修正已證實的額外間距或重複偏移；不把截圖當作 CSS px 或根因證據。
- 覆蓋普通瀏覽器與 Telegram WebView、觸控與外接指標、旋轉與鍵盤；必要安全區只由有明確責任的容器處理，保留手機及桌面行為。

## Impact

- Affected specs: tablet-landscape-header-density (new)
- Affected code:
  - Modified: internal/static/js/chat/ChatHeader.js、internal/static/index.html；只在量測顯示偏移責任需調整時涉及 internal/static/js/core/core.js、internal/static/js/session/SessionView.js、internal/static/js/ui/SessionTabs.js。
  - New: 無預定產品檔；瀏覽器驗證沿用 scripts/ 下既有工具。
  - Removed: 無。
- 無 API、DB、runner 或新套件；不變更目前發布候選。
