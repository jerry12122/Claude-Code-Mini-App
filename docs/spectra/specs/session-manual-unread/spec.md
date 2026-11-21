# session-manual-unread Specification

## Purpose

Allow owners to mark a session for later reading without falsifying message unread counts or changing execution state.

## Requirements

### Requirement: Independent manual unread marker

The system SHALL persist a session-level manual unread marker separately from per-message read receipts. Marking unread SHALL set that marker even for an empty session or a session with zero unread messages. It SHALL NOT change message read flags, unread_count, last_active, session order, running processes or queues. Telegram unread_count SHALL remain zero while its manual marker is available. Only authenticated owners SHALL read or modify this marker; guests SHALL NOT change owner read state.

#### Scenario: Mark an already read session

- **WHEN** the owner marks a session with unread_count zero as unread and reloads on another owner device
- **THEN** the marker SHALL remain set on both devices and unread_count SHALL remain zero
- **AND** a guest mutation attempt SHALL leave the marker unchanged

#### Scenario: Preserve genuine unread messages

- **WHEN** the owner marks a session with three unread completed messages as unread
- **THEN** unread_count SHALL remain three and all stored message receipts SHALL remain unchanged


<!-- @trace
source: session-organization
updated: 2026-11-21
code:
  - internal/api/session.go
  - internal/api/session_category.go
  - scripts/session-category-ui.test.py
  - internal/static/js/hooks/useChatSocket.js
  - internal/static/index.html
  - internal/db/manual_unread.go
  - scripts/sidebar-pin-marker.test.py
  - internal/static/js/session/SessionCategories.js
  - internal/static/js/chat/ChatHeader.js
  - internal/static/js/app.js
  - scripts/manual-unread-ui.test.py
  - scripts/sidebar-status-parity.test.py
  - internal/static/js/session/SessionView.js
  - scripts/remaining-ux.test.cjs
  - internal/db/session_category.go
  - internal/static/js/session/manualUnread.js
  - internal/db/session.go
  - internal/server/server.go
  - internal/static/js/vendor/tailwind.css
  - internal/db/migrate.go
tests:
  - internal/api/manual_unread_clear_test.go
  - internal/api/session_membership_test.go
  - internal/db/manual_unread_test.go
  - internal/server/manual_unread_guest_test.go
  - internal/ws/manual_unread_sync_test.go
  - internal/server/session_category_guest_test.go
  - internal/api/session_category_test.go
  - internal/db/session_category_test.go
  - internal/db/session_membership_test.go
  - internal/api/manual_unread_test.go
  - internal/server/session_membership_guest_test.go
-->

---
### Requirement: Revision guarded manual marker clearing

Every marker state change SHALL advance a server-owned per-session revision. Clearing a manual marker SHALL require its observed revision and SHALL succeed only when that revision still matches. Ordinary message-ID acknowledgements SHALL NOT clear the marker. Explicit mark-read SHALL clear the observed marker and acknowledge completed messages only. Read-all SHALL clear only marker revisions observed when the action began. An active session newly marked unread SHALL retain its marker until explicit mark-read or a later activation that confirms visible focused reading at the latest result; remaining in that session or reloading it SHALL NOT count as that later activation. Automatic clearing SHALL also require a nonempty loaded completed result. Stale clearing attempts SHALL preserve newer markers and return current state for reconciliation.

#### Scenario: Late read receipt preserves a newer reminder

- **WHEN** window A observes revision 4 and window B marks unread at revision 5 before A sends its clear
- **THEN** A SHALL NOT clear revision 5
- **AND** late message-only acknowledgements SHALL NOT clear it either

#### Scenario: Revisit or explicitly mark read

- **WHEN** an owner marks the currently open session unread
- **THEN** its marker SHALL persist while that activation remains open and after reload
- **AND** leaving and returning to visible focused latest completed content SHALL clear the observed revision
- **AND** an empty session SHALL retain the marker until explicit mark-read

#### Scenario: Read all preserves later work

