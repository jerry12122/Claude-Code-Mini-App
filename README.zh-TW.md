# Claude Code Mini App

> 用手機 Telegram 遠端操控伺服器上的 AI 編碼 CLI。**單一 Go 二進位**同時提供 REST、WebSocket 與 UI，無需獨立前端建置。

[![Version](https://img.shields.io/badge/version-0.2.0-blue)](#) [![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE) [![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)](#)

[English](README.md)

## 快速開始

**需求：** Go 1.25+、Telegram Bot Token（[@BotFather](https://t.me/BotFather)）、伺服器上已安裝並登入要用的 CLI（`claude`、`cursor agent`、`kiro-cli`、`gemini` 等）。

```bash
git clone https://github.com/jerry12122/Claude-Code-Mini-App
cd claude-miniapp
go build -o claude-miniapp ./cmd/server
cp config.example.yaml config.yaml   # 填 bot_token、whitelist_tg_ids
./claude-miniapp                     # → http://localhost:8080
```

## 功能

- **多代理** — Claude Code、Cursor Agent、Kiro CLI、Gemini CLI（依 Session 選擇）
- **即時串流** — WebSocket 對話與 Markdown 串流；多分頁同步
- **用量徽章** — Session header 顯示帳戶用量（如 Claude `5h 16% · Week 9%`）
- **Session 管理** — 多對話、各自綁定 `work_dir` 與權限模式
- **權限流程** — Claude 遭拒時可「允許一次」或切換模式
- **驗證** — Telegram `initData` + 白名單；可選內網密碼登入
- **選用 Shell** — 於 `work_dir` 執行指令（預設關閉）

## 為什麼用這個？

| | SSH + 終端機 | 一般 Telegram Bot | **本專案** |
|---|---|---|---|
| 手機體驗 | 差 | 純文字 | Mini App UI + 串流 |
| Session / 工作目錄 | 手動 | 通常沒有 | 內建、可持久 |
| 多 CLI | 自己接 | 一 bot 一工具 | Claude / Cursor / Kiro / Gemini |
| 部署 | SSH 金鑰 | Bot + 自寫邏輯 | 單一二進位 |

## 架構

```
Telegram Mini App / 瀏覽器
        ↕ WebSocket
┌──────────────────────────────┐
│  Go 二進位（Fiber + SQLite）   │
│  每則訊息 spawn CLI（無 PTY）   │
│  QuotaService（快取擷取）      │
└──────────────────────────────┘
```

每則使用者訊息 spawn 一個子進程。詳細規格：[`docs/spec/plan.md`](docs/spec/plan.md)、[`docs/spec/headless.md`](docs/spec/headless.md)。

## 安全

- 勿將含真實憑證的設定提交版本庫；生產環境勿開 `no_auth`。
- **`shell.enabled`** 會讓已驗證使用者在主機上執行 shell — 僅在可信網路啟用。白名單規則：[`docs/spec/shell-allowlist-schema.md`](docs/spec/shell-allowlist-schema.md)。

## 文件

| 主題 | 路徑 |
|---|---|
| 規格、API / WebSocket | [`docs/spec/plan.md`](docs/spec/plan.md) |
| 設定欄位 | [`config.example.yaml`](config.example.yaml) |
| 各 CLI 參考 | [`docs/spec/`](docs/spec/) |
| 用量 POC | [`poc/quota-percent/README.md`](poc/quota-percent/README.md) |

## 授權

MIT
