# room-queue-density Specification

## Purpose

Keep the room queue preview compact so owners can review queued work without crowding the conversation and composer, while retaining editing and touch access.

## Requirements

### Requirement: Compact queue preview preserves conversation space

The room queue SHALL use a compact preview with its count, a single-line summary for each unedited item, attachment identification and existing available actions. Longer queues SHALL scroll inside their existing bounded region rather than pushing the composer out of reach. With no queued items, local pending items, editor or error, the region SHALL occupy no space. Compaction SHALL NOT alter FIFO order, source isolation, queue ownership, batch scheduling or delivery behavior.

#### Scenario: One queued item and a long queue

- **WHEN** an owner adds one item and later enough items to exceed the queue region limit
- **THEN** the first state SHALL show a compact summary and the later state SHALL allow scrolling to every item inside the queue region
- **AND** the conversation and composer SHALL remain usable without a page-level horizontal scrollbar

##### Example: Empty queue releases space

- **GIVEN** one queued item is shown with no editor, local pending item or error
- **WHEN** that item leaves the queue
- **THEN** the queue region SHALL disappear without reserving its previous height


<!-- @trace
source: room-queue-compact
updated: 2026-11-21
code:
  - README.zh-TW.md
  - scripts/browser/conftest.py
  - internal/static/js/hooks/useRoomTimeline.js
  - VERSION
  - README.md
  - cmd/desktop/desktop_windows_amd64.syso
  - internal/static/js/session/RoomView.js
  - scripts/build-css.py
  - internal/version/version.go
  - CHANGELOG.md
  - internal/db/room_queue.go
  - internal/static/js/session/RoomControls.js
  - scripts/rooms.test.cjs
  - scripts/release/package.sh
tests:
  - internal/ws/ws_test_lifecycle_test.go
  - scripts/browser/test_ui_review.py
  - internal/server/room_queue_batch_test.go
  - internal/ws/quota_resume_test.go
  - internal/desktopupdate/release_contract_test.go
  - internal/ws/queue_test.go
  - internal/db/queue_batch_test.go
  - scripts/browser/test_room_queue_edit.py
  - internal/ws/share_test.go
-->

---
### Requirement: Queue density preserves controls and recovery

Refresh, edit, batch and cancel controls SHALL retain their existing enablement and behavior, accessible names and keyboard access. Touch input SHALL retain usable target areas. Editing, pending confirmations, preserved drafts and error messages SHALL remain readable and reachable through the queue region without fixed-height clipping. Compaction SHALL NOT auto-submit, discard a draft, acknowledge a batch or suppress a server error.

#### Scenario: Edit and recover in a compact region

- **WHEN** the owner opens an item editor and a save is rejected or the item leaves the queue
- **THEN** the draft, diagnostic and existing recovery controls SHALL remain reachable
- **AND** cancellation SHALL use the existing explicit action without silently discarding work

#### Scenario: Touch and keyboard actions remain available

- **WHEN** the owner uses touch or keyboard to reach queue actions in a narrow viewport
- **THEN** every available action SHALL remain operable without overlapping controls or hiding attachment identification
- **AND** a batch or error state SHALL remain distinguishable from a successful delivery

<!-- @trace
source: room-queue-compact
updated: 2026-11-21
code:
  - README.zh-TW.md
  - scripts/browser/conftest.py
  - internal/static/js/hooks/useRoomTimeline.js
  - VERSION
  - README.md
  - cmd/desktop/desktop_windows_amd64.syso
  - internal/static/js/session/RoomView.js
  - scripts/build-css.py
  - internal/version/version.go
  - CHANGELOG.md
  - internal/db/room_queue.go
  - internal/static/js/session/RoomControls.js
  - scripts/rooms.test.cjs
  - scripts/release/package.sh
tests:
  - internal/ws/ws_test_lifecycle_test.go
  - scripts/browser/test_ui_review.py
  - internal/server/room_queue_batch_test.go
  - internal/ws/quota_resume_test.go
  - internal/desktopupdate/release_contract_test.go
  - internal/ws/queue_test.go
  - internal/db/queue_batch_test.go
  - scripts/browser/test_room_queue_edit.py
  - internal/ws/share_test.go
-->