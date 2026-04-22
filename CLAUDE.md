# Claude Code 指引 (CLAUDE.md)

## 語言規則
**一律使用正體中文**回應所有訊息與註解，程式碼變數/函式名稱除外。

---

## 項目概述
Telegram Mini App，讓使用者透過手機遠端操控伺服器上的 `claude code` CLI。

詳細規格：`docs/spec/plan.md`
工作清單：`docs/plan/todo.md`

---

## 架構決策（不得更動）

### CLI 執行模式
- 每條訊息 **spawn 一個子進程**，用後即棄
- 全程使用 `-p`（非互動）+ `--output-format stream-json`
- 以 `--resume <session_id>` 保持對話連續性
- **不使用 PTY**

```bash
# 標準執行範本
claude -p "<prompt>" \
  --output-format stream-json \
  --resume <session_id> \
  --permission-mode <mode> \
  --allowedTools "<tools>"
```

### 通訊協議
- **WebSocket**（雙向單連線），不用 SSE

### 部署
- **Go 單一二進位**，同時提供 API + WebSocket + 靜態 HTML
- 不分離前後端部署

---

## stream-json 事件處理

### 關鍵頂層 type
| type | 用途 |
|---|---|
| `system` (subtype=`init`) | 取得 session_id、permissionMode |
| `stream_event` | 包裝 Anthropic API 事件，內層看 `event.type` |
| `result` | 最終結果，含 `session_id`、`permission_denials`、`cost` |

### 狀態機觸發對應
| stream-json 事件 | 切換至狀態 |
|---|---|
| 送出指令（進程啟動） | `THINKING` |
| `stream_event.event.type = "content_block_start"` (type=text) | `STREAMING` |
| `stream_event.event.type = "content_block_delta"` (text_delta) | 累積文字 |
| `result.stop_reason = "end_turn"` | `IDLE` |
| `result.permission_denials` 非空 | `AWAITING_CONFIRM` |

---

## Permission 授權流程

`AWAITING_CONFIRM` 不是 CLI 的 y/n 提示，而是後端解析 `result.permission_denials` 後主動觸發：

1. 後端收到 `result`，`permission_denials` 非空
2. 送 WS → 前端顯示授權對話框
3. 使用者選擇：
   - **允許此操作** → 加 `--allowedTools` 重新 resume
   - **允許並記住** → 更新 `sessions.permission_mode`，重新 resume

### Permission Mode
| 模式 | CLI 旗標 | 說明 |
|---|---|---|
| `default` | `--permission-mode default` | 預設，遇寫入/執行觸發授權流程 |
| `acceptEdits` | `--permission-mode acceptEdits` | 自動允許檔案讀寫 |
| `bypassPermissions` | `--permission-mode bypassPermissions` | 全部跳過（危險模式） |

---

## 參考文件
- `docs/spec/claude-code-cli.md` — CLI 旗標完整參考
- `docs/spec/headless.md` — `-p` 模式與 stream-json 用法
- `docs/spec/plan.md` — 完整規格書（Schema、WebSocket 格式、Roadmap）
