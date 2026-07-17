# Kiro 回覆可讀性與 ACP 調查結論

> 狀態：調查完成；問題 A（工具敘述混入正文）已用確定性 parser 修在 `internal/kiro/output.go`  
> 日期：2026-07-14  
> 實測環境：`kiro-cli-chat 2.12.1`、SQLite `claude-miniapp.db`  
> 關聯：`docs/spec/kiro-cli.md`、`internal/kiro/`、`poc/kiro-cli/`

---

## 1. 問題現象

Kiro session 在聊天室渲染後明顯比 Claude / Cursor / Codex 難讀：

1. 工具執行敘述（`Reading file:`、`✓ Successfully...`、`Searching for:` 等）混進最終回覆正文。
2. 程式碼幾乎沒有 Markdown code fence（`` ```go ``），只有單獨一行語言標籤（如 `go`）再接著貼原始碼。

前端 Markdown 渲染器看不到 fence，程式碼會被當成一般段落，排版混亂。

---

## 2. 資料來源與對照

從 DB `messages` 表抽取實際回覆（`role` 一律為 `claude`，需用 `sessions.agent_type` 區分）：

| 類型 | 樣本 session | 觀察 |
|---|---|---|
| kiro | `37ba1259-...`、`a5bac0b4-...` | 抽樣回覆 **0** 則含 `` ``` `` |
| claude | `c3b055e4-...` | 31 則回覆中約 **30** 則含 `` ``` `` |

樣本檔（POC）：

- `poc/kiro-cli/samples_kiro_msg1.raw.txt` / `.after.md`
- `poc/kiro-cli/samples_kiro_msg2.raw.txt` / `.after.md`
- 過濾／補 fence 實驗腳本：`poc/kiro-cli/markdown_experiment.py`

Kiro 典型「偽 fence」片段：

```text
A. 以 strategy/dispatch table 取代 if 堆疊
go
type transferHandlerFunc func(ctx context.Context, ...) error
```

---

## 3. 根因（兩個獨立問題）

### 3.1 問題 A：工具敘述混入正文（分類問題）

`internal/kiro/output.go` 的 `kiroStreamState` 以「是否見過以 `> ` 開頭的行」作為 `responseStarted` 開關：

- 首個 `> ` **之前**：累積為 `EventThinking`（覆寫式，不寫 DB）——正確。
- 首個 `> ` **之後**：一律當回覆延續 → `EventDelta` → 寫入 DB。

但 Kiro 實際輸出是 **答案 prose 與工具敘述交錯**，不是「工具全部跑完才開始最終答案」。  
`responseStarted` 一旦為 true 就永久成立，後續的 `Reading file:` / `✓ Successfully` 等全部被當成正文。

對照：Claude 走 `stream-json`，文字與 tool_use 本就是不同事件，不會混進最終文字。

### 3.2 問題 B：沒有標準 code fence（格式問題）

`kiro-cli chat --no-interactive` 的 stdout 是 **TTY／終端友善** 輸出，不是給程式消費的結構化 Markdown。  
實測與 help 皆顯示：此模式**沒有**「強制輸出標準 Markdown fence」的旗標。

`--format` 僅用於 `--list-models` / `--list-sessions`，**不**影響 chat 回覆本體。

---

## 4. 方案比較

兩個問題性質不同，不應共用同一套解法。

### 4.1 問題 A（工具敘述）

| 方案 | 評價 |
|---|---|
| Regex / prefix 解析固定字串模板 | **正確做法**。格式是 CLI 固定 signature，可枚舉、可測、確定性。 |
| 小模型分類 | 不適合：延遲、成本、非確定性，且答案本就唯一已知。 |

### 4.2 問題 B（補 fence）

| 方案 | 優點 | 缺點 |
|---|---|---|
| 字元統計 heuristic（中文字數、標點猜邊界） | 快、免費、確定性 | 天花板低，中英夾雜註解易誤判 |
| 小模型整段重寫 | 語意邊界較準 | 延遲／成本；生成式可能改寫原文；需再驗證是否走樣 |
| 依語言標籤用真 tokenizer／lexer 驗證邊界 | 比字元 heuristic 準，仍確定性、毫秒級 | 需維護多語言 tokenizer；仍是下游補救 |
| **換 ACP 入口（見下節）** | 協議層分開文字／工具；大機率保留模型原始 Markdown | 整合改動較大 |

實驗腳本（heuristic）可改善可讀性，但 fence 收尾偶有誤判——證明純字元 heuristic 有天花板。

### 4.3 結論優先序

1. **治本**：改走 `kiro-cli acp`（結構化協議）。
2. 若短期不換協定：問題 A 用確定性 marker 過濾；問題 B 優先考慮語言 tokenizer，不上小模型。

---

## 5. ACP 調查結論（方案 4）

### 5.1 能力確認

本機 `kiro-cli acp --help` 可用（版本 2.12.1）。  
通訊：JSON-RPC 2.0 over **stdin/stdout**（與現有 spawn 子進程架構相容）。

另有 `kiro-cli serve`（WebSocket ACP server，預設 port 8082），對本專案非必要；stdio ACP 即足夠。

官方文件：<https://kiro.dev/docs/cli/acp/>

### 5.2 核心方法與 session update

| Method / Update | 說明 |
|---|---|
| `initialize` | 握手與 capabilities |
| `session/new` | 建立 session，**直接回傳 `sessionId`** |
| `session/load` | 載入既有 session |
| `session/prompt` | 送 prompt |
| `session/cancel` | 取消 |
| `AgentMessageChunk` | 文字串流（最終答案） |
| `ToolCall` / `ToolCallUpdate` | 工具呼叫（與文字分離） |
| `TurnEnd` | 回合結束 |

### 5.3 對本專案的直接效益

| 現況痛點 | ACP 效果 |
|---|---|
| `--list-sessions` snapshot diff 猜 session id | `session/new` 直接給 id；可刪除／大幅簡化 `session.go` hack |
| 工具敘述混進 `content` | `ToolCall` 與 `AgentMessageChunk` 協議層分離 |
| 無 code fence | 推測 ACP 傳模型原始輸出（非 TTY 簡化）；**尚待登入環境完整 prompt round-trip 驗證** |
| 僅靠 `> ` 分 thinking／回覆 | 不再需要這套 stdout 行分類 |

POC 腳本：`poc/kiro-cli/acp_probe.js`  
實測：`initialize`、`session/new` 成功；本機 Cursor 環境 `kiro-cli whoami` 為未登入，完整 `session/prompt` 串流尚未在此環境驗證。

### 5.4 多工作目錄與常駐進程

**結論：`cwd` 綁在 session，不綁在 process。**

實測：同一 `kiro-cli acp` 進程連續兩次 `session/new`，分別帶不同 `cwd`，皆成功拿到不同 `sessionId`。  
Notification 皆帶 `sessionId`，可在同一條 stdio 上多 session 分流。

因此單一常駐進程可服務多個不同 `work_dir` 的 Kiro session，**不必**「一個目錄一個進程」。

### 5.5 兩種整合生命週期取捨

| 模式 | 做法 | 優點 | 缺點 |
|---|---|---|---|
| Per-message spawn | 每則訊息 spawn → initialize → session/new\|load → prompt → TurnEnd → kill | 與 `CLAUDE.md`「每訊息 spawn、用後即棄」一致 | 每次握手開銷；失去常駐效率 |
| 常駐單進程多工 | Server 生命週期綁一個（或少數）`kiro-cli acp`；依 `sessionId` 分派 | 延遲低；天然支援多 cwd | 需 process manager；重啟後 `session/load`；與 Claude runner 生命週期不一致 |

---

## 6. 現有介面是否需擴充

### 6.1 `agent.Runner`：MVP 不需改簽名

| ACP | 現有介面 |
|---|---|
| `sessionId` | `EventSessionInit`、`RunOptions.SessionID` |
| `AgentMessageChunk` | `EventDelta`、`EventStreamStart` |
| `ToolCall` / `ToolCallUpdate` | `EventToolStarted` / `EventToolCompleted` + `ToolCall` |
| `TurnEnd` | `EventDone` |
| 錯誤 | `EventError` |
| `cwd` | `RunOptions.WorkDir` |

`Run(ctx, opts, cb)` 語意仍可對應「跑一回合再返回」（底層用 ACP 即可）。

### 6.2 真正缺的是下游接線

`EventToolStarted` / `EventToolCompleted` 已在 `internal/agent/runner.go` 定義（antigravity 有發射），但：

- `internal/ws/handler.go` **未**處理這兩個 event
- 前端目前吃 `thinking` / `activity` / `delta`，**沒有**對應的 tool WS 訊息與 UI

ACP 分開工具與文字後，若不接線，結構化工具事件仍會落地失敗。

### 6.3 何時才要擴架構介面

僅在選擇 **常駐多工** 時：需在 `Runner` **之上**加 process manager（非改 `Run` 簽名）。  
`session/set_model`、`session/set_mode`、client fs callback、認證等可第二階段再擴。

---

## 7. 建議後續步驟（尚未實作）

1. **在已登入 Kiro 的環境**跑通 `acp_probe.js`（或等效 Go POC）：確認 `AgentMessageChunk` 是否含標準 fence，且 `ToolCall` 不進文字流。
2. 決定生命週期：**per-message**（架構一致）或 **常駐多工**（效率／多 cwd 友善）。
3. 實作 `internal/kiro` ACP runner，映射至既有 `agent.Event`。
4. 接 `EventToolStarted` / `EventToolCompleted` → WS → 前端（可先簡易 activity，再做工具卡片）。
5. 確認穩定後，更新 `docs/spec/kiro-cli.md`，並將本文件歸檔至 `docs/plan/done/`。

---

## 8. 明確不做／暫緩

- 不上小模型做全文 Markdown 重寫（失真與成本不划算）。
- 不以 heuristic 當長期治本方案（可當過渡或 ACP 驗證前的備援）。
- 不強制所有 agent 改常駐進程；Claude 仍維持現有 spawn 模式。
