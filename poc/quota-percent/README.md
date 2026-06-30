# POC：帳戶使用百分比（Claude / Cursor / Kiro）

## 結論（2026-06-30 實測）

**三個 provider 都可在 headless 環境取得帳戶使用 %**，不必用 PTY 互動 TUI。

| Provider | 方法 | 輸出 |
|---|---|---|
| **Claude** | `claude -p "/usage"` | 純文字：`Current session: 15% used`（UI 顯示為 `5h 15%`） |
| **Claude** | OAuth API `GET /api/oauth/usage` | JSON：`five_hour.utilization`, `seven_day.utilization` |
| **Cursor** | 讀 `state.vscdb` token → `GetCurrentPeriodUsage` | JSON：`autoPercentUsed`, `apiPercentUsed`, `totalPercentUsed` |
| **Kiro** | `kiro-cli chat "/usage" --no-interactive` | 純文字：`Credits (4.52 of 50)` + `9%` |

## 執行

```powershell
powershell -ExecutionPolicy Bypass -File poc/quota-percent/probe_quota.ps1
powershell -ExecutionPolicy Bypass -File poc/quota-percent/run_all_poc.ps1
```

```bash
go test ./internal/usage/... -v -run Quota
```

## 輸出檔

| 檔案 | 內容 |
|---|---|
| `samples/quota-report.json` | 三 provider 正規化報告 |
| `samples/claude-usage.txt` | Claude `/usage` 原文 |
| `samples/claude-oauth-usage.json` | Claude OAuth API（無 token） |
| `samples/cursor-period-usage.json` | Cursor dashboard API |
| `samples/kiro-usage.txt` | Kiro `/usage` stdout |

## Go parser

`internal/usage/quota.go`：

- `FromClaudeUsageText()`
- `FromClaudeOAuthUsageJSON()`
- `FromCursorPeriodUsageJSON()`
- `FromKiroUsageText()`

## 注意事項

### Claude
- `-p "/usage"` 與 OAuth API 數字可能差 1%（四捨五入）
- OAuth token 來自 `~/.claude/.credentials.json`，**勿 commit**
- `/usage` 文字輸出**不一定**含 `Current session: X% used`（有時僅顯示 subagent 統計）；此時 fallback OAuth API
- OAuth API 請求需加 `User-Agent: claude-code/<version>`，否則可能落入嚴格 rate limit bucket 而 429
- 建議 OAuth poll 間隔 >= 3 分鐘（用量資料變化慢）

### Cursor
- 依賴 **Cursor IDE 本機登入**（`cursorAuth/accessToken` in `state.vscdb`）
- API 為**非官方** dashboard endpoint，可能變更
- Pro 帳戶常有多個 %：`apiPercentUsed` vs `totalPercentUsed`

### Kiro
- `kiro-cli profile` 非 Pro 會失敗；`/usage` headless 可用
- 免費方案顯示 `Credits (used of limit)` + 進度條 `%`

## 與單回合 usage POC 的差異

| | `poc/usage-events/` | `poc/quota-percent/` |
|---|---|---|
| 語意 | 這一則訊息花了多少 | 帳戶方案還剩多少 % |
| Claude | `result.total_cost_usd` | `session 15%`, `week 9%` |
| Cursor | `usage.inputTokens` | `totalPercentUsed 30%` |
| Kiro | stderr `Credits: 0.02` | stdout `4.52/50 = 9%` |

## 整合建議

已整合至主應用（2026-06-30）：

- 後端：`internal/quota/`（Fetcher + global cache + `DisplayText`）
- HTTP：`GET /quota`、`GET /quota/:provider`、`POST /quota/:provider/refresh`
- WS：`sync.quota`、`quota_update`（Run 結束後刷新）、`refresh_quota`（手動，60s cooldown）
- 前端：三 theme Session header 顯示 `display_text` + ↻

Chat runner 維持 headless 不變；quota 為獨立 poll + cache。
