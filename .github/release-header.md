## 下載

| 平台 | 檔案 | 啟動 |
|---|---|---|
| Windows（桌面版視窗＋系統匣，推薦） | `claude-miniapp-@TAG@-windows-amd64.zip` | 雙擊 `claude-miniapp-desktop.exe` |
| Windows（純 server，用瀏覽器／Telegram 連） | 同上 zip | 執行 `claude-miniapp.exe` |
| Linux（純 server） | `claude-miniapp-@TAG@-linux-amd64.tar.gz` | `./claude-miniapp` |
| Windows 桌面 EXE（與 zip 內相同，供程式內更新下載用） | `claude-miniapp-desktop-@TAG@-windows-amd64.exe` | 只替換舊資料夾中的 `claude-miniapp-desktop.exe`，一般安裝請用 zip |

## 桌面版應用程式內更新（Windows）

- **首次需手動升級一次**：舊版（沒有更新功能）無法自我更新。下載本版 `claude-miniapp-@TAG@-windows-amd64.zip`，把舊資料夾中的 `claude-miniapp-desktop.exe` 換成新檔即可，`config.yaml`、資料庫與其他資料保持原位。之後的版本可在程式內更新。
- **檢查**：系統匣「檢查更新」或 設定 → 桌面更新。程式會在啟動時與之後每 6 小時檢查一次，只顯示結果，不會自行下載或重啟。
- **下載與重啟分開確認**：按「下載」才會開始下載；按「重新啟動並更新」才會替換並重啟，不會自動重啟。
- **忙碌時拒絕重啟**：只要有工作進行中（執行中的任務、shell 指令、待授權項目）或**佇列有項目（即使暫停）**，重啟會被拒絕（`update_busy`）。請先完成工作或清空佇列再試；已下載的更新會保留。
- **需要可寫入的安裝目錄**：`claude-miniapp-desktop.exe` 所在資料夾必須對目前使用者可寫入；更新程式不會要求系統管理員權限。
- **保留的內容**：`config.yaml`、`claude-miniapp.db`、`workspace/`（附件）、`logs/`、`internal/static/` 都不會被更新動作改動，只替換 `claude-miniapp-desktop.exe`。
- **不會自動更新**：Windows server（`claude-miniapp.exe`）與 Linux 版本，請手動更新。
- **手動恢復舊版**：升級前請先保留一份目前 EXE 的備份（例如複製為 `claude-miniapp-desktop.previous.exe`）。要回退時，關閉程式後把備份覆寫回 `claude-miniapp-desktop.exe`。資料不會自動降版。
- **需要可寫入的安裝目錄**：`claude-miniapp-desktop.exe` 所在資料夾必須對目前使用者可寫入；更新程式不會要求系統管理員權限。
- **保留的內容**：`config.yaml`、`claude-miniapp.db`、`workspace/`（附件）、`logs/`、`internal/static/` 都不會被更新動作改動，只替換 `claude-miniapp-desktop.exe`。
- **不會自動更新**：Windows server（`claude-miniapp.exe`）與 Linux 版本，請手動更新。
- **手動恢復舊版**：升級前請先備份目前的 EXE（例如複製為 `claude-miniapp-desktop.previous.exe`）。要回退時，關閉程式後把該備份覆寫回 `claude-miniapp-desktop.exe`。資料不會自動降版。
- **完整性**：下載檔會以 `checksums.txt` 中的 SHA256 比對，確認檔案與本次發佈一致；這不等於驗證發佈者身分（目前沒有簽章驗證）。
- **網路**：檢查與下載需要連到 GitHub；更新不代表程式可以離線使用。

## 首次設定

完整教學：[繁體中文](https://github.com/jerry12122/Claude-Code-Mini-App/blob/@TAG@/docs/getting-started.zh-TW.md) · [English](https://github.com/jerry12122/Claude-Code-Mini-App/blob/@TAG@/docs/getting-started.md)。解壓後也可直接開啟 `docs/getting-started.zh-TW.md`。

1. **解壓整個資料夾**：執行檔必須與 `internal/static/`（前端）放在一起，不能只搬 exe。
2. 在同一台機器、同一使用者下安裝並登入一個代理（`claude`、`codex`、`kiro-cli`、`cursor-agent`、`agy`），先在終端機確認可以對話。
3. 不必先建立 `config.yaml`，首次啟動會顯示設定精靈，協助設定密碼與檢查 CLI。Telegram Mini App 預設關閉；只用桌面／本機網頁不需要 Bot Token 或網域。已有設定檔會直接沿用。
4. 桌面與 server 二選一啟動。桌面版自動開啟精靈；server 版在主機開啟日誌中含一次性憑證的完整設定網址。儲存後輸入設定的密碼，再建立 Session，填主機上的工作目錄並送測試訊息。變更連接埠時依畫面提示重啟。

`no_auth: true` 僅供已隔離的本機開發環境：它不會限制 server 只監聽 localhost，不可搭配公開 Tunnel。

> **Windows 注意**
> - 執行檔未簽章，SmartScreen 可能跳「Windows 已保護您的電腦」→ 點「其他資訊」→「仍要執行」。
> - 桌面版需要 WebView2 Runtime：Windows 11 內建，Windows 10 請先[安裝](https://developer.microsoft.com/microsoft-edge/webview2/)。
> - 桌面版關閉視窗只會縮到系統匣，要結束請在匣圖示按右鍵 →「結束」（或 Ctrl+Q）。
> - 驗證檔案完整性：`checksums.txt` 內有 SHA256。
