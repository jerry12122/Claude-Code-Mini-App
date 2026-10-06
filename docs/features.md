# Features in detail

[← Back to README](../README.md) · [繁體中文](features.zh-TW.md)

## Agents & sessions

- **Multi-agent** — Claude Code, Cursor Agent, Codex, Kiro ACP (interactive permission prompts over Agent Client Protocol); per session. Existing Kiro CLI sessions keep working, but new ones use Kiro ACP (Gemini / Antigravity paused due to headless limits). Model lists are fetched from each CLI on startup (Claude included) and can be refreshed without a restart from Settings → General
- **Sessions** — Multiple conversations, each with its own agent, `work_dir` and permission mode
- **Permissions** — Shows the full command / file content. Claude prompt-tool and Kiro ACP approve a single operation; the legacy Claude denial flow allows the listed tools for the retry run. Edit tools can also be allowed with auto-accept edits
- **Quota badge** — Session header shows usage (e.g. Claude `5h 16% · Week 9%`)
- **Unread tracking** — Sessions with new activity are marked unread in the list; "read all" to clear. The list updates in real time over a `/events` WebSocket (30 s polling as a fallback); the tab title shows the unread count, and a toast appears when another session finishes or needs approval
- **Quick switcher** — Ctrl/Cmd+P opens a VS Code-style palette to jump to any session (search by name, directory, or branch)
- **Message queue** — Messages sent while a task is running are queued (persisted in the DB, survives restarts) and run in order; the queue pauses on failure or interrupt, and can be resumed or edited

## Chat

- **Live streaming** — WebSocket chat with Markdown; multi-tab sync
- **Code blocks** — syntax highlighting with language tag, one-click copy
- **Attachments** — 📎 upload or paste images / text files; they show as removable chips (with thumbnails) above the input and are handed to the agent by path (stored under `workspace/uploads/`, not your `work_dir`); drag & drop on desktop
- **Message actions** — Copy / forward buttons sit beside each message's timestamp; long-press a message for a floating menu (copy, plus forward on incoming messages)
- **Chat UX** — IME-safe Enter, Enter inserts a newline on touch devices, scrolling up isn't yanked back by streaming (with a "jump to latest" button), deleted sessions can be undone for 5 seconds

## Collaboration & integration

- **Temporary sharing** — Share a session with a friend via link + 6-digit PIN + nickname (default 1 hour, up to 7 days). Guests join from any browser as read-only `viewer` or full-access `editor`; messages show who said what, and ending the share kicks everyone out at once
- **MCP server** — Expose sessions to other agents over Streamable HTTP (`POST /mcp`, off by default): operate sessions, read chat history, and query cross-session activity; `ask_session` asks another session and waits for its answer, with a hop limit (`mcp_max_hops`) to stop agent-to-agent loops
- **Auth** — Telegram `initData` + allowlist; optional web login on private IPs. Forwarded IP headers are accepted only from `web.trusted_proxies` (loopback by default); configure exact CIDRs for other proxies, or an empty list to disable header handling
- **Optional shell** — Run commands in `work_dir` (off by default); when on, also shows "Open in VS Code" / "Open folder" buttons in the session header (desktop only)
- **Log viewer** — Settings → Logs streams the server log live (level filter, search, pause, copy; toggle Debug at runtime). Handy for the desktop build, which has no console; only the log since the current start is shown, the full history stays in `logs/server.log`

## Desktop window (Windows)

Same process, plus a window that loads the existing UI. The browser and Telegram Mini App keep using the same port. Closing the window hides it and leaves a tray icon: left-click shows the window, right-click offers Show or Quit. Ctrl+Q quits as well. F5 / Ctrl+R reloads the UI and F12 opens DevTools (a no-op with `-tags production`).

Win11 already includes the WebView2 runtime. `go build ./cmd/server` stays pure Go. The desktop build uses Wails v3 (the tray is built in):

```bash
go build -ldflags "-H windowsgui" -o claude-miniapp-desktop.exe ./cmd/desktop
```

Put the exe next to `config.yaml` and `internal/static/` (same layout as the server binary). From a repo checkout, `go run ./cmd/desktop` uses the working directory's config. Add `-tags production` to turn off Wails debug logging.

Frontend files are served from disk, so editing `internal/static/` only needs a reload (F5), not a rebuild. If a change still doesn't show up, use Settings → General → "Clear cache & reload".
