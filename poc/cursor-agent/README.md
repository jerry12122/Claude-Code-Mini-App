# POC: Cursor Agent CLI 協議與實作差異驗證

## 目的

驗證 [`internal/cursor/runner.go`](../../internal/cursor/runner.go) 與官方 Cursor Agent CLI（2026.05.20+）的相容性。

## 已確認項目（2026-06-29）

| 項目 | 官方現況 | 我們的實作 | 狀態 |
|---|---|---|---|
| 主命令 | `agent`（`cursor-agent` 為 alias） | `cursor-agent` | OK（alias 仍可用） |
| Headless | `-p` / `--print` | `--print` | OK |
| 串流格式 | `--output-format stream-json` NDJSON | 相同 | OK |
| 字元級 delta | `--stream-partial-output` + `timestamp_ms` 過濾 | 相同邏輯 | OK |
| Session resume | `--resume <chatId>` | `--resume` | OK |
| 工作目錄信任 | `--trust`（需配合 `--print`） | 相同 | OK |
| 強制執行 | `-f` / `--force` | `bypassPermissions` → `--force` | OK |
| Thinking 事件 | print mode 不輸出 | 未處理 | OK（預期行為） |
| result 兜底文字 | `result.result` 含完整回覆 | 原先未帶入 `EventDone.ResultText` | **已修正** |
| 非 partial 模式 | assistant 無 `timestamp_ms` 也應輸出 | 原先會全部被略過 | **已修正** |
| Headless 認證 | 需 `CURSOR_API_KEY` 或有效 login | 未預檢 | **已加強錯誤訊息** |

## 常見失敗原因（非協議 breaking change）

### 1. 認證失敗（本機 POC 實測）

```
Error: Authentication required. Please run 'agent login' first, or set CURSOR_API_KEY environment variable.
```

- `agent status` 可能顯示 logged in，但 `--print` headless 子進程仍可能讀不到 token
- **伺服器部署建議**：設定環境變數 `CURSOR_API_KEY`
- 互動式開發：執行 `agent login` 後確認 `agent -p "hi"` 可正常輸出

### 2. 空回覆（協議理解問題）

若啟用 `--stream-partial-output` 但 delta 過濾過嚴，可能略過所有 `assistant` 事件，導致前端空白。
修正：保留 partial 過濾，並在 `result` 事件帶入 `ResultText` 作兜底；非 partial 模式也能正常輸出 segment。

## 腳本

### `run_stream_json.ps1`

Live 測試：執行與 Go runner 相同的參數，逐行印出 NDJSON 並標記事件 type。

```powershell
powershell -ExecutionPolicy Bypass -File poc/cursor-agent/run_stream_json.ps1
```

### `run_all_poc.ps1`

離線單元測試 + live stream-json 一鍵執行；live 因認證失敗時仍回報離線測試結果。

```powershell
powershell -ExecutionPolicy Bypass -File poc/cursor-agent/run_all_poc.ps1
```

### 單元測試（離線，不需 API）

```bash
go test ./internal/cursor/... -v
```

使用官方文件範例 NDJSON 驗證 parser 與 dispatch 邏輯。

## 官方參考

- [Output format](https://cursor.com/docs/cli/reference/output-format)
- [Parameters](https://cursor.com/docs/cli/reference/parameters)
- [CLI Changelog](https://cursor.com/docs/cli/changelog)
