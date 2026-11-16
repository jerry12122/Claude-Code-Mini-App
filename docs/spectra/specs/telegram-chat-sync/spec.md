# telegram-chat-sync Specification

## Purpose

Show a single Telegram private-chat timeline shared with the user's other Telegram clients by reading from Telegram directly, without mirroring chat content into the local database, and load older history only on demand.

## Requirements

### Requirement: Private chat identity and cross-device direction

The system SHALL treat Telegram as the only source of chat content and SHALL NOT persist Telegram history or live messages in the local `messages` table. For a Telegram session bound to one private chat peer, the system SHALL display only messages exchanged with that peer under the currently authorized account. Messages sent by the authenticated user from any device SHALL be shown on the user's side; messages from the peer SHALL be shown on the peer's side. Groups, channels and other peers SHALL NOT be forwarded to the session.

#### Scenario: Message sent from another device appears

- **WHEN** the authenticated user sends a message to the bound peer from a phone or another Telegram client
- **THEN** the session receives it over WebSocket as a user-side message carrying the remote message identity

#### Scenario: Unrelated conversations are ignored

- **WHEN** an update arrives from a different private chat, a group, or a channel
- **THEN** it SHALL NOT be pushed to the session

#### Scenario: Account switch does not mix data

- **WHEN** the authorized account changes while the session is open
- **THEN** the client SHALL discard messages and pending items from the previous account scope and SHALL NOT send the old history cursor to the new scope


<!-- @trace
source: telegram-chat-sync-media
updated: 2026-10-20
code:
  - internal/server/server.go
  - internal/tgclient/events.go
  - internal/tgclient/send.go
  - internal/tgclient/runner.go
  - scripts/telegram-chat.test.cjs
  - internal/tgclient/media.go
  - internal/api/session.go
  - internal/api/telegram_media.go
  - internal/static/js/hooks/useChatSocket.js
  - go.sum
  - internal/api/share.go
  - internal/agent/factory.go
  - internal/static/js/session/NewSessionComposer.js
  - internal/ws/sync.go
  - internal/static/js/app.js
  - internal/ws/broadcast.go
  - internal/ws/telegram.go
  - internal/config/config.go
  - config.example.yaml
  - internal/db/message.go
  - internal/api/telegram_history.go
  - internal/db/session.go
  - internal/static/js/ui/uiAtoms.js
  - internal/tgclient/history.go
  - internal/static/js/chat/ChatView.js
  - internal/static/AGENT.md
  - internal/ws/handler.go
  - internal/static/js/session/SessionView.js
  - internal/tgclient/manager.go
  - internal/server/guest.go
  - internal/static/js/core/core.js
  - internal/static/js/hooks/telegramTimeline.js
  - internal/static/index.html
  - go.mod
tests:
  - internal/server/guest_test.go
  - internal/tgclient/helpers_test.go
  - internal/tgclient/media_test.go
  - internal/tgclient/history_test.go
  - internal/tgclient/send_mem_test.go
  - internal/ws/telegram_test.go
  - internal/server/telegram_history_api_test.go
  - internal/tgclient/manager_test.go
-->

---
### Requirement: Pass-through bounded history

The system SHALL serve `GET /sessions/:id/messages?pagination=telegram` by calling Telegram `messages.getHistory` for the session's bound peer, converting one page into message DTOs, and returning `{messages, next_cursor, has_more, sync_state}` without writing messages to the database. `limit` SHALL default to 50 and accept 1 to 100. `before` SHALL be an exclusive remote message ID in the range 1 to 2147483647. Each request SHALL query exactly one upstream page and SHALL NOT scan to the start of the history. Service or unsupported messages SHALL still advance the cursor.

#### Scenario: Latest page then older page

- **WHEN** the client requests without `before`, then with `before` set to the returned `next_cursor`
- **THEN** the first response contains the latest page in chronological order and the second contains only messages with smaller remote IDs, with no boundary message repeated

#### Scenario: End of history is only reported when confirmed

- **WHEN** Telegram returns an empty page, a complete (non-slice) result, or a page that reaches message ID 1
- **THEN** `has_more` is false and `next_cursor` is null; any other outcome keeps `has_more` true

