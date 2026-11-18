# 功能計劃：系統資源監控（CPU／記憶體）

> 狀態：第 1、2 節已實作並驗證（2026-11-18）；第 4 節假設已由使用者確認不需修改（2026-11-18）
> 建立日期：2026-11-18
> 來源：`todo.md`「系統資源監控 (CPU/Memory 狀態)」原本只有一行，沒有規格；以下為 supervisor 補的最小規格，**假設都標在第 4 節，使用者可推翻**。
> 目標：在手機或桌面上，不開終端機就能看到跑 agent 的那台伺服器現在忙不忙（CPU 與記憶體）。

## 1. 範圍（最小）

- 後端新增 `GET /system/stats`，受 `authMiddleware` 保護，回傳單次快照：
  `{ "cpu_percent": 0-100, "mem_used_bytes": N, "mem_total_bytes": N, "sampled_at": "RFC3339" }`。
- 前端在「設定頁」新增一個唯讀區塊「系統資源」，只在該區塊可見時每 3 秒輪詢一次，離開即停止。
- 訪客（分享聊天室、snapshot）不可存取：回 403，也不在訪客畫面顯示。

## 2. 實作約束

- **不新增第三方依賴。** Windows 用已是直接依賴的 `golang.org/x/sys/windows`（記憶體 `GlobalMemoryStatusEx`、CPU `GetSystemTimes`）；Linux 讀 `/proc/stat` 與 `/proc/meminfo`；其他平台回 501 與明確訊息。
- CPU 百分比由「前後兩次取樣差」算出：套件內保留上一次取樣，第一次呼叫沒有基準時回 `cpu_percent: null` 而不是 0，避免誤導。
- 新套件須符合 `docs/spec/backend-architecture.md` 的分層（屬 L1 領域工具），並通過 `go test ./internal/arch/`。
- 前端遵守 `docs/spec/frontend-architecture.md`（`node scripts/frontend-layering.test.cjs`）；沒有 npm，不引入套件；瀏覽器支援政策見 CLAUDE.md。

## 3. 完成標準

- 單元測試：Linux `/proc` 解析（含欄位缺漏、除以零）、CPU 差值計算（含第一次無基準、計數回繞）、未授權 401、訪客 403、不支援平台 501。
- `go build`、`go vet`、`go test`（排除 `poc/`）、`go test ./internal/arch/`、`node scripts/frontend-layering.test.cjs` 全過。
- 用臨時實例（獨立埠與密碼）實測：登入後設定頁看得到數值、與工作管理員大致相符（±15%）、離開區塊後不再有請求。

## 4. 假設（待使用者確認）

1. 只顯示整機 CPU／記憶體，不顯示磁碟、網路、各 agent 進程占用。
2. 放設定頁，不放頂欄（頂欄已有用量進度條，空間有限）。
3. 只做即時數值，不存歷史、不畫圖。
4. 單機的整機數值就夠；不做多機。

## 5. 不做

歷史曲線、告警門檻、各進程資源、磁碟與網路、跨主機。
