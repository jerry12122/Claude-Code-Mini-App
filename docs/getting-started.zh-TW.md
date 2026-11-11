# 從零開始使用

[← README](../README.zh-TW.md) · [English](getting-started.md)

完成這份教學後，你會在自己的電腦上啟動控制台，建立一個 Session，收到代理的第一則回覆。先完成本機使用，再選擇是否接 Telegram。

## 1. 下載控制台

到 [Releases](https://github.com/jerry12122/Claude-Code-Mini-App/releases/latest) 的 Assets 下載：

| 電腦 | 檔案 | 啟動方式 |
|---|---|---|
| Windows x64 | `claude-miniapp-v版本-windows-amd64.zip` | 桌面版：`claude-miniapp-desktop.exe`；純伺服器：`claude-miniapp.exe`，二選一 |
| Linux x64 | `claude-miniapp-v版本-linux-amd64.tar.gz` | 在解壓目錄執行 `./claude-miniapp` |
| 其他平台／架構 | 沒有對應的預編譯包 | 見下方[從原始碼建置](#從原始碼建置)，CLI 本身也須支援該環境 |

不要選 GitHub 自動產生的「Source code」壓縮檔，除非要自行建置。解壓**整個資料夾**到可寫入的位置，保留 `internal/static/`、`config.example.yaml` 與執行檔的相對位置。使用下載版不需要安裝 Go 或 Node.js；你選的代理 CLI 可能另有需求。

Windows 桌面版需要 [WebView2 Runtime](https://developer.microsoft.com/microsoft-edge/webview2/)。執行檔未簽章，若 SmartScreen 攔截，先確認檔案來自本專案 Release，再點「其他資訊 → 仍要執行」。

## 2. 安裝並登入一個代理

**只需安裝你要使用的那一個。** 在執行控制台的電腦、同一個作業系統使用者帳號下安裝及登入；手機只負責操作畫面。代理帳號需具備對應服務的使用權限與額度。

| 選單中的代理 | 官方安裝／登入說明 | 控制台需要找到的命令 |
|---|---|---|
| Claude | [Claude Code 快速開始](https://code.claude.com/docs/en/quickstart) | `claude` |
| Codex | [Codex CLI](https://developers.openai.com/codex/cli/) | `codex` |
| Cursor | [Cursor CLI 安裝](https://cursor.com/docs/cli/installation) | `cursor-agent` |
| Kiro ACP | [Kiro CLI](https://kiro.dev/docs/cli/) | `kiro-cli` |
| Antigravity | [Antigravity CLI](https://antigravity.google/docs/overview?tab=cli) | `agy` |

例如，選 Claude 時依官方文件安裝：

Windows PowerShell：

```powershell
irm https://claude.ai/install.ps1 | iex
```

Linux／macOS：

```bash
curl -fsSL https://claude.ai/install.sh | bash
```

安裝後開啟新的終端機，執行 `claude --version`，再執行 `claude`，依提示完成登入並送一句「只回覆 OK，不要使用工具」。收到回覆後退出 CLI。其他代理同樣先依官方教學登入、在終端機完成一次對話；例如 Codex 第一次執行 `codex` 時會提供登入選項。

確認命令位置：Windows 用 `Get-Command claude`；Linux／macOS 用 `command -v claude`。選其他代理時替換命令名稱。找不到時先依安裝程式提示修正 PATH，再重新開啟控制台。

**Cursor 注意：** 官方目前使用 `agent` 命令，但本專案仍呼叫 `cursor-agent`。必須確認 `Get-Command cursor-agent`／`command -v cursor-agent` 能找到可執行檔；只有 `agent` 或 PowerShell alias 不代表此整合可用。若安裝版本未提供相容命令，先使用其他代理。

Windows 控制台不能直接使用只安裝在 WSL 裡的 CLI。若 CLI 裝在 WSL，請在同一個 WSL 環境執行 Linux server；工作目錄也使用該環境的路徑。

## 3. 建立設定檔

首次啟動時不必先建立 `config.yaml`，程式會開啟設定精靈；已有設定檔就直接沿用，絕不覆寫。依下方第 4 步啟動：桌面視窗會自動顯示精靈；純 server 版請開啟終端機或 `logs/server.log` 印出的完整本機設定網址（包含 `?setup=…`）。

精靈依序引導：

1. **本機 CLI 檢查：** 顯示已安裝的代理與版本；找不到時提供官方安裝連結，仍可完成設定。
2. **登入與儲存：** 輸入至少 8 個字元的密碼，或產生隨機密碼並妥善保存；空白與 `change-me` 不可使用。沒有特殊需求就保留連接埠 `8080`、資料庫路徑 `./claude-miniapp.db`；資料庫目錄必須已存在且可寫入。「允許區網裝置登入」預設關閉。
3. **Telegram Mini App：** 預設關閉。只用桌面版或瀏覽器不需要 Bot Token、網域或 Tunnel；有需要才開啟，填入 BotFather Token 與數字 Telegram User ID。Telegram 需要外部可連入的 HTTPS 網址，精靈不會建立 Tunnel。
4. **確認並儲存：** 在程式工作目錄寫入最小必要的 `config.yaml`（發布包的桌面版會寫在 exe 旁）。連接埠未變時直接切到密碼登入；有變更則完整結束、重啟程式，再開啟畫面顯示的新網址。

設定 API 只接受帶有一次性設定憑證的本機連線。頁面讀到憑證後會從網址列移除；若儲存前重整，請重新開啟日誌中的完整網址。每次啟動憑證都會變更。完成後精靈與儲存 API 不提供編輯功能；之後需手動修改 `config.yaml` 再重啟。

**遠端 Linux server：** 在自己的電腦轉發相同連接埠到 server：

```bash
ssh -N -L 8080:127.0.0.1:8080 user@your-server
```

保持 SSH 執行，在自己電腦的瀏覽器開啟遠端 server 日誌中的完整 `http://127.0.0.1:8080/?setup=…` 網址。自己的 8080 埠必須空閒；Host 檢查要求轉發前後連接埠相同。首次設定不能使用區網網址或公開 Tunnel。

**檔案權限：** 設定檔以明文保存密碼與 Bot Token。Unix 寫入權限為 `0600`；Windows 繼承資料夾權限，請放在私人且可寫入的資料夾。儲存需要支援硬連結的檔案系統（例如 NTFS）；若隨身碟寫入失敗，請移到支援且可寫入的磁碟後重試。

### 手動設定（選用）

也可略過精靈，在程式目錄建立以下 `config.yaml`，替換範例密碼。這份設定允許本機密碼登入，不需要 Telegram：

```yaml
no_auth: false
web:
  password: "換成你自己的長密碼"
  allowed_cidrs:
    - "127.0.0.0/8"
    - "::1/128"
server:
  port: 8080
db:
  path: "./claude-miniapp.db"
```

桌面視窗也使用同一個密碼。進階選項見[設定範本](../config.example.yaml)。精靈不提供 `no_auth`；正常使用請保留登入驗證。

## 4. 啟動與登入

**Windows 桌面版：** 雙擊 `claude-miniapp-desktop.exe`，視窗會自動開啟。首次啟動先完成精靈；已有設定時輸入 `web.password`。

**Windows 純 server：** 在解壓資料夾開啟 PowerShell，執行：

```powershell
.\claude-miniapp.exe
```

**Linux server：** 在解壓資料夾開啟終端機，執行：

```bash
./claude-miniapp
```

server 啟動後保持終端機開著，在**同一台電腦**首次開啟日誌中含憑證的設定網址；完成後開啟 <http://127.0.0.1:8080>，輸入設定的密碼。不要同時啟動桌面版與 server 版占用同一個 port；桌面版已包含 server，也可用瀏覽器連入。

桌面版按 × 只會縮到系統匣；要關閉或套用修改後的設定，使用系統匣「結束」或 Ctrl+Q，再重開。server 用 Ctrl+C 結束。修改 `config.yaml` 後需要重新啟動。

## 5. 建立第一個 Session

1. 點側邊欄的 **＋**，開啟「新建 Session」。
2. 選擇第 2 步已安裝、登入並測試成功的代理。
3. 工作目錄填**執行 server 那台電腦上、已存在的專案資料夾絕對路徑**，例如 Windows 的 `C:/Users/你的帳號/Projects/demo` 或 Linux 的 `/home/你的帳號/projects/demo`。不是手機上的路徑，也不是 GitHub 網址；若尚無專案，先建立一個空資料夾。
4. 名稱可留空；權限維持預設，模型與 Effort 先留空，使用 CLI 自己的預設值。
5. 輸入「只回覆 OK，不要使用工具，也不要修改檔案」，按「建立並送出」。這是正常的模型請求，會使用代理帳號的額度。
6. **成功標準：** 畫面出現代理回覆，執行狀態結束，能再送第二則訊息。

若代理要求授權，先查看操作內容再決定是否允許，不需要為了跑通測試開啟全自動權限。沒有回覆時看下方[問題排解](#問題排解)。

Claude 顯示「需要你的回答」時，請按題目單選、多選或填寫「其他回答」，答完每題後按「送出回答」。這是需求確認，狀態會顯示「待回答」；送出後 Claude 會繼續原本的任務。重新連線可恢復尚未回答的問題，若不想繼續可按「中斷」。此功能不需要設定 `mcp_token`；重新啟動 server 會結束尚在等待的問答，屆時請重新提問。

模型選單會在背景載入。若只有「預設」，先等啟動抓取完成，或到「設定 → 一般」重新抓取，再重新開啟對話。沒有安裝的代理抓取失敗不影響你已安裝的代理。

## 6. 選用：從 Telegram 開啟

先完成第 5 步。本節保留 **`no_auth: false`**，並讓同一台電腦上的 Tunnel 將公開 HTTPS 網址轉到本機 server。

### 建立 Bot（尚未設定時）

到 [@BotFather](https://t.me/BotFather) 送出 `/newbot`，依[官方建立步驟](https://core.telegram.org/bots/tutorial#obtain-your-bot-token)取得 Token。將 `bot_token: "你的 Token"` 加入 `config.yaml`；已在精靈填入就不必重建 Bot。

### 填入自己的 Telegram ID

取得自己的數字 User ID（設定範本使用 [@userinfobot](https://t.me/userinfobot)，這是第三方 Bot，僅用來查自己的 ID，不要提供 Token 或密碼）。將設定中的空白名單替換為自己的 ID：

```yaml
whitelist_tg_ids:
  - 123456789 # 換成你自己的數字 ID，不是 @username，也不是 Bot 的 ID
```

重新啟動控制台。到**你剛建立的 Bot 私聊**按「開始」或送 `/start`，讓 Bot 可以傳通知給你；本專案沒有一般聊天 Bot 的指令回覆，所以沒有歡迎訊息是正常的。白名單只有一人時，網頁登入會自動綁定該人的通知；已有登入請登出再登入。預設只推播錯誤，不是每次成功都推播。

### 取得 HTTPS 網址

試用可使用 [Cloudflare Quick Tunnel](https://developers.cloudflare.com/tunnel/get-started/quick-tunnels/)，不需要自己的網域。開啟 [cloudflared 官方下載頁](https://developers.cloudflare.com/tunnel/downloads/)。Windows 選 Windows → Executable → 64-bit，將 `cloudflared-windows-amd64.exe` 存入一個資料夾，在該資料夾另開 PowerShell，保持控制台運行並執行：

```powershell
.\cloudflared-windows-amd64.exe tunnel --url http://127.0.0.1:8080
```

Linux／macOS 依同頁安裝 `cloudflared`，保持控制台運行，另開終端機執行：

```bash
cloudflared tunnel --url http://127.0.0.1:8080
```

複製輸出的 `https://隨機名稱.trycloudflare.com`。保持這個終端機開著；Quick Tunnel 是測試用途，重建後網址會改變。長期使用改採[固定主機名稱的 Tunnel](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/get-started/create-remote-tunnel/)，將相同的本機服務設為來源。

### 設定 Telegram 開啟按鈕

1. 在 @BotFather 送 `/setmenubutton`，選擇自己的 Bot。
2. 依提示填入剛取得的 HTTPS 網址與按鈕文字，例如「開啟控制台」。[官方選單按鈕說明](https://core.telegram.org/bots/webapps#launching-mini-apps-from-the-menu-button)。
3. 回到該 Bot 的私聊，點選單按鈕，**在 Telegram 內開啟**。
4. 應直接看到和電腦相同的 Session；送第二則測試訊息確認可收到回覆。

直接把公開網址貼進一般瀏覽器不會帶 Telegram 身分；上述設定會拒絕非本機的密碼登入，這是預期行為。不要為此把 `allowed_cidrs` 改成允許全網。Tunnel 與 server 同機時沿用上述 `trusted_proxies`；跨機器代理需設定代理的精確來源 CIDR。更換 Tunnel 網址後需重新執行 `/setmenubutton`。

## 問題排解

| 現象 | 先做什麼 |
|---|---|
| 沒有出現設定精靈／靜態頁 404 | 已有 `config.yaml` 時不會出現精靈；首次使用請從完整解壓資料夾啟動並開啟日誌的設定網址，確認檔名不是 `.yaml.txt` |
| 缺少 `bot_token` 或 `web.password` | 本機使用只需有效的網頁密碼（至少 8 字元、不可為 `change-me`）；開啟 Telegram 才需要真實 Bot Token |
| 密碼錯誤／仍要求登入 | 輸入 `web.password`，修改設定後完整退出並重啟；桌面版也需要登入 |
| `僅允許內網存取`／403 | 先在主機用 `127.0.0.1`；區網使用者需把自己的可信網段加入 `web.allowed_cidrs`，不要直接允許全網；Telegram 則確認從 Bot 選單開啟且 ID 在白名單 |
| 8080 被占用 | 關掉先前的 server 或系統匣中的桌面版；或修改 `server.port`，並一併更換瀏覽器與 Tunnel 的 port |
| `executable file not found`／找不到 CLI | 在相同使用者及環境用 `Get-Command`／`command -v` 查第 2 步的命令；修好 PATH 後重啟控制台。Cursor 需要 `cursor-agent` |
| 未登入／額度不足 | 回到同一使用者的終端機，先讓該 CLI 獨立完成一次對話；控制台不會代你登入或購買額度 |
| 找不到工作目錄 | 填主機上已存在、該使用者可存取的絕對路徑；Windows 與 WSL 路徑不可混用 |
| 模型清單沒有選項 | 設定 → 一般 → 重新抓取，再重新開啟對話；未安裝的代理報錯可忽略，已安裝的代理報錯請查看日誌 |
| 空白頁／樣式不完整 | 前端需要連到 React、Babel、Tailwind 等 CDN；檢查網路或阻擋規則，再重新載入。這不是完整離線包 |
| Telegram 開不起來／502 | 確認主機未休眠、server 與 Tunnel 都在運作，Bot 選單仍使用目前的 HTTPS 網址 |
| Telegram 驗證失敗 | 確認 Token 屬於選單所在的 Bot、白名單是你自己的 User ID，關掉 Mini App 再從 Bot 選單重開 |
| 沒有 Telegram 推播 | 先對自己的 Bot 按開始；確認通知 ID／白名單及 `notify` 設定。正常成功回覆預設不會推播 |

查看「設定 → 日誌」；若畫面打不開，在程式資料夾找 `logs/server.log`，或在終端機啟動純 server 查看錯誤。回報問題時附平台、代理、重現步驟與錯誤片段，遮掉 Token、密碼及私人內容。

## 從原始碼建置

需要 [Git](https://git-scm.com/downloads) 與 [Go 1.26.11 或更新版本](https://go.dev/dl/)。先確認 `git --version` 和 `go version` 可執行。

```bash
git clone https://github.com/jerry12122/Claude-Code-Mini-App
cd Claude-Code-Mini-App
```

Windows PowerShell：

```powershell
# 不必先建立 config.yaml，首次執行依第 3 步完成精靈；選擇一種建置方式
go build -o claude-miniapp.exe ./cmd/server
.\claude-miniapp.exe
```

Windows 桌面版改用：

```powershell
go build -tags production -ldflags "-H windowsgui" -o claude-miniapp-desktop.exe ./cmd/desktop
.\claude-miniapp-desktop.exe
```

Linux／macOS 純 server：

```bash
# 不必先建立 config.yaml，首次執行依第 3 步完成精靈
go build -o claude-miniapp ./cmd/server
./claude-miniapp
```

建置完成後接第 4、5 步。首次建置需下載 Go 依賴；不需要另外建置前端。macOS 沒有本專案的桌面視窗版本。

## 下次啟動與更新

從同一個程式資料夾啟動即可保留 Session。更新前完整停止程式，備份 `config.yaml`、資料庫（預設 `claude-miniapp.db`）與 `workspace/`，再更換執行檔及整份 `internal/static/`。不要以範例設定覆蓋自己的設定；若要還原／搬遷代理本身的會話，還需要保留該代理的本機資料。更多功能見[功能詳述](features.zh-TW.md)。
