# 評估：以 coder/acp-go-sdk 取代手寫 ACP 客戶端

日期：2026-11-19。結論：**暫不採用**，也不需要現在做任何程式變更。

## 背景

`internal/kiroacp/client.go`（470 行）與 `internal/antigravity/acp.go`（406 行）是兩份各自手寫的
JSON-RPC over stdio 客戶端，說的都是標準 ACP 方法（`initialize`、`session/new`、`session/prompt`、
`session/request_permission`、`session/update`、`session/load`、`session/cancel`、`session/set_mode`…）。
兩份合計約 880 行，另有約 2,400 行測試。

## 實驗（v0.20.0）

SDK 無外部依賴（`go list -m all` 只有它自己）。以假 agent 對 `ClientSideConnection` 送出一連串內容：

| 輸入 | SDK 行為 |
|---|---|
| 標準 `agent_message_chunk` | 正常交給 `SessionUpdate` |
| 未知的 `sessionUpdate` 種類（例如 `kiro_thinking_delta`） | 回 `-32602 no matching variant for union`，**整則丟棄**，handler 看不到 |
| `image` 區塊的 `data` 型別錯誤 | `invalid variant payload`，同樣丟棄 |
| 缺必填欄位的文字區塊 | 補成空字串後收下 |
| 擴充通知 `_kiro.dev/metadata` | 沒有 handler 就忽略 |
| 以上全部之後 | 連線存活，`Prompt` 正常回傳 `end_turn` |

## 為什麼不採用

1. **嚴格 vs 寬鬆。** SDK 是標準 ACP 的嚴格聯合型別；現有客戶端寬鬆解析，並明確讀取 kiro／agy 的非標準資料
   （例如 `sessionUpdateBody.images()` 讀工具結果中的圖片）。換過去要逐一核對專案實際讀取的每個欄位都被 SDK 型別涵蓋，
   否則是靜默丟資料。
2. **無法用真實 CLI 驗證。** 開發機沒有 `kiro-cli`、`agy`；現有的 ACP 測試都是假伺服器，無法證明行為等價。
3. **SDK 仍是 0.x。** v0.20.0 才「一次做完所有計畫中的破壞性變更」（見其 `MIGRATION_v0.20.md`），
   0.x 之間仍可能再變。
4. **容錯邏輯無論如何都要保留。** SDK 寫入是同步的 `w.Write`（持鎖）；專案客戶端針對「CLI 不讀 stdin、參數又大於
   pipe 容量」做了獨立 goroutine 寫入、`broken` 旗標與放棄語意，以及「授權請求不可卡住讀取迴圈」的專用 worker 與佇列上限。
   這些是實戰得來的，換 SDK 後還是得自己包一層，省下的只有型別與請求對應表。
5. **收益有限。** 最多省下約 300 行傳輸層程式，卻要重寫依賴未匯出欄位（`broken`、`inflightWrites`）的測試。

## 何時重評

- 兩個 CLI 都能在開發機實際連線，可以做等價性驗證（同一段真實對話，新舊客戶端輸出逐項比對）。
- SDK 到 1.0，或至少數個小版本沒有破壞性變更。
- 兩份客戶端開始需要同步修同一類問題（目前沒有）。

## 較便宜的替代（尚未做）

把兩份客戶端的共同傳輸（待處理表、讀取迴圈、帶逾時的請求、寫入放棄語意）抽成 `internal/acpconn`，
沒有外部依賴與型別嚴格性的風險。但需要先決定兩邊容錯語意是否要統一（目前不同），且測試要跟著搬，
因此列為後續項目，不在這次處理。
