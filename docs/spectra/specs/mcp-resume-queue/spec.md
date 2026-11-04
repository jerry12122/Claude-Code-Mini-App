# mcp-resume-queue Specification

## Purpose

The mcp-resume-queue capability lets external agents release a session's paused message queue through MCP, matching the resume action available in the UI. It exists because a failed run pauses the queue and, while paused, messages sent through MCP are only appended behind it and never run.

## Requirements

### Requirement: Resume queue tool
The MCP server SHALL provide a tool named `resume_queue` that accepts a required `session_id` and releases the pause on that session's message queue with the same behavior as the UI queue-resume action.

#### Scenario: Paused queue on an idle session
- **WHEN** an agent calls `resume_queue` for an idle session whose queue is paused and has pending messages
- **THEN** the system SHALL clear the pause, start executing the first queued message, and return `{ ok: true }`

#### Scenario: Paused queue on a busy session
- **WHEN** an agent calls `resume_queue` for a session that is running a task and has a paused queue
- **THEN** the system SHALL clear the pause without interrupting the task, and the next queued message SHALL run when the task ends

#### Scenario: Empty or unpaused queue
- **WHEN** an agent calls `resume_queue` for a session whose queue is empty or not paused
- **THEN** the system SHALL return `{ ok: true }` without any side effect

##### Example: Two queued messages after a quota failure
- **GIVEN** session "cloudserver" is idle with queue items 244 and 248 and `queue_paused` true
- **WHEN** an agent calls `resume_queue` for it
- **THEN** item 244 starts running, `queue_paused` becomes false, and item 248 remains queued

#### Scenario: Invalid session
- **WHEN** an agent calls `resume_queue` with a blank or non-existent `session_id`
- **THEN** the system SHALL return an error indicating the session was not found


<!-- @trace
source: session-failure-and-queue-resume
updated: 2026-11-04
code:
  - VERSION
  - internal/static/js/ui/SplitLayout.js
  - internal/mcp/query.go
  - docs/plan/todo.md
  - internal/static/js/chat/MentionMenu.js
  - internal/static/js/ui/SettingsModal.js
  - internal/static/js/setup/SetupWizard.js
  - internal/static/js/chat/MessageActions.js
  - internal/static/index.html
  - internal/db/queue.go
  - scripts/mermaid-render.test.cjs
  - docs/features.md
  - internal/static/js/share/SharesSection.js
  - internal/static/js/session/SessionView.js
  - internal/static/js/chat/MermaidBlock.js
  - docs/features.zh-TW.md
  - internal/ws/failure_record.go
  - internal/static/js/chat/SelectionQuotePopover.js
  - internal/static/js/session/NewSessionComposer.js
  - CHANGELOG.md
  - internal/version/version.go
  - internal/static/js/chat/chat-header.js
  - scripts/theme.test.cjs
  - internal/mcp/tools.go
  - README.md
  - internal/static/js/ui/ui-atoms.js
  - internal/static/js/share/ShareModal.js
  - internal/static/js/ui/SessionTabs.js
  - internal/static/js/ui/DesktopUpdateSection.js
  - internal/static/js/app.js
  - internal/db/session.go
  - internal/static/js/ui/LogsSection.js
  - internal/db/db.go
  - internal/static/js/share/JoinView.js
  - internal/static/js/chat/ChatView.js
  - internal/ws/handler.go
  - internal/static/js/core/core.js
  - internal/mcp/registry.go
  - README.zh-TW.md
tests:
  - internal/mcp/resume_queue_test.go
  - internal/mcp/queue_resume_e2e_test.go
  - internal/db/session_failure_test.go
  - internal/mcp/queue_test.go
  - internal/ws/failure_record_test.go
  - internal/mcp/ask_e2e_test.go
  - internal/mcp/list_sessions_test.go
-->

---
### Requirement: Resume does not alter turn state
Calling `resume_queue` SHALL NOT reset the session's cached reply text or error, and SHALL NOT mark the session as running unless a queued message actually starts.

#### Scenario: Resume with an empty queue
- **WHEN** an agent calls `resume_queue` for an idle session with an empty queue
- **THEN** a following `get_status` SHALL still report the session as idle


