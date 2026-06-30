# Antigravity CLI（agy）整合規格

> **取代 legacy Gemini CLI。** Gemini CLI 已退役；本專案曾以 `agent_type=antigravity`、子進程 `agy` 整合。  
> **應用狀態（2026-06）：** 因 headless #76 未解，`gemini` / `antigravity` 已在 Mini App **停用**（不可新建 Session；舊 Session 送訊息會回錯）。POC 與 runner 程式碼保留待 upstream 修復。  
> POC：`poc/antigravity-cli/` · 實作：`internal/antigravity/`

## 官方文件

- https://antigravity.google/docs/cli-overview
- https://github.com/google-antigravity/antigravity-cli
- 遷移指南：CLI Overview → *Migrating from Gemini CLI*

## Provider 定位

| 項目 | 規格 |
|---|---|
| 類型 | Local subprocess（`agy`） |
| Prompt | `--print` / `-p` + **stdin pipe**（多行 prompt，避開 Windows argv 截斷） |
| Session | `--conversation <id>`；`--continue` / `-c` 接最近對話 |
| 權限 | `--dangerously-skip-permissions`（對應 `bypassPermissions`） |
| 模型 | `--model <name>` |
| stream-json | **尚未穩定**；設 `CC_AGY_STREAM_JSON=1` 時 runner 會嘗試（forward compat） |

## Headless 整合風險（必讀）

Mini App 以 **stdout pipe** 讀取子進程輸出（無 PTY）。  
agy `--print` 在非 TTY 下可能 **exit 0 但 stdout 為空** → [Issue #76](https://github.com/google-antigravity/antigravity-cli/issues/76)。

**部署前請執行：**

```powershell
cd poc/antigravity-cli
./run_all_poc.ps1
```

若 `probe_headless.ps1` 失敗，需等待 upstream 修復或評估非架構內 workaround（本專案預設 **不使用 PTY**）。

## 啟動範本（print 模式，預設）

```bash
agy --print \
  [--conversation <session_id>] \
  [--model <model>] \
  [--dangerously-skip-permissions] \
  [--print-timeout 5m]
# prompt 經 stdin 餵入
```

## stream-json 模式（實驗）

當 agy 支援時（或 `CC_AGY_STREAM_JSON=1`）：

```bash
agy --output-format stream-json \
  [--conversation <session_id>] \
  ...
```

事件格式與 legacy Gemini CLI **相容**（`init` / `message` / `tool_use` / `tool_result` / `error` / `result`）。詳細欄位見 [`gemini-cli.md`](gemini-cli.md)（stream 章節仍適用）。

## Permission 對應

| Mini App `permission_mode` | agy |
|---|---|
| `default` | （無額外旗標） |
| `acceptEdits` | 無 1:1 CLI 旗標 |
| `bypassPermissions` | `--dangerously-skip-permissions` |
| `plan` | 未對應 |

**無** Claude 式 `permission_denials` / `allow_once`；WS `allow_once` 對 antigravity session 為 no-op。

## agent_type 遷移

| 舊值 | 新值 |
|---|---|
| `gemini` | `antigravity`（factory 自動別名） |

## 環境變數

| 變數 | 用途 |
|---|---|
| `CC_AGY_BIN` | 覆寫 agy 執行檔路徑 |
| `CC_AGY_STREAM_JSON=1` | 啟用 stream-json 嘗試 |

## 相關文件

- [`gemini-cli.md`](gemini-cli.md) — legacy 規格（stream-json 事件細節）；**已棄用為執行目標**
- [`headless.md`](headless.md) — 子進程 headless 通用模式
