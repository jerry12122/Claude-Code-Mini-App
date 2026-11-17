# 功能計畫書：Codex Runner 全面改接 ACP

> 狀態：**暫緩**（2026-09-30 決議先修 headless CLI，見 `docs/plan/done/codex-cli-refresh.md`；僅在需要 Codex 中途授權時再評估）
> 建立日期：2026-09-30
> 關聯規格：`docs/spec/codex-cli.md`（現況，待改寫）、`internal/kiroacp/`（現有 ACP 實作範本）

---

## 0. 背景與現況問題

目前 `internal/codex/runner.go` 是 **subprocess + JSONL**：直接 `exec.CommandContext` 呼叫 `codex exec --json`，解析 stdout 的 `thread.started` / `item.*` / `turn.completed` 事件。不是 ACP。

現況已確認過時：

- `docs/spec/codex-cli.md` 實測版本 `codex-cli 0.142.5`（2026-07-06），本機現裝 `0.159.2`，中間跨了多次發版。
- `buildArgs()` 用的 `--yolo` 在目前 `codex exec --help` 清單中已不存在，已被 `--dangerously-bypass-approvals-and-sandbox` 取代（需先驗證 `--yolo` 是否仍為隱藏別名，或已經失效）。
- 目前無 approval/sandbox 分離控制、無中途授權互動（Claude 有 permission_denied 流程，Kiro ACP 有 `session/request_permission`，Codex runner 完全沒有）。

## 1. 目標

把 `agent_type = "codex"` 的執行路徑改為走 **ACP（Agent Client Protocol，JSON-RPC over stdio）**，架構對齊現有 `internal/kiroacp/`（同樣是 spawn 子進程 + JSON-RPC，而非直接解析 CLI 專屬 JSONL）。

非目標：不動 Claude / Cursor / Kiro（非 ACP 版）/ Antigravity 的既有 runner。

## 2. 關鍵架構決策：Codex CLI 本身不是 ACP agent

**這是本計畫最大的前提差異，必須先確認使用者接受**：

- Kiro ACP 的模式是 Go 直接 `exec.Command("kiro-cli", "acp", ...)`，`kiro-cli` 原生支援 ACP，是單一子進程、單一語言依賴（就是 kiro-cli 本身）。
- Codex CLI（`codex` 執行檔）**沒有原生 ACP 模式**。它有 `codex exec`（現況用的）和 Experimental 的 `codex app-server`（自家 JSON-RPC 協定，不是 ACP）。
- 官方把 Codex 橋接到 ACP 的方式是另一個獨立套件 **`@agentclientprotocol/codex-acp`**（npm，Node.js，`agentclientprotocol` 組織維護，420 stars，非 OpenAI 官方但是 ACP 官方組織維護）。它的角色：啟動 `codex app-server` → 把 ACP 請求翻譯成 Codex app-server 操作 → 把 Codex 事件映射回 ACP 給 client。

也就是說改接 ACP 後的進程拓樸會變成：

```
現況：   Go server → spawn `codex exec --json`（單一進程，Go 解析 JSONL）

改接後： Go server → spawn `codex-acp`（Node 子進程，JSON-RPC over stdio）
                          └→ codex-acp 內部再 spawn/管理 codex app-server
```

**新增依賴**：Node.js runtime（本機已裝 v22.17.0、npm 10.9.2，可用 `npx -y @agentclientprotocol/codex-acp` 免安裝直接跑，或 `npm install -g` 常駐）。若部署環境（README 提到的「server」）沒有 Node，這個功能會直接壞掉——**這點需要在 README「Requires」章節同步補充**，目前只列了 Go 1.25+ 與各 CLI。

**風險**：多一層轉譯（Node 進程）意味著多一層可能的故障點、多一個要更新的相依版本（`codex-acp` 自身版本 + 內建的 `@openai/codex` 依賴版本），日後 codex CLI 升級後，還要等 `codex-acp` 跟進更新才會同步支援新行為。

若不想接受這個新依賴，替代方案是：只把現有 `codex exec --json` 整合修好（更新 `--yolo`→新旗標、驗證新版事件格式），不做 ACP 遷移。**這個決策點需要你先確認**，以下計畫假設你接受引入 `codex-acp` 這個 Node 依賴。

## 3. 與現有 KiroACP 架構的對應關係

