# Claude Code Mini App

> 自架的**多代理（multi-agent）控制台**，統一管理 Claude Code、Cursor Agent、Codex 與 Kiro。可從桌面視窗、任何瀏覽器，或手機上的 Telegram Mini App 操作。**單一 Go 二進位**，無需獨立前端建置。

[![Version](https://img.shields.io/badge/version-0.8.0-blue)](#) [![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE) [![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)](#)

[English](README.md)

![Agent Console：左側依專案分組的 Claude、Codex、Kiro、Cursor 會話，右側為串流對話](docs/images/console.png)

<sub>示範資料，取自臨時啟動的實例。</sub>

代理程式仍然跑在你放程式碼的那台機器上，本專案是它們前面的控制台。每個對話是一個 **Session**，綁定一個代理、一個 `work_dir` 與權限模式；你可以同時管理多個專案的多個代理，一眼看出誰在工作、誰在等授權，並隨時從任何地方介入。

| 入口 | 適用情境 | 驗證 |
|---|---|---|
| 🖥️ **桌面視窗**（Windows） | 在電腦前日常使用——系統匣、Ctrl+P 快速跳轉 | 本機 |
| 🌐 **瀏覽器** | 區網內任何裝置，或透過反向代理／Tunnel 遠端連線 | 網頁密碼（限 `web.allowed_cidrs` 內的 IP） |
| 📱 **Telegram Mini App** | 離開座位時——出錯推播、手機上即時授權 | Telegram `initData` + 白名單 |

三個入口連同一個伺服器，即時看到同樣的 Session。

## 🚀 快速開始

**需求：** 機器上已安裝並登入要用的 CLI（`claude`、`cursor agent`、`codex`、`kiro-cli` 等），以及 Telegram Bot Token（[@BotFather](https://t.me/BotFather)）——僅限本機使用時可改設 `no_auth: true`。

**下載（建議）：** 到 [Releases](https://github.com/jerry12122/Claude-Code-Mini-App/releases/latest) 下載 zip／tar.gz，整個資料夾解壓（執行檔旁必須有 `internal/static/`），把 `config.example.yaml` 複製成 `config.yaml`，再執行 `claude-miniapp-desktop.exe`（Windows 桌面版）或 `claude-miniapp`（純 server）。

> [!NOTE]
> Windows 執行檔未簽章，SmartScreen 可能跳警告：點「**其他資訊 → 仍要執行**」。桌面視窗需要 WebView2（Windows 11 已內建）。

**從原始碼建置**（Go 1.25+）：

```bash
git clone https://github.com/jerry12122/Claude-Code-Mini-App
cd Claude-Code-Mini-App
cp config.example.yaml config.yaml   # 填 bot_token、whitelist_tg_ids
go build -o claude-miniapp ./cmd/server && ./claude-miniapp   # → http://localhost:8080
```

Windows 也可建置桌面視窗：`go build -ldflags "-H windowsgui" -o claude-miniapp-desktop.exe ./cmd/desktop`。關視窗只會縮到系統匣，從匣選單或 Ctrl+Q 結束。建置細節與快捷鍵見[桌面視窗](docs/features.zh-TW.md#桌面視窗windows)。

## ✨ 功能

- **多代理** — Claude Code、Cursor Agent、Codex、Kiro ACP，依 Session 選擇。工具授權前會顯示完整指令或檔案內容
- **即時對話** — WebSocket 串流、Markdown、語法高亮程式碼與一鍵複製；多分頁同步
- **Session 管理** — 各自綁定 `work_dir` 與權限模式，即時的未讀與待授權標記、Ctrl/Cmd+P 快速跳轉，header 顯示帳戶用量
- **訊息佇列** — 執行中送出的訊息會排隊、重啟不遺失，失敗或中斷時暫停
- **附件** — 貼上或拖放圖片與檔案，以路徑交給 agent，存放在 `work_dir` 之外
- **臨時共享** — 連結 + 6 位數 PIN + 暱稱，預設 1 小時；訪客可為唯讀 `viewer` 或完整權限 `editor`
- **MCP server** — 讓其他 agent 操作 session、讀紀錄、查活動；`ask_session` 有跳數上限防止互問迴圈
- **Shell 與日誌** — 選用的 `work_dir` shell（預設關閉），以及設定 → 日誌的即時伺服器日誌

每項功能的詳細說明：[docs/features.zh-TW.md](docs/features.zh-TW.md)。

## 🤔 為什麼用這個？

| | SSH + tmux | 一般 Telegram Bot | **本專案** |
|---|---|---|---|
| 多代理同一畫面 | 一個終端機一個 | 一 bot 一工具 | Claude / Cursor / Codex / Kiro ACP 並列 |
| Session 狀態 | 手動 | 通常沒有 | 可持久，附未讀與待授權標記 |
| 協作 | 共用終端機 | 無 | 臨時 PIN 連結，viewer / editor |
| 代理互相呼叫 | 自己接 | 無 | 內建 MCP server（`ask_session`） |

## 🏗️ 架構

```
桌面視窗 · 瀏覽器 · Telegram Mini App · 共享訪客
        ↕ WebSocket / REST          MCP client ↔ POST /mcp
┌─────────────────────────────────┐
│  Go 二進位（Fiber + SQLite）      │
│  Session · 佇列 · 共享            │
│  每則訊息 spawn CLI（無 PTY）      │
│  QuotaService（快取擷取）         │
└────────────────┬────────────────┘
                 ↓
  claude · cursor agent · codex · kiro-cli (ACP)
```

每則使用者訊息 spawn 一個子進程。詳細規格：[`docs/spec/plan.md`](docs/spec/plan.md)、[`docs/spec/headless.md`](docs/spec/headless.md)。

## 🔒 安全

> [!WARNING]
> Shell、共享與 MCP token 都等同授予主機的控制權。僅在可信網路啟用，生產環境勿開 `no_auth`。

- 勿將含真實憑證的設定提交版本庫。
- **`shell.enabled`** — 讓已驗證使用者在主機上執行 shell。白名單規則：[`docs/spec/shell-allowlist-schema.md`](docs/spec/shell-allowlist-schema.md)。
- **共享** — 持有連結與 PIN 的人在到期或你結束前都能加入；`editor` 與你同權（可送訊息、核准工具），請優先給 `viewer`。
- **`mcp_token`** — 持有者可完全操控所有 session（含 shell）；比照 `bot_token` 等級保管，`/mcp` 僅在可信網路開放。
- **上傳** — 已驗證使用者可寫入任意類型與大小的檔案到 `./workspace/uploads/<session_id>/`（不是 `work_dir`），不受 `shell.enabled` 控制。

## 📚 文件

| 主題 | 路徑 |
|---|---|
| 每項功能詳述 | [`docs/features.zh-TW.md`](docs/features.zh-TW.md) |
| 規格、API / WebSocket | [`docs/spec/plan.md`](docs/spec/plan.md) |
| 設定欄位 | [`config.example.yaml`](config.example.yaml) |
| 各 CLI 參考 | [`docs/spec/`](docs/spec/) |

## 測試

本專案使用 BrowserStack 測試。

## 授權

MIT