#### Scenario: Invalid parameters are rejected before any upstream call

- **WHEN** `limit` is outside 1 to 100, or `before` is empty, zero, negative, non-numeric, or larger than 2147483647
- **THEN** the response is 400 with code `invalid_limit` or `invalid_cursor` and Telegram is not queried

#### Scenario: Rate limit and unavailability are not empty pages

- **WHEN** Telegram answers FLOOD_WAIT
- **THEN** the response is 429 with `Retry-After` and `retry_after_seconds`
- **WHEN** Telegram is not connected, the upstream call fails, the account changed during the request, or a non-empty page makes no cursor progress
- **THEN** the response is 503 with code `telegram_unavailable`, never an empty page or `has_more: false`

#### Scenario: Legacy clients without pagination parameters

- **WHEN** a Telegram session is requested without `pagination=telegram`
- **THEN** the response is an array containing only the latest bounded page


<!-- @trace
source: telegram-chat-sync-media
updated: 2026-10-20
code:
  - internal/server/server.go
  - internal/tgclient/events.go
  - internal/tgclient/send.go
  - internal/tgclient/runner.go
  - scripts/telegram-chat.test.cjs
  - internal/tgclient/media.go
  - internal/api/session.go
  - internal/api/telegram_media.go
  - internal/static/js/hooks/useChatSocket.js
  - go.sum
  - internal/api/share.go
  - internal/agent/factory.go
  - internal/static/js/session/NewSessionComposer.js
  - internal/ws/sync.go
  - internal/static/js/app.js
  - internal/ws/broadcast.go
  - internal/ws/telegram.go
  - internal/config/config.go
  - config.example.yaml
  - internal/db/message.go
  - internal/api/telegram_history.go
  - internal/db/session.go
  - internal/static/js/ui/uiAtoms.js
  - internal/tgclient/history.go
  - internal/static/js/chat/ChatView.js
  - internal/static/AGENT.md
  - internal/ws/handler.go
  - internal/static/js/session/SessionView.js
  - internal/tgclient/manager.go
  - internal/server/guest.go
  - internal/static/js/core/core.js
  - internal/static/js/hooks/telegramTimeline.js
  - internal/static/index.html
  - go.mod
tests:
  - internal/server/guest_test.go
  - internal/tgclient/helpers_test.go
  - internal/tgclient/media_test.go
  - internal/tgclient/history_test.go
  - internal/tgclient/send_mem_test.go
  - internal/ws/telegram_test.go
  - internal/server/telegram_history_api_test.go
  - internal/tgclient/manager_test.go
-->

---
### Requirement: Live updates over WebSocket

The system SHALL convert Telegram updates for the bound peer into the same message DTO used by history and push them over WebSocket without persisting them. The `telegram_scope` event SHALL carry only the verified scope and `sync_state` (`ready` or `offline`).

#### Scenario: Reload recovers the timeline

- **WHEN** the page is reloaded after messages arrived while it was closed
- **THEN** the latest page is fetched from Telegram and contains those messages, with no local catch-up step

#### Scenario: Sync carries scope only

- **WHEN** a WebSocket client connects to a Telegram session
- **THEN** the sync message carries the scope and sync state but no message history


<!-- @trace
source: telegram-chat-sync-media
updated: 2026-10-20
code:
  - internal/server/server.go
  - internal/tgclient/events.go
  - internal/tgclient/send.go
  - internal/tgclient/runner.go
  - scripts/telegram-chat.test.cjs
  - internal/tgclient/media.go
  - internal/api/session.go
  - internal/api/telegram_media.go
  - internal/static/js/hooks/useChatSocket.js
  - go.sum
  - internal/api/share.go
  - internal/agent/factory.go
  - internal/static/js/session/NewSessionComposer.js
  - internal/ws/sync.go
  - internal/static/js/app.js
  - internal/ws/broadcast.go
  - internal/ws/telegram.go
  - internal/config/config.go
  - config.example.yaml
  - internal/db/message.go
  - internal/api/telegram_history.go
  - internal/db/session.go
  - internal/static/js/ui/uiAtoms.js
  - internal/tgclient/history.go
  - internal/static/js/chat/ChatView.js
  - internal/static/AGENT.md
  - internal/ws/handler.go
  - internal/static/js/session/SessionView.js
  - internal/tgclient/manager.go
  - internal/server/guest.go
  - internal/static/js/core/core.js
  - internal/static/js/hooks/telegramTimeline.js
  - internal/static/index.html
  - go.mod
