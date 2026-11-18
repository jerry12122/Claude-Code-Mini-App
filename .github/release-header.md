## 下載

| 平台 | 檔案 | 啟動 |
|---|---|---|
| Windows x64（桌面版視窗＋系統匣，推薦） | `claude-miniapp-@TAG@-windows-amd64-setup.exe` | 安裝目前使用者後從開始功能表啟動，不需管理員權限 |
| Windows（便攜桌面／純 server） | `claude-miniapp-@TAG@-windows-amd64.zip` | 完整解壓後執行 `claude-miniapp-desktop.exe` 或 `claude-miniapp.exe`，二選一 |
| Linux（純 server） | `claude-miniapp-@TAG@-linux-amd64.tar.gz` | `./claude-miniapp` |
| Windows 桌面 EXE（與 Setup／ZIP 內相同，供程式內更新下載用） | `claude-miniapp-desktop-@TAG@-windows-amd64.exe` | 一般桌面安裝請用 Setup |

Setup 程式位於 `%LOCALAPPDATA%\Programs\ClaudeMiniApp`，資料位於 `%LOCALAPPDATA%\ClaudeMiniApp\data`，WebView2 profile 為其中的 `webview2`；解除安裝保留資料。提供開始功能表項目與選用桌面捷徑。重裝／解除安裝前先完成工作並從系統匣「結束」，關閉視窗只會隱藏；執行中或檔案鎖定時會拒絕維護，不會強制關閉。Setup 拒絕降版及覆蓋無法判定版本的既有 EXE。便攜版請依[手動遷移指引](https://github.com/jerry12122/Claude-Code-Mini-App/blob/@TAG@/docs/getting-started.zh-TW.md#便攜版遷移至安裝版)，在首次啟動前備份並搬移資料。

## 桌面版應用程式內更新（Windows）

- **首次需手動升級一次**：舊版（沒有更新功能）無法自我更新。下載本版 `claude-miniapp-@TAG@-windows-amd64.zip`，把舊資料夾中的 `claude-miniapp-desktop.exe` 換成新檔即可，`config.yaml`、資料庫與其他資料保持原位。之後的版本可在程式內更新。
- **檢查**：系統匣「檢查更新」或 設定 → 桌面更新。程式會在啟動時與之後每小時檢查一次，只顯示結果，不會自行下載或重啟。
- **下載與重啟分開確認**：按「下載」才會開始下載；按「重新啟動並更新」才會替換並重啟，不會自動重啟。
- **忙碌時拒絕重啟**：只要有工作進行中（執行中的任務、shell 指令、待授權項目）或**佇列有項目（即使暫停）**，重啟會被拒絕（`update_busy`）。請先完成工作或清空佇列再試；已下載的更新會保留。
- **需要可寫入的安裝目錄**：`claude-miniapp-desktop.exe` 所在資料夾必須對目前使用者可寫入；更新程式不會要求系統管理員權限。
- **保留的內容**：只替換 `claude-miniapp-desktop.exe`，保留安裝模式標記、設定、資料庫、附件、日誌與 WebView2 profile；安裝版仍沿用同一資料目錄。
- **不會自動更新**：Windows server（`claude-miniapp.exe`）與 Linux 版本，請手動更新。
- **回退注意**：升級前完整停止程式並備份 EXE、設定、資料庫及附件（含 SQLite 殘留 WAL）。已升級的資料庫不可交給舊 EXE，需一併還原對應的升級前資料庫備份；資料不會自動降版。
- **完整性**：下載檔會以 `checksums.txt` 中的 SHA256 比對，確認檔案與本次發佈一致；這不等於驗證發佈者身分（目前沒有簽章驗證）。
- **網路**：檢查與下載需要連到 GitHub；更新不代表程式可以離線使用。

## 首次設定

完整教學：[繁體中文](https://github.com/jerry12122/Claude-Code-Mini-App/blob/@TAG@/docs/getting-started.zh-TW.md) · [English](https://github.com/jerry12122/Claude-Code-Mini-App/blob/@TAG@/docs/getting-started.md)。ZIP 解壓後也可直接開啟 `docs/getting-started.zh-TW.md`。

1. **執行 Setup 或解壓 ZIP**：server 需完整解壓並保留旁邊的 `internal/static/`；桌面 EXE 已內嵌前端，可單獨執行，既有便攜設定與資料保留原位。Setup 安裝後從開始功能表啟動。
2. 在同一台機器、同一使用者下安裝並登入一個代理（`claude`、`codex`、`kiro-cli`、`cursor-agent`、`agy`），先在終端機確認可以對話。
3. 不必先建立 `config.yaml`，首次啟動會顯示設定精靈，協助設定密碼與檢查 CLI。Telegram Mini App 預設關閉；只用桌面／本機網頁不需要 Bot Token 或網域。已有設定檔會直接沿用。
4. 桌面與 server 二選一啟動。桌面版自動開啟精靈；server 版在主機開啟日誌中含一次性憑證的完整設定網址。儲存後輸入設定的密碼，再建立 Session，填主機上的工作目錄並送測試訊息。變更連接埠時依畫面提示重啟。

`no_auth: true` 僅供已隔離的本機開發環境：它不會限制 server 只監聽 localhost，不可搭配公開 Tunnel。

> **Windows 注意**
> - 應用程式與 Setup 未簽章；先確認來自本專案 Release，SmartScreen 若跳「Windows 已保護您的電腦」→ 點「其他資訊」→「仍要執行」。
> - 桌面版需要 [WebView2 Runtime](https://developer.microsoft.com/microsoft-edge/webview2/)。Setup 缺少時會執行隨包附帶、經 Microsoft 簽章的 bootstrapper，需連網下載 Runtime；離線且缺 Runtime 時無法完成，請先從微軟頁面安裝後重試。便攜版需自行安裝。
> - 桌面版關閉視窗只會縮到系統匣，要結束請在匣圖示按右鍵 →「結束」（或 Ctrl+Q）。
> - 驗證檔案完整性：`checksums.txt` 內有 SHA256。
