## Context

需求來源是本次對話：建立 session 時能填入類似 Gemini Gem 的角色／工作指令，且只支援 Claude、Codex、Antigravity。這份變更是功能提案，不實作程式。

已檢視的現況：Session 與 RunOptions 沒有專用指令欄位；SessionHandler.Create 解析建立資料後呼叫 DB.CreateSession；useNewSessionForm 由 NewSessionComposer 與 ForwardModal 共用。Claude 透傳 CliExtraArgs，Codex 只組裝已知 argv；Antigravity.Run 預設讀 stream-json，尚非 ACP。internal/kiroacp 有 provider 專用 ACP client，並無共用 ACP 套件。工作區有其他未提交修改，後續 apply 應重新讀取這些入口，不覆蓋無關修改。

2026-11-12 本機 POC 的觀察：Claude 2.1.322 的 append 入口有效；Codex 0.163.0 新會話的 developer_instructions 有效，續接改指令的行為未通過測試；agy 1.5.6 的 ACP 直接追加與跨程序 load 均通過，沒有建立自訂 agent 檔。poc/session-instructions/ 已被 repo 的 poc/ 規則忽略，因此它只是本機證據；以下保留完整傳輸契約與可重現驗收方式，不將其存在當作實作前提。

相關既有規格：console-usability 的命名與鍵盤可操作性、composer-model-controls 的 provider 範圍、session-failure-record 的失敗呈現。新增獨立 session-custom-instructions，不修改上述要求。

## Goals / Non-Goals

**Goals:**

- 在既有建立流程輸入 session 專屬指令，與首則 user 訊息分開。
- 只在 claude、codex、antigravity 的 agent 模式生效，儲存後固定，跨回合與重新啟動不遺失或串用。
- 使用 provider 支援的 system／developer 入口，不把文字拼到一般 Prompt，不建立自訂 agent 檔。
- 空白指令與既有 session 保持現有執行路徑。

**Non-Goals:**

- 不加入可重用 Gem 範本庫、角色市場、分享／匯入匯出、指令版本管理或建立後編輯功能。
- 不支援 Cursor、Kiro、Kiro ACP、Telegram 或 shell 的專屬指令；不變更其 runner。
- 不全面遷移 Antigravity 舊會話、不新增公開 antigravityacp 類型、不引入 SDK／npm 依賴，也不趁機重構 Kiro 的故障復原。
- 不保證模型永遠遵循指令；驗收的是儲存、隔離與傳輸契約，真實模型 POC 為補充證據。

## Decisions

### 建立時固定的 session 專屬指令

採用 JSON 欄位 system_instruction 與 Go 欄位 SystemInstruction，SQLite 使用 TEXT NOT NULL DEFAULT ''。SessionHandler.Create 接受此欄位，Session 的建立／查詢／列表皆能 round-trip；既有 DB 透過增量 migration 得到空字串。指令與 session 建立必須具有同一成功／失敗結果，不留下缺少指令的半成品。

先以 strings.TrimSpace 正規化外圍空白，保留內部換行、引號、反斜線與 Unicode。規格政策上限為 16384 個 UTF-8 bytes；超限與 U+0000 回 HTTP 400，不截斷。前端不以 JS 字串長度替代 UTF-8 bytes，必要檢查使用 TextEncoder。省略、空字串或修剪後空白等同未設定。

非空值僅接受 canonical agent_type claude、codex、antigravity，且 InputMode 正規化後為 agent；Telegram 暗號轉換完成後再驗證，避免 disguise 繞過。其他 runner／shell 回 400。PATCH 若出現 system_instruction 一律回 400，連同其他欄位皆不寫入；UI 明示「建立後固定，調整請另建會話」。與此欄位共存的 Claude CliExtraArgs 若包含 --system-prompt、--system-prompt-file、--append-system-prompt、--append-system-prompt-file（含等號形式），建立回 400 請使用者擇一，不猜測重複旗標的合併方式。空白指令不新增此限制。

替代方案：允許建立後更新會讓 Codex 已存 thread 與 DB 的設定不一致；把指令當 description 或第一則訊息則失去 provider 原生層級，故不採用。

