# quota-guard-settings Specification

## Purpose

The quota-guard-settings capability makes the quota guard, which pauses a session queue before the server auto-starts the next message when account usage is high, adjustable and switchable from the settings page. It exists so the threshold can be changed at runtime without editing the config file or restarting the server.

## Requirements

### Requirement: Quota guard settings stored on the server
The system SHALL store two general settings on the server: `quotaGuardEnabled` (boolean) and `quotaGuardPercent` (number greater than 0 and at most 100). When the owner has never saved them, the effective values SHALL derive from the `quota_guard_percent` configuration value: a value above 0 yields enabled with that percent, and a value of 0 yields disabled with percent 90. The general settings read endpoint SHALL return the effective values, so that a client which writes the whole object back does not change them unintentionally.

#### Scenario: Never saved with default config
- **WHEN** the owner has never saved the guard settings and `quota_guard_percent` is 90
- **THEN** the effective settings SHALL be `quotaGuardEnabled` true and `quotaGuardPercent` 90

#### Scenario: Never saved with guard disabled in config
- **WHEN** the owner has never saved the guard settings and `quota_guard_percent` is 0
- **THEN** the effective settings SHALL be `quotaGuardEnabled` false and `quotaGuardPercent` 90

#### Scenario: Saved values override config
- **WHEN** the owner saved `quotaGuardEnabled` true and `quotaGuardPercent` 75 while `quota_guard_percent` is 0
- **THEN** the effective settings SHALL be enabled with percent 75


<!-- @trace
source: quota-auto-resume
updated: 2026-11-19
code:
  - internal/static/js/session/RoomsSection.js
  - internal/static/js/chat/GitControls.js
  - internal/db/input_receipt.go
  - internal/static/index.html
  - internal/static/js/chat/ChatView.js
  - cmd/desktop/desktop_windows_amd64.syso
  - docs/reviews/2026-11-18-workroom-first-run-observations.md
  - internal/server/isolated.go
  - internal/static/js/session/RoomView.js
  - internal/server/room.go
  - internal/ws/broadcast.go
  - internal/ws/claudeperm.go
  - VERSION
  - docs/plan/todo/quota-auto-resume.md
  - CHANGELOG.md
  - internal/db/quota_resume.go
  - internal/static/js/hooks/rooms.js
  - internal/ws/attachment.go
  - internal/static/js/ui/SettingsModal.js
  - README.zh-TW.md
  - internal/ws/dispatch.go
  - internal/ws/handler.go
  - internal/ws/quota_guard.go
  - internal/quota/service.go
  - docs/plan/todo/workroom-optimization.md
  - docs/plan/todo.md
  - internal/db/session.go
  - internal/cursor/runner.go
  - internal/server/server.go
  - internal/static/js/session/RoomControls.js
  - internal/ws/sync.go
  - scripts/browser/conftest.py
  - internal/static/js/chat/SessionActionCards.js
  - config.example.yaml
  - scripts/browser/check_workroom_preview.py
  - scripts/console-usability.test.cjs
  - scripts/rooms.test.cjs
  - internal/static/js/session/RoomFormModal.js
  - internal/mcp/registry.go
  - scripts/fake-claude.cjs
  - scripts/telegram-chat.test.cjs
  - internal/db/room.go
  - internal/static/js/session/RoomMessageBubble.js
  - internal/db/db.go
  - internal/static/js/hooks/useChatSocket.js
  - docs/reviews/2026-11-18-workroom-optimization-verification.md
  - docs/plan/todo/supervisor-room.md
  - internal/db/room_timeline.go
  - internal/static/js/hooks/quotaResumeBanner.js
  - README.md
  - internal/api/settings.go
  - internal/db/general_settings.go
  - internal/static/js/chat/ChatHeader.js
  - docs/plan/todo/workroom-optimization-preview.html
  - internal/db/queue.go
  - internal/version/version.go
  - internal/server/quota_resume.go
  - internal/mcp/tools.go
  - scripts/general-settings-ui.test.cjs
  - docs/plan/todo/claude-permission-prompt.md
  - internal/api/rooms.go
  - internal/mcp/envelope.go
  - scripts/quota-resume-banner.test.cjs
  - docs/plan/todo/ux-feedback.md
  - internal/ws/quota_resume.go
  - internal/db/room_workflow.go
  - internal/static/js/hooks/useRoomTimeline.js
  - internal/static/js/core/core.js
  - internal/db/attachment.go
  - internal/static/js/chat/QuotaResumeBanner.js
  - scripts/composer-attachment.test.py
