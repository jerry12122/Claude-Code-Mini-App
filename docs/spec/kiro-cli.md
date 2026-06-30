# Kiro CLI 整合規格

> 版本：kiro-cli 2.10.0（實測）  
> 參考：[官方 headless 文件](https://kiro.dev/docs/cli/headless/)  
> 關聯實作：`internal/kiro/runner.go`

---

## 1. 執行模式概覽

Kiro CLI 使用 `--no-interactive` 旗標做無互動（headless）執行，等同於 Claude 的 `-p` 模式。

```bash
# 首回合（建立新 session）
kiro-cli chat --no-interactive --trust-all-tools "<prompt>"

# 後續回合（resume）
kiro-cli chat --no-interactive --trust-all-tools --resume-id <SESSION_ID> "<prompt>"
```

---

## 2. 啟動命令模板

### 2.1 首回合

```bash
kiro-cli chat \
  --no-interactive \
  --trust-all-tools \
  "<prompt>"
```

### 2.2 Resume 回合

```bash
kiro-cli chat \
  --no-interactive \
  --trust-all-tools \
  --resume-id <SESSION_ID> \
  "<prompt>"
```

> 提示詞為 positional argument（最後一個），以雙引號包裹防止 shell 展開。

---

## 3. 輸出格式

### 3.1 stdout（回應文字）

每行回應帶有 `> ` 前綴及 ANSI 控制碼：

```
\x1b[m> \x1b[0m<回應文字>
```

Go runner 需同時剝除：
1. ANSI escape sequences：`\x1b\[[0-9;?]*[A-Za-z]`
2. 行首 `> ` 前綴（剝除 ANSI 後剩餘的前兩字元）

### 3.2 stderr（系統訊息）

```
\x1b[...mAll tools are now trusted...
\x1b[...m Credits: 0.03 ⋅ Time: 2s\x1b[...m
```

stderr 內容僅供 log，不轉發給前端。  
其中包含 ANSI 控制碼、工具信任提示、credits 計費資訊等。

---

## 4. Session ID 擷取策略

### 背景

`--no-interactive` 模式下，stdout/stderr **不含** session id。  
（這是已知限制，參見 [GitHub issue #9066](https://github.com/kirodotdev/Kiro/issues/9066)）

### 擷取流程

```
首回合完成（process exit 0）
    ↓
執行 kiro-cli chat --list-sessions（同工作目錄）
    ↓
解析 stderr 中的第一行 "Chat SessionId: <UUID>"
    ↓（若成功）
發送 EventSessionInit { SessionID }
存入 DB：sessions.agent_session_id
    ↓（若失敗）
降級：單回合模式（不保存 session id，下次仍走首回合）
```

### `--list-sessions` 輸出格式（stderr，剝除 ANSI 後）

```
Chat sessions for <WorkDir>:

Chat SessionId: a6dd5cab-245a-46b5-9f6a-0d01c6bd21c2
  2 seconds ago | say hello in one word | 2 msgs | classic

...
```

正則匹配：`Chat SessionId:\s+([0-9a-f-]{36})`，取第一筆（最新）。

---

## 5. Permission / 工具信任

| 模式 | 旗標 | 說明 |
|---|---|---|
| 預設（全信任） | `--trust-all-tools` | 自動允許所有工具呼叫，無需中途確認 |
| 限制工具 | `--trust-tools=read,grep,write` | 僅信任指定類別 |

> 當前整合版本固定使用 `--trust-all-tools`。  
> Kiro 沒有 Claude 的 `permission_denials` 機制，`EventPermDenied` 永不觸發。

---

## 6. agent.Event 映射

| 時機 | agent.Event | 說明 |
|---|---|---|
| 進程啟動 | `EventStreamStart` | 告知前端進入 STREAMING 狀態 |
| 每行 stdout 文字 | `EventDelta { Text }` | 剝除 ANSI 與 `> ` 後發送 |
| 首回合完成，取得 session id | `EventSessionInit { SessionID }` | 存入 DB |
| 進程正常結束 | `EventDone { SessionID }` | 切回 IDLE |
| 進程非零 exit / 解析錯誤 | `EventError { Err }` | 前端顯示錯誤 |

---

## 7. 狀態機

Kiro 沒有 Claude 的 permission 流程，只走三個狀態：

```
IDLE → THINKING → STREAMING → IDLE
```

前端不需顯示「授權確認對話框」（`AWAITING_CONFIRM` 對 Kiro 不適用）。

---

## 8. 錯誤碼

| exit code | 語意 |
|---|---|
| 0 | 成功 |
| 1 | 一般執行錯誤 |
| 2 | 引數解析錯誤（如 prompt 格式問題） |
| 3 | `--require-mcp-startup` 失敗 |

---

## 9. 不在此範圍

- Kiro 的互動式 slash commands（`/model`、`/agent` 等）
- `--effort` 等進階旗標（可透過 `ExtraArgs` 擴充）
- `--agent` 自訂 agent profile（可透過 `ExtraArgs` 擴充）
