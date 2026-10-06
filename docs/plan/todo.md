# 工作清單 (Todo)

> 整體方向見 `docs/plan/roadmap.md`。已完成的項目請移至 `docs/plan/done/`。

---

## 🚀 待執行項目
- [x] 附件輸入與聊天呈現：輸入框內佔位／重試、訊息附件卡片、驗證預覽與歷史持久化；實機驗證待補（見 [計劃](todo/attachment-experience.md)）
- [ ] Claude ask 規則走 miniapp 授權：`bypassPermissions` 下命中 `permissions.ask` 時跳出允許／拒絕，按允許同輪執行（見 [計劃](todo/claude-permission-prompt.md)）
- [ ] 手機實機驗證：TG WebView（iOS／Android）📎 檔案選擇器、佇列 UI
- [ ] 驗證 Codex／Cursor／Kiro 能以絕對路徑讀取上傳的圖片（Claude 的 Read 工具可讀圖）
- [ ] （未來優化，2026-10-06 決定暫緩）agent 圖片的 session／分享驗證與歷史 URL 遷移；目前 `internal/static/uploads` 仍由公開靜態路由讀取，需先確認相容性與授權載入方式
- [ ] 主要情境即時回饋與可靠性：授權／中斷 send 失敗處理、WS 斷線指示、用量刷新（手機）、建立 session 重複、載入中與登入卡住（見 [計劃](todo/ux-feedback.md)）

## ✅ 本輪完成（2026-10-06）

- [x] 2026-10-06：`RealIP` 只採信設定的代理（預設 loopback），XFF 由右往左找來源，拒絕無效來源；Claude 舊重跑授權明確提示本次放行工具，新中途授權保留單次操作（見 [紀錄](done/trusted-proxy-and-permission-scope.md)）

## ✅ 本輪完成（2026-09-30）

- [x] 訊息佇列：執行中送出的訊息排隊、完成後自動續跑、失敗／中斷／拒絕授權暫停（存 DB）
- [x] MCP envelope hop 計數（預設上限 5，`mcp_max_hops` 可設）
- [x] MCP `ask_session`：阻塞等回覆 + timeout
- [x] 上傳圖片／檔案給 agent（存 runtime 目錄的 `workspace/uploads/<session_id>/`，不污染專案 `work_dir`）

---

## 📈 進階規劃
- [x] 桌面視窗殼第一版（Wails，見 `docs/plan/todo/desktop-window.md`）
- [x] 桌面版系統匣：關視窗後從匣裡叫回／結束（見 `docs/plan/todo/desktop-window.md`）
- [x] 臨時共享聊天室：PIN + 連結 + 暱稱，讓朋友限時加入同一聊天室協作；實機驗證待補（見 [紀錄](done/share-chat.md)）
- [ ] 共享聊天室實機驗證：snapshot 橫幅、到期前 5 分鐘提示、editor 實際送訊息、非內網 IP 帶 guest token 的存取測試
- [ ] 共享聊天室對外開放：Cloudflare Tunnel 部署與實機驗證；`RealIP` 可信代理檢查已完成，其他代理須設定 `web.trusted_proxies`
- [ ] Telegram 改為可選模組：`bot_token` 可留空、關閉空 key 的 `initData` 驗證、`RealIP` 只信任已知代理（見 [計劃](todo/telegram-optional.md)）
- [ ] 支援更多 AI 工具 (例如 OpenAI o1, deepseek 等，若有 CLI)
- [ ] 檔案總管功能 (瀏覽 work_dir 檔案)
- [ ] 系統資源監控 (CPU/Memory 狀態)
- [x] Kiro ACP：前端已重新啟用（需 kiro-cli >= 2.16.0；`session/load` PoC 通過，見 `poc/kiro-cli/acp_same_cwd_resume_poc.js`／`docs/plan/done/kiro-output-markdown-and-acp.md`）
- [x] Session @mention + MCP 諮詢署名（見 `docs/plan/done/session-mention-mcp.md`）
