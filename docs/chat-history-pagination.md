# Chat history pagination

The browser requests `history_limit=100` on `/sessions/:id/ws`. Non-Telegram sync
then contains the newest 100 messages in ascending ID order, plus
`history_has_more: true` when older messages exist. Omit `history_limit` (or use
`0`) to retain the original complete-history sync. Positive limits are capped at
200; malformed or out-of-range values close the connection.

`GET /sessions/:id/messages?limit=100&before=<message-id>` returns an array of the
newest messages strictly before that ID, in ascending ID order. `limit` accepts
1–200; `before` accepts a positive int64 ID. Invalid values return HTTP 400.
Omitting both parameters preserves the original complete array. A page with
fewer rows than the requested limit is the final page.

Snapshot guests remain bounded by their snapshot message ID, including when
`before` refers to a newer message. Telegram retains its existing
`pagination=telegram` cursor contract.

Ordinary HTTP requests time out after 15 seconds, including response body reads.
Uploads, attachment downloads and manual model discovery use a 120-second limit.
Timeouts never automatically retry writes.

Attachment files and mention chips survive switching sessions within the current
page. Unsent attachment Files are backed up in the browser's IndexedDB, separately
for each session/share/nickname, and restored with fresh preview URLs after reload.
Interrupted uploads restore as retryable errors; completed uploads retain their
attachment IDs. Saving/restoring errors and retry controls are visible in the
composer. A failed restore does not overwrite the saved files. Storage depends
on browser quota/availability: users should wait for saving to finish before
closing the page, and clearing site data removes these local backups. Deleting a
session also attempts to clear its local file backup. Mention chips remain held
in memory; text and the exact payload/IDs of unacknowledged messages are retained
in localStorage, including already-uploaded attachment IDs.
Pending messages are displayed separately from the next draft and are never
resent automatically. “重試確認” sends the original request ID; explicit rejection
restores the original content to the draft. Guest drafts use a separate storage
scope for each share/nickname.

## Input acceptance and retry

WebSocket `input` messages may include `request_id` (1–128 bytes). The server
atomically stores a receipt and queue item before replying `input_accepted`.
Receipts are scoped to the session and authenticated actor (owner, or share plus
guest nickname). Reusing an ID with the same text and ordered attachment IDs
replies `input_accepted` without enqueuing it again; different content is rejected.
Receipts survive restart, history reset, queue removal and task completion, and
are removed when the session is deleted. A retry of a removed queue item confirms
its previous acceptance; it does not restore the removed work.

`input_rejected` normally means the input was not accepted. The codes
`update_in_progress` and `retry_input` require keeping the original pending ID;
they must not be converted into a new input. Disconnects and acknowledgement
timeouts likewise leave the result unknown. Older clients omitting the ID retain
their previous behavior without deduplication. Shell commands use their existing
confirmation flow and are not automatically retried.

Acceptance is distinct from completion. This prevents duplicate acceptance, not
exactly-once execution of an external CLI/Telegram side effect. A crash after
queue promotion can leave a user message without a completed reply. On startup,
pending replies retain their partial content with `status: "interrupted"`, and
affected/running session queues are paused for manual review and resume.

Each WebSocket has one writer with at most 128 queued events and a five-second
write deadline. A full queue or failed write closes that connection; the browser
reconnects and synchronizes persisted state. This isolates slow consumers without
silently dropping individual stream events.

## Long conversations and session list

Above 200 loaded messages, only rows near the viewport (with 800 pixels of
overscan per side) are mounted. Row heights are measured and cached; the visible
message and its offset anchor prepends and subsequent image/font/layout changes.
Scrolling to latest follows the end. Height arithmetic is O(n) in loaded messages;
only mounted rows create message DOM. Browsers without ResizeObserver render the
full loaded list. “顯示全部已載入訊息” also disables windowing for browser find and
cross-message selection; it does not fetch unloaded history.

Scroll correction runs for message/layout changes, not unrelated composer
renders. Moving away from the bottom stops following immediately; deferred
initial jumps and approval panels respect that choice. Away from the bottom,
only an explicit local send or “latest” action requests a jump. Task completion no longer focuses the
composer; initial focus skips selections, other controls, inactive panes and
sessions that changed while the focus callback was pending.

The session list distinguishes loading/failure from a successful empty result,
keeps the last successful list during failures, and offers retry. Every refresh
cancels the previous request and checks a generation before applying results;
unmount also invalidates requests, preventing stale responses from updating state.

## Verification scope

CI was intentionally left unchanged. Runnable checks were added for durable
acceptance/rollback/reopen, restart interruption, dispatcher retries, bounded
socket queues and browser draft/acknowledgement behavior. Only compilation and
static syntax checks were performed; no tests or application were executed.
`scripts/remaining-ux.test.cjs` adds runnable checks for list-response races,
virtual-window boundaries/stable keys and IndexedDB commit/abort/recovery paths.
It has not been executed; actual scrolling, storage and latency remain unmeasured.
It also drives the virtual-list effects to check that unrelated rerenders do
not rewind a pending user scroll, while prepending still preserves the anchor,
and checks small upward scrolling and automatic-focus guards.