tests:
  - internal/api/rooms_test.go
  - internal/ws/quota_cancel_test.go
  - internal/api/general_settings_test.go
  - internal/db/room_workflow_test.go
  - internal/db/quota_resume_test.go
  - internal/ws/attachment_test.go
  - internal/mcp/envelope_test.go
  - internal/ws/quota_guard_test.go
  - scripts/browser/test_rooms.py
  - internal/ws/quota_resume_test.go
  - internal/server/quota_resume_test.go
  - internal/db/room_source_test.go
  - internal/ws/queue_test.go
  - scripts/browser/test_workroom_layout.py
  - internal/server/guest_test.go
  - cmd/desktop/install_windows_test.go
  - internal/ws/claudeperm_test.go
  - internal/db/general_settings_test.go
  - internal/api/session_instruction_test.go
  - internal/server/quota_e2e_test.go
-->

---
### Requirement: Guard settings validation
The system SHALL reject a general settings write whose `quotaGuardPercent` is not a number greater than 0 and at most 100, SHALL respond with a client error naming the field, and SHALL NOT change any stored setting when it rejects.

##### Example: validation boundaries
| `quotaGuardPercent` | Result |
|---------------------|--------|
| 0 | rejected |
| -5 | rejected |
| 100.5 | rejected |
| 100 | accepted |
| 0.5 | accepted |
| 90 | accepted |

#### Scenario: Out-of-range write
- **WHEN** a client writes the general settings with `quotaGuardPercent` 150
- **THEN** the system SHALL respond with a client error and the previously stored settings SHALL remain


<!-- @trace
source: quota-auto-resume
updated: 2026-11-19
code:
  - internal/static/js/session/RoomsSection.js
  - internal/static/js/chat/GitControls.js
  - internal/db/input_receipt.go
  - internal/static/index.html
  - internal/static/js/chat/ChatView.js
  - cmd/desktop/desktop_windows_amd64.syso
  - docs/reviews/2026-11-18-workroom-first-run-observations.md
  - internal/server/isolated.go
  - internal/static/js/session/RoomView.js
  - internal/server/room.go
  - internal/ws/broadcast.go
  - internal/ws/claudeperm.go
  - VERSION
  - docs/plan/todo/quota-auto-resume.md
  - CHANGELOG.md
  - internal/db/quota_resume.go
  - internal/static/js/hooks/rooms.js
  - internal/ws/attachment.go
  - internal/static/js/ui/SettingsModal.js
  - README.zh-TW.md
  - internal/ws/dispatch.go
  - internal/ws/handler.go
  - internal/ws/quota_guard.go
  - internal/quota/service.go
  - docs/plan/todo/workroom-optimization.md
  - docs/plan/todo.md
  - internal/db/session.go
  - internal/cursor/runner.go
  - internal/server/server.go
  - internal/static/js/session/RoomControls.js
  - internal/ws/sync.go
  - scripts/browser/conftest.py
  - internal/static/js/chat/SessionActionCards.js
  - config.example.yaml
  - scripts/browser/check_workroom_preview.py
  - scripts/console-usability.test.cjs
  - scripts/rooms.test.cjs
  - internal/static/js/session/RoomFormModal.js
  - internal/mcp/registry.go
  - scripts/fake-claude.cjs
  - scripts/telegram-chat.test.cjs
  - internal/db/room.go
  - internal/static/js/session/RoomMessageBubble.js
  - internal/db/db.go
  - internal/static/js/hooks/useChatSocket.js
  - docs/reviews/2026-11-18-workroom-optimization-verification.md
  - docs/plan/todo/supervisor-room.md
  - internal/db/room_timeline.go
  - internal/static/js/hooks/quotaResumeBanner.js
  - README.md
  - internal/api/settings.go
  - internal/db/general_settings.go
  - internal/static/js/chat/ChatHeader.js
  - docs/plan/todo/workroom-optimization-preview.html
  - internal/db/queue.go
  - internal/version/version.go
  - internal/server/quota_resume.go
  - internal/mcp/tools.go
  - scripts/general-settings-ui.test.cjs
  - docs/plan/todo/claude-permission-prompt.md
  - internal/api/rooms.go
  - internal/mcp/envelope.go
  - scripts/quota-resume-banner.test.cjs
  - docs/plan/todo/ux-feedback.md
  - internal/ws/quota_resume.go
  - internal/db/room_workflow.go
  - internal/static/js/hooks/useRoomTimeline.js
  - internal/static/js/core/core.js
  - internal/db/attachment.go
  - internal/static/js/chat/QuotaResumeBanner.js
  - scripts/composer-attachment.test.py