<!-- @trace
source: session-failure-and-queue-resume
updated: 2026-11-04
code:
  - VERSION
  - internal/static/js/ui/SplitLayout.js
  - internal/mcp/query.go
  - docs/plan/todo.md
  - internal/static/js/chat/MentionMenu.js
  - internal/static/js/ui/SettingsModal.js
  - internal/static/js/setup/SetupWizard.js
  - internal/static/js/chat/MessageActions.js
  - internal/static/index.html
  - internal/db/queue.go
  - scripts/mermaid-render.test.cjs
  - docs/features.md
  - internal/static/js/share/SharesSection.js
  - internal/static/js/session/SessionView.js
  - internal/static/js/chat/MermaidBlock.js
  - docs/features.zh-TW.md
  - internal/ws/failure_record.go
  - internal/static/js/chat/SelectionQuotePopover.js
  - internal/static/js/session/NewSessionComposer.js
  - CHANGELOG.md
  - internal/version/version.go
  - internal/static/js/chat/chat-header.js
  - scripts/theme.test.cjs
  - internal/mcp/tools.go
  - README.md
  - internal/static/js/ui/ui-atoms.js
  - internal/static/js/share/ShareModal.js
  - internal/static/js/ui/SessionTabs.js
  - internal/static/js/ui/DesktopUpdateSection.js
  - internal/static/js/app.js
  - internal/db/session.go
  - internal/static/js/ui/LogsSection.js
  - internal/db/db.go
  - internal/static/js/share/JoinView.js
  - internal/static/js/chat/ChatView.js
  - internal/ws/handler.go
  - internal/static/js/core/core.js
  - internal/mcp/registry.go
  - README.zh-TW.md
tests:
  - internal/mcp/resume_queue_test.go
  - internal/mcp/queue_resume_e2e_test.go
  - internal/db/session_failure_test.go
  - internal/mcp/queue_test.go
  - internal/ws/failure_record_test.go
  - internal/mcp/ask_e2e_test.go
  - internal/mcp/list_sessions_test.go
-->

---
### Requirement: Respect maintenance gating
While the server is in maintenance and refusing new work, `resume_queue` SHALL NOT take any queued message out of the queue.

#### Scenario: Resume during maintenance
- **WHEN** an agent calls `resume_queue` while the maintenance gate rejects new work
- **THEN** the queued messages SHALL remain queued

<!-- @trace
source: session-failure-and-queue-resume
updated: 2026-11-04
code:
  - VERSION
  - internal/static/js/ui/SplitLayout.js
  - internal/mcp/query.go
  - docs/plan/todo.md
  - internal/static/js/chat/MentionMenu.js
  - internal/static/js/ui/SettingsModal.js
  - internal/static/js/setup/SetupWizard.js
  - internal/static/js/chat/MessageActions.js
  - internal/static/index.html
  - internal/db/queue.go
  - scripts/mermaid-render.test.cjs
  - docs/features.md
  - internal/static/js/share/SharesSection.js
  - internal/static/js/session/SessionView.js
  - internal/static/js/chat/MermaidBlock.js
  - docs/features.zh-TW.md
  - internal/ws/failure_record.go
  - internal/static/js/chat/SelectionQuotePopover.js
  - internal/static/js/session/NewSessionComposer.js
  - CHANGELOG.md
  - internal/version/version.go
  - internal/static/js/chat/chat-header.js
  - scripts/theme.test.cjs
  - internal/mcp/tools.go
  - README.md
  - internal/static/js/ui/ui-atoms.js
  - internal/static/js/share/ShareModal.js
  - internal/static/js/ui/SessionTabs.js
  - internal/static/js/ui/DesktopUpdateSection.js
  - internal/static/js/app.js
  - internal/db/session.go
  - internal/static/js/ui/LogsSection.js
  - internal/db/db.go
  - internal/static/js/share/JoinView.js
  - internal/static/js/chat/ChatView.js
  - internal/ws/handler.go
  - internal/static/js/core/core.js
  - internal/mcp/registry.go
  - README.zh-TW.md
tests:
  - internal/mcp/resume_queue_test.go
  - internal/mcp/queue_resume_e2e_test.go
  - internal/db/session_failure_test.go
  - internal/mcp/queue_test.go
  - internal/ws/failure_record_test.go
  - internal/mcp/ask_e2e_test.go
  - internal/mcp/list_sessions_test.go
-->