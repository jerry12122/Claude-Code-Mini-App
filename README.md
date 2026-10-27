# Claude Code Mini App

> A self-hosted **multi-agent console** for Claude Code, Cursor Agent, Codex, Kiro and Antigravity. Run and supervise all your coding agents in one place — from a desktop window, any browser, or a Telegram Mini App on your phone. **One Go binary**, no separate frontend build.

[![Version](https://img.shields.io/badge/version-0.16.0-blue)](#) [![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE) [![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)](#)

[繁體中文](README.zh-TW.md)

![Agent Console: split view with Claude and Codex sessions, tab groups, unread badges, and sessions for Claude, Cursor, Codex, Kiro and Antigravity grouped by project](docs/images/split-view.png)

<sub>Demo data on a throwaway instance. The UI is shown in Traditional Chinese.</sub>

Your agents keep running on the machine where your code lives; this app is the console in front of them. Each conversation is a **session** bound to a `work_dir`, an agent, and a permission mode, so you can juggle several agents across several projects, see which ones are working or waiting for approval, and step in from wherever you are.

| Way in | For | Auth |
|---|---|---|
| 🖥️ **Desktop window** (Windows) | Daily use at your PC — session tabs and split view, tray icon, Ctrl+P switcher | same web password as the browser |
| 🌐 **Browser** | Any device on your LAN, or remote through a reverse proxy / tunnel | web password (restricted to `web.allowed_cidrs`) |
| 📱 **Telegram Mini App** | Away from your desk — phone push on errors, approvals on the go | Telegram `initData` + allowlist |

All three talk to the same server and see the same sessions in real time.

## 🚀 Quick Start

1. Download the release archive for your OS from [Releases](https://github.com/jerry12122/Claude-Code-Mini-App/releases/latest) and extract it.
2. Run `claude-miniapp-desktop.exe` (Windows desktop) or `claude-miniapp` (server). For server builds, open <http://127.0.0.1:8080>.
3. Follow the first-run wizard in your browser or desktop window to set a password and detect available CLI agents.

Detailed walkthrough: [Getting started from scratch](docs/getting-started.md) · [Telegram remote setup](docs/getting-started.md#6-optional-open-from-telegram) · [Troubleshooting](docs/getting-started.md#troubleshooting)

## ✨ Features

- **Multi-agent** — Claude Code, Cursor Agent, Codex, Kiro ACP and Antigravity, chosen per session. Tool approvals show the full command or file content before you allow them
- **Live chat** — WebSocket streaming with Markdown, syntax-highlighted code and one-click copy; multiple tabs stay in sync
- **Sessions** — each bound to a `work_dir` and permission mode, with real-time unread and needs-approval badges, a Ctrl/Cmd+P quick switcher, and account-usage progress bars in the header
- **Tabs & split view** (desktop, ≥1024px) — open sessions as tabs, split into two groups, drag a session from the sidebar (or a tab) to place it; `Ctrl+\` splits or merges. Model, effort and permission mode sit in the input bar
- **Message queue** — messages sent while a task runs are queued, survive restarts, and pause on failure or interrupt
- **Attachments** — paste or drag & drop images and files; the agent gets them by path, stored outside your `work_dir`
- **Temporary sharing** — link + 6-digit PIN + nickname, default 1 hour; guests join as read-only `viewer` or full-access `editor`
- **MCP server** — let other agents operate sessions, read history and query activity; `ask_session` has a hop limit against agent-to-agent loops
- **Desktop auto-update** (Windows) — in-app release check and one-click upgrade with task busy-guard ([details](docs/features.md#desktop-auto-update))
- **Shell & logs** — optional shell in `work_dir` (off by default) and a live server-log viewer in Settings → Logs

Every feature in detail: [docs/features.md](docs/features.md).

## 🤔 Why this?

| | SSH + tmux | Generic Telegram bot | **This app** |
|---|---|---|---|
| Multiple agents, one view | One terminal each | One bot, one tool | Claude / Cursor / Codex / Kiro ACP / Antigravity side by side |
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
  claude · cursor-agent · codex · kiro-cli (ACP) · agy
```

Each user message spawns a short-lived subprocess. Details: [`docs/spec/plan.md`](docs/spec/plan.md), [`docs/spec/headless.md`](docs/spec/headless.md).

## 🔒 Security

Designed for local and trusted network environments. Sensitive capabilities (direct host shell, web sharing, and MCP server) are opt-in and disabled by default. Never run with `no_auth: true` over public networks. Details & allowlist: [`docs/features.md`](docs/features.md) · [`docs/spec/shell-allowlist-schema.md`](docs/spec/shell-allowlist-schema.md).

## 📚 Documentation

| Topic | Path |
|---|---|
| Install, sign in, and get your first reply | [`docs/getting-started.md`](docs/getting-started.md) |
| All features in detail | [`docs/features.md`](docs/features.md) |
| Spec & API / WebSocket | [`docs/spec/plan.md`](docs/spec/plan.md) |
| Config reference | [`config.example.yaml`](config.example.yaml) |
| Claude / Cursor / Codex / Kiro / Antigravity CLI | [`docs/spec/`](docs/spec/) |

## Testing

This project is tested with BrowserStack.

## License

MIT
