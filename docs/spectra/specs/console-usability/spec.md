# console-usability Specification

## Purpose

Console usability defines the shared interaction and readability baseline for the existing Agent Console. It keeps desktop workspaces and mobile remote control operable without replacing the established visual identity or transport behavior.

## Requirements

### Requirement: Keyboard-visible and named shared controls

Shared icon actions and form controls SHALL expose accessible names. Keyboard focus SHALL remain visible in both supported themes, including inside the composer and on controls revealed through focus rather than pointer hover. Important actions SHALL have a click or keyboard alternative to drag and hover.

#### Scenario: Reach a shared icon action without a mouse

- **WHEN** a keyboard user focuses a header or composer icon action in either theme
- **THEN** the action exposes its purpose and a visible focus indicator without requiring hover

#### Scenario: Composer receives keyboard focus

- **WHEN** the message textarea receives keyboard focus
- **THEN** the composer identifies the focused input and its focused boundary remains visible


<!-- @trace
source: console-ux-polish
updated: 2026-11-06
code:
  - internal/static/index.html
  - internal/static/js/chat/ChatView.js
  - internal/static/js/chat/chat-header.js
  - internal/static/js/core/core.js
  - internal/static/js/ui/ui-atoms.js
tests:
  - scripts/console-usability.test.cjs
  - scripts/theme.test.cjs
  - scripts/ux-reliability.test.cjs
-->

---
### Requirement: Compact controls and zoom retain access to controls

The desktop composer SHALL preserve its existing compact control sizing and action order. Compact mobile layouts SHALL expose a Plus entry beside the input and send or interrupt actions and SHALL place attachment and supported setting controls in a bottom sheet. Mobile entry and sheet controls SHALL provide touch-operable targets and SHALL remain reachable when space is constrained. Browser zoom SHALL remain enabled. Permission, model and effort selectors SHALL remain available according to the session's existing agent and role rules. Mobile textarea sizing SHALL follow content and available space without changing its height limit solely because focus leaves the composer.

#### Scenario: Use supported selectors on a phone

- **WHEN** a session owner opens a Claude agent-mode chat on a phone and activates Plus
- **THEN** the supported permission, model, effort and attachment actions are reachable in the sheet, including by internal scrolling, without horizontal page scrolling

#### Scenario: Enlarge a narrow interface

- **WHEN** the user enlarges text or uses browser zoom
- **THEN** message input, Plus, sheet dismissal, and important actions remain reachable and the page does not disable zoom through viewport restrictions

#### Scenario: Preserve desktop controls

- **WHEN** the user views an editable chat using the desktop layout
- **THEN** the composer retains its inline tools, existing compact sizing and action order

#### Scenario: Read a multiline draft after blur

- **WHEN** a mobile user leaves a textarea containing a multiline draft to operate Plus or a sheet control
- **THEN** blur alone does not collapse the textarea or hide its attachment chips


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
### Requirement: Long content remains readable in both themes

The chat header SHALL provide a keyboard- and touch-accessible disclosure for the complete session name and work directory when the summary is truncated. Expanded identity text, prose and long unbroken tokens SHALL wrap within the available chat width. Code blocks SHALL preserve source formatting and support local horizontal scrolling without causing page-wide overflow. Usage information and state labels SHALL remain readable in both themes, with normal text meeting a 4.5:1 foreground/background contrast ratio.

#### Scenario: Read a long work directory

- **WHEN** a path is truncated in a narrow chat header
- **THEN** the user can expose the complete path without relying exclusively on a pointer tooltip

#### Scenario: Read a long code line in a split group

- **WHEN** a code line exceeds its chat group's width
- **THEN** the code block supports local horizontal scrolling and the workspace remains within the viewport

#### Scenario: Read usage and state across themes

- **WHEN** the user switches between the existing light and dark themes
- **THEN** usage values and meaningful state text satisfy the text contrast contract and state meaning is available as text rather than color alone


<!-- @trace
source: console-ux-polish
updated: 2026-11-06
code:
  - internal/static/index.html
  - internal/static/js/chat/ChatView.js
  - internal/static/js/chat/chat-header.js
  - internal/static/js/core/core.js
  - internal/static/js/ui/ui-atoms.js
tests:
  - scripts/console-usability.test.cjs
  - scripts/theme.test.cjs
  - scripts/ux-reliability.test.cjs
-->

---
### Requirement: Failure feedback preserves recoverable input

Visual refinements SHALL preserve the existing authorization, queue and transport semantics. A disconnected composer SHALL keep its draft editable and explain unavailable delivery. Failed send or authorization actions SHALL preserve recoverable input and expose a recovery instruction. Streaming content SHALL NOT be announced token by token through a live region. Feedback SHALL NOT delay high-frequency keyboard actions or require motion to convey state.

#### Scenario: Edit a draft while disconnected

- **WHEN** the chat loses its connection while a draft exists
- **THEN** the draft remains editable and delivery unavailability is explained without discarding the draft

#### Scenario: An authorization action cannot be delivered

- **WHEN** an authorization action fails to reach the existing transport
- **THEN** its controls and pending request remain available and feedback explains how the user can retry

#### Scenario: Reduced motion keeps state understandable

- **WHEN** reduced motion is enabled and an operation changes state
- **THEN** text and control state communicate the change without waiting for animation completion

<!-- @trace
source: console-ux-polish
updated: 2026-11-06
code:
  - internal/static/index.html
  - internal/static/js/chat/ChatView.js
  - internal/static/js/chat/chat-header.js
  - internal/static/js/core/core.js
  - internal/static/js/ui/ui-atoms.js
tests:
  - scripts/console-usability.test.cjs
  - scripts/theme.test.cjs
  - scripts/ux-reliability.test.cjs
-->