tests:
  - internal/api/rooms_test.go
  - internal/ws/quota_cancel_test.go
  - internal/api/general_settings_test.go
  - internal/db/room_workflow_test.go
  - internal/db/quota_resume_test.go
  - internal/ws/attachment_test.go
  - internal/mcp/envelope_test.go
  - internal/ws/quota_guard_test.go
  - scripts/browser/test_rooms.py
  - internal/ws/quota_resume_test.go
  - internal/server/quota_resume_test.go
  - internal/db/room_source_test.go
  - internal/ws/queue_test.go
  - scripts/browser/test_workroom_layout.py
  - internal/server/guest_test.go
  - cmd/desktop/install_windows_test.go
  - internal/ws/claudeperm_test.go
  - internal/db/general_settings_test.go
  - internal/api/session_instruction_test.go
  - internal/server/quota_e2e_test.go
-->

---
### Requirement: Guard reads settings at evaluation time
Before the server automatically starts the next queued message, the system SHALL read the current effective guard settings. When `quotaGuardEnabled` is false it SHALL NOT pause the queue for quota reasons. When true, it SHALL apply the existing rules with the current `quotaGuardPercent`: the queue is paused when the cached quota of that provider is not stale and any window that has a percent has used at least that percent; stale or missing data, and windows without a percent, SHALL NOT pause. A message sent directly by the user and the user pressing "Continue" SHALL NOT be affected by the guard. A change of the settings SHALL apply to the next evaluation without restarting the server or reconnecting clients.

#### Scenario: Threshold lowered at runtime
- **WHEN** the owner changes `quotaGuardPercent` from 90 to 40 while a connected session is running with a queued message and the provider window is at 50%
- **THEN** when the running message finishes the queue SHALL be paused with a reason that states 50% used and the 40% threshold

#### Scenario: Guard switched off at runtime
- **WHEN** the owner turns `quotaGuardEnabled` off while a queued message waits and the provider window is at 95%
- **THEN** the next automatic start SHALL run the queued message

#### Scenario: Direct send is not guarded
- **WHEN** the guard is enabled at 10% and the window is at 95% and the user sends a message to an idle session
- **THEN** the message SHALL run


