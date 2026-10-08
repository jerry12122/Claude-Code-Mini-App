# Changelog

格式參考 [Keep a Changelog](https://keepachangelog.com/zh-TW/1.1.0/)，版本號遵循 [SemVer](https://semver.org/lang/zh-TW/)。

發版流程：更新 `VERSION`、`internal/version/version.go`、README 版本徽章與本檔 → commit → 打 `vX.Y.Z` tag 並 push，GitHub Actions 會自動建置並發佈 Release（說明文字取自本檔對應段落）。

## [0.8.0] - 2026-10-08

### 新增
- Claude 授權：新增 `/mcp/perm`（`--permission-prompt-tool` 專用，沿用 `mcp_token`），`bypassPermissions` 下命中 `permissions.ask` 規則時會跳出允許／拒絕，按允許後同輪執行；未設定 `mcp_token` 時行為與過去相同
- 設定新增 `web.trusted_proxies`（預設 `127.0.0.1/32`、`::1/128`），只有來自可信代理的連線才採信 `CF-Connecting-IP`／`X-Forwarded-For`
- 故障後手動建立接手會話：ErrHandoff 自動交接未成功時，聊天室顯示「建立接手會話」，沿用 `NewSessionComposer` 預填原名稱／工作目錄／供應商／權限／model／effort 與可編輯接手首則訊息，可改供應商（切換時清除不相容的 model／CLI 參數／effort）；`POST /sessions` 新增可選 `effort` 欄位供建立時寫入；MCP 關閉時改帶入現有聊天室的有限歷史（含裁切標示）；僅擁有者可見，訪客不可建立

### 變更
- 真實 IP 判斷改為：僅在連線來自可信代理時才讀取代理標頭，`X-Forwarded-For` 由右往左跳過可信代理，避免偽造左側 IP 繞過 `allowed_cidrs`
- 授權提示明確標示授權範圍
- 文件：README 重新定位為多代理控制台，補上三種入口、下載安裝、臨時共享與示範截圖，功能細節移至 `docs/features`
- Release 說明加上下載與首次設定指引

### 修正
- Kiro ACP 故障復原：以本輪專屬 log／完整 RPC 原因分類，補齊啟動逾時、有限重試、取消與進程／授權收尾；修正 stdin 阻塞、EOF 蓋掉 RPC 錯誤及歷史工具回放影響 watchdog，授權請求維持依序處理
- 被 deny 規則擋下的呼叫（`non_execution_kind=permission-rule`）不再被誤判為等待授權，允許重跑後仍被拒也不會無限等授權
- MCP 發起的 run 沒有 `tg_id` 時，待授權通知在白名單僅一人且非訪客的情況下改退回該使用者推送 Telegram
- Kiro ACP 讀取 stdout 的單行上限提高到 16MB，避免大型 tool 結果觸發 `bufio.Scanner: token too long` 而無法恢復
- 停止鈕在呼吸燈期間仍可再按：第一次送出優雅停止，稍後再按才強制停止

## [0.7.0] - 2026-10-06

### 新增
- 臨時共享聊天室：後端（DB、API、訪客驗證、WS 協作）與前端（訪客加入頁、分享彈窗、設定分享頁）；加入頁支援 `?pin=&name=` 預填，分享卡片可「複製連結＋PIN」
- 設定頁新增「日誌」分頁，即時串流伺服器日誌（等級篩選、搜尋、暫停、複製）
- 外觀設定新增對話文字大小滑桿
- Kiro 專屬幽靈圖示
- Claude 模型清單改由 `claude` 自己回報，設定頁可不重啟重新抓取
- 附件卡片改善、驗證預覽與歷史持久化

### 變更
- 使用者上傳不再限制大小、數量與副檔名
- 設定視窗各分頁統一尺寸，一般／外觀頁加大區塊間距
- codex／kiro 的 model badge 改以下拉選擇為準
- 移除過期的 `index.html` 備份檔

### 修正
- Kiro ACP 授權改以 session 為單位，修正斷線後 allow/deny 被忽略而永遠卡在等授權
- 主要情境操作補上即時回饋，失敗不再靜默
- Claude `tool_result.content` 為字串時不再解析失敗
- 共享聊天室 WS 連線回收競態

## [0.6.0] - 2026-10-01

### 新增
- Windows 桌面版視窗殼（Wails v3）與系統匣：關閉視窗隱藏到匣，左鍵顯示、右鍵選單，Ctrl+Q 結束，F5／Ctrl+R 重整，F12 DevTools
- Ctrl/Cmd+P 快速跳轉 session（可依名稱、目錄、分支搜尋）
- 附件 chip／拖放上傳、貼上剪貼簿截圖；session 列表透過 `/events` WebSocket 即時推播
- 訊息長按選單，複製／轉發按鈕移到時間戳旁
- 聊天室 UX：輸入法 Enter 防誤送、手機 Enter 換行、上捲不被串流拉回並提供「跳到最新」、刪除 session 5 秒內可復原

### 變更
- 上傳檔改存 runtime 的 `workspace/uploads/`，不污染專案 `work_dir`

### 修正
- 桌面版吃舊快取導致 Ctrl+P 無效（新增 F12 與「清除快取並重載」）
- 補上遺漏的 `CREATE_NO_WINDOW`，避免無主控台時子行程彈出黑窗

## [0.5.0] - 2026-09-30

### 新增
- 訊息佇列：執行中送出的訊息排隊（存 DB、重啟不遺失），失敗或中斷時暫停，可恢復或編輯
- MCP `ask_session` 與 hop 上限（`mcp_max_hops`），防止 agent 互問迴圈；`list_activity` 工具可跨 session 依時間查活動
- 檔案上傳
- 對話中以 `@mention` 諮詢其他 session
- 新建 session 改走 Kiro ACP，Kiro ACP 支援互動式授權與 MCP 設定載入
- 一般設定頁（含 `vscodeNoAdmin`）；外觀設定持久化到 SQLite
- 未讀追蹤與「全部已讀」
- session 中途切換 model 與 effort；補齊完整 Claude model 清單
- 日誌改用 slog + zap，同時寫 stdout 與 rotate 檔
- 獨立 `work_dirs` 表記住工作目錄

### 修正
- SQLite DSN 語法錯誤，WAL 與 `busy_timeout` 先前未生效
- MCP registry 並發寫入 WebSocket 的 race 與重複連線
- 對齊 codex-cli 0.159.x，quota 改讀 rollout `rate_limits`
- `KiroFetcher` 讀錯輸出管道導致 usage 顯示 `--`
- 停止改為兩段式：先優雅信號，第二次才強制砍樹

## [0.4.0] - 2026-09-02

### 新增
- 會話 header 新增「開啟 VSCode」「開啟目錄」按鈕
- Kiro ACP 從 Kiro `mcp.json` 載入 MCP 並傳入 ACP session
- 側欄收合為迷你 icon rail，可直接切換 session
- quota 用量文字依 80%／100% 門檻變色；進入會話時若快取過期會背景補打
- MCP 截圖落地成 markdown，打通 claude／cursor／kiro 傳圖
- 設定視窗（markdown 顏色與自訂 CSS）
- session 側欄改單行列表與資料夾群組樹狀縮排

### 修正
- Kiro ACP http MCP server 的 headers 需為陣列，物件會讓 `kiro-cli acp` 無聲崩潰
- taskEnd 依 msgID 對帳，避免舊任務誤刪新任務登記
- 手機燈箱鎖背景捲動並支援雙指縮放

## [0.3.0] - 2026-08-03

### 新增
- MCP server（Streamable HTTP，預設關閉），讓其他 agent 操作 session
- Codex CLI headless runner 與 quota 剩餘時間顯示
- Agent／Shell 失敗時推送 Telegram 錯誤通知（含 CLI stderr）
- Agent Console 介面作為預設主題；訊息時間戳與 agent 圖示
- Kiro ACP 實驗性 runner
- Web token 過期自動登出

### 變更
- Gemini 遷移至 `agy`（Antigravity），headless 整合暫停

### 修正
- WebSocket 寫入序列化，避免 concurrent write panic
- 防止 Claude 背景任務提早完成與空白回覆

## [0.2.0] - 2026-06-30

### 新增
- Kiro runner、帳號 quota 介面、POC 腳本
