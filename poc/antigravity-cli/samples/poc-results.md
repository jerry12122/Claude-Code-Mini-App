# Antigravity POC 實測結果

**日期：** 2026-06-30  
**環境：** Windows · agy 1.0.14 · 已登入 `jerry90522@gmail.com`  
**路徑：** `C:\Users\user\AppData\Local\agy\bin\agy.exe`（`CC_AGY_BIN`）

## 摘要

| 探測 | 結果 | 說明 |
|---|---|---|
| `probe_flags` | **PASS** | 旗標已寫入 `flags.txt`；**無** `--output-format stream-json` |
| `probe_headless` | **FAIL（已知）** | `--print` + pipe：exit 0，stdout **0 bytes** |
| `probe_stream_json` | **SKIP** | agy 1.0.14 不支援 stream-json 旗標 |

## Headless 細節

已登入後仍重現 [Issue #76](https://github.com/google-antigravity/antigravity-cli/issues/76)：

- `agy --print "Reply PONG" --dangerously-skip-permissions` → exit 0，stdout 空
- stdin 餵 prompt 同樣空
- 與 auth 無關，為 **非 TTY stdout** 行為

## Mini App 整合結論

- `internal/antigravity` runner（pipe + `--print`）**目前無法取得回應文字**
- `CC_AGY_STREAM_JSON=1` **無效**（CLI 尚無旗標）
- 需等待 upstream 修 #76，或另開架構決策（例如 agy 專用 PTY — 與 Claude runner 不同路徑）

## 重跑

```powershell
$env:CC_AGY_BIN = "$env:LOCALAPPDATA\agy\bin\agy.exe"
cd poc/antigravity-cli
./run_all_poc.ps1
```
