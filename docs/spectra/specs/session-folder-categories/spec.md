# session-folder-categories Specification

## Purpose

Allow owners to organize sessions into custom categories within existing work-directory groups without moving files or changing session execution.

## Requirements

### Requirement: Folder category management

The system SHALL allow the owner to create, rename and delete one level of categories scoped to an existing nonempty work directory. Names SHALL be trimmed, contain 1 through 80 Unicode code points, and be unique by exact trimmed spelling within that directory. Invalid input SHALL leave stored data unchanged. Categories SHALL have stable identifiers independent of names and SHALL persist across reloads and devices.

#### Scenario: Manage independent categories

- **WHEN** the owner creates a category named Review in directory A and another named Review in directory B, then renames the first
- **THEN** the identifiers and memberships SHALL remain stable and directory B SHALL remain unchanged
- **AND** an empty, overlength or duplicate name within A SHALL be rejected without mutation

#### Scenario: Delete without deleting work

- **WHEN** the owner deletes a category containing sessions
- **THEN** its sessions SHALL become unclassified in one transaction
- **AND** their history, queue, running process and working directory SHALL remain unchanged


<!-- @trace
source: session-organization
updated: 2026-11-21
code:
  - internal/api/session.go
  - internal/api/session_category.go
  - scripts/session-category-ui.test.py
  - scripts/session-category-drop.test.cjs
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
### Requirement: Directory scoped session membership

Each session SHALL belong to at most one category of its own work directory or remain unclassified. Existing and newly created sessions SHALL default to unclassified. The server SHALL reject a missing or foreign-directory category without mutation. Changing a session work directory through any supported path SHALL clear an incompatible category atomically. Category changes SHALL NOT update session activity timestamps or message read state.

#### Scenario: Assign and clear membership

- **WHEN** the owner assigns an unclassified session in A to a category in A, reloads, then removes the assignment
- **THEN** the assignment SHALL survive reload and subsequently return to unclassified
- **AND** attempts to assign a category in B or a deleted identifier SHALL fail without changing the session

#### Scenario: Concurrent delete and assignment

- **WHEN** a category deletion races with assignment to that category
- **THEN** the committed session SHALL reference an existing category in its own directory or be unclassified
- **AND** no dangling membership SHALL remain


<!-- @trace
source: session-organization
updated: 2026-11-21
code:
  - internal/api/session.go
  - internal/api/session_category.go
  - scripts/session-category-ui.test.py
  - scripts/session-category-drop.test.cjs
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
### Requirement: Category presentation and owner isolation

The expanded directory group SHALL display named categories in ascending name order and an unclassified section last, omitting empty unclassified sections. Empty named categories SHALL remain visible when no search is active. Existing session search SHALL filter sessions before category rendering and reveal matching categories. Existing session sort SHALL apply within each category. Pinned sessions SHALL remain in the existing pinned group without duplication and retain their category for unpinning. Synthetic pinned, Telegram and unset-directory groups SHALL NOT acquire category management. Only authenticated owners SHALL mutate categories or memberships; guest access SHALL NOT expose category metadata. All category actions SHALL be available with keyboard and touch without requiring drag and drop.

#### Scenario: Search and pinned sessions

- **WHEN** a categorized session is pinned and later unpinned, or found through session search
- **THEN** it SHALL appear once in the appropriate existing pinned group or its directory category
- **AND** search SHALL reveal its matching row without losing stored membership

#### Scenario: Access and device consistency

- **WHEN** the owner changes categories with keyboard or touch and another owner device reloads
- **THEN** both devices SHALL show the same stored categories and membership
- **AND** a guest attempt to read category metadata or mutate organization SHALL be denied without mutation

### Requirement: Drag a session onto a category

The owner SHALL be able to drag a session row, including a pinned session, onto an existing category in that session's own work directory, or back to unclassified. The unclassified drop target SHALL appear during the drag when that directory has no visible unclassified section, and SHALL NOT be a permanent panel. A drop on the same category SHALL NOT write. A drop SHALL NOT open the session, move it across directories, or clear manual unread. Cancelling, dropping external text or files, or dropping a missing, foreign, or virtual-group item SHALL leave membership unchanged. A failed assignment SHALL reload stored membership and keep the error visible. Guests SHALL NOT assign by drag. Keyboard and touch SHALL still assign through the existing menu.

#### Scenario: Move within one directory

- **WHEN** the owner drags an uncategorized or pinned session onto a category of the same work directory, or drags a categorized session to another category or to unclassified
- **THEN** the stored category SHALL match the drop target after reload
- **AND** pin, unread, manual reminder, sort, active session, and queue state SHALL remain unchanged

#### Scenario: Rejected drops

- **WHEN** the drag is cancelled, comes from outside the session list, targets another directory or a deleted category, or the assignment request fails
- **THEN** the session SHALL remain in its stored category
- **AND** a failed request SHALL NOT leave the row visually moved

<!-- @trace
source: session-organization
updated: 2026-11-21
code:
  - internal/api/session.go
  - internal/api/session_category.go
  - scripts/session-category-ui.test.py
  - scripts/session-category-drop.test.cjs
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