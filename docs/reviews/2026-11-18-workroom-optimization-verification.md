# 工作室首版優化驗收

2026-11-18。依已核准的 [優化計畫](../plan/todo/workroom-optimization.md) 實作，Cursor `grok-4.7-high` 執行派工 #9、#12、#14，Codex PM 獨立驗收。程式已整合回主工作樹；未 commit、push、重啟正式服務或修改正式設定／資料庫。

## 已交付

- 訊息來源寫入結構化欄位；普通輸入不能靠信封文字或偽造 `source_session_id` 冒充代理。歷史來源不明時明確降級顯示。原文與傳遞資訊保留。
- 成員狀態區分暫停、排隊、執行、授權及回答；暫停且 queue=0 仍顯示暫停，不以 100% 額度推斷停止原因。PM 依角色排序最左。
- 工作室容器達 960px 顯示對話與派工側欄；窄版用工作概況切換，保留草稿及閱讀位置。50 成員、長名稱、10,000 字備忘及長文均納入 fixture。
- 工作室內沿用具名授權／回答卡片，頂部待處理按鈕可回對話並定位。送出鎖防同步連點，Shell 確認逾時可重試，傳送失敗保留請求。
- 閒置、額度停止且佇列暫停的舊 PM 可以交接；執行中或未完成授權會拒絕交接。顯示移交數量，保留新 PM 自身的暫停及私人佇列，留下交接事件；草稿不自動改傳新 PM。
- MCP 沒有新增等待、結束回合或工具攔截限制。

## 驗證與版本

執行者原先無法執行終端；保持其 `default` 權限及指定模型，由 PM 使用原有權限執行驗收。CLI 的「已完成」沒有直接視為驗收通過。

先在 `D:/Temp/cma-workroom-optimization-20261118` 退修，再以主樹最新 758 個來源檔建立 `D:/Temp/cma-workroom-integration-20261118` 整合副本。三方合併保留 `db.go`、`server.go`、`ws/handler.go` 的額度恢復變更，`Generation` 與本次 `SourceSessionID` 都保留。29 個本次來源／測試檔回填前先備份，回填後 SHA256 與受驗副本完全相同；其他來源檔的 bytes 保持原樣。

| 檢查 | 結果 |
| --- | --- |
| `go test ./cmd/... ./internal/...` | 全部通過，含 DB／API／MCP／WS／server／arch |
| `go build ./cmd/... ./internal/...`、`go vet ./cmd/... ./internal/...` | 通過 |
| frontend-layering、rooms、console-usability、ux-reliability、remaining-ux、session-instructions | 6 組通過 |
| Edge：`test_rooms.py`、`test_workroom_layout.py` | 3 passed；整合副本 45.66 秒 |
| Edge：`test_regression.py` | 7 passed；18.23 秒 |
| PM 額外對抗檢查：普通瀏覽器 WS 偽造既存 session 的來源欄位 | 1 passed；7.16 秒；來源欄位被清除，仍為「你」 |
| 獨立 `production` 桌面 EXE | 建置成功，91,986,944 bytes；未執行 |

瀏覽器案例使用獨立 HTTP／WebSocket／SQLite 與假 CLI，沒有動用真實 provider 帳號。測過 1280、996、960、768px 容器，以及真實 390／320px viewport、深淺色、高反差背景、低高度。200% 為 CSS viewport 減半並加倍 device pixels 的重排模擬，不宣稱已操作桌面瀏覽器的原生縮放選單。可見主要控制項至少 44px；使用者正文的 computed style 對比檢查至少 4.5:1；不是整站逐元素對比稽核。

交接回歸包含 `TestRoomHandoffFromQuotaPausedPM`、`TestRoomHandoffPreservesMemoAttachmentsAndReports`、`TestRoomHandoffRecordsEventAndKeepsSuccessorState`、WS 附件交接及問答的重連／重複提交檢查。

## 實際退修與畫面

PM 修正測試的登入等待、展開真正長文、OKLCH 顏色讀取與縮放設定；保留草稿、scrollTop、閱讀錨點與溢出斷言。純測試設定失敗不當成產品行為失敗的證據。

產品量測曾重現窄版外層 `scrollHeight=1532`、可用高度 900。逐段移除內容及定位探測確認：訊息裡絕對定位的 `sr-only` 文字以時間軸外層作定位參考，越過時間軸的捲動邊界。僅讓時間軸 `position: relative` 即回到 900，完整案例隨後通過；320px 另讓 PM 晶片可收縮，保留成員按鈕。

Modern Web Guidance：我查閱 `css-layout`，依「Do NOT use `overflow: hidden` to mask layout issues」修正定位參考，沒有用工作室或 body 的 hidden 遮住溢出。絕對定位與捲動邊界的機制另對照 [W3C CSS 視覺效果規則](https://www.w3.org/TR/CSS22/visufx.html)。

已目視 [寬版](../../test-results/workroom-optimization/container-1280.png)、[320px](../../test-results/workroom-optimization/viewport-320.png) 與 [重排模擬](../../test-results/workroom-optimization/zoom-dark.png)。另保留 `pending-two.png`、`memo.png`、`light.png`、`dark.png`、`contrast.png`、`short.png`。這批圖來自通過的隔離實作版，與整合受驗版的前端內容相同；不是正式服務截圖。截圖留在忽略的 `test-results/`。

## 執行產物與限制

- EXE：`D:/Temp/miniapp-workroom-optimization-20261118/runtime/claude-miniapp-desktop-workroom.exe`。
- EXE SHA256：`6E14523BDEC808708E4B4890A97DABCA5E8F8AB468F1BBA1CD35C34C3FB77F4C`。
- 29 檔雜湊：同一 runtime 目錄的 `accepted-files.json`；原始及整合前備份分別為 `baseline-files/`、`pre-integration-files/`。
- 對抗檢查：同目錄 `test_source_boundary.py`，使用整合副本的 `scripts/browser/conftest.py` plugin 執行。
- pytest 的臨時 server 已由 fixture 清理；8080 的正式程序未由本工作重啟。
- 真實 Cursor 額度耗盡後的完整操作、Wails 執行畫面、Telegram Android／iOS、手機鍵盤仍待實機。假 CLI／單元測試與 EXE 建置不能替代這些驗收。計畫完整實機清單保留待辦，不整批勾選。
- 動作 WS 只在待回應成員上開啟，不帶歷史、不標已讀、不 drain；仍有 presence、額度快照／過期刷新與有效請求還原的既有副作用。

本工作依核准的普通計畫執行，沒有對應的活動 Spectra change；未標記或封存其他工作室的 change。

## 2026-11-19 重跑

在 HEAD `9734cb2`（已含 quota-auto-resume 與其後的清理）以臨時虛擬環境（`pytest-playwright` 0.7.2、Edge）重跑：`test_rooms.py`＋`test_workroom_layout.py` 3 passed（35.7 秒）、`test_regression.py` 7 passed（18.1 秒），與上表一致。整合後的 `handler.go`、`SettingsModal.js`、`ChatView.js` 重構沒有造成工作室或授權面板的回歸。仍未驗：Wails、Telegram Android／iOS、真實額度停止的交接。
