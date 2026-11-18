# 工作清單 (Todo)

> 整體方向見 `docs/plan/roadmap.md`。已完成的項目請移至 `docs/plan/done/`。

---

## 🚀 待執行項目
- [ ] Windows x64 安裝包發版驗收：實作與本機 smoke／46 項發布檢查完成，尚待 Win10／乾淨帳戶、真實 Runtime 缺少、安裝版跨版更新及手動遷移驗收；普通全套 Cursor 測試仍有逾時（見 [紀錄](../reviews/2026-11-18-windows-installer.md)、[規劃](todo/windows-installer.md)）
- [ ] 桌面自我更新實機驗收：從 v0.13.0 在真實 GitHub 自動更新到下一版（目前只在 fixture 驗證）、點系統匣圖示「檢查更新」、真實 helper 啟動失敗；已知限制：外部／分享連結無法交外部瀏覽器（Wails beta.26 Windows 無導航攔截 API）、替換與還原都被鎖時不自動重啟（見封存的 design.md）
- [x] 版本化桌面 EXE 自更新後檔名不變：評估完成（2026-11-18），不需改程式。便攜 ZIP 內本來就是固定名稱 `claude-miniapp-desktop.exe`（`scripts/release/package.sh`），安裝器入口也是固定名稱，README 推薦的預設入口已是 Setup 安裝包；帶版號的 EXE 只是自我更新用的獨立資產，只有手動直接下載該檔執行的人會遇到檔名仍帶舊版號
- [x] `go mod tidy` 既有差異已處理（2026-11-18）：實際差異是 `x/crypto`、`x/sys` 由 indirect 改為直接依賴，並補／移除 `go.sum` 過期行；原本記載的 `fasthttp`、`jsonschema-go` 與當時現況不符
- [x] `internal/cursor` 的 `runner_test.go:251`（「Run 卡在孤兒進程占住的 stdout」）偶發失敗已修（2026-11-18）：根因是 result 後放棄等待時 `KillTree` 殺不到已退出 wrapper 底下的孤兒（PID 不存在），`cmd.Wait` 只能等 `WaitDelay` 5 秒；負載下前段啟動時間再疊加就超過 7 秒逾時。這也是產品行為問題（result 後有孤兒時任務多卡約 5 秒）。放棄分支把 `WaitDelay` 縮到 200ms；同負載條件下 8 次由失敗 7 次變為 0 次，單次耗時 6.1s→約 1.4s。已知餘險：孤兒進程本身未被殺掉（Windows 限制），約 7 秒後自行結束；前段啟動時間在本機波動大，原因未查
- [ ] Antigravity 後續驗收：真實 CLI 多輪對話、權限模式、帳戶配額與 TG WebView；回合 Token 統計串接待補（見 [整合計劃](agy-integration.md)）；補充（2026-11-18 核對）：「回合 Token 統計」不是只補 agy 一條線，而是尚未設計的跨 provider 資料流（`agent.Event` 沒有用量欄位；`internal/usage` 的單回合 `Info` 與 `FromClaudeResult` 等解析函式沒有呼叫者，該套件其餘部分則被額度功能使用）。已寫設計草案並建議預設不做，待使用者決定（見 [草案](todo/turn-token-usage.md)）
- [x] 附件輸入與聊天呈現：輸入框內佔位／重試、訊息附件卡片、驗證預覽與歷史持久化；實機驗證待補（見 [計劃](todo/attachment-experience.md)）
- [ ] Claude ask 規則走 miniapp 授權：`bypassPermissions` 下命中 `permissions.ask` 時跳出允許／拒絕，按允許同輪執行（見 [計劃](todo/claude-permission-prompt.md)）；補充（2026-11-18 核對）：實作已在 `150c406`、`3b3228a`（`internal/ws/claudeperm.go`、`/mcp/perm`），計劃文件狀態寫「尚未實作」已過時；仍待補的是計劃第 6 節的真實 `claude -p` 整合驗證與桌面／瀏覽器／TG 手動驗證
- [ ] 手機實機驗證：TG WebView（iOS／Android）📎 檔案選擇器、佇列 UI
- [x] 驗證 Codex／Cursor／Kiro 能以絕對路徑讀取工作目錄外的圖片（2026-11-18）：claude／codex／kiroacp／antigravity／cursor 皆讀到影像且像素核對正確；測試路徑是 `workspace/rooms/…`，不是真實附件上傳流程，附件流程的實機驗證仍待補（見 [supervisor 試跑紀錄](todo/supervisor-room.md)）
- [ ] （未來優化，2026-10-06 決定暫緩）agent 圖片的 session／分享驗證與歷史 URL 遷移；目前 `internal/static/uploads` 仍由公開靜態路由讀取，需先確認相容性與授權載入方式
- [ ] 主要情境即時回饋與可靠性：授權／中斷 send 失敗處理、WS 斷線指示、用量刷新（手機）、建立 session 重複、載入中與登入卡住（見 [計劃](todo/ux-feedback.md)）；2026-11-18 已用臨時實例實測 2.2 斷線橫幅、2.1 離線 `/reset`、2.6 離線登入皆通過，其餘（授權／中斷按鈕、用量刷新、建立 session 重試、Slow 3G）與手機實機仍待補，見計劃第 7 節
- [ ] Kiro 故障復原：重新建置／啟動新版後觀察真實 EmptyResponse；自動接手的權限／工具設定與連鎖防護仍待 review（手動入口及 handoff 測試 race 已完成，見 [驗收紀錄](done/kiro-fault-recovery.md)）
- [ ] 接手提示的整頁重載／離線恢復，及 TG WebView／真實供應商接手實機驗收（目前提示保存在 App 記憶體，見 [紀錄](done/manual-session-handoff.md)）

