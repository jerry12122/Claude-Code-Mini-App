# telegram-chat-media Specification

## Purpose

Show photos, videos and files of a Telegram private chat without storing them locally, load only images automatically, download videos and files on explicit request, and send uploaded photos and files to Telegram.

## Requirements

### Requirement: Structured private-chat attachments

The system SHALL represent photos, videos and documents in Telegram messages as structured attachments with `id`, `name`, `mime_type`, `size` and a content `url` of the form `/sessions/:id/tg-media/:msgId`, and SHALL NOT expose remote locators, access hashes, file references or local file paths in any public field. Importing or listing messages SHALL NOT read any media content.

#### Scenario: History and live messages carry the same attachment shape

- **WHEN** a message with a photo or document is delivered through a history page or a live event
- **THEN** its attachment has the same fields and a `tg-media` url, and no media bytes were requested to build it


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
### Requirement: Automatic visible image previews

The client SHALL request the content of an image attachment only when it enters the visible area, SHALL display it inline, and SHALL let the user retry a failed preview.

#### Scenario: Off-screen images are not fetched

- **WHEN** a loaded page contains an image that has not been scrolled into view
- **THEN** no content request is made for it until it becomes visible


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
### Requirement: Explicit video and file download

The client SHALL NOT request the content of a video or a general file until the user explicitly clicks download, and SHALL NOT create a `video` source or prefetch a thumbnail for videos. The server SHALL respond to `GET /sessions/:id/tg-media/:msgId` with `Content-Disposition: inline` only for `image/*` content without `download=1`, and `attachment` otherwise.

#### Scenario: Nothing is downloaded before the click

- **WHEN** a page with a video and a document is rendered
- **THEN** only their metadata and a download control are shown and no content request is sent

#### Scenario: Click downloads only the chosen attachment

- **WHEN** the user clicks download on one document
- **THEN** exactly that attachment is requested with `download=1` and served as an attachment


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
### Requirement: Streamed transfer without local storage

The server SHALL stream media from Telegram (`upload.getFile`) to the HTTP response without writing it to disk or buffering the whole file in memory, SHALL set `Cache-Control: private, no-store` and `X-Content-Type-Options: nosniff`, and SHALL treat the remote file name as display text only. Requests SHALL use the existing authentication; a non-positive or non-numeric `msgId` SHALL be rejected with 400 and an unavailable connection SHALL produce 503 `telegram_unavailable`.

#### Scenario: Media request without a connection

- **WHEN** media is requested while Telegram is not connected
- **THEN** the response is 503 with code `telegram_unavailable`


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
### Requirement: Send uploaded photos and files

The system SHALL reuse the existing upload and WebSocket input flow to send attachments to Telegram. `image/jpeg`, `image/png` and `image/webp` SHALL be sent as photos; every other file SHALL be sent as a document keeping its original file name. Text is optional. Multiple attachments SHALL be sent in selection order as separate messages; attachments already sent SHALL be kept and the remaining ones marked `failed`, and the server path of an upload SHALL NOT appear in the sent content.

#### Scenario: Photo versus document

- **WHEN** the user sends a PNG and a ZIP
- **THEN** the PNG is sent as a photo and the ZIP as a document named like the original file

#### Scenario: Attachment-only message

- **WHEN** the user sends an attachment with no text
- **THEN** it is sent and no extra confirmation bubble is added

#### Scenario: Partial failure

- **WHEN** the second of three attachments fails to send
- **THEN** the first stays sent, the failed and remaining items are marked `failed`, and resending can duplicate the already sent item

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