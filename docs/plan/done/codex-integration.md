# 功能計畫書：Codex CLI 整合

> 狀態：已完成  
> 建立日期：2026-04-19  
> 完成日期：2026-07-06  
> 關聯規格：`docs/spec/codex-cli.md`、`poc/codex-cli/`

---

## 實作摘要

- `internal/codex/`：`Runner` + JSONL 事件解析
- `agent.EventActivity` + WS `activity` 訊息
- `internal/quota/codex.go` 帳戶額度 fetcher
- 四個前端主題已啟用 Codex 選項

**實測 CLI 版本**：codex-cli 0.142.5  
**Headless 旗標**：`-c 'approval_policy="never"'`（非 `--ask-for-approval`）、關閉 stdin、移除 `TERM`

---

## 11. 實作任務

### Phase E1 — CodexRunner 後端

- [x] `internal/agent/runner.go` 新增 `EventActivity`
- [x] `internal/codex/events.go`、`runner.go`
- [x] `ws/handler.go` blank import + `EventActivity` 廣播

### Phase E2 — 測試

- [x] `internal/codex/runner_test.go`
- [x] `poc/codex-cli/` 端對端 POC（auth、thread_id、resume）

### Phase E3 — 設定文件

- [x] `docs/spec/codex-cli.md` 實測結論
- [x] `poc/codex-cli/README.md`

---

（其餘規格章節見 git 歷史；詳細設計以 `docs/spec/codex-cli.md` 為準）
