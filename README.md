# Claude Code Mini App

> A self-hosted **multi-agent console** for Claude Code, Cursor Agent, Codex and Kiro. Run and supervise all your coding agents in one place — from a desktop window, any browser, or a Telegram Mini App on your phone. **One Go binary**, no separate frontend build.

[![Version](https://img.shields.io/badge/version-0.8.0-blue)](#) [![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE) [![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)](#)

[繁體中文](README.zh-TW.md)

![Agent Console: sessions for Claude, Codex, Kiro and Cursor grouped by project, with a streaming chat on the right](docs/images/console.png)

<sub>Demo data on a throwaway instance. The UI is shown in Traditional Chinese.</sub>

Your agents keep running on the machine where your code lives; this app is the console in front of them. Each conversation is a **session** bound to a `work_dir`, an agent, and a permission mode, so you can juggle several agents across several projects, see which ones are working or waiting for approval, and step in from wherever you are.

| Way in | For | Auth |
|---|---|---|
| 🖥️ **Desktop window** (Windows) | Daily use at your PC — tray icon, Ctrl+P switcher | local |
| 🌐 **Browser** | Any device on your LAN, or remote through a reverse proxy / tunnel | web password (restricted to `web.allowed_cidrs`) |
| 📱 **Telegram Mini App** | Away from your desk — phone push on errors, approvals on the go | Telegram `initData` + allowlist |

All three talk to the same server and see the same sessions in real time.

## 🚀 Quick Start

**Requires:** the CLI(s) you use (`claude`, `cursor agent`, `codex`, `kiro-cli`) installed and logged in on the machine, plus a Telegram bot token ([@BotFather](https://t.me/BotFather)) — or `no_auth: true` for local-only use.

**Download (recommended):** grab the zip / tar.gz from [Releases](https://github.com/jerry12122/Claude-Code-Mini-App/releases/latest), extract the whole folder (the executable needs `internal/static/` beside it), copy `config.example.yaml` to `config.yaml`, then run `claude-miniapp-desktop.exe` (Windows desktop) or `claude-miniapp` (server only).

> [!NOTE]
> The Windows executables are unsigned, so SmartScreen may warn: click **More info → Run anyway**. The desktop window needs WebView2 (built into Windows 11).

**From source** (Go 1.25+):

```bash
git clone https://github.com/jerry12122/Claude-Code-Mini-App
cd Claude-Code-Mini-App
cp config.example.yaml config.yaml   # set bot_token, whitelist_tg_ids
go build -o claude-miniapp ./cmd/server && ./claude-miniapp   # → http://localhost:8080
```

On Windows you can also build the desktop window: `go build -ldflags "-H windowsgui" -o claude-miniapp-desktop.exe ./cmd/desktop`. Closing its window hides it to the tray; quit from the tray menu or with Ctrl+Q. Build notes and shortcuts: [Desktop window](docs/features.md#desktop-window-windows).

## ✨ Features

- **Multi-agent** — Claude Code, Cursor Agent, Codex and Kiro ACP, chosen per session. Tool approvals show the full command or file content before you allow them
- **Live chat** — WebSocket streaming with Markdown, syntax-highlighted code and one-click copy; multiple tabs stay in sync
- **Sessions** — each bound to a `work_dir` and permission mode, with real-time unread and needs-approval badges, a Ctrl/Cmd+P quick switcher, and an account-quota badge in the header
- **Message queue** — messages sent while a task runs are queued, survive restarts, and pause on failure or interrupt
- **Attachments** — paste or drag & drop images and files; the agent gets them by path, stored outside your `work_dir`
- **Temporary sharing** — link + 6-digit PIN + nickname, default 1 hour; guests join as read-only `viewer` or full-access `editor`
- **MCP server** — let other agents operate sessions, read history and query activity; `ask_session` has a hop limit against agent-to-agent loops
- **Shell & logs** — optional shell in `work_dir` (off by default) and a live server-log viewer in Settings → Logs

Every feature in detail: [docs/features.md](docs/features.md).

## 🤔 Why this?

| | SSH + tmux | Generic Telegram bot | **This app** |
|---|---|---|---|
| Multiple agents, one view | One terminal each | One bot, one tool | Claude / Cursor / Codex / Kiro ACP side by side |
| Session state | Manual | Usually none | Persisted, with unread and approval badges |
| Collaboration | Share a terminal | None | Temporary PIN link, viewer / editor |
| Agent-to-agent | You wire it | None | Built-in MCP server (`ask_session`) |

## 🏗️ Architecture

```
Desktop window · Browser · Telegram Mini App · Share guests
        ↕ WebSocket / REST          MCP clients ↔ POST /mcp
┌─────────────────────────────────┐
│   Go binary (Fiber + SQLite)    │
│   Sessions · queue · sharing    │
│   spawn CLI per message (no PTY)│
│   QuotaService (cached fetch)   │
└────────────────┬────────────────┘
                 ↓
  claude · cursor agent · codex · kiro-cli (ACP)
```

Each user message spawns a short-lived subprocess. Details: [`docs/spec/plan.md`](docs/spec/plan.md), [`docs/spec/headless.md`](docs/spec/headless.md).

## 🔒 Security

> [!WARNING]
> Shell, shares and the MCP token all grant control of the host. Enable them only on networks you trust, and never use `no_auth` in production.

- Keep real secrets out of git.
- **`shell.enabled`** — shell access on the host for authenticated users. Allowlist rules: [`docs/spec/shell-allowlist-schema.md`](docs/spec/shell-allowlist-schema.md).
- **Shares** — anyone holding the link and PIN can join until it expires or you end it; an `editor` has the same power as you (sending messages, approving tool use), so prefer `viewer`.
- **`mcp_token`** — full session control (including shell) for any client holding it; treat it like `bot_token` and expose `/mcp` only on trusted networks.
- **Uploads** — any authenticated user can write files of any type and size into `./workspace/uploads/<session_id>/` (not your `work_dir`), independent of `shell.enabled`.

## 📚 Documentation

| Topic | Path |
|---|---|
| All features in detail | [`docs/features.md`](docs/features.md) |
| Spec & API / WebSocket | [`docs/spec/plan.md`](docs/spec/plan.md) |
| Config reference | [`config.example.yaml`](config.example.yaml) |
| Claude / Cursor / Codex / Kiro / Antigravity CLI | [`docs/spec/`](docs/spec/) |

## Testing

This project is tested with BrowserStack.

## License

MIT
