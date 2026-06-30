# POC: kiro-cli 整合驗證

## 驗證目標

1. `kiro-cli chat --no-interactive` 可在 Go 環境透過 `exec.Command` 正常啟動
2. stdout 回應格式（`> ` 前綴剝除）可被正確解析
3. 首回合後能透過 `--list-sessions` 取得 session id
4. 以 `--resume-id` 能穩定延續對話
5. 在工作目錄隔離條件下 session id 唯一且可辨識

## 前置需求

- `kiro-cli` 已安裝且在 PATH 中（實測版本：2.10.0）
- Kiro 帳號已登入

## 腳本說明

### `capture_session_id.ps1`
執行首回合無互動訊息，並從 `--list-sessions` stderr 擷取最新 session id。
輸出：session id 字串（成功）或空字串（失敗）。

### `resume_smoke_test.ps1`
接受 `$SessionId` 參數，以 `--resume-id` 延續第二輪對話，驗證對話連貫性。

### `run_all_poc.ps1`
整合以上腳本的端對端驗證，列印成功/失敗摘要。

### `classify_output.ps1`
驗證 stdout 思考鏈 vs 最終回覆的分類規則（首個 `> ` 行為分界）。

## 已確認規格

| 項目 | 結果 |
|---|---|
| 執行檔類型 | 原生 .exe（免 cmd.exe wrapper） |
| 提示詞傳遞方式 | positional arg（最後一個引數） |
| stdout 格式 | `> <回應文字>` 每行帶 `> ` 前綴 |
| session id 位置 | `--list-sessions` 的 **stderr** |
| session id 格式 | `Chat SessionId: <UUID>` |
| resume 旗標 | `--resume-id <UUID>` |
| 信任工具 | `--trust-all-tools` |
| exit code | 0 = 成功，其他 = 失敗 |

## 成功標準

- 同一工作目錄下兩輪對話穩定接續
- session id 可落盤供後端 Go runner 引用