tests:
  - internal/server/guest_test.go
  - internal/tgclient/helpers_test.go
  - internal/tgclient/media_test.go
  - internal/tgclient/history_test.go
  - internal/tgclient/send_mem_test.go
  - internal/ws/telegram_test.go
  - internal/server/telegram_history_api_test.go
  - internal/tgclient/manager_test.go
-->

---
### Requirement: Text send with in-memory reconciliation

The system SHALL send text with a fresh `random_id` kept only in memory, show a pending bubble identified by `client_message_id`, and replace it with the real message when `updateMessageID` arrives, using `replaces_client_message_id`. A failure SHALL mark the pending item `failed` with a visible error and SHALL NOT retry automatically.

#### Scenario: Successful send

- **WHEN** the user sends text and Telegram confirms with the real message ID
- **THEN** the pending bubble is replaced by the real message and no extra confirmation bubble is added

#### Scenario: Failed send

- **WHEN** the send fails because Telegram is not connected, the account does not match, the RPC is rejected, or FLOOD_WAIT is returned
- **THEN** the pending item is marked `failed`, no RPC is made for an account mismatch, and the user can resend manually


<!-- @trace
source: telegram-chat-sync-media
updated: 2026-10-20
code:
  - internal/server/server.go
  - internal/tgclient/events.go
  - internal/tgclient/send.go
  - internal/tgclient/runner.go
  - scripts/telegram-chat.test.cjs
  - internal/tgclient/media.go
  - internal/api/session.go
  - internal/api/telegram_media.go
  - internal/static/js/hooks/useChatSocket.js
  - go.sum
  - internal/api/share.go
  - internal/agent/factory.go
  - internal/static/js/session/NewSessionComposer.js
  - internal/ws/sync.go
  - internal/static/js/app.js
  - internal/ws/broadcast.go
  - internal/ws/telegram.go
  - internal/config/config.go
  - config.example.yaml
  - internal/db/message.go
  - internal/api/telegram_history.go
  - internal/db/session.go
  - internal/static/js/ui/uiAtoms.js
  - internal/tgclient/history.go
  - internal/static/js/chat/ChatView.js
  - internal/static/AGENT.md
  - internal/ws/handler.go
  - internal/static/js/session/SessionView.js
  - internal/tgclient/manager.go
  - internal/server/guest.go
  - internal/static/js/core/core.js
  - internal/static/js/hooks/telegramTimeline.js
  - internal/static/index.html
  - go.mod
tests:
  - internal/server/guest_test.go
  - internal/tgclient/helpers_test.go
  - internal/tgclient/media_test.go
  - internal/tgclient/history_test.go
  - internal/tgclient/send_mem_test.go
  - internal/ws/telegram_test.go
  - internal/server/telegram_history_api_test.go
  - internal/tgclient/manager_test.go
-->

---
### Requirement: Stable paginated timeline

The client SHALL merge history pages, live messages and pending items by remote message ID (and `client_message_id` for pending items) without overwriting already loaded pages. Loading older messages SHALL be triggered only by user scrolling to the top or an explicit control, SHALL allow at most one in-flight page request per session view, SHALL stop at the end of history, and SHALL offer retry on errors. Split panes SHALL keep independent cursors and loading state, and late responses for a previous session SHALL be ignored.

#### Scenario: Duplicate delivery of the same message

- **WHEN** the same remote message arrives through both a history page and a live event
- **THEN** exactly one entry is shown

#### Scenario: Error is not the end of history

- **WHEN** an older-page request fails with 429, 503 or a network error
- **THEN** the control shows a retry (respecting `Retry-After` for 429) and `has_more` is not treated as false

#### Scenario: Reading position is preserved

