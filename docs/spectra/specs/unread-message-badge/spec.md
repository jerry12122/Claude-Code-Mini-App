# unread-message-badge Specification

## Purpose

Provides a chat-application-style unread message counter badge on session cards and sidebars, displaying the exact count of new incoming messages since the session was last read.

## Requirements

### Requirement: Unread message count calculation

The system SHALL compute `unread_count` for each session representing the total number of completed non-user messages that have not been acknowledged as read. Pending replies SHALL NOT count until they complete. Telegram sessions SHALL always report an `unread_count` of 0, because their messages are not stored locally.

#### Scenario: Accurate count of unread incoming messages

- **WHEN** a session receives completed assistant, shell, or agent messages
- **THEN** the session query SHALL return `unread_count` matching the exact number of incoming messages

#### Scenario: Task completion requires reading the result

- **WHEN** a task completes and returns to idle at the point where its completion notification is sent
- **THEN** its completed reply SHALL become unread, including when the user read the session during execution or disconnected before completion
- **AND** task completion SHALL NOT itself acknowledge reading

#### Scenario: Foreground reading acknowledges loaded results

- **WHEN** the owner views a loaded completed result in the focused chat, with the document visible and focused and the chat scrolled to the latest result
- **THEN** the frontend SHALL acknowledge the loaded completed message IDs
- **AND** background pages, unfocused panes, pending replies and reading older history SHALL NOT acknowledge the new result
- **AND** delayed receipts SHALL NOT acknowledge messages absent from the receipt

#### Scenario: Read all preserves future completions

- **WHEN** the user marks all sessions read while a task is running
- **THEN** only already completed results SHALL be acknowledged
- **AND** the running reply SHALL become unread upon completion

#### Scenario: Read receipts survive upgrades and restarts

- **WHEN** an existing database is upgraded
- **THEN** historical completed messages SHALL retain the read state implied by the previous `last_read_at`
- **AND** subsequent restarts SHALL preserve explicit per-message read receipts

#### Scenario: User messages excluded from unread count

- **WHEN** a user posts an outgoing prompt
- **THEN** the system SHALL NOT count user-authored messages toward `unread_count`

#### Scenario: Telegram sessions report no unread count

- **WHEN** a Telegram session is listed
- **THEN** its `unread_count` SHALL be 0 regardless of incoming Telegram messages


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
  - internal/static/js/ui/ui-atoms.js
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
### Requirement: Unread count badge rendering in UI
The frontend SHALL display a pill-shaped unread badge on each session card and sidebar icon when `unread_count > 0`.

#### Scenario: Display exact numeric badge
- **WHEN** a session has an `unread_count` between 1 and 99
- **THEN** the UI SHALL render the numeric badge showing that exact number

#### Scenario: Display overflow badge
- **WHEN** a session has an `unread_count` greater than 99
- **THEN** the UI SHALL render `99+` inside the badge

#### Scenario: Clear only after confirming reading
- **WHEN** the user navigates into or clicks a session with unread messages
- **THEN** navigation alone SHALL NOT clear unread results
- **AND** the badge SHALL reflect the backend count after the visible chat acknowledges loaded completed results
- **AND** active sessions with unread results SHALL retain their badge and contribute to the document title count

<!-- @trace
source: post-message-and-unread-badge
updated: 2026-10-13
code:
  - scripts/selection-detect.test.cjs
  - internal/static/js/chat/SelectionQuotePopover.js
  - scripts/split-pane-quote.test.cjs
  - internal/mcp/server.go
  - internal/static/index.html
  - internal/mcp/tools.go
  - internal/server/server.go
  - internal/static/js/core/core.js
  - scripts/popover-position.test.cjs
  - internal/db/message.go
  - internal/ws/handler.go
  - scripts/quote-reply-action.test.cjs
  - scripts/format-quote.test.cjs
  - internal/static/js/chat/ChatView.js
  - scripts/selection-quote-integration.test.cjs
  - internal/ws/broadcast.go
  - internal/db/session.go
  - scripts/textarea-autogrow.test.cjs
  - internal/static/js/session/SessionView.js
tests:
  - internal/mcp/post_message_test.go
  - internal/db/unread_smoke_test.go
  - internal/api/session_test.go
-->
