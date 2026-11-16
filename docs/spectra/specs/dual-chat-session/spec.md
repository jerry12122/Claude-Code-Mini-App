# dual-chat-session Specification

## Purpose

Manages independent session selection, concurrent dual WebSocket streams, isolated tool approvals, and layout persistence for split view. This ensures multiple agents run concurrently without state conflicts or race conditions.

## Requirements

### Requirement: Independent session selection per pane
Each group SHALL maintain its own tab list and active tab, and show the independent conversation view of its active tab.

#### Scenario: Select different session in secondary pane
- **WHEN** user chooses session B in the second group while the first group displays session A
- **THEN** the second group loads session B while the first group continues displaying session A

#### Scenario: Quick switcher target focused pane
- **WHEN** user triggers Ctrl+P quick switcher while the second group has active focus
- **THEN** the selected session opens as a tab in the second group without changing the first group's active tab


<!-- @trace
source: session-tabs
updated: 2026-10-16
code:
  - README.md
  - internal/static/js/hooks/tabGroups.js
  - internal/quota/kiro.go
  - docs/getting-started.md
  - internal/static/js/ui/uiAtoms.js
  - internal/antigravity/models.go
  - README.zh-TW.md
  - internal/ws/handler.go
  - internal/static/js/hooks/useChatSocket.js
  - scripts/tab-groups.test.cjs
  - internal/static/AGENT.md
  - .github/release-header.md
  - internal/server/server.go
  - internal/quota/codex.go
  - internal/static/js/ui/SessionTabs.js
  - internal/quota/windows.go
  - internal/static/js/core/core.js
  - .github/workflows/release.yml
  - internal/quota/antigravity.go
  - AGENTS.md
  - internal/version/version.go
  - config.example.yaml
  - internal/static/js/ui/SettingsModal.js
  - internal/quota/cursor.go
  - internal/static/js/session/SessionView.js
  - internal/quota/claude.go
  - docs/getting-started.zh-TW.md
  - internal/static/js/app.js
  - CLAUDE.md
  - internal/static/js/ui/SplitLayout.js
  - internal/quota/result.go
  - internal/static/js/chat/ChatHeader.js
  - internal/static/index.html
  - internal/static/js/chat/ChatView.js
tests:
  - internal/server/models_sync_test.go
  - internal/quota/windows_test.go
  - internal/version/version_test.go
  - internal/ws/queue_test.go
  - internal/antigravity/models_test.go
  - docs/spec/antigravity-cli.md
  - internal/antigravity/runner_test.go
-->

---
### Requirement: Concurrent WebSocket streaming
Both panes SHALL establish and maintain concurrent WebSocket connections to their respective sessions without cross-talk or race conditions.

#### Scenario: Simultaneous streaming in both panes
- **WHEN** tasks run concurrently in both primary and secondary sessions
- **THEN** both panes stream token deltas and tool events into their respective message lists simultaneously

#### Scenario: Disconnect handling per pane
- **WHEN** network connectivity drops for one session's WebSocket
- **THEN** only that specific pane displays reconnecting status while the other pane continues operating normally


<!-- @trace
source: split-chat-view
updated: 2026-10-12
code:
  - CLAUDE.md
  - docs/plan/done.md
  - internal/static/js/chat/ChatView.js
  - internal/static/js/ui/SplitLayout.js
  - internal/static/js/app.js
  - internal/static/index.html
  - .agents/skills/spectra-propose/SKILL.md
  - .cursorrules
  - .agents/skills/spectra-verify/SKILL.md
  - .agents/skills/spectra-discuss/SKILL.md
  - internal/static/js/chat/ChatHeader.js
  - .spectra.yaml
  - internal/static/js/session/SessionView.js
  - README.md
  - README.zh-TW.md
  - .agents/skills/spectra-audit/SKILL.md
  - .agents/skills/spectra-drift/SKILL.md
  - .agents/skills/spectra-archive/SKILL.md
  - .agents/skills/spectra-analyze/SKILL.md
  - .agents/skills/spectra-debug/SKILL.md
  - .agents/skills/spectra-review/SKILL.md
  - .agents/skills/spectra-commit/SKILL.md
  - .agents/skills/spectra-apply/SKILL.md
  - .agents/skills/spectra-ingest/SKILL.md
