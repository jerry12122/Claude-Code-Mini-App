# session-custom-instructions 驗證紀錄

日期：2026-11-12。環境：Windows amd64、Go 1.26.11、Node 22.17.0、Chrome 157.0.8103.39。

## 結果

10 個需求、32 個場景與 1 個邊界範例皆有測試對照；沒有未覆蓋場景，也沒有排除的場景。實作符合建立後固定、限定三個 runner、原生傳遞、空白相容的設計。無 Critical／Warning。此紀錄是本次執行證據，不是永久有效的驗證旗標；後續改動需重驗受影響部分。

| 需求 | 實作位置 | 測試與範例 | 執行證據 |
| --- | --- | --- | --- |
| Supported creation surfaces offer a separate instruction field | `useNewSessionForm.js`、`NewSessionComposer.js:126`、`ForwardModal.js:286` | Node 表單／handler 測試；Chrome 六個流程 | passed-current |
| Creation validates and atomically persists instructions | `internal/api/session.go:148`、`internal/db/session.go:70` | API validation／storage failure、DB create failure；下列 byte 表格 | passed-current |
| Instructions are fixed and isolated per session | `internal/api/session.go:272`、`internal/ws/handler.go:550` | PATCH atomic、DB reopen、WS lifecycle／isolation／shell | passed-current |
| Existing sessions and empty instructions retain their execution path | `internal/db/db.go`、各 runner 非空判斷 | migration、Claude／Codex exact argv、AGY transport selection | passed-current |
| Claude appends the saved instruction natively | `internal/claude/runner.go:78` | `TestBuildClaudeArgsSystemInstruction`、真實 CLI new／resume | passed-current |
| Codex receives saved developer instructions | `internal/codex/runner.go:217` | `TestBuildArgsDeveloperInstructions`、真實 CLI new／resume | passed-current |
| Antigravity uses direct ACP instructions without an agent file | `internal/antigravity/acp_session.go:51` | metadata／capability／requested options、真實 new／load | passed-current |
| Antigravity ACP preserves lifecycle and event semantics | `internal/antigravity/acp.go`、`acp_runner.go:26` | fake subprocess lifecycle／replay／terminal／permission／cancel | passed-current |
| Copy and handoff preserve configuration without forwarding it as a message | `internal/ws/handoff.go:55`、表單 hook、`ChatView.js` | handoff／Node copy／forward isolation；Chrome forward new | passed-current |
| Configuration text does not leak through transport logging or share metadata | Claude／Codex log helpers、ACP scrubbed errors、WS guest send filter | redaction、validation no-echo、live／snapshot guest HTTP、guest handoff | passed-current |

## 逐場景對照

以下每列分類均為「有測試」。測試名稱為 Go test；`Node` 表示 `scripts/session-instructions.test.cjs`。Chrome 為本機驗收 fixture，使用實際 React 元件與 native keyboard CDP；周邊選單元件與 API／WS 發送為替身。真實後端的建立、資料與授權另外由 Go 測試覆蓋。

| Spec Scenario | 測試 |
| --- | --- |
| Create an instructed conversation | Node actual submit handler；Chrome new × 3 |
| Enter multiline instructions with a keyboard | Chrome CDP Enter 插入換行、POST 次數為 0、label／help 關聯 |
| Switch supported and unsupported runners | Node supported draft 保留、unsupported 清空／省略 |
| Reset or copy creation settings | Node reset／applyFromSession／prefill／editable copy |
| Persist a supported value | `TestCreateSessionInstructionValidation/claude` 的規格原文 |
| Enforce the byte boundary without truncation | `TestCreateSessionInstructionValidation` ASCII／Unicode 邊界 |
| Reject unsupported targets and malformed values | 同測試的 runner／shell／Telegram disguise／JSON 型別／NUL |
| Reject conflicting Claude custom flags | 同測試四個 flags 的分離／等號／空白相容案例 |
| Roll back failed instruction persistence | `TestCreateSessionInstructionStorageFailure`、`TestSessionInstructionCreateFailure` |
| Read after restart | `TestSessionInstructionRoundTrip` 重開 DB；`TestSessionInstructionRunLifecycle` 每輪取得已存值；真實跨程序 runner POC |
| Reject changes atomically | `TestPatchSessionInstructionImmutable` 的 name／CLI args 加 string／null／empty／number |
| Queue and reset retain instructions | `TestSessionInstructionRunLifecycle` 失敗後排隊重連續跑、resume、reset CLI ID |
| Sessions sharing a directory stay independent | `TestSessionInstructionIsolation` A／B 值與工作目錄保持空白 |
| Shell mode does not consume instructions | `TestSessionInstructionShellDoesNotConsume` |
| Upgrade an existing database | `TestSessionInstructionMigration` |
| Create without instructions | API 空白相容；`TestBuildClaudeArgs`、`TestBuildArgsDeveloperInstructions/empty`、`TestInstructionTransportSelection/empty` |
| New and resumed Claude invocation | `TestBuildClaudeArgsSystemInstruction` 精確 argv、snapshot／MCP／model／effort 不變 |
| Encode complex Codex instructions | `TestBuildArgsDeveloperInstructions/complex` 精確字串與解碼；包含 DEL 跳脫 |
| Resume without changing the instruction | 同測試 thread ID／prompt／原值；真實同值 resume |
| Create and load through ACP | `TestACPInstructionMetadata` new／load／empty prompt／absolute cwd |
| Missing instruction or load capability | `TestACPCapabilityFailure` false／型別錯誤／no load |
| Unsupported requested model or permission setting | `TestACPRequestedOptions` 接受與拒絕 model／effort／mode、bypass 明確旗標 |
| Permission request requires the existing decision path | `TestACPPermissionRequest` callback／nil／invalid；`TestSessionInstructionAntigravityPermission` 重連 allow／deny／interrupt |
| Resume replay does not duplicate history | `TestACPReplayAndTerminalEvents/session-events` 只有新回答、單一完成與 session ID |
| Stream tools and activity | 同測試 thought 快照、三個 activity、工具 ID／輸出／成功狀態 |
| Transport failure or unsupported blocking request | `TestACPRequestLifecycle` error／EOF／timeout／malformed；`TestACPReplayAndTerminalEvents` RPC／EOF／unknown／nonzero；capability auth |
| Owner cancels the run | `TestACPCancellation` cancel notification、無成功完成；lifecycle pending 清空與程序回收 |
| Copy settings into a new session | Node copy／manual handoff handler；Chrome forward new × 3 |
| Automatic handoff carries the role | `TestHandoffPreservesInstruction` |
| Forward to an existing conversation | Node actual forward handler 不 POST／PATCH、不改目標、不混入原指令 |
| Inspect generated transport logs | Claude `TestRunSystemInstructionRedacted`；Codex／AGY `TestInstructionTransportLogsRedacted`；validation no-echo |
| Inspect visitor session data | `TestShareDoesNotExposeInstruction` live／snapshot／history／owner read；`TestHandoffInstructionOwnerOnly` |

