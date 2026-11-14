# composer-model-controls Specification

## Purpose

Defines where the model, effort, and permission-mode selectors appear in the chat UI. They live in the composer action row beside the attachment button, so a user can adjust them next to the message being sent.

## Requirements

### Requirement: Composer action row hosts model, effort, and permission controls

For a session owner in agent input mode, the chat composer SHALL expose the permission-mode, model, and effort selectors supported by that session's agent type. Desktop layouts SHALL render these controls in the action row immediately after the attachment button. Compact mobile layouts SHALL expose them inside the tools sheet reached by the composer's Plus button, with the current selection visible for each supported control. The sheet SHALL also retain the owner's allowed input-mode switch. Agent-type availability, busy-state disabling, dangerous permission emphasis, and next-send application SHALL retain their existing behavior.

#### Scenario: Controls visible in agent mode

- **WHEN** the session owner views a Claude session with the composer in agent input mode
- **THEN** desktop shows the permission-mode, model, and effort selectors after the attachment button and compact mobile exposes them through Plus in the tools sheet

#### Scenario: Controls hidden in shell mode

- **WHEN** the composer is switched to shell input mode
- **THEN** none of the three selectors is rendered in either the desktop action row or the mobile sheet and the owner's allowed mode switch remains reachable

#### Scenario: Controls hidden for guests

- **WHEN** a guest opens a shared chat
- **THEN** none of the three selectors or the owner-only mode switch is rendered and a writable guest retains the existing attachment action through the mobile sheet


<!-- @trace
source: mobile-composer-sheet
updated: 2026-11-14
code:
  - internal/static/js/session/SessionView.js
  - internal/server/server.go
  - scripts/mobile-composer-mock.test.py
  - internal/db/busy.go
  - internal/static/sw.js
  - internal/static/assets.go
  - scripts/telegram-chat.test.cjs
  - README.md
  - internal/db/session.go
  - internal/static/js/hooks/useNewSessionForm.js
  - internal/quota/antigravity.go
  - internal/db/db.go
  - internal/static/js/hooks/useChatSocket.js
  - internal/static/manifest.webmanifest
  - README.zh-TW.md
  - internal/static/AGENT.md
  - internal/static/js/chat/ChatView.js
  - internal/ws/handler.go
  - internal/static/index.html
  - scripts/console-usability.test.cjs
  - internal/static/js/app.js
  - scripts/composer-attachment.test.py
  - docs/prototypes/mobile-composer-sheet.dc.html
  - internal/static/js/ui/ui-atoms.js
  - internal/quota/codex.go
  - internal/agent/factory.go
  - internal/api/upload.go
  - internal/static/js/ui/SessionTabs.js
  - internal/static/assets_production.go
  - internal/static/js/chat/ComposerTools.js
  - internal/mcp/tools.go
  - internal/static/robots.txt
  - scripts/context-menu.test.cjs
  - scripts/service-worker.test.cjs
  - internal/usage/quota.go
  - internal/api/session.go
  - CHANGELOG.md
  - internal/static/js/chat/ForwardModal.js
tests:
  - internal/ws/unread_test.go
  - internal/quota/display_test.go
  - internal/db/busy_test.go
  - internal/usage/quota_test.go
  - docs/spec/codex-cli.md
  - docs/spec/antigravity-cli.md
  - internal/db/unread_smoke_test.go
  - docs/spec/kiro-cli.md
  - internal/server/crawler_guard_test.go
  - internal/server/start_helper_test.go
  - internal/quota/codex_test.go
  - internal/ws/kiro_readonly_test.go
  - internal/mcp/kiro_readonly_test.go
  - internal/quota/windows_test.go
  - internal/api/session_test.go
  - internal/agent/factory_test.go
-->

---
### Requirement: Selector availability follows agent type

The composer SHALL hide a selector that the session's agent type does not support.

#### Scenario: Cursor session has no effort selector

- **WHEN** the session agent type is `cursor`
- **THEN** the effort selector is not rendered and the model and permission selectors remain

#### Scenario: Codex or Kiro session has no permission selector

- **WHEN** the session agent type is `codex` or `kiro`
- **THEN** the permission-mode selector is not rendered


<!-- @trace
source: composer-model-controls
updated: 2026-10-16
code:
  - internal/quota/claude.go
  - internal/static/AGENT.md
  - internal/quota/cursor.go
  - internal/static/js/chat/chat-header.js
  - internal/quota/antigravity.go
  - internal/server/server.go
  - internal/quota/codex.go
  - internal/quota/result.go
  - internal/static/js/chat/ChatView.js
  - internal/static/js/ui/ui-atoms.js
  - internal/static/js/hooks/tabGroups.js
  - CLAUDE.md
  - internal/ws/handler.go
  - internal/static/js/hooks/useChatSocket.js
  - internal/static/index.html
  - AGENTS.md
  - internal/static/js/session/SessionView.js
  - internal/quota/kiro.go
  - internal/static/js/core/core.js
  - internal/static/js/app.js
  - internal/quota/windows.go
  - internal/static/js/ui/SessionTabs.js
  - internal/static/js/ui/SettingsModal.js
  - internal/version/version.go
  - scripts/tab-groups.test.cjs
