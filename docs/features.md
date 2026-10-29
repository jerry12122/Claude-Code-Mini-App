# Features in detail

[← Back to README](../README.md) · [繁體中文](features.zh-TW.md)

## Agents & sessions

- **Multi-agent** — Claude Code, Cursor Agent, Codex, Antigravity (`agy`, NDJSON streaming), Kiro ACP (interactive permission prompts over Agent Client Protocol); per session. Existing Kiro CLI sessions keep working, but new ones use Kiro ACP. Legacy `gemini` sessions use the Antigravity runner; choose Antigravity for new sessions. Supported model lists are fetched from each CLI on startup (Claude included) and can be refreshed without a restart from Settings → General
- **Sessions** — Multiple conversations, each with its own agent, `work_dir` and permission mode
- **Permissions** — Shows the full command / file content. Claude prompt-tool and Kiro ACP approve a single operation; the legacy Claude denial flow allows the listed tools for the retry run. Edit tools can also be allowed with auto-accept edits
- **Usage bars** — The session header shows account usage as progress bars (e.g. Claude `5h 16% · Week 9%`, with the plan when known). Antigravity shows separate Gemini and Claude/GPT allowances; the compact mobile badge shows the highest usage percentage
- **Unread tracking** — Sessions with new activity are marked unread in the list; "read all" to clear. The list updates in real time over a `/events` WebSocket (30 s polling as a fallback); the tab title shows the unread count, and a toast appears when another session finishes or needs approval
- **Quick switcher** — Ctrl/Cmd+P opens a VS Code-style palette to jump to any session (search by name, directory, or branch)
- **Tabs & split groups** (desktop, ≥1024px) — Each open session is a tab in a group with a tab bar (name, agent icon, status dot, close button; middle-click closes). A session lives in only one tab, so opening one that is already open just focuses it. Only the active tab of a group is mounted. Closing the active tab selects its right neighbour, else the left. A second group appears side by side or stacked; the active group has a highlighted frame
- **Drag & drop placement** — Drag a session from the sidebar onto a tab bar (inserts where you drop) or onto a group's content area: the centre adds it to that group; with a single group, an edge splits off a second group (left/right = side by side, top/bottom = stacked). With two groups, dropping anywhere only adds to that group, and dropping an already-open session moves it. Drag tabs to reorder or move them between groups; Esc cancels. Layout is restored after a reload and is stored per browser. Below 1024px the app keeps the single pane and shows no tabs
- **Shortcuts** — `Ctrl+\` splits the active tab into a new group (needs at least two tabs) or merges two groups back; `Ctrl+Alt+←/→` moves focus between groups; `Ctrl+Alt+O` switches side-by-side/stacked. They are ignored during IME composition
- **Message queue** — Messages sent while a task is running are queued (persisted in the DB, survives restarts) and run in order; the queue pauses on failure or interrupt, and can be resumed or edited. If the agent has answered but its process is still waiting (for example for a scheduled wake-up), the stop button stays available and a new message is queued until it ends

## Chat

- **Live streaming** — WebSocket chat with Markdown; multi-tab sync
- **Code blocks** — syntax highlighting with language tag, one-click copy
- **Attachments** — 📎 upload or paste images / text files; they show as removable chips (with thumbnails) above the input and are handed to the agent by path (stored under `workspace/uploads/`, not your `work_dir`); drag & drop on desktop
- **Message actions** — Copy / forward buttons sit beside each message's timestamp; long-press a message for a floating menu (copy, plus forward on incoming messages)
- **Input bar controls** — Model, effort and permission mode are chips in the input bar (not the header), so you can change them right before sending
- **Chat UX** — IME-safe Enter, Enter inserts a newline on touch devices, scrolling up isn't yanked back by streaming (with a "jump to latest" button), deleted sessions can be undone for 5 seconds

## Collaboration & integration

- **Temporary sharing** — Share a session with a friend via link + 6-digit PIN + nickname (default 1 hour, up to 7 days). Guests join from any browser as read-only `viewer` or full-access `editor`; messages show who said what, and ending the share kicks everyone out at once
- **MCP server** — Expose sessions to other agents over Streamable HTTP (`POST /mcp`, off by default): operate sessions, read chat history, and query cross-session activity; `ask_session` asks another session and waits for its answer, with a hop limit (`mcp_max_hops`) to stop agent-to-agent loops

  Queue tools use the same MCP authentication: `get_queue({"session_id":"…"})` returns `{"queue":[{"id":123,"content":"…","created_at":"…"}],"queue_paused":false}` in FIFO order (`queue: []` when empty). Items may also include `author` and `attachments`. `delete_queue_message({"session_id":"…","message_id":123})` removes only that session's queued item and broadcasts the updated queue, returning `{"ok":true}` even if the item is already absent. Both reject blank or unknown sessions; deletion also rejects non-positive IDs. Deletion does not interrupt running tasks or resume a paused queue.

- **Auth** — Telegram `initData` + allowlist; optional web login on private IPs. Forwarded IP headers are accepted only from `web.trusted_proxies` (loopback by default); configure exact CIDRs for other proxies, or an empty list to disable header handling
- **Optional shell** — Run commands in `work_dir` (off by default); when on, also shows "Open in VS Code" / "Open folder" buttons in the session header (desktop only)
- **Version info** — Settings shows the app version and the Git commit it was built from
- **Log viewer** — Settings → Logs streams the server log live (level filter, search, pause, copy; toggle Debug at runtime). Handy for the desktop build, which has no console; only the log since the current start is shown, the full history stays in `logs/server.log`

## Desktop window (Windows)

Same process, plus a window that loads the existing UI. The browser and Telegram Mini App keep using the same port. Closing the window hides it and leaves a tray icon: left-click shows the window, right-click offers Show or Quit. Ctrl+Q quits as well. F5 / Ctrl+R reloads the UI and F12 opens DevTools (a no-op with `-tags production`).

Win11 already includes the WebView2 runtime. `go build ./cmd/server` stays pure Go. The desktop build uses Wails v3 (the tray is built in):

```bash
go build -tags production -ldflags "-H windowsgui" -o claude-miniapp-desktop.exe ./cmd/desktop
```

Put the exe next to `config.yaml` and `internal/static/` (same layout as the server binary). From a repo checkout, `go run ./cmd/desktop` uses the working directory's config. `-tags production` embeds the frontend in the exe and turns off Wails debug logging. The in-app updater needs it, so release builds always use it. `go run ./cmd/desktop` stays in development mode and reads `internal/static/` from disk.

Frontend files are served from disk, so editing `internal/static/` only needs a reload (F5), not a rebuild. If a change still doesn't show up, use Settings → General → "Clear cache & reload".

### Desktop auto-update

The Windows desktop app can check GitHub Releases and update itself. Only `claude-miniapp-desktop.exe` is replaced.

- **Upgrade once by hand first.** Versions before the updater cannot update themselves. Download the new `claude-miniapp-v<X.Y.Z>-windows-amd64.zip` and replace `claude-miniapp-desktop.exe` in your existing folder, keeping `config.yaml` and your data. After that, updates happen inside the app.
- **Check:** tray → Check for updates, or Settings → Desktop update. The app also checks at startup and every 6 hours, and only shows the result. It never downloads or restarts on its own.
- **Download and restart are separate clicks.** Download starts only when you click it. Restart and update also needs a click, and the app refuses it while work is busy.
- **Busy refusal:** a restart is refused (`update_busy`) while any session has a running task, a shell command, a pending approval, or any queued message, even if the queue is paused. Finish the work or clear the queue, then try again. The downloaded update is kept.
- **Writable folder required:** the folder containing the exe must be writable by your user. The updater does not ask for administrator rights.
- **Kept as-is:** `config.yaml`, `claude-miniapp.db`, `workspace/` (attachments), `logs/`, and `internal/static/` are not changed. Only `claude-miniapp-desktop.exe` is replaced.
- **Leftover previous exe:** Windows cannot delete a running executable, so the updater renames the old one aside as `<name>.exe.old.<number>`. The app deletes every such leftover the next time it starts, so no rollback copy is kept — back up the exe yourself before updating if you want one.
- **Not auto-updated:** the Windows server (`claude-miniapp.exe`) and Linux builds. Update those by hand.
- **Manual restore:** before upgrading, keep a backup copy of the current exe. To go back, close the app and copy the backup over the exe:

```powershell
Copy-Item .\claude-miniapp-desktop.exe .\claude-miniapp-desktop.previous.exe
# To roll back: close the app, then
Copy-Item .\claude-miniapp-desktop.previous.exe .\claude-miniapp-desktop.exe -Force
```

  Only the exe files are touched. Data is not downgraded automatically.
- **Integrity:** downloads are checked against the SHA256 in `checksums.txt`. That proves the file matches the release. It does not prove who published it, because there is no signature check yet.
- **Network:** checking and downloading need access to GitHub. An update does not make the app work offline.
