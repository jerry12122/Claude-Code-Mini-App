# Changelog

格式參考 [Keep a Changelog](https://keepachangelog.com/zh-TW/1.1.0/)，版本號遵循 [SemVer](https://semver.org/lang/zh-TW/)。

發版流程：更新 `VERSION`、`internal/version/version.go`、README 版本徽章與本檔 → commit → 打 `vX.Y.Z` tag 並 push，GitHub Actions 會自動建置並發佈 Release（說明文字取自本檔對應段落）。

## [0.16.0] - 2026-10-27

### 新增
- 聊天室原生支援 Mermaid 圖表渲染：自動識別 Markdown 訊息中的 ````mermaid` 程式碼區塊，並非同步渲染為高解析向量 SVG 圖表，包含流程圖（Flowchart）、序列圖（Sequence Diagram）、狀態圖、類別圖等常見圖表
- 圖表工具列與雙向切換：支援在「圖表檢視」與「原始碼」之間流暢切換，並提供一鍵複製原始 Mermaid 代碼按鈕
- 串流生成防護：在 Agent 串流輸出過程中保持繪圖中提示，待串流代碼完整結束後才非同步執行圖表繪製，避免不完整語法導致的報錯與畫面閃爍
- 語法錯誤優雅降級：若遇到 Mermaid 語法解析失敗，自動攔截原生錯誤並切回原始碼區塊，同時標示友善警告提示
- 深色主題適配與燈箱放大檢視：依據 OKLCH 色彩空間自訂深色主題色彩；點擊圖表或放大鏡可開啟全螢幕向量燈箱，支援雙擊/滾輪縮放與點擊空白處單次關閉
- Telegram 會話路徑與偽裝支援：支援會話目錄路徑以 `tg://` 開頭時自動建立 Telegram 會話，並支援將選定 Agent 保存為偽裝外觀（前端卡片、分頁、頭像皆同步顯示偽裝圖示）
- 桌面版開發人員工具快捷鍵：正式發布版本支援透過快速鍵序列觸發開啟 DevTools
- 新增開源社群規範：新增 CONTRIBUTING.md、SECURITY.md 與 GitHub Issue/PR 範本

### 修正
- 分割視窗燈箱事件隔離：加入 DOM 階層包含度判斷，避免左右雙欄分割視窗時同時響應全域圖表放大事件導致開啟雙重燈箱
- 會話拖曳聚焦：拖放會話到群組時自動聚焦該會話

## [0.15.0] - 2026-10-20

### 新增
- 首次使用設定精靈：沒有 `config.yaml` 時直接引導設定登入密碼、連接埠與資料庫路徑，可產生隨機密碼；區網登入與 Telegram Mini App 預設關閉，只用桌面版或本機網頁不需要 Bot Token、網域或 Tunnel
- 精靈檢查 Claude、Codex、Cursor、Kiro 與 Antigravity CLI 的可用性與版本，提供官方安裝連結；沒有安裝 CLI 也能先儲存設定
- 設定模式僅接受本機連線，驗證 Host 與一次性憑證；設定檔原子寫入且不覆寫已有檔案。儲存需要支援硬連結的檔案系統（例如 NTFS）
- 連接埠不變時儲存後直接切換到密碼登入；變更連接埠時提示重啟與新網址。桌面版首次設定會寫在發布包 exe 旁邊

### 變更
- `bot_token` 改為可選；沒有 Token 時需有效的網頁密碼，並停用 Telegram initData 驗證與通知
- 中英文快速開始改以精靈為主，補上遠端 server 透過 SSH 連接埠轉發進行首次設定的方式

## [0.14.0] - 2026-10-20

### 新增
- Telegram 私聊會話（預設停用，需在 `config.yaml` 設定 `telegram_client`，範例見 `config.example.yaml`）：與綁定的對象雙向對話，手機或電腦版發出的訊息也會即時出現；往上捲動才載入較早的訊息；圖片進入畫面才載入，影片與檔案點擊才下載；可用既有的上傳按鈕傳送照片與檔案。內容直接從 Telegram 讀取，不存進本機資料庫，所以未連線時看不到歷史；Telegram 會話不開放分享，也不計未讀、不推送「任務完成」通知
- 桌面版啟動時清除更新留下的舊 exe（`<exe>.old.<時間戳>`），一個都不留：只刪檔名嚴格吻合、純數字後綴的一般檔案，刪不掉的檔案只記錄、下次啟動再試，不影響啟動。注意：更新後不再有上一版的殘留檔可當還原用，要能回退請在更新前自己備份 exe

### 修正
- 會話分頁列可用滑鼠滾輪橫向捲動
- 分割窗格時，另一個窗格載入完成或任務結束不再搶走正在輸入的游標
- 狀態標籤的執行中提示過長時截斷為「...」，不再擠到右側圖示

## [0.13.2] - 2026-10-19

### 變更
- 桌面更新視窗的更新說明改以 Markdown 渲染（標題、項目符號、行內程式碼、程式碼區塊），並只顯示「更新內容」的 changelog，不再顯示下載表與首次設定。說明內的 HTML 一律只顯示為文字，連結只留文字
- 注意：更新說明由「目前執行中的版本」的畫面渲染，所以從 0.13.1 更新到本版時仍是舊的純文字樣式；從本版更新到之後的版本才會看到新樣式

## [0.13.1] - 2026-10-19

### 變更
- 文件：README 與功能說明補上會話分頁、分割檢視、拖放擺放、快捷鍵與輸入列控制；更新主視覺截圖並新增分割檢視截圖
- 本版只有文件與截圖差異，桌面程式行為與 0.13.0 相同（版本號與建置 commit 不同）

## [0.13.0] - 2026-10-16

### 新增
- Windows 桌面版自我更新：系統匣與設定的「檢查更新」，固定查詢 GitHub 正式版；下載與「重新啟動並更新」都需手動確認，只替換 `claude-miniapp-desktop.exe`，保留設定、資料庫、附件與日誌
- 桌面更新的忙碌保護：有進行中的任務、shell、待授權或佇列項目（含暫停中）時拒絕重啟（`update_busy`），更新不會強停工作
- 發佈資產新增 `claude-miniapp-desktop-v<X.Y.Z>-windows-amd64.exe`（與 ZIP 內的桌面 EXE 位元組相同），`checksums.txt` 涵蓋 ZIP、tar.gz 與桌面 EXE

### 變更
- 桌面版 `build.bat` 改用 `-tags production`，前端嵌入執行檔；`go run ./cmd/desktop` 仍為開發模式（讀取磁碟前端）
- 發佈流程改為草稿優先：先建立草稿、上傳並比對所有資產，全部通過才公開為 latest；已公開的版本不會被覆寫
- 發佈前檢查 tag、`VERSION`、`internal/version/version.go` 的 Version 一致，且 tag 指向觸發建置的 commit

## [0.12.0] - 2026-10-16

### 新增
- 桌面版會話分頁（≥1024px）：每個群組上方有分頁列（名稱、agent 圖示、狀態點、關閉鈕、中鍵關閉），只掛載作用中分頁的 ChatView；開啟已開啟的會話只聚焦、不重複建立
- 分頁群組與拖放：從側欄拖曳會話到分頁列或內容區（中央加入、單群組邊緣建立第二群組），分頁可重排或跨群組移動，拖曳時顯示放置預覽；分頁與群組狀態持久化，並自動遷移舊的雙欄狀態
- 快捷鍵：`Ctrl+\` 分割／合併群組、`Ctrl+Alt+←/→` 切換群組焦點（排除 IME 組字與 AltGr）
- 輸入列模型控制：Model／Effort／權限模式移到輸入列，改為精簡 chip
- 用量進度條：頂欄與手機晶片以進度條顯示帳戶用量窗口與方案；後端 quota payload 新增選填的 `windows`／`plan` 欄位，`display_text` 保留
- Antigravity 模型選單同步：啟動及設定頁「重新抓取」時透過 `agy models` 取得清單（失敗或空清單不覆寫既有選項）
- 設定頁顯示版本與 Git SHA（`/config` 回傳 `version`、`sha`）
- 文件：新增中英文 Getting Started 指南

### 變更
- 移除側欄會話列的「在副視窗開啟」按鈕，第二群組改以拖放或 `Ctrl+\` 開啟；窄螢幕仍為單欄
- 雙欄時作用中面板的外框改畫在內容上層並加粗提亮；側欄選中的會話列加上明顯外框
- 頂欄不再放 Model／Effort／權限控制，移除頂欄收合

### 修正
- Claude 回完話但程序未退出（例如等待 `ScheduleWakeup`）時，畫面顯示閒置、後端卻視為忙碌而讓新訊息排隊且無法中斷；現在程序超過 3 秒未退出會顯示中斷鈕
- Claude 的提問與工具授權分開處理

## [0.11.0] - 2026-10-14

### 新增
- 選取文字引言回覆：框選訊息正文後，可將選取內容轉為 Markdown 引用並插入目前面板的輸入框；支援雙會話分屏
- MCP `post_message`：外部 Agent 可直接寫入指定會話，支援訊息角色、作者與可選 Telegram 通知，寫入時不啟動 Agent CLI
- 未讀訊息數字徽章：會話卡片與側邊欄顯示未讀數量，超過 99 顯示 `99+`；進入會話或全部標為已讀時立即清零

### 變更
- 引言工具列支援邊界定位，輸入框依內容增高，超過高度上限時可捲動

## [0.10.0] - 2026-10-12

### 新增
- 雙會話分屏功能（Split Chat View）：支援在單一視窗中同時檢視並操作兩個獨立會話
- 雙向排版切換：支援水平並排（左右分屏）與垂直堆疊（上下分屏，針對直屏顯示器優化）
- 分割比例自訂：支援滑鼠與觸控拖曳調整主副欄比例（具備最小寬度 320px 與最小高度 200px 保護），雙擊分隔線可快速重設為 50/50 等比
- 直觀佈局控制：側邊欄（Aside）提供分屏開啟/關閉開關，對話頂欄（Chat Header）提供直觀雙板塊方向切換按鈕 `[ || ]` / `[ = ]`，副視窗提供獨立關閉鈕
- 鍵盤快捷鍵：支援全域快捷鍵 `Ctrl+Alt+O` 快速切換橫向/縱向分屏方向
- 狀態持久化：分屏啟用狀態、副會話選擇、方向偏好與自訂比例自動儲存至 LocalStorage，重新整理頁面無縫還原
- 手機與窄螢幕自適應：小螢幕下平滑降級為分頁切換模式（主視窗 / 副視窗 Tab），操作互不干擾

## [0.9.0] - 2026-10-12

### 變更
- 重新啟用 Antigravity（`agy`）：預設解析 NDJSON 串流，支援模型、推理強度與權限模式；舊 `gemini` Session 使用 Antigravity runner，新建請選 Antigravity
- Antigravity 配額顯示 Gemini 與 Claude/GPT 的 5 小時／每週已使用比例；手機精簡徽章顯示最高使用率

### 修正
- Antigravity 純文字後備模式成功退出但無輸出時回報錯誤，避免會話停留在執行中
- Antigravity 模型標籤與選擇同步，涵蓋切換、執行前推送、重新連線與清除選擇

### 驗證範圍
- 全專案測試、建置、Node 配額檢查與 Chrome 元件驗證通過；本輪使用假 CLI，真實 CLI／Telegram WebView 實機驗收及回合 Token 統計串接待補

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
