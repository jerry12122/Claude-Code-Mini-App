# 任務異常 Telegram 通知

> 當 Agent 或 Shell 任務出錯、非正常完成時，主動推送 TG 訊息（含錯誤摘要），避免使用者關閉 Mini App 後無從得知。

---

## 背景

現有 TG 通知僅覆蓋：
- ✅ 任務正常完成
- ⚠️ 需要授權確認

缺少：CLI 錯誤、子進程異常退出、Shell 非零 exit、provider `is_error` 等。

---

## 實作清單

- [x] `internal/tg/alert.go` — `TaskAlert`、訊息格式化、`NotifyTask`
- [x] 強化 `internal/tg/notify.go` — HTTP 狀態檢查、Markdown escape
- [x] `config.yaml` 新增 `notify.*` 選項
- [x] `internal/ws/handler.go` — outcome 狀態機、Agent/Shell 錯誤通知
- [x] 修正 Cursor `EventError` + `EventDone` 誤發 ✅
- [x] `internal/claude/runner.go` — `result.is_error` 送 `EventError`
- [x] 單元測試 + `go test ./...`

---

## 觸發矩陣

| 情境 | TG 通知 | 設定鍵 |
|------|---------|--------|
| Agent 正常完成 | ✅ | （既有） |
| 需要授權 | ⚠️ | （既有） |
| Agent 錯誤 / 進程失敗 | ❌ + 錯誤摘要 | `notify.on_error` |
| Shell 非零 exit / 錯誤 | ❌ + 錯誤摘要 | `notify.on_shell_error` |
| 使用者中斷 | ⏹（可選） | `notify.on_cancel` |

---

## 設定範例

```yaml
notify:
  on_error: true
  on_cancel: false
  on_shell_error: true
  error_preview_len: 800
  include_prompt: true
  prompt_preview_len: 120
```

---

## 訊息範本

```
❌ *my-project* 任務失敗

類型：claude
指令：幫我 refactor…
錯誤：exit status 1: …

請開啟 App 查看詳情
```