### 三種 runner 使用原生注入並保留空白路徑

RunOptions 增加 SystemInstruction，由每次 runAgent 讀取該 session 的已存值；重試、佇列與重連沿用同值，不接收 WS 單回合覆寫。shell 執行不接收它。既有指令 session 切換 shell 再切回 agent 不刪除既存指令。

- Claude：將非空值作為單一 --append-system-prompt 引數；保留內建 system prompt。每輪帶相同文字，建立後不修改，因此不需要強制 --system-prompt-snapshot off，也不改使用者原有 snapshot 設定。
- Codex：以 -c developer_instructions=<quoted-string> 傳入；使用 encoding/json 的字串編碼形成 TOML 可接受的 basic string，測試換行、引號、反斜線及非 ASCII。新 thread 與 resume 均攜帶相同值，不承諾以 resume 改寫已存指令；保持現有 model、effort 與 sandbox 行為。
- Antigravity：僅非空 SystemInstruction 選擇 agy acp，原 agent_type 不變。空值仍使用現有 stream-json／明確設定的 print 後備模式；非空時不允許以 print 後備丟掉指令。由固定在 session 的欄位決定傳輸，避免同一會話跨回合切換協定。

不將內容寫入 AGENTS.md、CLAUDE.md、.agents/agents 或使用者全域設定。不在 argv 日誌輸出指令原文或整段 Codex override；使用標記／bytes 計數，解析與驗證錯誤也不回顯原文。owner session API 能讀取設定；訪客分享資訊與一般訊息歷史不因新增欄位而暴露原文，也不將它加入使用者訊息或轉發正文。

### Antigravity ACP 只服務有指令的 session

provider 內新增 ACP 路徑，使用 Go 標準庫的 JSON、pipes 與既有程序樹控制。借用 internal/kiroacp/client.go 的 framing 與 callback 設計經驗，但不搬動 Kiro 專用程式或引入通用 provider framework。

流程：啟動 Binary() 指定的 agy acp → initialize → session/new 或 session/load → session/prompt。initialize 需確認 agentCapabilities._meta.antigravity.systemPrompt.append 為 true，並在 load 前確認 loadSession；未支援就回報失敗，不能靜默退回 user prompt 或一般 CLI。

session/new 與 session/load 的 params 都包含 cwd、client mcpServers 與以下 JSON 結構；只有 load 加入 sessionId：

    {"_meta":{"antigravity":{"systemPrompt":{"append":"session 指令原文"}}}}

session/prompt 的 prompt 陣列只包含本輪使用者內容，不含專屬指令。MCP 與預設客製設定沿用 agy 原生設定，不套用 POC 為隔離實驗才使用的 include-default-customizations=false。不得為此新增自動授權；既有 permission_mode 仍決定 default／accept-edits／plan／bypassPermissions 的權限，傳輸與模式選擇以 server 宣告為準，要求的 model／effort 不支援時給具體錯誤。

session/update 映射既有 EventDelta、EventThinking／EventActivity、EventToolStarted／EventToolCompleted；對 session/load 的歷史回放不送成本輪增量。用 session/prompt 回應判定本輪完成，成功且程序收尾後只送一次 EventDone；傳回的 SessionID 供下輪 load 使用。權限請求接現有 RequestPermission 回呼，呼叫端未提供則回 cancelled，不自動允許。必要的 provider 問答若目前無 bridge，明確取消並回報可理解的失敗，不讓未回覆的 RPC 卡住。

使用已快取的本機登入；缺登入／能力／RPC 方法、逾時、EOF、子進程失敗與取消皆走既有錯誤／停止語意。ctx 取消先送 session/cancel，再以既有程序樹控制結束程序；沒有固定字串錯誤時也要回傳可觀測錯誤。OnStart 在程序啟動後記錄 PID，stdout／stderr 分別消費，待處理 RPC 於 EOF／取消／逾時釋放。所有等待設界限，使用既有 runner 的 timeout 與 WaitDelay 原則，不增加永久背景程序。

### 建立表單與設定沿用

