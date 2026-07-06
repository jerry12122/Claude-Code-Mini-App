# POC: codex-cli 整合驗證

## 驗證目標

1. `codex exec --json` 可在 Go 環境透過 `exec.Command` 正常啟動
2. JSONL stdout 可解析 `thread.started`、`item.*`、`turn.completed`
3. 首回合後能從 `thread.started` 取得 `thread_id`
4. 以 `codex exec resume <thread_id>` 能穩定延續對話
5. 帳戶額度可從 headless slash command 取得

## 前置需求

- `codex` 已安裝且在 PATH 中
- 已登入（`codex login status` exit 0）或設定 `CODEX_API_KEY`

## 腳本說明

| 腳本 | 用途 |
|------|------|
| `probe_auth.ps1` | 驗證認證狀態 |
| `capture_thread_id.ps1` | 首回合 JSONL，擷取 `thread_id` |
| `resume_smoke_test.ps1` | resume 第二輪對話 |
| `parse_events.ps1` | 驗證 item type → 活動標籤映射 |
| `probe_quota.ps1` | 探測 `/status` 或 `/usage` 額度來源 |
| `run_all_poc.ps1` | 端對端 runner |

## 已確認規格

| 項目 | 結果 |
|------|------|
| Headless 命令 | `codex exec --json --skip-git-repo-check --cd <dir> --sandbox workspace-write --ask-for-approval never` |
| Session ID | `thread.started` 事件的 `thread_id` |
| Resume | `codex exec resume <thread_id> --json ...` |
| 文字輸出 | `item.completed` where `type=agent_message` |
| 無逐字 delta | 整段文字一次送出 |
| Trust-all 對應 | `workspace-write` + `ask-for-approval never`（無 `--trust-all-tools`） |

## 成功標準

- 同一工作目錄下兩輪對話穩定接續
- `thread_id` 可落盤供後端 Go runner 引用
- 至少一條 quota 來源可用
