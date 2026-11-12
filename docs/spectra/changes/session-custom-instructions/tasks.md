## 1. 儲存與建立邊界

- [x] 1.1 落實「建立時固定的 session 專屬指令」的相容儲存與共用 RunOptions.SystemInstruction，讓 Existing sessions and empty instructions retain their execution path，並讓 Session get/list round-trip 正規化後原文；定位 internal/db/db.go、internal/db/session.go、internal/agent/runner.go。先寫 TestSessionInstructionMigration、TestSessionInstructionRoundTrip、TestSessionInstructionCreateFailure，驗證舊 DB 空值、新欄位與建立失敗不殘留；執行 go test ./internal/db ./internal/agent。
- [x] 1.2 [after: 1.1] 完成 Creation validates and atomically persists instructions 與建立後不可修改的 API 邊界：只接受三種 canonical runner 的 agent 模式，修剪外圍空白、保留內文；驗證 16384 UTF-8 bytes、U+0000、JSON 型別、Telegram 轉換後的拒絕、Claude 指令旗標衝突，PATCH 含該欄位時所有欄位皆不寫入。定位 internal/api/session.go；以 TestCreateSessionInstructionValidation 的規格邊界表、TestPatchSessionInstructionImmutable 與失敗清除案例驗證，執行 go test ./internal/api ./internal/db。

## 2. Claude 與 Codex 原生傳遞

- [x] 2.1 [after: 1.1] 實現 Claude appends the saved instruction natively：buildClaudeArgs 在非空時傳單一 append 引數，新建／續接同值，不改首則 user Prompt、權限／MCP 及 snapshot 政策；空值不加旗標，對應設計「三種 runner 使用原生注入並保留空白路徑」。定位 internal/claude/runner.go；先寫 TestBuildClaudeArgsSystemInstruction 覆蓋 Unicode、換行、引號／反斜線、空值及 resume，執行 go test ./internal/claude。
- [x] 2.2 [after: 1.1] 實現 Codex receives saved developer instructions：buildArgs 對 exec／exec resume 傳相同且正確編碼的 developer_instructions，內容不得拆成額外參數，空值與既有 model／effort／sandbox 不變；定位 internal/codex/runner.go。以 TestBuildArgsDeveloperInstructions 的精確 argv／編碼案例驗證複雜文字、thread ID 與空值，執行 go test ./internal/codex。

## 3. Antigravity 局部 ACP 路徑

- [x] 3.1 [after: 1.1] 建立 provider 內部有限等待的 ACP 傳輸，完成啟動 Binary()、OnStart、JSON-RPC 配對、stderr 消費、取消／EOF 時釋放待處理請求與程序樹收尾，不重構其他 runner；對應「Antigravity ACP 只服務有指令的 session」與 Antigravity ACP preserves lifecycle and event semantics 的傳輸部分。定位 internal/antigravity/acp.go 及對應 Go 測試；用 fake agy 子進程的 TestACPRequestLifecycle 驗證 response/error/EOF/timeout/context cancel，不需真實帳號，執行 go test ./internal/antigravity。
- [x] 3.2 [after: 3.1] 完成 Antigravity uses direct ACP instructions without an agent file：檢查 append／load 能力，session/new 與 session/load 傳相同 metadata、保留 sessionId，session/prompt 只帶 user 文字；依宣告套用原有 model／effort／permission 語意，不支援則明確失敗。以 TestACPInstructionMetadata、TestACPCapabilityFailure、TestACPRequestedOptions 在 fake server 斷言原文、new/load、空 user prompt 邊界與缺能力時不送 prompt；執行 go test ./internal/antigravity。
- [x] 3.3 [after: 3.2] 將 Antigravity 非空指令導向 ACP，空值維持既有 stream-json／print 選路；非空時不能透過 print 後備丟掉指令。完成 Antigravity ACP preserves lifecycle and event semantics 的回放抑制、文字／thinking/activity／工具事件、權限回呼及成功完成唯一性；無 callback 拒絕授權，未知 blocking request 明確取消／報錯。以 TestInstructionTransportSelection、TestACPReplayAndTerminalEvents、TestACPPermissionRequest、TestACPCancellation 的 fake 子進程整合案例及既有 runner 測試驗證，執行 go test ./internal/antigravity。

## 4. 每次執行與接手沿用設定

- [x] 4.1 [after: 1.2, 2.1, 2.2, 3.3] 完成 Instructions are fixed and isolated per session 與 Copy and handoff preserve configuration without forwarding it as a message 的服務端契約：runAgent 每次從 session 讀入共用欄位，queue／retry／重連／clear CLI ID 後不遺失；shell 不注入、回 agent 保留；同 runner 自動與手動 handoff 資料帶原值。Antigravity ACP 的權限接既有 callback，不因啟用指令放寬權限。定位 internal/ws/handler.go、internal/ws/handoff.go；以 TestSessionInstructionRunLifecycle、TestSessionInstructionIsolation、TestHandoffPreservesInstruction 及既有 queue／permission 測試驗證；執行 go test ./internal/ws。

## 5. 建立畫面與資料可見範圍

- [x] 5.1 [after: 1.2, 4.1] 完成「建立表單與設定沿用」及 Supported creation surfaces offer a separate instruction field：NewSessionComposer 名稱與首則訊息之間顯示有 label/help 的多行欄位，ForwardModal 新建區同規則並提供 Antigravity；useNewSessionForm 保留支援 runner 間的 draft、切不支援者清空並省略、reset 清空、複製／手動接手回填；直接轉發到既有 session 不改指令。定位 internal/static/js/session/NewSessionComposer.js、internal/static/js/hooks/useNewSessionForm.js、internal/static/js/chat/ForwardModal.js、internal/static/js/chat/ChatView.js。新增 scripts/session-instructions.test.cjs 驗證 state／creation payload／不混入訊息／複製與清除；以真實瀏覽器確認鍵盤 Enter 只換行、label/help 可讀及三種 runner 的建立流程，執行 node scripts/session-instructions.test.cjs 與 go test ./internal/static。
- [x] 5.2 [after: 1.2, 2.1, 2.2, 3.3, 4.1] 完成 Configuration text does not leak through transport logging or share metadata：Claude／Codex argv 日誌不顯示指令內容，ACP 不記錄原始 instruction payload；驗證錯誤不回顯內容，guestSessionInfo 與歷史不因 Session 新欄位而暴露指令或加入 synthetic user message。定位三種 runner 的記錄點、internal/api/share.go 與對應 share／guest 測試；以 TestInstructionTransportLogsRedacted、TestShareDoesNotExposeInstruction 與訊息歷史斷言驗證，不變更訪客既有權限；執行 go test ./internal/claude ./internal/codex ./internal/antigravity ./internal/api ./internal/server。

## 6. 驗收與使用說明

- [x] 6.1 [after: 5.1, 5.2] 更新 docs/spec/claude-code-cli.md、docs/spec/codex-cli.md、docs/spec/antigravity-cli.md，記錄三種原生入口、建立後固定、空白沿用舊路徑及缺 ACP 能力的錯誤；按 spec 場景對照測試。執行相關前端檢查、go test ./internal/... 與 go test -tags production ./internal/...；用隔離 work_dir 補跑 Claude／Codex 新建及同指令續接、agy ACP new/load 的短 marker POC，marker 僅置於 instruction，保存版本與結果；若外部認證／配額阻擋須列出未驗證項，不宣稱通過。完成後使用 $spectra-verify session-custom-instructions 檢查實作符合規格，並執行 spectra validate session-custom-instructions，確認規格、設計、測試與文件一致。