<!-- @trace
source: quota-auto-resume
updated: 2026-11-19
code:
  - internal/static/js/session/RoomsSection.js
  - internal/static/js/chat/GitControls.js
  - internal/db/input_receipt.go
  - internal/static/index.html
  - internal/static/js/chat/ChatView.js
  - cmd/desktop/desktop_windows_amd64.syso
  - docs/reviews/2026-11-18-workroom-first-run-observations.md
  - internal/server/isolated.go
  - internal/static/js/session/RoomView.js
  - internal/server/room.go
  - internal/ws/broadcast.go
  - internal/ws/claudeperm.go
  - VERSION
  - docs/plan/todo/quota-auto-resume.md
  - CHANGELOG.md
  - internal/db/quota_resume.go
  - internal/static/js/hooks/rooms.js
  - internal/ws/attachment.go
  - internal/static/js/ui/SettingsModal.js
  - README.zh-TW.md
  - internal/ws/dispatch.go
  - internal/ws/handler.go
  - internal/ws/quota_guard.go
  - internal/quota/service.go
  - docs/plan/todo/workroom-optimization.md
  - docs/plan/todo.md
  - internal/db/session.go
  - internal/cursor/runner.go
  - internal/server/server.go
  - internal/static/js/session/RoomControls.js
  - internal/ws/sync.go
  - scripts/browser/conftest.py
  - internal/static/js/chat/SessionActionCards.js
  - config.example.yaml
  - scripts/browser/check_workroom_preview.py
  - scripts/console-usability.test.cjs
  - scripts/rooms.test.cjs
  - internal/static/js/session/RoomFormModal.js
  - internal/mcp/registry.go
  - scripts/fake-claude.cjs
  - scripts/telegram-chat.test.cjs
  - internal/db/room.go
  - internal/static/js/session/RoomMessageBubble.js
  - internal/db/db.go
  - internal/static/js/hooks/useChatSocket.js
  - docs/reviews/2026-11-18-workroom-optimization-verification.md
  - docs/plan/todo/supervisor-room.md
  - internal/db/room_timeline.go
  - internal/static/js/hooks/quotaResumeBanner.js
  - README.md
  - internal/api/settings.go
  - internal/db/general_settings.go
  - internal/static/js/chat/ChatHeader.js
  - docs/plan/todo/workroom-optimization-preview.html
  - internal/db/queue.go
  - internal/version/version.go
  - internal/server/quota_resume.go
  - internal/mcp/tools.go
  - scripts/general-settings-ui.test.cjs
  - docs/plan/todo/claude-permission-prompt.md
  - internal/api/rooms.go
  - internal/mcp/envelope.go
  - scripts/quota-resume-banner.test.cjs
  - docs/plan/todo/ux-feedback.md
  - internal/ws/quota_resume.go
  - internal/db/room_workflow.go
  - internal/static/js/hooks/useRoomTimeline.js
  - internal/static/js/core/core.js
  - internal/db/attachment.go
  - internal/static/js/chat/QuotaResumeBanner.js
  - scripts/composer-attachment.test.py
tests:
  - internal/api/rooms_test.go
  - internal/ws/quota_cancel_test.go
  - internal/api/general_settings_test.go
  - internal/db/room_workflow_test.go
  - internal/db/quota_resume_test.go
  - internal/ws/attachment_test.go
  - internal/mcp/envelope_test.go
  - internal/ws/quota_guard_test.go
  - scripts/browser/test_rooms.py
  - internal/ws/quota_resume_test.go
  - internal/server/quota_resume_test.go
  - internal/db/room_source_test.go
  - internal/ws/queue_test.go
  - scripts/browser/test_workroom_layout.py
  - internal/server/guest_test.go
  - cmd/desktop/install_windows_test.go
  - internal/ws/claudeperm_test.go
  - internal/db/general_settings_test.go
  - internal/api/session_instruction_test.go
  - internal/server/quota_e2e_test.go
-->

---
### Requirement: Guard controls in the settings page
The settings page "General" section SHALL show a switch for the quota guard and a numeric input for the percent, SHALL disable the input while the switch is off, SHALL save changes immediately, and SHALL show the server error text when the server rejects a value, restoring the last accepted values.

#### Scenario: Invalid value rejected in the UI
- **WHEN** the owner enters 150 in the percent input
- **THEN** the page SHALL show the server error and SHALL restore the previously saved percent

