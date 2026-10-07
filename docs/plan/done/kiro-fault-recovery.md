# Kiro ACP 故障判斷與復原

日期：2026-10-07。程式與測試完成，尚未重啟正在使用的桌面程式。

## 故障處置

| 證據／狀況 | 處置 |
| --- | --- |
| 本輪 ACP error.data 或模型串流 log 出現 MonthlyLimitReached、Throttling、Some(403)、Some(429) | 回報帳號／額度／節流錯誤，不換會話 |
| stopReason=refusal，且沒有產出 | 回報拒絕，不重試 |
| 本輪故障證據含 EmptyResponse | 直接回傳 ErrHandoff；這是換乾淨會話的策略訊號，不代表已證明 session 永久損壞 |
| session/load 明確回 Session not found | 直接回傳 ErrHandoff，不重試不存在的 SID |
| 啟動逾時、無動靜、stdout 關閉、load 失敗、prompt 成功但無產出、原因不明的 -32603 | 沒有文字／工具活動時重試一次，仍失敗才回傳 ErrHandoff；已有產出則不重試 |
| 使用者取消／父 context 結束 | 不重試、不交接、不送完成事件 |
| 無法確認舊進程退出，或授權 worker 無法結束 | 一般錯誤，停止自動復原，不宣告完成 |

重試沿用輸入 SID；首次對話沒有 SID 時重新 session/new。工具活動也算已有產出，以避免重跑已執行的工具。

## 實作重點

- 每次子進程使用專屬暫存 KIRO_CHAT_LOG_FILE，只設定於 cmd.Env；分類不讀共用 log，只讀本輪模型串流 ERROR 行與完整 ACP error.data。子進程收尾後讀 log，隨後刪除。
- initialize 與 session/new|load 共用 60 秒啟動上限，load 另有 20 秒上限。prompt 無動靜上限為 300 秒；工具執行中與等待使用者授權不計 idle，load 回放工具不影響 watchdog。
- stdin 寫入可被 context／close 中止等待，寫入仍序列處理；放棄半途寫入後封鎖後續寫入。ACP 回應與 EOF 同時抵達時保留已收到的回應，避免遺失故障原因。
- 單一授權 worker 維持請求順序，reader 不等使用者；佇列上限 32，滿載時拒絕新請求而不堵住 reader。
- 收尾先取消本輪 context、關 stdin；失敗給 2 秒、成功給 5 秒優雅退出，必要時 KillTree，再最多等 5 秒確認退出。授權 worker 另最多等 2 秒。只有收尾成功後才送 EventDone；遲到的更新／授權不再對外處理。
- 已檢閱 WS askUser：等待時有 rctx.Done() 分支，取消後清除待授權，且不把狀態恢復為執行中。Kiro 對不遵守取消的其他 callback 仍有 worker 收尾失敗保護。

## 驗證

主會話獨立執行並通過：

```powershell
go build ./...
go vet ./...
go test ./... -count=1
go test -race ./internal/kiroacp -count=1
go test -overlay poc/kiro-recovery/blocked-write-overlay.json ./internal/kiroacp -run '^TestReview' -count=1
go run ./poc/kiro-recovery
```

- 假 CLI 涵蓋成功、重試後成功、log／error.data 的 EmptyResponse、未知 -32603、額度、refusal、持續空回覆、hang、部分輸出、bad params、長工具、load 回放、log 隔離、setup 逾時、取消、阻塞寫入、收尾失敗與授權回收。
- 主會話另外建立可重現失敗的三個檢查：阻塞寫入的 deadline、授權序列性、回應緊接 EOF。修補後全通過；對方也做了移除修補會失敗的突變驗證。
- 本機 Windows 的原生 pipe 測試確認 close 可以釋放卡住的寫入。Kiro package race 通過；不代表所有 package 都通過 race。
- 真實 kiro-cli-chat 2.28.0：不存在的 SID 在 prompt 前回 -32603，error.data 帶 Session not found；約 2.299 秒、只啟動一次、ErrHandoff=true，Error／Done／Delta 事件皆為 0。啟動前取消不建立進程。完成後確認 smoke 進程退出、專屬 log 清除；不呼叫模型或 MCP。

PoC 位於 [poc/kiro-recovery](../../../poc/kiro-recovery)，受現有 ignore 規則排除；其中 overlay 路徑為本機絕對路徑。正式回歸測試留在 internal/kiroacp。

## 接下來

- 重新建置／啟動新版後觀察真實故障。EmptyResponse 尚未於真 CLI 強制重現；300／60 秒門檻以縮短時間的測試驗證，完整門檻與 Unix pipe 尚未實機驗證。硬崩潰可能留下暫存 log，沒有加入背景清理器。
- 工具本身卡死時依既定要求不自動 idle timeout，需使用者中斷。
- ErrHandoff 之後的自動／手動接手已完成，行為與限制見 [接手紀錄](manual-session-handoff.md)；其他供應商 runner 未修改。