`Instruction size policy` 範例的五列均覆蓋：正規化後空字串、16384／16385 個 `a`、5461 個 `中` 加 `a`／`ab`。接受值精確比較原文，拒絕值檢查 HTTP 400、error 欄位與零 session。

## 執行命令與結果

本次執行皆成功（passed-current）：

```text
go test ./internal/...
go test -tags production ./internal/...
go test -race ./internal/antigravity
node --test scripts/session-instructions.test.cjs scripts/context-menu.test.cjs scripts/console-usability.test.cjs scripts/ux-reliability.test.cjs scripts/tab-groups.test.cjs
spectra validate session-custom-instructions
```

逐項核對時補強 AGY activity 斷言，之後重跑 `go test ./internal/antigravity` 與 production 同套件均通過；其他套件來源／測試未變，沿用上述完整執行證據。新行為均先保留預期失敗再實作；既有保護另用 mutation 確認：移除 DB 指令寫入、破壞 argv redaction copy、洩漏 share metadata／ACP log／validation body、移除 thinking activity，各對應測試確實失敗，再還原至通過狀態。未留下故意破壞的修改。

Chrome 本機命令：`go run ./poc/session-instructions/browser-fixture.go`、`node poc/session-instructions/browser-check.cjs`。六個流程皆確認 label／help、native Enter 換行而不送出、獨立 creation payload 與純 user message，另確認 Cursor 不顯示欄位。這些 fixture 與結果在 gitignored `poc/`，不是安裝或 CI 的必要檔案。

## 真實 runner POC

正式 Go runner、隔離 TEMP work_dir、本機既有訂閱登入；未建立自訂 agent／workspace 規則檔。User prompt 固定為 `Reply according to your role. Do not use tools.`；marker 只在 `RunOptions.SystemInstruction`。每個 provider 的兩次執行是不同 CLI 程序，第二次沿用同一 instruction 與 CLI session ID。

| Provider 版本 | Marker | 新建 | 續接 |
| --- | --- | --- | --- |
| Claude Code 2.1.322 | `POC_CLAUDE_1794462643854432500` | 完全相同 | 完全相同 |
| codex-cli 0.163.0 | `POC_CODEX_1794462659090425300` | 完全相同 | 完全相同 |
| agy 1.5.6 | `POC_ANTIGRAVITY_1794463324158245400` | 完全相同 | 完全相同 |

本機 JSON：`poc/session-instructions/results-native-runners.json`、`results-native-agy-runner.json`、`results-browser.json`。補充 read-only native ACP 設定 RPC 確認 `session/set_mode(plan)`、`session/set_config_option(thought_level=low)`、`session/set_model(gemini-3.8-flash-medium)` 均成功。沒有登入或配額阻擋。模型回覆僅為注入的補充證據，不保證模型永遠遵循或保密。

## 範圍限制

本次只歸屬明確列出的 feature 檔案。工作區原先有 Kiro 停用／唯讀、DB busy、MCP、上傳及文件等修改；重疊檔案的其他 hunks 保留，Spectra 的 `preexisting_dirty` 提醒視為近似歸屬，沒有把其他修改算成本功能。無發布、commit 或 archive。
