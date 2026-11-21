## Purpose

Keep the room queue preview compact so owners can review queued work without crowding the conversation and composer, while retaining editing and touch access.

## ADDED Requirements

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
