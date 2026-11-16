# quota-usage-bars Specification

## Purpose

Defines how account usage quota is exposed as structured windows and rendered as compact per-runner progress bars in the chat header. It keeps the header short while preserving the full text as a fallback.

## Requirements

### Requirement: Quota payload carries structured windows

The quota snapshot and payload sent over the API, WebSocket, and MCP SHALL include an optional `windows` list and an optional `plan`, and MUST keep `display_text` unchanged.

#### Scenario: Claude snapshot includes windows

- **WHEN** the quota service returns a Claude snapshot with session and weekly data
- **THEN** the payload contains `windows` entries with keys `session` and `weekly` carrying their percent values, and `display_text` equals the previously produced text

#### Scenario: No data available

- **WHEN** a provider returns no usable windows
- **THEN** `windows` is omitted or empty and `display_text` is the existing placeholder

##### Example: window keys per runner

| Runner | Window keys in order |
| ------ | -------------------- |
| claude, codex | session, weekly |
| cursor | billing_total, billing_auto, billing_api |
| kiro | credits |
| antigravity | session, weekly, session_claude, weekly_claude |


<!-- @trace
source: quota-usage-bars
updated: 2026-10-16
code:
  - internal/quota/cursor.go
  - internal/static/js/chat/ChatHeader.js
  - internal/static/js/hooks/useChatSocket.js
  - internal/quota/kiro.go
  - internal/version/version.go
  - scripts/tab-groups.test.cjs
  - internal/static/js/chat/ChatView.js
  - internal/quota/windows.go
  - internal/quota/result.go
  - internal/static/js/ui/SessionTabs.js
  - internal/static/js/app.js
  - internal/static/js/ui/uiAtoms.js
  - internal/static/js/core/core.js
  - internal/static/js/ui/SettingsModal.js
  - AGENTS.md
  - internal/quota/antigravity.go
  - CLAUDE.md
  - internal/quota/codex.go
  - internal/static/js/hooks/tabGroups.js
  - internal/quota/claude.go
  - internal/ws/handler.go
  - internal/static/js/session/SessionView.js
  - internal/static/AGENT.md
  - internal/server/server.go
  - internal/static/index.html
tests:
  - internal/ws/queue_test.go
  - internal/quota/windows_test.go
  - internal/version/version_test.go
-->

---
### Requirement: Header renders runner-specific usage bars

The chat header SHALL render usage as compact progress bars whose shape depends on the session runner, using `windows` data instead of parsing `display_text`.

#### Scenario: Claude or Codex shows two bars

- **WHEN** the runner is claude or codex and both `session` and `weekly` windows are present
- **THEN** the header shows a 5h bar and a Week bar, each with its percentage

#### Scenario: Cursor shows only the total bar

- **WHEN** the runner is cursor with `billing_total`, `billing_auto`, and `billing_api` windows
- **THEN** the header shows only the Total bar and the Auto and API values appear only in the tooltip

#### Scenario: Kiro shows credits with usage numbers

- **WHEN** the runner is kiro with a `credits` window that has used and limit values
- **THEN** the header shows one Credits bar with the used/limit numbers and the plan appears only in the tooltip

#### Scenario: Antigravity shows two grouped pairs without text

- **WHEN** the runner is antigravity with all four windows
- **THEN** the header shows a Gemini group and a Claude/GPT group, each with a 5h bar and a Week bar, and the bars carry no percentage text

#### Scenario: Antigravity stays on one line

- **WHEN** the runner is antigravity with all four windows on a 1280px-wide desktop viewport
- **THEN** the usage area occupies a single line of the header sub-row without wrapping


<!-- @trace
source: quota-usage-bars
updated: 2026-10-16
code:
  - internal/quota/cursor.go
  - internal/static/js/chat/ChatHeader.js
  - internal/static/js/hooks/useChatSocket.js
  - internal/quota/kiro.go
  - internal/version/version.go
  - scripts/tab-groups.test.cjs
  - internal/static/js/chat/ChatView.js
  - internal/quota/windows.go
  - internal/quota/result.go
  - internal/static/js/ui/SessionTabs.js
  - internal/static/js/app.js
  - internal/static/js/ui/uiAtoms.js
  - internal/static/js/core/core.js
  - internal/static/js/ui/SettingsModal.js
  - AGENTS.md
  - internal/quota/antigravity.go
  - CLAUDE.md
  - internal/quota/codex.go
  - internal/static/js/hooks/tabGroups.js
  - internal/quota/claude.go
  - internal/ws/handler.go
  - internal/static/js/session/SessionView.js
  - internal/static/AGENT.md
  - internal/server/server.go
  - internal/static/index.html
tests:
  - internal/ws/queue_test.go
  - internal/quota/windows_test.go
  - internal/version/version_test.go
-->

---
### Requirement: Usage bar color follows thresholds and state

Each bar SHALL be colored by its own window percent and by snapshot state: 100% or more red, 80% or more orange, stale gray, error amber, otherwise the default color.

#### Scenario: Window above eighty percent

- **WHEN** a window has percent 85
- **THEN** its bar is drawn in the orange style

#### Scenario: Window at one hundred percent

- **WHEN** a window has percent 100
- **THEN** its bar is drawn in the red style


