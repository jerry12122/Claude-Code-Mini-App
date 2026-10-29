# 功能詳述

[← 回 README](../README.zh-TW.md) · [English](features.md)

## 代理與 Session

- **多代理** — Claude Code、Cursor Agent、Codex、Antigravity（`agy`，NDJSON 串流）、Kiro ACP（透過 Agent Client Protocol 提供互動式授權提示）；依 Session 選擇。既有 Kiro CLI Session 仍可執行，新建請改用 Kiro ACP；舊 `gemini` Session 使用 Antigravity runner，新建請選 Antigravity。支援的模型清單於啟動時向各 CLI 取得（含 Claude），也可在 設定 → 一般 不重啟直接重新抓取
- **Session 管理** — 多對話、各自綁定代理、`work_dir` 與權限模式
- **權限流程** — 顯示完整指令／檔案內容；Claude prompt-tool 與 Kiro ACP 可允許單次操作。舊 Claude 遭拒後選「允許工具並重試」，會在本次重試放行列出的工具；編輯類工具另可允許並自動允許編輯
- **用量進度條** — Session header 以進度條顯示帳戶用量（如 Claude `5h 16% · Week 9%`，已知方案時一併顯示）；Antigravity 分別顯示 Gemini 與 Claude/GPT 額度，手機精簡徽章顯示最高使用率
- **未讀追蹤** — 列表標示有新動態的 Session，可一鍵「全部標為已讀」。列表透過 `/events` WebSocket 即時更新（輪詢 30 秒作為保底）；分頁標題顯示未讀數，其他 Session 完成或待授權時會跳 toast
- **快速跳轉** — Ctrl/Cmd+P 開啟類 VS Code 的跳轉面板，可依名稱、目錄、分支搜尋並切換 Session
- **分頁與分割群組**（桌面，≥1024px）— 每個開啟的 Session 是群組內的一個分頁，分頁列顯示名稱、agent 圖示、狀態點與關閉鈕（中鍵也可關閉）。同一個 Session 只會在一個分頁，開啟已開啟的 Session 只會聚焦；每個群組只掛載作用中的分頁。關閉作用中分頁會選右邊鄰居，沒有則選左邊。可再分出第二個群組（左右並排或上下堆疊），作用中的群組有明顯外框
- **拖放擺放** — 從側欄把 Session 拖到分頁列（插在放開的位置）或群組內容區：中央＝加入該群組；只有一個群組時，拖到邊緣會分出第二個群組（左右＝並排、上下＝堆疊）；已有兩個群組時，放在任何位置都只是加入該群組，已開啟的 Session 則會移動過去。分頁本身可拖曳重排或在群組間移動；按 Esc 取消。重新整理後會還原版面，且依瀏覽器各自保存。小於 1024px 維持單欄、不顯示分頁
- **快捷鍵** — `Ctrl+\` 把作用中分頁分到新群組（至少要有兩個分頁）或把兩個群組合併；`Ctrl+Alt+←/→` 在群組間切換焦點；`Ctrl+Alt+O` 切換並排／堆疊。輸入法組字中不會觸發
- **訊息佇列** — 執行中送出的訊息會排隊（存 DB、重啟不遺失）依序執行；失敗／中斷時暫停，可手動繼續或移除。若 agent 已回完話但程序仍在等待（例如排程喚醒），中斷鈕仍可使用，新訊息會排隊到程序結束

## 聊天

- **即時串流** — WebSocket 對話與 Markdown 串流；多分頁同步
- **程式碼區塊** — 語法高亮 + 語言標籤，一鍵複製
- **附件上傳** — 📎 上傳或貼上圖片／文字檔，在輸入框上方以可移除的 chip 顯示（圖片有縮圖），以路徑交給 agent 讀取（存於 `workspace/uploads/`，不寫入專案 `work_dir`）；桌面版支援拖放
- **訊息操作** — 複製／轉發按鈕常駐在每則訊息的時間旁；長按訊息開啟浮動選單（複製，非自己的訊息另有轉發）
- **輸入列控制** — Model、Effort 與權限模式是輸入列上的 chip（不在 header），送出前就能直接切換
- **聊天體驗** — 輸入法選字的 Enter 不會誤送出；觸控裝置 Enter 為換行；往上翻舊訊息時不會被串流拉回底部（附「跳到最新」按鈕）；刪除 Session 後 5 秒內可復原

## 協作與整合

- **臨時共享** — 以連結 + 6 位數 PIN + 暱稱把 Session 分享給朋友（預設 1 小時，最長 7 天）。訪客用任何瀏覽器加入，角色為唯讀 `viewer` 或完整權限 `editor`；訊息會標示說話者，結束分享即刻踢出所有訪客
- **MCP server** — 透過 Streamable HTTP（`POST /mcp`）讓其他 agent 操作 session、讀聊天紀錄、查跨 session 活動；`ask_session` 同步詢問另一個 session 並取回答覆，互問有跳數上限（`mcp_max_hops`）防止無限迴圈（預設關閉）

  佇列工具沿用 MCP 驗證：`get_queue({"session_id":"…"})` 依 FIFO 回傳 `{"queue":[{"id":123,"content":"…","created_at":"…"}],"queue_paused":false}`（空佇列為 `queue: []`），項目也可能含 `author`、`attachments`。`delete_queue_message({"session_id":"…","message_id":123})` 只刪除該 session 的待執行項目並推播最新佇列，回傳 `{"ok":true}`；項目已不存在時仍成功。兩者皆拒絕空白或不存在的 session，刪除另拒絕非正整數 ID。不會中斷執行中的任務或恢復暫停的佇列。

- **驗證** — Telegram `initData` + 白名單；可選內網密碼登入。`web.trusted_proxies` 預設只信任同機 loopback 代理的 IP 標頭；其他代理須設定精確 CIDR，空清單則停用標頭解析
- **選用 Shell** — 於 `work_dir` 執行指令（預設關閉）；開啟後會在會話 header 顯示「開啟 VSCode／開啟目錄」按鈕（僅桌面版）
- **版本資訊** — 設定頁顯示應用程式版本與建置所依據的 Git commit
- **日誌檢視** — 設定 → 日誌即時串流伺服器日誌（等級篩選、搜尋、暫停、複製；可在執行期切換 Debug）。桌面版沒有 console 時很好用；只顯示本次啟動後的日誌，完整紀錄仍在 `logs/server.log`

## 桌面視窗（Windows）

同一個行程多開一個視窗，顯示現有網頁，並繼續聽 port。瀏覽器與 Telegram Mini App 照舊連線。關視窗只會隱藏，系統匣留著：左鍵叫回視窗，右鍵選「開啟視窗」或「結束」。Ctrl+Q 也會結束。F5／Ctrl+R 重新載入畫面，F12 開 DevTools（加 `-tags production` 時無作用）。

Win11 已內建 WebView2。`go build ./cmd/server` 維持純 Go、不需 CGO。桌面版用 Wails v3（系統匣是框架內建的）：

```bash
go build -tags production -ldflags "-H windowsgui" -o claude-miniapp-desktop.exe ./cmd/desktop
```

exe 要跟 `config.yaml`、`internal/static/` 放在一起（與伺服器版相同）。在專案根目錄開發時，`go run ./cmd/desktop` 會用目前工作目錄的設定。`-tags production` 會把前端嵌入執行檔，並關掉 Wails 的 debug log。桌面程式內更新需要此標籤，發佈版一律使用。`go run ./cmd/desktop` 維持開發模式，讀取磁碟上的 `internal/static/`。

前端檔案是直接從磁碟讀取，改 `internal/static/` 只需重新載入（F5），不必重新編譯。若改了仍沒生效，到 設定 → 一般 →「清除快取並重新載入」。

### 桌面版自動更新

Windows 桌面版可以查詢 GitHub Release 並**自行更新**，只會替換 `claude-miniapp-desktop.exe`。

- **先手動升級一次。** 沒有更新功能的舊版無法自我更新。請下載新版 `claude-miniapp-v<X.Y.Z>-windows-amd64.zip`，把現有資料夾中的 `claude-miniapp-desktop.exe` 換成新檔，保留 `config.yaml` 與資料。之後的版本可在程式內更新。
- **檢查：** 系統匣 →「檢查更新」，或 設定 → 桌面更新。程式會在啟動時與之後每 6 小時檢查一次，只顯示結果，不會自行下載或重啟。
- **下載與重啟分開確認。** 按「下載」才會開始下載；「重新啟動並更新」同樣需要點擊，工作忙碌時程式會拒絕。
- **忙碌時拒絕重啟：** 只要有任一 session 正在執行任務、執行 shell 指令、有待授權項目，或**佇列有項目（即使暫停）**，重啟會被拒絕（`update_busy`）。請先完成工作或清空佇列再試；已下載的更新會保留。
- **需要可寫入的安裝目錄：** `claude-miniapp-desktop.exe` 所在資料夾必須對目前使用者可寫入；更新程式不會要求系統管理員權限。
- **保留不動的內容：** `config.yaml`、`claude-miniapp.db`、`workspace/`（附件）、`logs/`、`internal/static/` 都不會被更新動作改動，只替換 `claude-miniapp-desktop.exe`。
- **殘留的舊版 exe：** Windows 無法刪除執行中的 exe，所以更新時會把舊版改名成 `<名稱>.exe.old.<數字>` 放在一旁。程式下次啟動時會把這類殘留檔**全部刪除**，所以**不會保留還原檔**；想要能回退，請在更新前自己備份 exe（見下方）。
- **不會自動更新：** Windows server（`claude-miniapp.exe`）與 Linux 版本，請手動更新。
- **手動恢復舊版：** 升級前請先保留一份目前 EXE 的備份。要回退時，關閉程式後把備份覆寫回去：

```powershell
Copy-Item .\claude-miniapp-desktop.exe .\claude-miniapp-desktop.previous.exe
# 要回退時：先關閉程式，再執行
Copy-Item .\claude-miniapp-desktop.previous.exe .\claude-miniapp-desktop.exe -Force
```

  只會動到 EXE 檔，資料不會自動降版。
- **完整性：** 下載檔會以 `checksums.txt` 中的 SHA256 比對，確認檔案與本次發佈一致。這**不能**證明發佈者身分，因為目前沒有簽章驗證。
- **網路：** 檢查與下載需要連到 GitHub；更新不代表程式可以離線使用。
