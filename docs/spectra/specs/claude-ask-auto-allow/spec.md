# claude-ask-auto-allow Specification

## Purpose

The claude-ask-auto-allow capability lets the owner skip the manual approval of Claude tool calls that match a `permissions.ask` rule while the session runs in `bypassPermissions` mode, by having the server answer allow on their behalf. It is a single global switch that is off by default, and every automatic allow leaves an audit trail so nothing is allowed silently.

## Requirements

### Requirement: Global auto-allow switch
The system SHALL store a boolean general setting `claudeAutoAllowAsk` on the server, defaulting to `false` when no value has been saved. The settings page "General" section SHALL expose this switch and persist changes immediately; turning it on SHALL require an explicit confirmation that states ask rules will be answered allow without asking the owner. Guest (shared-session) identities SHALL NOT be able to read or change this setting.

#### Scenario: Default is off
- **WHEN** the general settings have never been saved
- **THEN** `claudeAutoAllowAsk` SHALL read as `false`

#### Scenario: Turning on needs confirmation
- **WHEN** the owner flips the switch on in the settings page
- **THEN** the page SHALL show a confirmation and SHALL save `true` only after the owner confirms

#### Scenario: Guest cannot change it
- **WHEN** a guest token requests the general settings endpoint
- **THEN** the request SHALL be rejected with a forbidden status and the stored value SHALL NOT change


<!-- @trace
source: claude-ask-auto-allow
updated: 2026-11-18
code:
  - internal/api/settings.go
  - internal/ws/attachment.go
  - scripts/composer-attachment.test.py
  - internal/static/js/chat/ChatHeader.js
  - internal/db/general_settings.go
  - internal/static/js/ui/SettingsModal.js
  - internal/static/index.html
  - internal/ws/claudeperm.go
  - internal/ws/quota_guard.go
  - internal/ws/handler.go
  - docs/plan/todo/claude-permission-prompt.md
  - internal/db/attachment.go
  - internal/static/js/chat/GitControls.js
  - VERSION
  - internal/db/room.go
  - internal/api/rooms.go
  - internal/version/version.go
  - scripts/console-usability.test.cjs
  - internal/static/js/session/RoomView.js
  - CHANGELOG.md
  - README.md
  - README.zh-TW.md
  - internal/server/server.go
  - internal/static/js/chat/ChatView.js
  - internal/static/js/core/core.js
  - internal/static/js/session/RoomsSection.js
  - internal/db/db.go
  - internal/db/queue.go
  - internal/db/quota_resume.go
  - scripts/general-settings-ui.test.cjs
  - docs/plan/todo.md
  - internal/static/js/session/RoomControls.js
  - internal/db/session.go
  - internal/static/js/chat/SessionActionCards.js
  - scripts/fake-claude.cjs
  - cmd/desktop/desktop_windows_amd64.syso
tests:
  - internal/server/guest_test.go
  - internal/api/general_settings_test.go
  - internal/api/session_instruction_test.go
  - internal/ws/quota_guard_test.go
  - cmd/desktop/install_windows_test.go
  - internal/db/room_workflow_test.go
  - scripts/browser/test_rooms.py
  - internal/ws/attachment_test.go
  - internal/db/quota_resume_test.go
  - internal/ws/claudeperm_test.go
  - internal/api/rooms_test.go
  - internal/db/general_settings_test.go
-->

---
### Requirement: Automatic allow for ask-rule prompts
When `claudeAutoAllowAsk` is true and a Claude run whose session permission mode is `bypassPermissions` receives a tool permission request through the permission prompt channel, the system SHALL answer allow with the original tool input, immediately, without recording a pending permission, without changing the session status to awaiting confirmation, without broadcasting a permission request to clients, and without sending a Telegram confirmation notice. The setting SHALL be read at the moment each request arrives, so a change applies to later requests without restarting the server or reconnecting.

#### Scenario: Ask rule is allowed automatically
- **WHEN** the switch is on, the session is in `bypassPermissions`, and Claude requests `Bash` with command `curl --version` which matches an ask rule
- **THEN** the run SHALL continue with the command allowed and no permission panel SHALL be shown