在 NewSessionComposer 的 session 名稱與第一則訊息之間，放有可存取 label 的多行「專屬指令（選填）」；說明可設定角色、回覆風格、工作規則，並提示建立後固定，不將其藏在 CLI 進階欄位。ForwardModal 的新建區使用同樣的欄位規則，補齊 antigravity 選項以符合受支援集合。

useNewSessionForm 管理 systemInstruction：支援 runner 互切時保留，轉不支援 runner 清空並從 payload 省略；reset 清空；prefill／applyFromSession 只對受支援目標帶入值。既有 session 的「複製自」、轉發新建與手動接手沿用此設定；自動 handoff 也複製到新 session。直接轉發到既有 session 不修改其設定、不傳送來源指令。

## Implementation Contract

- API：POST /sessions 可選 system_instruction:string；成功 201 回傳正規化後同值。GET 的 owner session 資料可讀取同值，既有欄位不改名；PATCH 不能修改此欄位。限制錯誤為既有 {"error":"..."} 的 400 回應，儲存失敗為 500 且不留下半成品。
- 執行：RunOptions.SystemInstruction 與 Prompt 是不同欄位；第一次與後續 run、佇列、程序重啟後、agent session id 重設後都從相同的 session 持久值取用；同 work_dir 的不同 session 不共享內容。
- 相容：空指令保留原 runner 路徑與 argv；不建立自訂 agent 檔、不新增 runner 選項、不讓不支援 runner 的非空值被忽略。
- 失敗：不支援 ACP 能力／模式或注入失敗不降級、不假裝完成；回報現有 run error。若拒絕 PATCH 或 POST，既有資料不變；非空欄位不授予更多工具權限。
- 驗收：DB migration／round-trip／建立失敗清除測試；HTTP 支援集合、長度與字元邊界、不可修改測試；Claude／Codex argv 精確斷言與日誌不洩露斷言；假的 agy ACP 子進程驗證 new/load metadata、回放抑制、權限／取消／EOF／錯誤及 terminal event 唯一性；WS fake runner 驗證兩個同目錄 session、queue／retry／clear／handoff 的值；前端狀態與實際建立 payload／可存取欄位測試。
- 真實 POC 的 marker 僅在專屬指令、不得出現在 user prompt：Claude 與 Codex 新 session、相同指令的跨程序續接；agy 新會話及 load，紀錄確實收到當前指令。這是補充證據，不能替代假程序與邊界測試。
- 範圍：只實作本提案三種 runner 的建立時指令功能及其必要資料、表單、執行與驗證；不加入建立後編輯／可重用 Gem 管理，也不修改其他 runner 的產品功能。

## Risks / Trade-offs

- Codex 續接改指令沒有通過本機 POC → 指令建立後固定；複製設定到新 session 才使用新內容。
- ACP 與 CLI 的模型／權限選項不完全一致 → 先以 fake server 與帳號能力檢查保持語意；unsupported 明確失敗，不偷偷換模型或放寬權限。
- Antigravity ACP 增加傳輸實作成本 → 只影響有指令的 session，保持原 provider 內局部改動；不抽象整個 runner 架構。
- 舊 agy 缺少追加能力 → 在 run 前檢查能力並提示更新，不丟失設定或降級。
- profile／設定檔與同目錄 session 相互污染 → 指令只經 argv／ACP 傳輸，不寫共享檔。
- 指令可能含私有角色內容 → argv 日誌須遮蔽，分享資料不擴充此原文；不把模型遵循測試視為保密保證。

## Migration Plan

先加入可空字串的資料欄位與相容 API，再接共用參數、各 provider 路徑，最後開放表單；每個任務結束保持 build／受影響測試通過。新增欄位由啟動 migration 填入空值，沒有既有資料改寫。回滾時保留 DB 新欄位，舊程式會忽略它；有指令的新 session 不應交由舊版繼續執行，以免失去角色設定，需回復新版或另開空白會話。

## Open Questions

無待使用者決定事項。建立後固定、空白相容、限定三種 runner 為此提案採用的第一版範圍；若使用者另要求修改既有 session 指令，再建立後續變更，不放寬本契約。
