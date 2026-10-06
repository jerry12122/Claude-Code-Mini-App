# 功能詳述

[← 回 README](../README.zh-TW.md) · [English](features.md)

## 代理與 Session

- **多代理** — Claude Code、Cursor Agent、Codex、Kiro ACP（透過 Agent Client Protocol 提供互動式授權提示）；依 Session 選擇。既有的 Kiro CLI Session 仍可執行，新建請改用 Kiro ACP（Gemini / Antigravity 因 headless 限制暫停）。模型清單於啟動時向各 CLI 取得（含 Claude），也可在 設定 → 一般 不重啟直接重新抓取
- **Session 管理** — 多對話、各自綁定代理、`work_dir` 與權限模式
- **權限流程** — 顯示完整指令／檔案內容；Claude prompt-tool 與 Kiro ACP 可允許單次操作。舊 Claude 遭拒後選「允許工具並重試」，會在本次重試放行列出的工具；編輯類工具另可允許並自動允許編輯
- **用量徽章** — Session header 顯示帳戶用量（如 Claude `5h 16% · Week 9%`）
- **未讀追蹤** — 列表標示有新動態的 Session，可一鍵「全部標為已讀」。列表透過 `/events` WebSocket 即時更新（輪詢 30 秒作為保底）；分頁標題顯示未讀數，其他 Session 完成或待授權時會跳 toast
- **快速跳轉** — Ctrl/Cmd+P 開啟類 VS Code 的跳轉面板，可依名稱、目錄、分支搜尋並切換 Session
- **訊息佇列** — 執行中送出的訊息會排隊（存 DB、重啟不遺失）依序執行；失敗／中斷時暫停，可手動繼續或移除

## 聊天

- **即時串流** — WebSocket 對話與 Markdown 串流；多分頁同步
- **程式碼區塊** — 語法高亮 + 語言標籤，一鍵複製
- **附件上傳** — 📎 上傳或貼上圖片／文字檔，在輸入框上方以可移除的 chip 顯示（圖片有縮圖），以路徑交給 agent 讀取（存於 `workspace/uploads/`，不寫入專案 `work_dir`）；桌面版支援拖放
- **訊息操作** — 複製／轉發按鈕常駐在每則訊息的時間旁；長按訊息開啟浮動選單（複製，非自己的訊息另有轉發）
- **聊天體驗** — 輸入法選字的 Enter 不會誤送出；觸控裝置 Enter 為換行；往上翻舊訊息時不會被串流拉回底部（附「跳到最新」按鈕）；刪除 Session 後 5 秒內可復原

## 協作與整合

- **臨時共享** — 以連結 + 6 位數 PIN + 暱稱把 Session 分享給朋友（預設 1 小時，最長 7 天）。訪客用任何瀏覽器加入，角色為唯讀 `viewer` 或完整權限 `editor`；訊息會標示說話者，結束分享即刻踢出所有訪客
- **MCP server** — 透過 Streamable HTTP（`POST /mcp`）讓其他 agent 操作 session、讀聊天紀錄、查跨 session 活動；`ask_session` 同步詢問另一個 session 並取回答覆，互問有跳數上限（`mcp_max_hops`）防止無限迴圈（預設關閉）
- **驗證** — Telegram `initData` + 白名單；可選內網密碼登入。`web.trusted_proxies` 預設只信任同機 loopback 代理的 IP 標頭；其他代理須設定精確 CIDR，空清單則停用標頭解析
- **選用 Shell** — 於 `work_dir` 執行指令（預設關閉）；開啟後會在會話 header 顯示「開啟 VSCode／開啟目錄」按鈕（僅桌面版）
- **日誌檢視** — 設定 → 日誌即時串流伺服器日誌（等級篩選、搜尋、暫停、複製；可在執行期切換 Debug）。桌面版沒有 console 時很好用；只顯示本次啟動後的日誌，完整紀錄仍在 `logs/server.log`

## 桌面視窗（Windows）

同一個行程多開一個視窗，顯示現有網頁，並繼續聽 port。瀏覽器與 Telegram Mini App 照舊連線。關視窗只會隱藏，系統匣留著：左鍵叫回視窗，右鍵選「開啟視窗」或「結束」。Ctrl+Q 也會結束。F5／Ctrl+R 重新載入畫面，F12 開 DevTools（加 `-tags production` 時無作用）。

Win11 已內建 WebView2。`go build ./cmd/server` 維持純 Go、不需 CGO。桌面版用 Wails v3（系統匣是框架內建的）：

```bash
go build -ldflags "-H windowsgui" -o claude-miniapp-desktop.exe ./cmd/desktop
```

exe 要跟 `config.yaml`、`internal/static/` 放在一起（與伺服器版相同）。在專案根目錄開發時，`go run ./cmd/desktop` 會用目前工作目錄的設定。加 `-tags production` 會關掉 Wails 的 debug log。

前端檔案是直接從磁碟讀取，改 `internal/static/` 只需重新載入（F5），不必重新編譯。若改了仍沒生效，到 設定 → 一般 →「清除快取並重新載入」。