## ✅ 本輪完成（2026-10-16，v0.12.0／v0.13.0）

- [x] 深色／淺色／跟隨系統主題切換（設定頁「外觀」最上方，只存本機 `cc_theme_v1`，預設深色；語意 token 加 `tailwind.config` 映射，涵蓋 Ctrl+P、下拉選單、浮層、程式碼高亮與 Mermaid；規格見 `docs/spectra/specs/theme-switching`）
- [x] 桌面版會話分頁：群組分頁列、側欄與分頁拖放（中央加入、單群組邊緣分割）、`Ctrl+\` 分割／合併、`Ctrl+Alt+←/→` 切換群組焦點；移除側欄「在副視窗開啟」按鈕；作用中面板與側欄選中列加外框（規格：`docs/spectra/specs/session-tabs`）
- [x] 輸入列模型控制（Model／Effort／權限模式移到輸入列）與頂欄用量進度條（`docs/spectra/specs/composer-model-controls`、`quota-usage-bars`）
- [x] 修正 claude 回完話但程序未退出（`ScheduleWakeup`）時畫面 IDLE、後端卻視為忙碌而讓新訊息排隊且無法中斷：超過 3 秒未退出改推 `lingering`，前端顯示中斷鈕
- [x] Antigravity 以 `agy models` 同步模型選單；設定頁顯示版本與 Git SHA
- [x] 桌面版自我更新（Wails updater）：固定穩定版來源、強制 SHA256、確認後才下載與重啟、六小時背景檢查、任務維護閘門（有工作或排隊中就不重啟）、production 嵌入前端（規格：`docs/spectra/specs/desktop-distribution`、`desktop-self-update`；已知限制見 `docs/spectra/changes/archive/2026-10-16-desktop-self-update/design.md`）
- [x] 發佈流程：草稿優先、比對通過才公開為 latest、已公開版本禁止覆寫、新增版本化桌面 EXE 資產；真實 GitHub 首次發佈（v0.13.0）失敗過一次（Windows CR 污染 asset id），已修並加本機回歸測試（`scripts/release/`）
- [x] Windows 真程序端到端驗證腳本（`scripts/e2e/`），抓到並修正兩個單元測試漏掉的缺陷：桌面啟動即 panic、ExecJS 永不送出導致更新橋接失效

## ✅ 本輪完成（2026-10-12）

- [x] Antigravity 審查修復：空白輸出收尾、模型切換／執行／重連標籤同步、Gemini／Claude/GPT 配額與手機耗盡警示；回歸測試、全專案測試、建置及 Chrome 元件驗證通過（見 [完成紀錄](done/agy-review-fixes.md)）；真實 CLI 與 TG WebView 實機驗收待補

## ✅ 本輪完成（2026-10-07）

- [x] Kiro ACP 故障判斷與有限重試：隔離 log、啟動／idle 逾時、取消與收尾、阻塞寫入、EOF 錯誤保留、授權序列與回收；全專案測試、Kiro race 及真 CLI 2.28.0 smoke 通過（見 [驗收紀錄](done/kiro-fault-recovery.md)）；尚未重啟服務，接手流程見下項
- [x] 故障後手動建立接手會話：ErrHandoff 自動交接未成功時廣播結構化資訊，沿用 `NewSessionComposer` 預填設定與可編輯接手訊息，可改供應商；MCP 關閉時改帶入有限歷史；訪客不可見（見 [驗收紀錄](done/manual-session-handoff.md)）

## ✅ 本輪完成（2026-10-06）

- [x] 跨供應商工作接續 PoC：Claude 真實額度耗盡切到 Codex、Codex／Kiro 雙向摘要交接、無最後摘要恢復及原生 resume 基線（見 [實測紀錄](../../poc/agent-handoff/agent-handoff-poc.md)）；尚未接入聊天 UI／DB
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
- [x] Telegram 改為可選模組（第一、二期已完成，2026-11-18 核對並補上缺的啟動提示）：`bot_token` 可留空（需 `web.password`）、空 token 時 `tg.Verify` 拒絕且標頭／查詢的 `initData` 一律當沒帶（測試 `TestEmptyBotTokenIgnoresInitData`）、`RealIP` 只信任設定的代理；第三期（信任上游認證）仍為選做、未決定（見 [計劃](todo/telegram-optional.md)）
- [ ] Supervisor 工作室：第一期已試跑；額度硬守門已實作並驗證（2026-11-18，`quota_guard_percent` 預設 90，排隊訊息自動啟動時檢查）；使用者批准做第二期「唯讀虛擬群聊」，規格見 [計劃](todo/supervisor-room.md) 7.1，尚未開工；第三期（群組輸入與 @ 路由）尚未開始
- [x] 支援更多 AI 工具（若有 CLI）：評估完成（2026-11-18），目前不新增。OpenAI 模型已由 Codex CLI 提供（本專案已整合，模型可切換）；DeepSeek 查無官方編碼代理 CLI，只有第三方 PyPI 套件 `deepseek-agent`，不屬官方、需自備 API key，而本專案以 `bypassPermissions` 執行代理，不建議未經審查就接入第三方代理。之後出現明確的官方 CLI 再重開
- [x] 檔案總管功能 (瀏覽 work_dir 檔案)：已實作並驗證（2026-11-18）。唯讀、限單一 session 的 work_dir、受保護憑證檔名、訪客 403；supervisor 以臨時實例做過對抗性複核（路徑跳出、junction、備用資料流、大小寫、洩漏）。規格的假設與受保護名單已由使用者確認維持現狀（2026-11-18）；已知限制見 [計劃](todo/file-explorer.md) 第 7 節（硬連結、Windows symlink 測試 SKIP、`~數字` 檔名誤擋）
- [x] 系統資源監控 (CPU/Memory 狀態)：已實作並驗證（2026-11-18）。後端 `GET /system/stats`（新套件 `internal/sysmon`，無第三方依賴，訪客 403）、設定頁唯讀「系統資源」區塊（可見時每 3 秒輪詢）；實測 CPU 與 Windows `Get-Counter` 差在 15 個百分點內、記憶體差約 0.1 GB。規格的假設已由使用者確認不需修改（2026-11-18）（見 [計劃](todo/system-monitor.md)）；已知限制：Linux 路徑只有解析測試與跨平台編譯，沒在 Linux 機器跑過
- [x] Kiro ACP：前端已重新啟用（需 kiro-cli >= 2.16.0；`session/load` PoC 通過，見 `poc/kiro-cli/acp_same_cwd_resume_poc.js`／`docs/plan/done/kiro-output-markdown-and-acp.md`）
- [x] Session @mention + MCP 諮詢署名（見 `docs/plan/done/session-mention-mcp.md`）