#### Scenario: Switch off keeps the panel
- **WHEN** the switch is off and the same request arrives
- **THEN** the system SHALL ask the owner through the existing permission panel and wait

#### Scenario: Switch flipped between requests
- **WHEN** the switch is off for the first request in a run and the owner turns it on before a second request in the same run
- **THEN** the first request SHALL keep its panel and the second request SHALL be allowed automatically

##### Example: applicability
| Switch | Session permission mode | Request | Result |
|--------|-------------------------|---------|--------|
| on | `bypassPermissions` | tool permission via ask rule | allowed automatically |
| off | `bypassPermissions` | tool permission via ask rule | panel shown |
| on | `default` | tool permission | existing flow, no automatic allow |
| on | `acceptEdits` | tool permission | existing flow, no automatic allow |
| on | `bypassPermissions` | `AskUserQuestion` | question shown to the owner |
| on | `bypassPermissions` | tool blocked by a `deny` rule | still blocked |


<!-- @trace
source: claude-ask-auto-allow
updated: 2026-11-18
code:
  - internal/api/settings.go
  - internal/ws/attachment.go
  - scripts/composer-attachment.test.py
  - internal/static/js/chat/ChatHeader.js
  - internal/db/general_settings.go
  - internal/static/js/ui/SettingsModal.js
  - internal/static/index.html
  - internal/ws/claudeperm.go
  - internal/ws/quota_guard.go
  - internal/ws/handler.go
  - docs/plan/todo/claude-permission-prompt.md
  - internal/db/attachment.go
  - internal/static/js/chat/GitControls.js
  - VERSION
  - internal/db/room.go
  - internal/api/rooms.go
  - internal/version/version.go
  - scripts/console-usability.test.cjs
  - internal/static/js/session/RoomView.js
  - CHANGELOG.md
  - README.md
  - README.zh-TW.md
  - internal/server/server.go
  - internal/static/js/chat/ChatView.js
  - internal/static/js/core/core.js
  - internal/static/js/session/RoomsSection.js
  - internal/db/db.go
  - internal/db/queue.go
  - internal/db/quota_resume.go
  - scripts/general-settings-ui.test.cjs
  - docs/plan/todo.md
  - internal/static/js/session/RoomControls.js
  - internal/db/session.go
  - internal/static/js/chat/SessionActionCards.js
  - scripts/fake-claude.cjs
  - cmd/desktop/desktop_windows_amd64.syso
tests:
  - internal/server/guest_test.go
  - internal/api/general_settings_test.go
  - internal/api/session_instruction_test.go
  - internal/ws/quota_guard_test.go
  - cmd/desktop/install_windows_test.go
  - internal/db/room_workflow_test.go
  - scripts/browser/test_rooms.py
  - internal/ws/attachment_test.go
  - internal/db/quota_resume_test.go
  - internal/ws/claudeperm_test.go
  - internal/api/rooms_test.go
  - internal/db/general_settings_test.go
-->

---
### Requirement: Audit trail for automatic allows
For every automatic allow the system SHALL append one notice line to the reply of the current turn that contains the tool name and a summary of its input, and SHALL write a server log entry with the session identifier, tool name, and the same summary. The summary SHALL be the command text for `Bash` and the compact input JSON for other tools, truncated to 200 characters with an ellipsis marker when longer.

#### Scenario: Bash command is recorded
- **WHEN** a `Bash` request with command `curl --version` is allowed automatically
- **THEN** the turn reply SHALL contain a line naming `Bash` and `curl --version`, and the server log SHALL contain an entry for it

#### Scenario: Long input is truncated
- **WHEN** an automatically allowed `Bash` command is 500 characters long
- **THEN** the notice and the log summary SHALL each hold at most 200 characters of it plus the truncation marker


