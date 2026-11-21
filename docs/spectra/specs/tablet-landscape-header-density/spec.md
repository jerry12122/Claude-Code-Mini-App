# tablet-landscape-header-density Specification

2026-11-21 使用者更正：桌面與平板橫向要恢復第二列（工作目錄、模型、額度、重新整理）。把資訊壓成一列不再是成功條件。上方多餘 gap 與列數分開；沒有原 iPad inset 量測不得宣稱 gap 已修。本檔是現行契約，封存的 change 不改、舊驗收不回溯改寫。

## Purpose

Keep the desktop and landscape-tablet conversation header readable in two rows, while preserving host safety clearances, touch interactions, and the existing phone layout.

## Requirements

### Requirement: Desktop and landscape tablet header keeps a second information row

The system SHALL place the session title, state, Git badge, and primary actions on the first row, and the work directory, model, quota, and quota refresh on a second row beneath it, for desktop and tablet landscape. Collapsing that secondary information onto the first row SHALL NOT satisfy this requirement. An unnecessary top gap is a separate defect and SHALL be claimed fixed only when measured padding or inset evidence identifies an offset the host does not require.

#### Scenario: Secondary information stays below the title row

- **WHEN** an owner views a conversation on a desktop or landscape tablet with a work directory, model, and quota
- **THEN** the title row contains the title, state, Git, and primary actions, and the work directory, model, quota, and refresh are on a row whose top is below the first row

#### Scenario: Top gap is not inferred from row count

- **WHEN** no original iPad inset and padding capture is available
- **THEN** the implementation does not treat a shorter or taller header as proof that the top gap is fixed


<!-- @trace
source: tablet-landscape-header-compact
updated: 2026-11-21
code:
  - scripts/session-ask-indicator.test.cjs
  - VERSION
  - README.zh-TW.md
  - cmd/desktop/desktop_windows_amd64.syso
  - internal/version/version.go
  - CHANGELOG.md
  - scripts/build-css.py
  - docs/examples/miniapp-pm-example/references/authoring-guide.md
  - internal/static/index.html
  - docs/examples/miniapp-pm-example/SKILL.md
  - internal/static/js/session/SessionView.js
  - scripts/sidebar-status-parity.test.py
  - scripts/sidebar-pin-marker.test.py
  - internal/static/js/session/RoomControls.js
  - internal/static/js/chat/ChatHeader.js
  - README.md
  - scripts/release/package.sh
  - internal/static/js/ui/ContextMenu.js
tests:
  - scripts/chat-header-rows.test.py
  - internal/mcp/post_message_test.go
  - internal/desktopupdate/release_contract_test.go
-->

---
### Requirement: Touch targets and host clearance remain usable

The system SHALL keep application controls clear of operating-system and Telegram host controls, preserve usable touch targets, and retain normal layouts when host inset APIs are absent. The system SHALL respond to orientation, host inset, fullscreen, and keyboard changes without retaining obsolete offsets.

#### Scenario: Host clearance changes after rotation

- **WHEN** a Telegram tablet rotates or reports updated content safe-area insets
- **THEN** application controls remain outside required host clearances, the conversation and composer remain operable, and obsolete spacing is removed

#### Scenario: Browser without Telegram APIs

- **WHEN** the conversation opens in a normal browser or a host without content-safe-area APIs
- **THEN** header actions remain operable without script errors or unnecessary host-only spacing

#### Scenario: Other layouts retain access

- **WHEN** the user changes between tablet portrait, phone, desktop, split-pane, or an open-keyboard layout
- **THEN** session navigation, header actions, conversation scrolling, and composer access remain available without new clipping or page-level horizontal scrolling

<!-- @trace
source: tablet-landscape-header-compact
updated: 2026-11-21
code:
  - scripts/session-ask-indicator.test.cjs
  - VERSION
  - README.zh-TW.md
  - cmd/desktop/desktop_windows_amd64.syso
  - internal/version/version.go
  - CHANGELOG.md
  - scripts/build-css.py
  - docs/examples/miniapp-pm-example/references/authoring-guide.md
  - internal/static/index.html
  - docs/examples/miniapp-pm-example/SKILL.md
  - internal/static/js/session/SessionView.js
  - scripts/sidebar-status-parity.test.py
  - scripts/sidebar-pin-marker.test.py
  - internal/static/js/session/RoomControls.js
  - internal/static/js/chat/ChatHeader.js
  - README.md
  - scripts/release/package.sh
  - internal/static/js/ui/ContextMenu.js
tests:
  - scripts/chat-header-rows.test.py
  - internal/mcp/post_message_test.go
  - internal/desktopupdate/release_contract_test.go
-->