- **WHEN** read-all races with a new manual mark or a pending reply completing
- **THEN** the newer manual mark and the newly completed unread message SHALL remain unread independently


<!-- @trace
source: session-organization
updated: 2026-11-21
code:
  - internal/api/session.go
  - internal/api/session_category.go
  - scripts/session-category-ui.test.py
  - internal/static/js/hooks/useChatSocket.js
  - internal/static/index.html
  - internal/db/manual_unread.go
  - scripts/sidebar-pin-marker.test.py
  - internal/static/js/session/SessionCategories.js
  - internal/static/js/chat/ChatHeader.js
  - internal/static/js/app.js
  - scripts/manual-unread-ui.test.py
  - scripts/sidebar-status-parity.test.py
  - internal/static/js/session/SessionView.js
  - scripts/remaining-ux.test.cjs
  - internal/db/session_category.go
  - internal/static/js/session/manualUnread.js
  - internal/db/session.go
  - internal/server/server.go
  - internal/static/js/vendor/tailwind.css
  - internal/db/migrate.go
tests:
  - internal/api/manual_unread_clear_test.go
  - internal/api/session_membership_test.go
  - internal/db/manual_unread_test.go
  - internal/server/manual_unread_guest_test.go
  - internal/ws/manual_unread_sync_test.go
  - internal/server/session_category_guest_test.go
  - internal/api/session_category_test.go
  - internal/db/session_category_test.go
  - internal/db/session_membership_test.go
  - internal/api/manual_unread_test.go
  - internal/server/session_membership_guest_test.go
-->

---
### Requirement: Accessible manual unread presentation

The owner session menu SHALL expose mark-unread and mark-read actions through keyboard and touch. With a marker and zero unread_count the session SHALL display a nonnumeric reminder with an accessible label. With genuine unread messages the existing exact numeric badge and overflow behavior SHALL remain; the accessible label SHALL also identify a manual reminder when present. Document title totals SHALL count actual unread messages only. A failed mutation SHALL be reported and reconciled with server state rather than left as a false success.

#### Scenario: Numeric badges remain truthful

- **WHEN** sessions with manual markers have unread_count values zero, three and 100
- **THEN** they SHALL show a reminder, the numeric badge 3 and the numeric badge 99+ respectively
- **AND** their combined contribution to the document title unread total SHALL be 103

#### Scenario: Accessible and failed actions

- **WHEN** an owner uses keyboard or touch to mark unread and the server rejects the request
- **THEN** the UI SHALL announce the failure and reconcile the displayed marker with server state

<!-- @trace
source: session-organization
updated: 2026-11-21
code:
  - internal/api/session.go
  - internal/api/session_category.go
  - scripts/session-category-ui.test.py
  - internal/static/js/hooks/useChatSocket.js
  - internal/static/index.html
  - internal/db/manual_unread.go
  - scripts/sidebar-pin-marker.test.py
  - internal/static/js/session/SessionCategories.js
  - internal/static/js/chat/ChatHeader.js
  - internal/static/js/app.js
  - scripts/manual-unread-ui.test.py
  - scripts/sidebar-status-parity.test.py
  - internal/static/js/session/SessionView.js
  - scripts/remaining-ux.test.cjs
  - internal/db/session_category.go
  - internal/static/js/session/manualUnread.js
  - internal/db/session.go
  - internal/server/server.go
  - internal/static/js/vendor/tailwind.css
  - internal/db/migrate.go
tests:
  - internal/api/manual_unread_clear_test.go
  - internal/api/session_membership_test.go
  - internal/db/manual_unread_test.go
  - internal/server/manual_unread_guest_test.go
  - internal/ws/manual_unread_sync_test.go
  - internal/server/session_category_guest_test.go
  - internal/api/session_category_test.go
  - internal/db/session_category_test.go
  - internal/db/session_membership_test.go
  - internal/api/manual_unread_test.go
  - internal/server/session_membership_guest_test.go
-->