<!-- @trace
source: claude-ask-auto-allow
updated: 2026-11-18
code:
  - internal/api/settings.go
  - internal/ws/attachment.go
  - scripts/composer-attachment.test.py
  - internal/static/js/chat/ChatHeader.js
  - internal/db/general_settings.go
  - internal/static/js/ui/SettingsModal.js
  - internal/static/index.html
  - internal/ws/claudeperm.go
  - internal/ws/quota_guard.go
  - internal/ws/handler.go
  - docs/plan/todo/claude-permission-prompt.md
  - internal/db/attachment.go
  - internal/static/js/chat/GitControls.js
  - VERSION
  - internal/db/room.go
  - internal/api/rooms.go
  - internal/version/version.go
  - scripts/console-usability.test.cjs
  - internal/static/js/session/RoomView.js
  - CHANGELOG.md
  - README.md
  - README.zh-TW.md
  - internal/server/server.go
  - internal/static/js/chat/ChatView.js
  - internal/static/js/core/core.js
  - internal/static/js/session/RoomsSection.js
  - internal/db/db.go
  - internal/db/queue.go
  - internal/db/quota_resume.go
  - scripts/general-settings-ui.test.cjs
  - docs/plan/todo.md
  - internal/static/js/session/RoomControls.js
  - internal/db/session.go
  - internal/static/js/chat/SessionActionCards.js
  - scripts/fake-claude.cjs
  - cmd/desktop/desktop_windows_amd64.syso
tests:
  - internal/server/guest_test.go
  - internal/api/general_settings_test.go
  - internal/api/session_instruction_test.go
  - internal/ws/quota_guard_test.go
  - cmd/desktop/install_windows_test.go
  - internal/db/room_workflow_test.go
  - scripts/browser/test_rooms.py
  - internal/ws/attachment_test.go
  - internal/db/quota_resume_test.go
  - internal/ws/claudeperm_test.go
  - internal/api/rooms_test.go
  - internal/db/general_settings_test.go
-->

---
### Requirement: Scope limits
The automatic allow SHALL NOT affect `deny` rules, SHALL NOT answer `AskUserQuestion` questions, SHALL NOT apply to agents other than Claude, and SHALL NOT apply to a run whose session permission mode is not `bypassPermissions`. A request that is waiting in a panel when the switch is turned on SHALL stay waiting for the owner's answer.

#### Scenario: Question still asked
- **WHEN** the switch is on and Claude calls `AskUserQuestion` in a `bypassPermissions` session
- **THEN** the question SHALL be shown to the owner and the run SHALL wait for the answer

#### Scenario: Waiting panel is untouched
- **WHEN** a permission panel is already waiting and the owner turns the switch on
- **THEN** that panel SHALL remain until the owner answers it

<!-- @trace
source: claude-ask-auto-allow
updated: 2026-11-18
code:
  - internal/api/settings.go
  - internal/ws/attachment.go
  - scripts/composer-attachment.test.py
  - internal/static/js/chat/ChatHeader.js
  - internal/db/general_settings.go
  - internal/static/js/ui/SettingsModal.js
  - internal/static/index.html
  - internal/ws/claudeperm.go
  - internal/ws/quota_guard.go
  - internal/ws/handler.go
  - docs/plan/todo/claude-permission-prompt.md
  - internal/db/attachment.go
  - internal/static/js/chat/GitControls.js
  - VERSION
  - internal/db/room.go
  - internal/api/rooms.go
  - internal/version/version.go
  - scripts/console-usability.test.cjs
  - internal/static/js/session/RoomView.js
  - CHANGELOG.md
  - README.md
  - README.zh-TW.md
  - internal/server/server.go
  - internal/static/js/chat/ChatView.js
  - internal/static/js/core/core.js
  - internal/static/js/session/RoomsSection.js
  - internal/db/db.go
  - internal/db/queue.go
  - internal/db/quota_resume.go
  - scripts/general-settings-ui.test.cjs
  - docs/plan/todo.md
  - internal/static/js/session/RoomControls.js
  - internal/db/session.go
  - internal/static/js/chat/SessionActionCards.js
  - scripts/fake-claude.cjs
  - cmd/desktop/desktop_windows_amd64.syso
tests:
  - internal/server/guest_test.go
  - internal/api/general_settings_test.go
  - internal/api/session_instruction_test.go
  - internal/ws/quota_guard_test.go
  - cmd/desktop/install_windows_test.go
  - internal/db/room_workflow_test.go
  - scripts/browser/test_rooms.py
  - internal/ws/attachment_test.go
  - internal/db/quota_resume_test.go
  - internal/ws/claudeperm_test.go
  - internal/api/rooms_test.go
  - internal/db/general_settings_test.go
-->