<!-- @trace
source: quota-usage-bars
updated: 2026-10-16
code:
  - internal/quota/cursor.go
  - internal/static/js/chat/ChatHeader.js
  - internal/static/js/hooks/useChatSocket.js
  - internal/quota/kiro.go
  - internal/version/version.go
  - scripts/tab-groups.test.cjs
  - internal/static/js/chat/ChatView.js
  - internal/quota/windows.go
  - internal/quota/result.go
  - internal/static/js/ui/SessionTabs.js
  - internal/static/js/app.js
  - internal/static/js/ui/uiAtoms.js
  - internal/static/js/core/core.js
  - internal/static/js/ui/SettingsModal.js
  - AGENTS.md
  - internal/quota/antigravity.go
  - CLAUDE.md
  - internal/quota/codex.go
  - internal/static/js/hooks/tabGroups.js
  - internal/quota/claude.go
  - internal/ws/handler.go
  - internal/static/js/session/SessionView.js
  - internal/static/AGENT.md
  - internal/server/server.go
  - internal/static/index.html
tests:
  - internal/ws/queue_test.go
  - internal/quota/windows_test.go
  - internal/version/version_test.go
-->

---
### Requirement: Tooltip shows full usage detail

The usage area SHALL expose a tooltip listing each window name, percentage, and reset countdown when parsable, followed by the full `display_text`.

#### Scenario: Reset time parsable

- **WHEN** a window has a valid `resets_at`
- **THEN** the tooltip shows the time remaining until reset for that window

#### Scenario: Reset time not parsable

- **WHEN** a window `resets_at` cannot be parsed
- **THEN** the tooltip omits the countdown for that window and shows no error


<!-- @trace
source: quota-usage-bars
updated: 2026-10-16
code:
  - internal/quota/cursor.go
  - internal/static/js/chat/ChatHeader.js
  - internal/static/js/hooks/useChatSocket.js
  - internal/quota/kiro.go
  - internal/version/version.go
  - scripts/tab-groups.test.cjs
  - internal/static/js/chat/ChatView.js
  - internal/quota/windows.go
  - internal/quota/result.go
  - internal/static/js/ui/SessionTabs.js
  - internal/static/js/app.js
  - internal/static/js/ui/uiAtoms.js
  - internal/static/js/core/core.js
  - internal/static/js/ui/SettingsModal.js
  - AGENTS.md
  - internal/quota/antigravity.go
  - CLAUDE.md
  - internal/quota/codex.go
  - internal/static/js/hooks/tabGroups.js
  - internal/quota/claude.go
  - internal/ws/handler.go
  - internal/static/js/session/SessionView.js
  - internal/static/AGENT.md
  - internal/server/server.go
  - internal/static/index.html
tests:
  - internal/ws/queue_test.go
  - internal/quota/windows_test.go
  - internal/version/version_test.go
-->

---
### Requirement: Mobile chip shows the highest window

The compact usage chip on narrow viewports SHALL show a small bar and percentage for the window with the highest percent, and tapping it SHALL still refresh the quota.

#### Scenario: Highest window selected

- **WHEN** windows have percents 43 and 16
- **THEN** the chip shows a bar and `43%`


<!-- @trace
source: quota-usage-bars
updated: 2026-10-16
code:
  - internal/quota/cursor.go
  - internal/static/js/chat/ChatHeader.js
  - internal/static/js/hooks/useChatSocket.js
  - internal/quota/kiro.go
  - internal/version/version.go
  - scripts/tab-groups.test.cjs
  - internal/static/js/chat/ChatView.js
  - internal/quota/windows.go
  - internal/quota/result.go
  - internal/static/js/ui/SessionTabs.js
  - internal/static/js/app.js
  - internal/static/js/ui/uiAtoms.js
  - internal/static/js/core/core.js
  - internal/static/js/ui/SettingsModal.js
  - AGENTS.md
  - internal/quota/antigravity.go
  - CLAUDE.md
  - internal/quota/codex.go
  - internal/static/js/hooks/tabGroups.js
  - internal/quota/claude.go
  - internal/ws/handler.go
  - internal/static/js/session/SessionView.js
  - internal/static/AGENT.md
  - internal/server/server.go
  - internal/static/index.html
tests:
  - internal/ws/queue_test.go
  - internal/quota/windows_test.go
  - internal/version/version_test.go
-->

---
### Requirement: Text fallback when windows are missing

The header SHALL fall back to the existing text display when `windows` is missing, empty, or contains no key known to the runner layout, and MUST NOT raise an error.

#### Scenario: Older backend without windows

- **WHEN** the quota payload has `display_text` but no `windows`
- **THEN** the header shows the existing text segments

#### Scenario: Unknown window keys

- **WHEN** every window key is unknown to the runner layout
- **THEN** the header shows the existing text segments

<!-- @trace
source: quota-usage-bars
updated: 2026-10-16
code:
  - internal/quota/cursor.go
  - internal/static/js/chat/ChatHeader.js
  - internal/static/js/hooks/useChatSocket.js
  - internal/quota/kiro.go
  - internal/version/version.go
  - scripts/tab-groups.test.cjs
  - internal/static/js/chat/ChatView.js
  - internal/quota/windows.go
  - internal/quota/result.go
  - internal/static/js/ui/SessionTabs.js
  - internal/static/js/app.js
  - internal/static/js/ui/uiAtoms.js
  - internal/static/js/core/core.js
  - internal/static/js/ui/SettingsModal.js
  - AGENTS.md
  - internal/quota/antigravity.go
  - CLAUDE.md
  - internal/quota/codex.go
  - internal/static/js/hooks/tabGroups.js
  - internal/quota/claude.go
  - internal/ws/handler.go
  - internal/static/js/session/SessionView.js
  - internal/static/AGENT.md
  - internal/server/server.go
  - internal/static/index.html
tests:
  - internal/ws/queue_test.go
  - internal/quota/windows_test.go
  - internal/version/version_test.go
-->