tests:
  - internal/quota/windows_test.go
  - internal/ws/queue_test.go
  - internal/version/version_test.go
-->

---
### Requirement: Selections apply on next send

Changing a selector SHALL only update the draft selection; the corresponding `set_model`, `set_effort`, or `set_mode` message MUST be sent when the next message is submitted, matching existing behavior.

#### Scenario: Change model then send

- **WHEN** the user picks a different model and then submits a message
- **THEN** the client sends `set_model` with the chosen model before the message and the session reports it as the active model

#### Scenario: Change without sending

- **WHEN** the user picks a different effort and does not submit
- **THEN** no `set_effort` message is sent


<!-- @trace
source: composer-model-controls
updated: 2026-10-16
code:
  - internal/quota/claude.go
  - internal/static/AGENT.md
  - internal/quota/cursor.go
  - internal/static/js/chat/chat-header.js
  - internal/quota/antigravity.go
  - internal/server/server.go
  - internal/quota/codex.go
  - internal/quota/result.go
  - internal/static/js/chat/ChatView.js
  - internal/static/js/ui/ui-atoms.js
  - internal/static/js/hooks/tabGroups.js
  - CLAUDE.md
  - internal/ws/handler.go
  - internal/static/js/hooks/useChatSocket.js
  - internal/static/index.html
  - AGENTS.md
  - internal/static/js/session/SessionView.js
  - internal/quota/kiro.go
  - internal/static/js/core/core.js
  - internal/static/js/app.js
  - internal/quota/windows.go
  - internal/static/js/ui/SessionTabs.js
  - internal/static/js/ui/SettingsModal.js
  - internal/version/version.go
  - scripts/tab-groups.test.cjs
tests:
  - internal/quota/windows_test.go
  - internal/ws/queue_test.go
  - internal/version/version_test.go
-->

---
### Requirement: Selectors disabled while the agent is busy

The three selectors MUST be disabled whenever the session state is THINKING, STREAMING, AWAITING_CONFIRM, or any shell-running or shell-confirm state.

#### Scenario: Agent is streaming

- **WHEN** the session state is STREAMING
- **THEN** the permission, model, and effort selectors cannot be changed

#### Scenario: Agent is idle

- **WHEN** the session state is IDLE
- **THEN** the three selectors can be changed


<!-- @trace
source: composer-model-controls
updated: 2026-10-16
code:
  - internal/quota/claude.go
  - internal/static/AGENT.md
  - internal/quota/cursor.go
  - internal/static/js/chat/chat-header.js
  - internal/quota/antigravity.go
  - internal/server/server.go
  - internal/quota/codex.go
  - internal/quota/result.go
  - internal/static/js/chat/ChatView.js
  - internal/static/js/ui/ui-atoms.js
  - internal/static/js/hooks/tabGroups.js
  - CLAUDE.md
  - internal/ws/handler.go
  - internal/static/js/hooks/useChatSocket.js
  - internal/static/index.html
  - AGENTS.md
  - internal/static/js/session/SessionView.js
  - internal/quota/kiro.go
  - internal/static/js/core/core.js
  - internal/static/js/app.js
  - internal/quota/windows.go
  - internal/static/js/ui/SessionTabs.js
  - internal/static/js/ui/SettingsModal.js
  - internal/version/version.go
  - scripts/tab-groups.test.cjs
tests:
  - internal/quota/windows_test.go
  - internal/ws/queue_test.go
  - internal/version/version_test.go
-->

---
### Requirement: Dangerous permission mode is visually emphasized

The permission selector SHALL use the amber danger style when the current mode is `bypassPermissions` or `yolo`.

#### Scenario: Bypass mode selected

- **WHEN** the permission mode is `bypassPermissions`
- **THEN** the permission selector renders in the amber danger style


<!-- @trace
source: composer-model-controls
updated: 2026-10-16
code:
  - internal/quota/claude.go
  - internal/static/AGENT.md
  - internal/quota/cursor.go
  - internal/static/js/chat/chat-header.js
  - internal/quota/antigravity.go
  - internal/server/server.go
  - internal/quota/codex.go
  - internal/quota/result.go
  - internal/static/js/chat/ChatView.js
  - internal/static/js/ui/ui-atoms.js
  - internal/static/js/hooks/tabGroups.js
  - CLAUDE.md
  - internal/ws/handler.go
  - internal/static/js/hooks/useChatSocket.js
  - internal/static/index.html
  - AGENTS.md
  - internal/static/js/session/SessionView.js
  - internal/quota/kiro.go
  - internal/static/js/core/core.js
  - internal/static/js/app.js
  - internal/quota/windows.go
  - internal/static/js/ui/SessionTabs.js
  - internal/static/js/ui/SettingsModal.js
  - internal/version/version.go
  - scripts/tab-groups.test.cjs