| 面向 | KiroACP（現有範本） | Codex ACP（新） |
|---|---|---|
| 子進程 | `kiro-cli acp` | `npx -y @agentclientprotocol/codex-acp`（或 global 安裝後的 `codex-acp`） |
| 語言/runtime 依賴 | 無額外（kiro-cli 本身） | Node.js（新增系統依賴） |
| JSON-RPC transport | stdin/stdout | stdin/stdout（相同） |
| session 建立 | `session/new` / `session/load` | 同為 ACP 標準方法，預期相同 |
| 中途授權 | `session/request_permission` | ACP 標準方法，`codex-acp` README 標注支援「permission request」事件 |
| 認證 | kiro-cli 既有登入態 | `CODEX_API_KEY` / `OPENAI_API_KEY` / ChatGPT 登入，透過環境變數傳給 `codex-acp` 子進程 |
| MCP servers | `buildACPMcpServers`（讀 kiro 的 mcp.json） | `codex-acp` 支援 client 提供 MCP servers（stdio + HTTP），需另外設計來源（沿用 codex 自己的 `~/.codex/config.toml` 內 mcp 設定，或不提供） |

## 4. Phase 拆分

### Phase 0 — 決策確認與 POC（阻塞後續全部工作）

- [ ] 與使用者確認接受 Node.js 子進程依賴（見第 2 節）
- [ ] 本機 POC：手動跑 `npx -y @agentclientprotocol/codex-acp`，用簡單 JSON-RPC（`initialize` → `session/new` → `session/prompt`）測試最小可行流程，記錄：
  - 啟動延遲（npx 每次都要 resolve 套件 vs global 安裝）
  - `initialize` 回應的 `protocolVersion`、`authMethods`
  - 認證：`CODEX_API_KEY` 環境變數是否即可免登入直接跑
  - `session/update` 事件的實際 payload 格式（agent_message_chunk / tool_call 是否與 kiroacp 格式一致，還是有差異欄位）
  - 錯誤情境：無認證、無網路、prompt 中途取消（ctx cancel）
- [ ] 決定安裝策略：`npx -y`（每次執行都可能觸發套件解析/下載，有延遲與離線風險）vs 部署時 `npm install -g @agentclientprotocol/codex-acp` 固定版本（建議後者，pin 版本號，避免 `npx -y` 未鎖定版本時默默拉新版）
- [ ] 產出 POC 記錄，更新/取代 `docs/spec/codex-cli.md`（或新增 `docs/spec/codex-acp.md`）

若 POC 發現 `codex-acp` 事件格式、認證流程與預期差異過大或不穩定（例如 Experimental 特性造成的斷流），**在此階段就要重新評估是否繼續整份計畫**，而不是硬做下去。

### Phase 1 — 新增 `internal/codexacp` 套件（不動舊 `internal/codex`）

參考 `internal/kiroacp/` 的檔案切分：

- [ ] `internal/codexacp/client.go`：JSON-RPC over stdio client（可考慮抽出 kiroacp 的 `rpcRequest`/`rpcResponse`/`client` 到共用套件如 `internal/acpclient`，避免兩份幾乎一樣的程式碼——**這是本計畫中值得做的重構點，因為兩套 ACP client 邏輯會高度重複**）
- [ ] `internal/codexacp/runner.go`：實作 `agent.Runner`，`Name()` 回傳新的 agent type（見第 5 節命名）
  - spawn `codex-acp`（或 global 安裝路徑，仿造 `internal/codex/bin.go` 的 `ResolveBin` 模式做 `ResolveCodexACPBin`）
  - `initialize` → `session/new`（新 session）或 `session/load`（resume，若 codex-acp 支援）→ `session/prompt`
  - `onUpdate` 映射 `session/update` 到 `agent.Event`（agent_message_chunk → EventDelta，tool_call/tool_call_update → EventActivity，比照 kiroacp 邏輯）
  - `onPermission` 映射 `session/request_permission` 到 `opts.RequestPermission`（比照 kiroacp，這是現有純 JSONL 版 Codex runner 完全沒有的能力，屬於能力提升）
- [ ] `internal/codexacp/models.go` 或沿用 `internal/model/resolve.go` 的既有 codex model 解析（需確認 ACP session/new 回應是否帶 model 資訊，比照 kiroacp 的 `modelSnapshot`）
- [ ] 認證環境變數傳遞：`CODEX_API_KEY` 沿用現有 `internal/codex/bin.go` 的 `HasAuthConfig()` 判斷邏輯

### Phase 2 — Agent type 命名與並存策略

需要決定：

- **選項 A**：直接把 `agent.TypeCodex = "codex"` 的 runner 實作換掉（`internal/codex` 整包移除或保留不註冊），既有 session 無痛切換到 ACP 版本。
- **選項 B**：新增 `agent.TypeCodexACP = "codexacp"`，與舊 `"codex"` 並存（比照 `TypeKiro` vs `TypeKiroACP` 的模式：`createDisabledAgentTypes` 讓舊版不可新建但既有 session 仍可跑）。

**建議選項 B**，理由：
1. 與現有 Kiro / KiroACP 並存的先例一致，risk 更低，可漸進切換。
2. ACP 版本依賴 Node.js，若目標環境沒裝 Node，舊 `"codex"` 仍可用，不會整個功能失效。
3. 使用者標題明確說「codex 全部改接 acp」，但實際遷移仍建議先並存驗證一段時間再考慮下架舊版（下架與否留給 Phase 4 決定，不在本計畫預設砍掉舊代碼）。

