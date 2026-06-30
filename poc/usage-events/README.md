# POC：Claude / Cursor / Kiro 額度與用量事件表達

## 目的

比對三個 provider 在 **成功回合** 與 **額度／限流失敗** 時，用量資訊從哪裡、以什麼格式出現；供後續統一 `EventDone.Usage` 或 WS `usage` 訊息設計參考。

## 執行

```powershell
# Live 擷取三 provider usage + 輸出正規化 JSON
powershell -ExecutionPolicy Bypass -File poc/usage-events/probe_usage.ps1

# Live + Go parser 測試
powershell -ExecutionPolicy Bypass -File poc/usage-events/run_all_poc.ps1
```

輸出：
- `poc/usage-events/samples/usage-report.json` — 三 provider 正規化用量
- `poc/usage-events/samples/claude-result.ndjson` — Claude 原始 result 行
- `poc/usage-events/samples/cursor-result.ndjson` — Cursor 原始 result 行
- `poc/usage-events/samples/kiro-stderr.txt` — Kiro 原始 stderr

Go 正規化 parser：`internal/usage/usage.go`

```bash
go test ./internal/usage/... -v
```

### 正規化輸出範例（2026-06-30 實測）

```json
{
  "providers": {
    "claude": {
      "provider": "claude",
      "ok": true,
      "cost_usd": 0.0073953,
      "input_tokens": 3,
      "output_tokens": 4,
      "cache_read_tokens": 24421,
      "duration_ms": 2280
    },
    "cursor": {
      "provider": "cursor",
      "ok": true,
      "input_tokens": 31076,
      "output_tokens": 173,
      "cache_read_tokens": 448,
      "duration_ms": 9230
    },
    "kiro": {
      "provider": "kiro",
      "ok": true,
      "credits": 0.05,
      "duration_text": "2s"
    }
  }
}
```

---

## 成功回合：用量出現位置（2026-06-30 實測）

| Provider | 通道 | 格式 | 主要欄位 | 我們 runner 現況 |
|---|---|---|---|---|
| **Claude Code** | stdout 最後一行 NDJSON `type=result` | JSON | `total_cost_usd`, `usage.input_tokens`, `usage.output_tokens`, `usage.cache_*`, `modelUsage` | **未解析** |
| **Cursor Agent** | stdout 最後一行 NDJSON `type=result` | JSON（camelCase） | `usage.inputTokens`, `usage.outputTokens`, `usage.cacheReadTokens`, `usage.cacheWriteTokens`, `duration_ms` | **未解析**（`events.go` 無 Usage 欄位） |
| **Kiro CLI** | **stderr** 尾端純文字 | ANSI + 文字 | `Credits: 0.05 ⋅ Time: 2s` | **僅 log stderr**，不轉發前端 |

### Claude 實測 sample（精簡）

```json
{
  "type": "result",
  "subtype": "success",
  "total_cost_usd": 0.146595,
  "usage": {
    "input_tokens": 3,
    "output_tokens": 4,
    "cache_read_input_tokens": 0,
    "cache_creation_input_tokens": 24421
  },
  "api_error_status": null,
  "session_id": "..."
}
```

- **美元成本**：有 `total_cost_usd`（含 cache 建立成本）
- **Token 明細**：snake_case，欄位最完整
- **額外**：`modelUsage`、`iterations` 可按模型拆分

### Cursor 實測 sample（精簡）

```json
{
  "type": "result",
  "subtype": "success",
  "duration_ms": 11142,
  "usage": {
    "inputTokens": 25728,
    "outputTokens": 55,
    "cacheReadTokens": 5792,
    "cacheWriteTokens": 0
  },
  "session_id": "..."
}
```

- **美元成本**：**無**（僅 token）
- **命名**：camelCase（官方 output-format 文件未列 `usage`，但 live 有）
- **init 事件**：`apiKeySource: "login"|"env"|"flag"` 表示認證來源，非用量

### Kiro 實測 sample

**stderr（剝 ANSI 後）：**

```
All tools are now trusted (!). ...
Credits: 0.05 ⋅ Time: 2s
```

- **計費單位**：Credits（非 token、非 USD）
- **時間**：同一行 `Time: Ns`
- **stdout**：只有 `> ` 回覆行，**不含** credits
- **互動模式**：另有 `/usage` 指令查帳戶總額度（headless 無法用）

---

## 額度／限流失敗：表達方式

| Provider | 典型通道 | 典型訊息／欄位 | 結構化？ |
|---|---|---|---|
| **Claude** | `result` 或 stderr | `is_error: true`, `api_error_status` 非 null；或 API rate limit 文字 | 部分（result JSON） |
| **Cursor** | stderr；可能無 terminal result | `Authentication required...`；rate limit 多為 stderr 非 JSON | 否 |
| **Kiro** | stdout（thinking 區）或 stderr | `⚠️ Kiro rate limit reached: Request quota exceeded...`；`Too many requests...`；`60-minute credit limit exceeded` | 否（純文字） |

### Kiro 限流特徵（社群回報）

- 月額 credits 未用完仍可能觸發 **短窗口 throttle**
- 錯誤常混在 stdout 工具輸出區（我們目前映射為 `EventThinking`）
- 失敗回合 stderr 仍可能出現 `Credits: X ⋅ Time: Ys`（有消耗但無有效回覆）

### Claude 限流特徵

- stream-json 理想路徑：`result` + `is_error` / `api_error_status`
- 也可能 process 非 0 exit + stderr 文字

### Cursor 限流特徵

- headless 失敗常 **無** terminal `result` 行
- OAuth：`agent status` 顯示 logged in，但 `-p` 子進程仍可能 `Authentication required`（與用量無關但常混淆）

---

## 與本專案現況的差距

| 層級 | 現況 |
|---|---|
| `agent.Event` | 無 `Usage` / `CostUSD` 欄位 |
| Claude / Cursor runner | 收到 `result` 只取 `session_id`、`ResultText`、`permission_denials` |
| Kiro runner | stderr 整段寫 log，不解析 `Credits:` |
| WS → 前端 | `plan.md` 曾規劃 `{ type: "result", cost_usd }`，**未實作** |
| 前端 UI | 無用量／額度顯示 |

---

## 建議正規化 schema（整合時）

```json
{
  "provider": "claude|cursor|kiro",
  "cost_usd": 0.146595,
  "credits": 0.05,
  "input_tokens": 25728,
  "output_tokens": 55,
  "cache_read_tokens": 5792,
  "cache_write_tokens": 0,
  "duration_ms": 11142,
  "raw": {}
}
```

映射規則：

- **Claude**：`cost_usd ← total_cost_usd`，token 欄位 snake_case 直填
- **Cursor**：token 欄位 camelCase → snake；`cost_usd` 留空
- **Kiro**：`credits ← regex Credits:`；token 留空；限流錯誤用 stderr+stdout 關鍵字分類為 `EventError` 子型 `quota_exceeded`

---

## 參考

- `docs/spec/kiro-cli.md` §3.2 stderr Credits
- `docs/spec/plan.md` WS `cost_usd`（規劃）
- Claude live：`claude -p ... --output-format stream-json --verbose`
- Cursor live：`agent -p ... --output-format stream-json --trust`