tests:
  - internal/quota/windows_test.go
  - internal/ws/queue_test.go
  - internal/version/version_test.go
-->

---
### Requirement: Chat header no longer duplicates the controls

The chat header SHALL NOT render model, effort, or permission-mode selectors, nor a toggle for those selectors, on desktop or mobile. Existing header identity, model labels, quota information, and session-details disclosure SHALL remain governed by their current behavior. Desktop selectors SHALL remain inline in the composer and compact mobile selectors SHALL be reachable by activating the composer's Plus button without requiring textarea focus.

#### Scenario: Desktop header

- **WHEN** a session owner views a chat using the desktop layout
- **THEN** the header shows no model, effort, or permission selectors while its existing identity, model label, and quota information remain available

#### Scenario: Mobile header

- **WHEN** a session owner views a chat using the compact mobile layout
- **THEN** the header does not duplicate permission, model, or effort controls and Plus provides access to supported selectors without focusing the message input or expanding header details


<!-- @trace
source: mobile-composer-sheet
updated: 2026-11-14
code:
  - internal/static/js/session/SessionView.js
  - internal/server/server.go
  - scripts/mobile-composer-mock.test.py
  - internal/db/busy.go
  - internal/static/sw.js
  - internal/static/assets.go
  - scripts/telegram-chat.test.cjs
  - README.md
  - internal/db/session.go
  - internal/static/js/hooks/useNewSessionForm.js
  - internal/quota/antigravity.go
  - internal/db/db.go
  - internal/static/js/hooks/useChatSocket.js
  - internal/static/manifest.webmanifest
  - README.zh-TW.md
  - internal/static/AGENT.md
  - internal/static/js/chat/ChatView.js
  - internal/ws/handler.go
  - internal/static/index.html
  - scripts/console-usability.test.cjs
  - internal/static/js/app.js
  - scripts/composer-attachment.test.py
  - docs/prototypes/mobile-composer-sheet.dc.html
  - internal/static/js/ui/ui-atoms.js
  - internal/quota/codex.go
  - internal/agent/factory.go
  - internal/api/upload.go
  - internal/static/js/ui/SessionTabs.js
  - internal/static/assets_production.go
  - internal/static/js/chat/ComposerTools.js
  - internal/mcp/tools.go
  - internal/static/robots.txt
  - scripts/context-menu.test.cjs
  - scripts/service-worker.test.cjs
  - internal/usage/quota.go
  - internal/api/session.go
  - CHANGELOG.md
  - internal/static/js/chat/ForwardModal.js
tests:
  - internal/ws/unread_test.go
  - internal/quota/display_test.go
  - internal/db/busy_test.go
  - internal/usage/quota_test.go
  - docs/spec/codex-cli.md
  - docs/spec/antigravity-cli.md
  - internal/db/unread_smoke_test.go
  - docs/spec/kiro-cli.md
  - internal/server/crawler_guard_test.go
  - internal/server/start_helper_test.go
  - internal/quota/codex_test.go
  - internal/ws/kiro_readonly_test.go
  - internal/mcp/kiro_readonly_test.go
  - internal/quota/windows_test.go
  - internal/api/session_test.go
  - internal/agent/factory_test.go
-->

---
### Requirement: Selectors in other screens are unchanged

The new-session form and the forward dialog SHALL keep their existing full-width model, effort, and permission selectors.

#### Scenario: New session form

- **WHEN** the user opens the new-session form
- **THEN** the model, effort, and permission selectors look and behave as before

<!-- @trace
source: composer-model-controls
updated: 2026-10-16
code:
  - internal/quota/claude.go
  - internal/static/AGENT.md
  - internal/quota/cursor.go
  - internal/static/js/chat/chat-header.js
  - internal/quota/antigravity.go
  - internal/server/server.go
  - internal/quota/codex.go
  - internal/quota/result.go
  - internal/static/js/chat/ChatView.js
  - internal/static/js/ui/ui-atoms.js
  - internal/static/js/hooks/tabGroups.js
  - CLAUDE.md
  - internal/ws/handler.go
  - internal/static/js/hooks/useChatSocket.js
  - internal/static/index.html
  - AGENTS.md
  - internal/static/js/session/SessionView.js
  - internal/quota/kiro.go
  - internal/static/js/core/core.js
  - internal/static/js/app.js
  - internal/quota/windows.go
  - internal/static/js/ui/SessionTabs.js
  - internal/static/js/ui/SettingsModal.js
  - internal/version/version.go
  - scripts/tab-groups.test.cjs
tests:
  - internal/quota/windows_test.go
  - internal/ws/queue_test.go
  - internal/version/version_test.go
-->