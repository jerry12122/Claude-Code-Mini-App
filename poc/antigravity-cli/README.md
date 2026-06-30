# POC: Antigravity CLI（agy）

## 背景

Google 已以 **Antigravity CLI**（指令 `agy`）取代 legacy **Gemini CLI**（`gemini`）。  
本 repo 的 Mini App 以 **子進程 + stdout pipe** 整合 headless 模式，與 agy 目前限制直接相關。

## 官方文件

| 資源 | URL |
|---|---|
| CLI Overview | https://antigravity.google/docs/cli-overview |
| Getting Started | https://antigravity.google/docs/cli-getting-started |
| GitHub | https://github.com/google-antigravity/antigravity-cli |
| 從 Gemini CLI 遷移 | https://antigravity.google/docs/cli-overview（Migrating from Gemini CLI） |

## 已知 headless 限制（整合前必讀）

- **`agy -p` / `--print` 在非 TTY stdout（pipe、redirect、Go `exec`）可能完全無輸出**，exit code 仍為 0  
  → [google-antigravity/antigravity-cli#76](https://github.com/google-antigravity/antigravity-cli/issues/76)
- **目前 agy 1.0.x 無 `--output-format stream-json`**（legacy `gemini` 才有）；社群正請求 `--format stream-json`
- 設定目錄仍多在 `~/.gemini/antigravity-cli/`（與 Gemini 生態共用路徑）
- Session 延續：`--conversation <id>`；最近對話：`-c` / `--continue`

## 安裝

```powershell
irm https://antigravity.google/cli/install.ps1 | iex
agy --version
agy auth login   # 或互動執行 agy 完成 Google 登入
```

**安裝後未重開 terminal：** 當次 session 可覆寫路徑：

```powershell
$env:CC_AGY_BIN = "$env:LOCALAPPDATA\agy\bin\agy.exe"
$env:Path = "$env:LOCALAPPDATA\agy\bin;" + $env:Path
```

## 腳本

| 腳本 | 用途 |
|---|---|
| `probe_flags.ps1` | 列印 `agy --help`、檢查 `--output-format` 是否存在 |
| `probe_headless.ps1` | 模擬 Go runner：pipe stdout，測 `-p` 是否有輸出 |
| `probe_stream_json.ps1` | 若設 `CC_AGY_STREAM_JSON=1` 或 CLI 已支援，測 NDJSON |
| `run_all_poc.ps1` | 依序執行上述探測 |

## 成功標準（POC）

1. `agy` 在 PATH 且已登入
2. `probe_flags.ps1` 記錄可用旗標（寫入 `samples/flags.txt`）
3. `probe_headless.ps1`：
   - **PASS**：pipe 下 stdout 非空，或 stderr 明確拒絕（可解析）
   - **FAIL（已知）**：exit 0 但 stdout 空 → 與 #76 一致，Mini App 需等 upstream 或 PTY workaround
4. 若 `--output-format stream-json` 可用：`probe_stream_json.ps1` 收到 `init` / `message` / `result` 事件

## 最新實測（2026-06-30）

已登入 agy 1.0.14 後重跑：flags **PASS**、headless **FAIL（#76）**、stream-json **SKIP**。  
詳見 [`samples/poc-results.md`](samples/poc-results.md) 與 [`samples/poc-latest.txt`](samples/poc-latest.txt)。

## Go Runner 對應

- 套件：`internal/antigravity/`
- 指令：`agy --print` + stdin prompt（預設）
- 環境變數 `CC_AGY_STREAM_JSON=1`：嘗試 `--output-format stream-json`（forward compat）
- 環境變數 `CC_AGY_BIN`：覆寫 agy 路徑
- `agent_type`：`antigravity`（DB 舊值 `gemini` 自動別名）

## 權限對應

| Mini App permission_mode | agy 行為 |
|---|---|
| `default` | 無額外旗標（工具確認由 agy 設定決定） |
| `acceptEdits` | 無 1:1 旗標（UI 保留，runner 不附加） |
| `bypassPermissions` / `yolo` | `--dangerously-skip-permissions` |

無 Claude 式 `allow_once` 互動流程。