- **WHEN** an older page is prepended or an image finishes loading while the user is reading above the bottom
- **THEN** the visible message stays anchored and a new message shows a click-to-scroll hint instead of forcing the view to the bottom


<!-- @trace
source: telegram-chat-sync-media
updated: 2026-10-20
code:
  - internal/server/server.go
  - internal/tgclient/events.go
  - internal/tgclient/send.go
  - internal/tgclient/runner.go
  - scripts/telegram-chat.test.cjs
  - internal/tgclient/media.go
  - internal/api/session.go
  - internal/api/telegram_media.go
  - internal/static/js/hooks/useChatSocket.js
  - go.sum
  - internal/api/share.go
  - internal/agent/factory.go
  - internal/static/js/session/NewSessionComposer.js
  - internal/ws/sync.go
  - internal/static/js/app.js
  - internal/ws/broadcast.go
  - internal/ws/telegram.go
  - internal/config/config.go
  - config.example.yaml
  - internal/db/message.go
  - internal/api/telegram_history.go
  - internal/db/session.go
  - internal/static/js/ui/uiAtoms.js
  - internal/tgclient/history.go
  - internal/static/js/chat/ChatView.js
  - internal/static/AGENT.md
  - internal/ws/handler.go
  - internal/static/js/session/SessionView.js
  - internal/tgclient/manager.go
  - internal/server/guest.go
  - internal/static/js/core/core.js
  - internal/static/js/hooks/telegramTimeline.js
  - internal/static/index.html
  - go.mod
tests:
  - internal/server/guest_test.go
  - internal/tgclient/helpers_test.go
  - internal/tgclient/media_test.go
  - internal/tgclient/history_test.go
  - internal/tgclient/send_mem_test.go
  - internal/ws/telegram_test.go
  - internal/server/telegram_history_api_test.go
  - internal/tgclient/manager_test.go
-->

---
### Requirement: Sharing is disabled for Telegram sessions

The system SHALL NOT allow sharing of Telegram sessions. Creating a share for a Telegram session SHALL be rejected for every mode, guest requests bound to a Telegram session SHALL be denied even when the share was created before sharing was disabled, and the client SHALL NOT show a share control for Telegram sessions.

#### Scenario: Share creation is rejected

- **WHEN** a share is created for a Telegram session with mode `live` or `snapshot`
- **THEN** the response is 400

#### Scenario: Pre-existing share is denied

- **WHEN** a guest holding a share token created before this change requests messages or media of a Telegram session
- **THEN** the response is 403 with code `share_forbidden`

<!-- @trace
source: telegram-chat-sync-media
updated: 2026-10-20
code:
  - internal/server/server.go
  - internal/tgclient/events.go
  - internal/tgclient/send.go
  - internal/tgclient/runner.go
  - scripts/telegram-chat.test.cjs
  - internal/tgclient/media.go
  - internal/api/session.go
  - internal/api/telegram_media.go
  - internal/static/js/hooks/useChatSocket.js
  - go.sum
  - internal/api/share.go
  - internal/agent/factory.go
  - internal/static/js/session/NewSessionComposer.js
  - internal/ws/sync.go
  - internal/static/js/app.js
  - internal/ws/broadcast.go
  - internal/ws/telegram.go
  - internal/config/config.go
  - config.example.yaml
  - internal/db/message.go
  - internal/api/telegram_history.go
  - internal/db/session.go
  - internal/static/js/ui/uiAtoms.js
  - internal/tgclient/history.go
  - internal/static/js/chat/ChatView.js
  - internal/static/AGENT.md
  - internal/ws/handler.go
  - internal/static/js/session/SessionView.js
  - internal/tgclient/manager.go
  - internal/server/guest.go
  - internal/static/js/core/core.js
  - internal/static/js/hooks/telegramTimeline.js
  - internal/static/index.html
  - go.mod
tests:
  - internal/server/guest_test.go
  - internal/tgclient/helpers_test.go
  - internal/tgclient/media_test.go
  - internal/tgclient/history_test.go
  - internal/tgclient/send_mem_test.go
  - internal/ws/telegram_test.go
  - internal/server/telegram_history_api_test.go
  - internal/tgclient/manager_test.go
-->