- [ ] `internal/agent/factory.go` 新增 `TypeCodexACP = "codexacp"`
- [ ] `internal/codexacp/runner.go` 的 `init()` 呼叫 `agent.Register(agent.TypeCodexACP, ...)`
- [ ] 沿用 `createDisabledAgentTypes` 機制，把舊 `TypeCodex` 標記為不可新建（若使用者確認要走向全面替換），文案比照 `TypeKiro: "請改用 Kiro ACP（kiroacp）建立新會話"`

### Phase 3 — 周邊整合點更新

逐一比對 kiroacp 在系統中被引用的所有位置，Codex ACP 需要同樣程度的接入：

- [ ] `internal/model/resolve.go`：新增 codexacp 的 model 解析路徑（若與純 `codex` 不同格式）
- [ ] `internal/quota/codex.go`：確認 quota fetcher 是否仍可共用（quota 走的是 `codex` CLI 本身的 `/status` 或 usage 資訊，理論上與走 ACP 與否無關，應可直接沿用，需驗證）
- [ ] `internal/static/js/ui/uiAtoms.js`、`NewSessionComposer.js`、`useNewSessionForm.js`：新建 session 表單加入 codexacp 選項（agent type 下拉、permission mode 選項是否適用）
- [ ] `internal/static/js/ui/uiAtoms.js` 的 `permModeOptionsFor(agentType)`：codexacp 是否需要獨立的 permission mode 選項（比照 kiroacp 有 default/plan/acceptEdits/bypassPermissions）
- [ ] MCP tools（`internal/mcp/tools.go`）与 `createSession` 的 agent_type 驗證邏輯
- [ ] `docs/spec/` 新增 `codex-acp.md`，記錄 Phase 0 POC 結論（實測版本、事件格式、已知差異）

### Phase 4 — 測試

- [ ] `internal/codexacp/runner_test.go`：比照 `internal/kiroacp/runner_test.go` 的測試案例（`TestBuildArgs`、`TestClientRequestPermission`、`TestParseSessionResult` 等），但改成 codex-acp 的協定細節
- [ ] `internal/codexacp/runner_integration_test.go`：比照 `internal/kiroacp/runner_integration_test.go`（`TestInteractivePermission_E2E`），需要本機已裝 `codex-acp` + 有效認證才能跑，用 build tag 或環境變數 skip
- [ ] 手動端對端驗證：透過 Telegram Mini App UI 建立 codexacp session，跑一輪含工具呼叫（例如要求它讀檔）的對話，確認事件流、權限請求、session resume 都正常

### Phase 5（可選，需另行確認）— 下架舊 `internal/codex`

僅在 Phase 0-4 都驗證穩定、且使用者確認要「全部改接」而非長期並存後才進行：

- [ ] 移除 `internal/codex/` 整包（runner.go / bin.go / events.go / models.go）
- [ ] `agent.TypeCodex` 改為 alias 到 `TypeCodexACP`（比照 `TypeGemini` → `TypeAntigravity` 的 `normalizeAgentType` 模式），避免舊 session 資料庫記錄的 `agent_type='codex'` 失效
- [ ] 更新 `docs/spec/codex-cli.md` 標記為棄用，指向新文件

## 5. 開放問題（需要使用者決策，不擅自假設）

1. **是否接受新增 Node.js 系統依賴？** 這是本計畫成立的前提，若不接受，應改做「修復現有 subprocess 整合」而非 ACP 遷移。
2. **並存（選項 B）還是直接取代（選項 A）？** 建議並存，但需你確認。
3. **`npx -y` 每次動態解析 vs 部署時 pin 版本 global 安裝？** 建議 pin 版本，但需要決定部署腳本/文件要不要新增這個安裝步驟（README 的 Quick Start 需同步更新）。
4. **是否要做 kiroacp/codexacp 共用 ACP client 的抽層重構？** 技術上乾淨，但屬於範圍外的額外重構，需你確認是否要做（Ponytail 原則：非必要不加抽象；但兩份幾乎一樣的 JSON-RPC client 程式碼重複本身也是一種技術債，值得提出讓你決定）。

## 6. 驗收標準（Phase 0-4 完成後）

- [ ] `codexacp` session 可建立新對話、可 resume、可中途授權工具呼叫
- [ ] 事件正確映射到現有 WS `serverMsg` 格式，前端無需大改即可顯示
- [ ] quota badge 仍正確顯示 codex 額度
- [ ] 單元測試 + 至少一次手動端對端驗證通過
- [ ] `docs/spec/codex-acp.md` 記錄實測版本號與已知差異
- [ ] README「Requires」章節註明 Node.js 依賴（若走 Phase 0 決策為接受新依賴）