<!-- @trace
source: quota-auto-resume
updated: 2026-11-19
code:
  - internal/static/js/session/RoomsSection.js
  - internal/static/js/chat/GitControls.js
  - internal/db/input_receipt.go
  - internal/static/index.html
  - internal/static/js/chat/ChatView.js
  - cmd/desktop/desktop_windows_amd64.syso
  - docs/reviews/2026-11-18-workroom-first-run-observations.md
  - internal/server/isolated.go
  - internal/static/js/session/RoomView.js
  - internal/server/room.go
  - internal/ws/broadcast.go
  - internal/ws/claudeperm.go
  - VERSION
  - docs/plan/todo/quota-auto-resume.md
  - CHANGELOG.md
  - internal/db/quota_resume.go
  - internal/static/js/hooks/rooms.js
  - internal/ws/attachment.go
  - internal/static/js/ui/SettingsModal.js
  - README.zh-TW.md
  - internal/ws/dispatch.go
  - internal/ws/handler.go
  - internal/ws/quota_guard.go
  - internal/quota/service.go
  - docs/plan/todo/workroom-optimization.md
  - docs/plan/todo.md
  - internal/db/session.go
  - internal/cursor/runner.go
  - internal/server/server.go
  - internal/static/js/session/RoomControls.js
  - internal/ws/sync.go
  - scripts/browser/conftest.py
  - internal/static/js/chat/SessionActionCards.js
  - config.example.yaml
  - scripts/browser/check_workroom_preview.py
  - scripts/console-usability.test.cjs
  - scripts/rooms.test.cjs
  - internal/static/js/session/RoomFormModal.js
  - internal/mcp/registry.go
  - scripts/fake-claude.cjs
  - scripts/telegram-chat.test.cjs
  - internal/db/room.go
  - internal/static/js/session/RoomMessageBubble.js
  - internal/db/db.go
  - internal/static/js/hooks/useChatSocket.js
  - docs/reviews/2026-11-18-workroom-optimization-verification.md
  - docs/plan/todo/supervisor-room.md
  - internal/db/room_timeline.go
  - internal/static/js/hooks/quotaResumeBanner.js
  - README.md
  - internal/api/settings.go
  - internal/db/general_settings.go
  - internal/static/js/chat/ChatHeader.js
  - docs/plan/todo/workroom-optimization-preview.html
  - internal/db/queue.go
  - internal/version/version.go
  - internal/server/quota_resume.go
  - internal/mcp/tools.go
  - scripts/general-settings-ui.test.cjs
  - docs/plan/todo/claude-permission-prompt.md
  - internal/api/rooms.go
  - internal/mcp/envelope.go
  - scripts/quota-resume-banner.test.cjs
  - docs/plan/todo/ux-feedback.md
  - internal/ws/quota_resume.go
  - internal/db/room_workflow.go
  - internal/static/js/hooks/useRoomTimeline.js
  - internal/static/js/core/core.js
  - internal/db/attachment.go
  - internal/static/js/chat/QuotaResumeBanner.js
  - scripts/composer-attachment.test.py
tests:
  - internal/api/rooms_test.go
  - internal/ws/quota_cancel_test.go
  - internal/api/general_settings_test.go
  - internal/db/room_workflow_test.go
  - internal/db/quota_resume_test.go
  - internal/ws/attachment_test.go
  - internal/mcp/envelope_test.go
  - internal/ws/quota_guard_test.go
  - scripts/browser/test_rooms.py
  - internal/ws/quota_resume_test.go
  - internal/server/quota_resume_test.go
  - internal/db/room_source_test.go
  - internal/ws/queue_test.go
  - scripts/browser/test_workroom_layout.py
  - internal/server/guest_test.go
  - cmd/desktop/install_windows_test.go
  - internal/ws/claudeperm_test.go
  - internal/db/general_settings_test.go
  - internal/api/session_instruction_test.go
  - internal/server/quota_e2e_test.go
-->