# POC：各 Runner Session Model 擷取與顯示

## 目的

比對各 provider 在 headless 模式下，**實際使用的 model** 從哪裡取得；供前端「像 QuotaBadge 一樣顯示目前 model」設計參考。

設計原則（與需求對齊）：

1. **以對話（session）為準**：優先從該次 CLI 串流的 init / result 事件讀取。
2. **拿不到再 fallback**：Session 設定的 `--model` → 全域 CLI 設定 / `--list-models` 預設。

## 執行

```powershell
# Live 擷取 + 輸出 model-report.json
powershell -ExecutionPolicy Bypass -File poc/model-display/probe_model.ps1

# Live + Go parser 測試
powershell -ExecutionPolicy Bypass -File poc/model-display/run_all_poc.ps1
```

輸出：

- `poc/model-display/samples/model-report.json`
- Go 正規化：`internal/model/resolve.go`

```bash
go test ./internal/model/... -v
```

---

## 步驟 1：原始輸出是否有 model？

| Runner | 原始輸出有 model？ | 位置 | 欄位 |
|--------|-------------------|------|------|
| **Claude** | ✅ 有 | stdout NDJSON | `system/subtype=init` → `model`；`result` → `modelUsage` keys |
| **Cursor** | ✅ 有 | stdout NDJSON | `system/subtype=init` → `model`（`result` **無** model） |
| **Antigravity/Gemini** | ✅ 有（stream-json） | stdout NDJSON | `init` → `model`；`result.stats.models` keys |
| **Codex** | ❌ 無（截至 2026-07） | JSONL | `thread.started` 僅 `thread_id`（[openai/codex#14736](https://github.com/openai/codex/issues/14736)） |
| **Kiro** | ❌ 無 | stdout/stderr | 僅回覆文字 + Credits；無 model 欄位 |

### Claude 實測 init（2026-07-10）

```json
{"type":"system","subtype":"init","session_id":"...","model":"claude-sonnet-5",...}
```

### Claude 實測 result.modelUsage（備援）

```json
{"type":"result","modelUsage":{"claude-sonnet-5":{"inputTokens":2,...}}}
```

### Cursor 實測 init（2026-07-10）

```json
{"type":"system","subtype":"init","model":"Composer 2.5 Fast","session_id":"..."}
```

### Codex 實測 sample（無 model）

```json
{"type":"thread.started","thread_id":"019f35d2-322c-7342-a6e0-1b26ce4904ed"}
```

---

## 步驟 2：官方文件 / issue fallback 路徑

| Runner | Stream 無 model 時的官方建議 |
|--------|------------------------------|
| **Claude** | `--model` 覆寫 session；未指定時用 `~/.claude/settings.json` 的 `model` |
| **Cursor** | `--model`；未指定時帳戶預設 `auto`（`agent models` 列表） |
| **Gemini/Antigravity** | `--model`；`init.model` 為別名解析後實際名（見 `docs/spec/gemini-cli.md`） |
| **Codex** | `-m/--model` 或 `~/.codex/config.toml` 的 `model`；**JSONL 尚不反映**（enhancement 進行中） |
| **Kiro** | `--model`；預設用 `kiro-cli chat --list-models` 中 `*` 標記的模型（目前為 `auto`） |

---

## 步驟 3：各 Runner 取得方式與 UI 顯示範例

前端預期採用與 `QuotaBadge` 相同模式：後端組 `display_text`，前端 `font-mono text-[10px]` 直接顯示。

| Runner | 取得方式（優先序） | 實測 model | `display_text` 範例 | `title` 提示（可選） |
|--------|-------------------|------------|---------------------|---------------------|
| **Claude** | `init.model` | `claude-sonnet-5` | `claude-sonnet-5` | 來自 stream init |
| **Claude** | `result.modelUsage`（init 缺失時） | `claude-sonnet-4-6` | `claude-sonnet-4-6` | 來自 result 用量拆分 |
| **Claude** | `settings.json` fallback | `claude-sonnet-5` | `claude-sonnet-5` | 來自 CLI 全域設定 |
| **Cursor** | `init.model` | `Composer 2.5 Fast` | `Composer 2.5 Fast` | 來自 stream init |
| **Cursor** | 帳戶預設 fallback | `auto` | `auto` | Cursor 帳戶預設 |
| **Kiro** | `--list-models` 預設 | `auto` | `auto` | Kiro 預設模型 |
| **Kiro** | Session `--model` | `claude-sonnet-4.6` | `claude-sonnet-4.6` | Session 指定 |
| **Codex** | `-m` flag | `codex-mini-latest` | `codex-mini-latest` | Session 指定 |
| **Codex** | `config.toml` fallback | `o4-mini` | `o4-mini` | Codex 全域設定 |
| **Antigravity** | `init.model`（fixture） | `gemini-3.1-pro` | `gemini-3.1-pro` | 來自 stream init |

> **注意**：同一 session 跨回合 model 可能變（例如 Claude `/model`、Cursor 切換、或 `auto` 路由）。POC 以**每回合 stream 回報**為準，不做跨回合快取（依需求暫不處理快取）。

---

## 與本專案現況的差距

| 層級 | 現況 |
|------|------|
| `agent.Event` | 無 `Model` 欄位 |
| Claude / Cursor runner | init 有 `model` 但未解析轉發 |
| WS → 前端 | 無 `model_update` 訊息 |
| 前端 UI | 僅新建/轉發表單有 Model 輸入，**聊天畫面無即時顯示** |
| DB `sessions` | model 可能藏在 `cli_extra_args` 的 `--model`（Claude），Cursor/Codex 尚未一致持久化 |

### 建議整合路徑（後續，非本 POC 範圍）

1. Runner 在 `EventSessionInit`（或首個含 model 的事件）帶 `Model` 欄位。
2. WS 廣播 `{ type: "model_update", model: "...", display_text: "..." }`（可與 `quota_update` 並列）。
3. 前端在 header 加 `ModelBadge`，樣式對齊 `QuotaBadge`。
4. Codex / Kiro 在 stream 無 model 時，啟動前解析 Session `--model` 或全域預設，作為初始顯示；每回合仍以 stream 覆寫（若有）。

---

## 參考

- `docs/spec/gemini-cli.md` § init.model
- `docs/spec/cursor-agent-cli.md` § system/init
- `docs/spec/codex-cli.md` + [openai/codex#14736](https://github.com/openai/codex/issues/14736)
- `docs/spec/kiro-cli.md`（headless 無 model 輸出）
- `poc/usage-events/`（用量 POC 平行結構）