-->

---
### Requirement: Independent prompt submission and tool approval
Each pane SHALL provide its own input composer, stop button, and permission approval dialogs without blocking the other pane.

#### Scenario: Tool approval in one pane while typing in other
- **WHEN** secondary session requests tool permission
- **THEN** approval dialog appears exclusively over the secondary pane while primary pane remains interactive

#### Scenario: Stop task in single pane
- **WHEN** user clicks stop button on primary pane
- **THEN** only the primary session task aborts while secondary session execution continues uninterrupted


<!-- @trace
source: split-chat-view
updated: 2026-10-12
code:
  - CLAUDE.md
  - docs/plan/done.md
  - internal/static/js/chat/ChatView.js
  - internal/static/js/ui/SplitLayout.js
  - internal/static/js/app.js
  - internal/static/index.html
  - .agents/skills/spectra-propose/SKILL.md
  - .cursorrules
  - .agents/skills/spectra-verify/SKILL.md
  - .agents/skills/spectra-discuss/SKILL.md
  - internal/static/js/chat/ChatHeader.js
  - .spectra.yaml
  - internal/static/js/session/SessionView.js
  - README.md
  - README.zh-TW.md
  - .agents/skills/spectra-audit/SKILL.md
  - .agents/skills/spectra-drift/SKILL.md
  - .agents/skills/spectra-archive/SKILL.md
  - .agents/skills/spectra-analyze/SKILL.md
  - .agents/skills/spectra-debug/SKILL.md
  - .agents/skills/spectra-review/SKILL.md
  - .agents/skills/spectra-commit/SKILL.md
  - .agents/skills/spectra-apply/SKILL.md
  - .agents/skills/spectra-ingest/SKILL.md
-->

---
### Requirement: Split view persistence
The application SHALL persist the tab groups, each group's active tab, the focused group, pane ratio, and orientation in local storage across browser reloads.

#### Scenario: Page reload restores split state
- **WHEN** user reloads the browser while two groups are open
- **THEN** both groups restore with their previous tabs and active tabs and the saved pane ratio

#### Scenario: Auto clear invalid secondary session
- **WHEN** a persisted tab's session was deleted on server
- **THEN** application removes that tab, and if the second group is left empty it reverts to single group mode gracefully

<!-- @trace
source: session-tabs
updated: 2026-10-16
code:
  - README.md
  - internal/static/js/hooks/tabGroups.js
  - internal/quota/kiro.go
  - docs/getting-started.md
  - internal/static/js/ui/uiAtoms.js
  - internal/antigravity/models.go
  - README.zh-TW.md
  - internal/ws/handler.go
  - internal/static/js/hooks/useChatSocket.js
  - scripts/tab-groups.test.cjs
  - internal/static/AGENT.md
  - .github/release-header.md
  - internal/server/server.go
  - internal/quota/codex.go
  - internal/static/js/ui/SessionTabs.js
  - internal/quota/windows.go
  - internal/static/js/core/core.js
  - .github/workflows/release.yml
  - internal/quota/antigravity.go
  - AGENTS.md
  - internal/version/version.go
  - config.example.yaml
  - internal/static/js/ui/SettingsModal.js
  - internal/quota/cursor.go
  - internal/static/js/session/SessionView.js
  - internal/quota/claude.go
  - docs/getting-started.zh-TW.md
  - internal/static/js/app.js
  - CLAUDE.md
  - internal/static/js/ui/SplitLayout.js
  - internal/quota/result.go
  - internal/static/js/chat/ChatHeader.js
  - internal/static/index.html
  - internal/static/js/chat/ChatView.js
tests:
  - internal/server/models_sync_test.go
  - internal/quota/windows_test.go
  - internal/version/version_test.go
  - internal/ws/queue_test.go
  - internal/antigravity/models_test.go
  - docs/spec/antigravity-cli.md
  - internal/antigravity/runner_test.go
-->