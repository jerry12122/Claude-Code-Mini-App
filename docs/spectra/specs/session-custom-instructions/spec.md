# session-custom-instructions Specification

## Purpose

Session custom instructions let an owner define a conversation-specific role, response style, and working rules while creating a session. The capability uses native instruction channels for Claude, Codex, and Antigravity without creating shared agent files or altering the first user message.

## Requirements

### Requirement: Supported creation surfaces offer a separate instruction field

The system SHALL offer an optional multiline field named 專屬指令（選填） when creating an agent-mode session for claude, codex, or antigravity, both in New Session and in forward-to-new-session flows. The field SHALL have an accessible label and SHALL explain that its value is fixed after creation. It SHALL remain separate from the first user message and custom CLI arguments. Cursor, Kiro, Kiro ACP, Telegram, and new shell sessions SHALL NOT offer this field.

#### Scenario: Create an instructed conversation

- **WHEN** the owner selects any supported runner, enters instructions and a first message, and submits New Session
- **THEN** the creation request SHALL contain the instructions as system_instruction
- **AND** only the first message SHALL be sent as user message content

#### Scenario: Enter multiline instructions with a keyboard

- **WHEN** the owner focuses the instruction field and presses Enter within multiline text
- **THEN** the field SHALL insert a newline without submitting the session form
- **AND** its label and help text SHALL be available to assistive technology

#### Scenario: Switch supported and unsupported runners

- **WHEN** the owner switches between claude, codex, and antigravity
- **THEN** the current instruction draft SHALL remain available
- **WHEN** the owner switches to an unsupported runner
- **THEN** the instruction draft SHALL be cleared and omitted from its creation request

#### Scenario: Reset or copy creation settings

- **WHEN** the creation form is reset
- **THEN** its instruction draft SHALL be empty
- **WHEN** settings from a supported session are copied into a supported new-session form
- **THEN** the source instruction SHALL populate the field and remain editable until creation


<!-- @trace
source: session-custom-instructions
updated: 2026-11-13
code:
  - internal/static/js/chat/ForwardModal.js
  - cmd/desktop/main_windows.go
  - internal/static/js/chat/ChatView.js
  - internal/static/js/hooks/useNewSessionForm.js
  - internal/static/js/core/core.js
  - scripts/context-menu.test.cjs
  - internal/api/open.go
  - CHANGELOG.md
  - docs/features.md
  - internal/db/busy.go
  - internal/server/server.go
  - internal/mcp/tools.go
  - cmd/desktop/desktop_windows_amd64.syso
  - README.md
  - README.zh-TW.md
  - internal/api/session.go
  - internal/api/upload.go
  - internal/ws/handler.go
  - internal/static/js/chat/chat-header.js
  - internal/static/js/ui/SessionTabs.js
  - cmd/desktop/desktop.rc
  - config.example.yaml
  - internal/agent/factory.go
  - docs/features.zh-TW.md
tests:
  - internal/mcp/kiro_readonly_test.go
  - internal/api/open_test.go
  - internal/db/busy_test.go
  - docs/spec/kiro-cli.md
  - cmd/desktop/icon_windows_amd64_test.go
  - internal/ws/kiro_readonly_test.go
  - internal/agent/factory_test.go
-->

---
### Requirement: Creation validates and atomically persists instructions

POST /sessions SHALL accept an optional string system_instruction, trim outer Unicode whitespace, and preserve inner text, line breaks, quotes, backslashes, and Unicode. The normalized value SHALL be at most 16384 UTF-8 bytes and SHALL NOT contain U+0000. Non-empty values SHALL be accepted only for canonical agent types claude, codex, and antigravity in agent input mode after provider and Telegram-target normalization. Invalid types, unsupported targets, forbidden characters, and oversized values SHALL return HTTP 400 with an error field, without creating a session. A successful HTTP 201 SHALL return the stored normalized value; storage failure SHALL return HTTP 500 without leaving a partial session.

#### Scenario: Persist a supported value

- **WHEN** a supported create request contains the instruction "  回答使用繁體中文\n路徑 C:\\work  "
- **THEN** the stored and returned system_instruction SHALL be "回答使用繁體中文\n路徑 C:\\work"

#### Scenario: Enforce the byte boundary without truncation

- **WHEN** the normalized input has the following UTF-8 size
- **THEN** the system SHALL produce the listed outcome without truncating text

##### Example: Instruction size policy

| Normalized value | UTF-8 bytes | Expected outcome |
| --- | --- | --- |
| Empty string | 0 | Accepted as no instructions |
| 16384 ASCII a characters | 16384 | Accepted unchanged |
| 16385 ASCII a characters | 16385 | HTTP 400; no new session |
| 5461 copies of 中 followed by a | 16384 | Accepted unchanged |
| 5461 copies of 中 followed by ab | 16385 | HTTP 400; no new session |

#### Scenario: Reject unsupported targets and malformed values

- **WHEN** a create request provides a non-empty instruction for cursor, kiro, kiroacp, telegram, shell input mode, or a target normalized to Telegram
- **THEN** the response SHALL be HTTP 400 and no session SHALL be created
- **WHEN** system_instruction is a non-string JSON value or contains U+0000
- **THEN** the response SHALL be HTTP 400 and no session SHALL be created

#### Scenario: Reject conflicting Claude custom flags

- **WHEN** a Claude create request combines non-empty system_instruction with a custom --system-prompt, --system-prompt-file, --append-system-prompt, or --append-system-prompt-file argument, including an equals-value form
- **THEN** the system SHALL return HTTP 400 asking the owner to choose one instruction input path
- **AND** empty system_instruction SHALL preserve existing custom-argument behavior

#### Scenario: Roll back failed instruction persistence

- **WHEN** the system cannot persist the instruction while creating the session
- **THEN** it SHALL report HTTP 500 without retaining a session missing the requested instruction


<!-- @trace
source: session-custom-instructions
updated: 2026-11-13
code:
  - internal/static/js/chat/ForwardModal.js
  - cmd/desktop/main_windows.go
  - internal/static/js/chat/ChatView.js
  - internal/static/js/hooks/useNewSessionForm.js
  - internal/static/js/core/core.js
  - scripts/context-menu.test.cjs
  - internal/api/open.go
  - CHANGELOG.md
  - docs/features.md
  - internal/db/busy.go
  - internal/server/server.go
  - internal/mcp/tools.go
  - cmd/desktop/desktop_windows_amd64.syso
  - README.md
  - README.zh-TW.md
  - internal/api/session.go
  - internal/api/upload.go
  - internal/ws/handler.go
  - internal/static/js/chat/chat-header.js
  - internal/static/js/ui/SessionTabs.js
  - cmd/desktop/desktop.rc
  - config.example.yaml
  - internal/agent/factory.go
  - docs/features.zh-TW.md
tests:
  - internal/mcp/kiro_readonly_test.go
  - internal/api/open_test.go
  - internal/db/busy_test.go
  - docs/spec/kiro-cli.md
  - cmd/desktop/icon_windows_amd64_test.go
  - internal/ws/kiro_readonly_test.go
  - internal/agent/factory_test.go
-->

---
### Requirement: Instructions are fixed and isolated per session

The system SHALL persist system_instruction separately from message history and SHALL expose it through owner session reads. PATCH /sessions/:id SHALL reject any request containing system_instruction with HTTP 400 before modifying any requested field. The system SHALL use the stored instruction for every agent run, retry, queued input, reconnection, process restart, and fresh CLI conversation after clearing the CLI session ID. A temporary switch to shell mode SHALL NOT erase the stored value and SHALL NOT inject it into shell commands. Different sessions SHALL NOT share instruction content through their working directory.

#### Scenario: Read after restart

- **WHEN** the application restarts after an instructed session was created
- **THEN** owner session reads and the next run SHALL retain that session's original normalized instruction

#### Scenario: Reject changes atomically

- **WHEN** PATCH contains system_instruction together with a new name or custom CLI arguments
- **THEN** the system SHALL reject the request and leave all requested session fields unchanged

#### Scenario: Queue and reset retain instructions

- **WHEN** input is retried or dequeued after reconnecting, or the session starts a fresh CLI conversation after its CLI session ID is cleared
- **THEN** the runner SHALL receive the same stored instruction separately from the user prompt

#### Scenario: Sessions sharing a directory stay independent

- **GIVEN** two sessions share a work_dir and have instructions A and B
- **WHEN** both sessions run
- **THEN** each runner SHALL receive only its own stored instruction
- **AND** no instruction SHALL be written to a custom agent file, workspace rules file, or user-global configuration

#### Scenario: Shell mode does not consume instructions

- **WHEN** an instructed session temporarily executes in shell mode and later returns to agent mode
- **THEN** shell commands SHALL receive no instruction text
- **AND** subsequent agent runs SHALL receive the original stored value


<!-- @trace
source: session-custom-instructions
updated: 2026-11-13
code:
  - internal/static/js/chat/ForwardModal.js
  - cmd/desktop/main_windows.go
  - internal/static/js/chat/ChatView.js
  - internal/static/js/hooks/useNewSessionForm.js
  - internal/static/js/core/core.js
  - scripts/context-menu.test.cjs
  - internal/api/open.go
  - CHANGELOG.md
  - docs/features.md
  - internal/db/busy.go
  - internal/server/server.go
  - internal/mcp/tools.go
  - cmd/desktop/desktop_windows_amd64.syso
  - README.md
  - README.zh-TW.md
  - internal/api/session.go
  - internal/api/upload.go
  - internal/ws/handler.go
  - internal/static/js/chat/chat-header.js
  - internal/static/js/ui/SessionTabs.js
  - cmd/desktop/desktop.rc
  - config.example.yaml
  - internal/agent/factory.go
  - docs/features.zh-TW.md
tests:
  - internal/mcp/kiro_readonly_test.go
  - internal/api/open_test.go
  - internal/db/busy_test.go
  - docs/spec/kiro-cli.md
  - cmd/desktop/icon_windows_amd64_test.go
  - internal/ws/kiro_readonly_test.go
  - internal/agent/factory_test.go
-->

---
### Requirement: Existing sessions and empty instructions retain their execution path

A database upgrade SHALL initialize existing sessions with empty instructions. Omitted, empty, or whitespace-only instructions SHALL add no provider instruction arguments and SHALL retain the original runner transport and configured fallback behavior. No new public agent_type SHALL be required for this feature.

#### Scenario: Upgrade an existing database

- **WHEN** the application upgrades a database containing sessions without an instruction column
- **THEN** every existing session SHALL retain its prior data and read an empty system_instruction

#### Scenario: Create without instructions

- **WHEN** a session is created without system_instruction or with whitespace-only text
- **THEN** it SHALL run with the same instruction arguments and transport as the pre-feature implementation
- **AND** an Antigravity session SHALL remain on its existing CLI path


<!-- @trace
source: session-custom-instructions
updated: 2026-11-13
code:
  - internal/static/js/chat/ForwardModal.js
  - cmd/desktop/main_windows.go
  - internal/static/js/chat/ChatView.js
  - internal/static/js/hooks/useNewSessionForm.js
  - internal/static/js/core/core.js
  - scripts/context-menu.test.cjs
  - internal/api/open.go
  - CHANGELOG.md
  - docs/features.md
  - internal/db/busy.go
  - internal/server/server.go
  - internal/mcp/tools.go
  - cmd/desktop/desktop_windows_amd64.syso
  - README.md
  - README.zh-TW.md
  - internal/api/session.go
  - internal/api/upload.go
  - internal/ws/handler.go
  - internal/static/js/chat/chat-header.js
  - internal/static/js/ui/SessionTabs.js
  - cmd/desktop/desktop.rc
  - config.example.yaml
  - internal/agent/factory.go
  - docs/features.zh-TW.md
tests:
  - internal/mcp/kiro_readonly_test.go
  - internal/api/open_test.go
  - internal/db/busy_test.go
  - docs/spec/kiro-cli.md
  - cmd/desktop/icon_windows_amd64_test.go
  - internal/ws/kiro_readonly_test.go
  - internal/agent/factory_test.go
-->

---
### Requirement: Claude appends the saved instruction natively

For a non-empty instruction, the Claude runner SHALL pass its normalized value as one --append-system-prompt argument while retaining the default system prompt. New and resumed turns SHALL use the same saved value without altering the user Prompt or forcing a different system-prompt snapshot policy.

#### Scenario: New and resumed Claude invocation

- **WHEN** a Claude session with an instruction containing quotes, newlines, backslashes, and Unicode starts or resumes
- **THEN** its argv SHALL contain the complete instruction in one append argument
- **AND** its user prompt, model, effort, permission mode, and existing MCP permission bridge SHALL retain their established meaning


<!-- @trace
source: session-custom-instructions
updated: 2026-11-13
code:
  - internal/static/js/chat/ForwardModal.js
  - cmd/desktop/main_windows.go
  - internal/static/js/chat/ChatView.js
  - internal/static/js/hooks/useNewSessionForm.js
  - internal/static/js/core/core.js
  - scripts/context-menu.test.cjs
  - internal/api/open.go
  - CHANGELOG.md
  - docs/features.md
  - internal/db/busy.go
  - internal/server/server.go
  - internal/mcp/tools.go
  - cmd/desktop/desktop_windows_amd64.syso
  - README.md
  - README.zh-TW.md
  - internal/api/session.go
  - internal/api/upload.go
  - internal/ws/handler.go
  - internal/static/js/chat/chat-header.js
  - internal/static/js/ui/SessionTabs.js
  - cmd/desktop/desktop.rc
  - config.example.yaml
  - internal/agent/factory.go
  - docs/features.zh-TW.md
tests:
  - internal/mcp/kiro_readonly_test.go
  - internal/api/open_test.go
  - internal/db/busy_test.go
  - docs/spec/kiro-cli.md
  - cmd/desktop/icon_windows_amd64_test.go
  - internal/ws/kiro_readonly_test.go
  - internal/agent/factory_test.go
-->

---
### Requirement: Codex receives saved developer instructions

For a non-empty instruction, the Codex runner SHALL pass developer_instructions using a correctly quoted configuration override for both exec and exec resume. The override SHALL preserve the exact normalized text as a single argument value. The feature SHALL NOT present changing an existing Codex thread's instructions as supported behavior.

#### Scenario: Encode complex Codex instructions

- **WHEN** the saved instruction contains quotes, line breaks, backslashes, and Unicode
- **THEN** the configuration override SHALL decode to exactly that value
- **AND** no part of the value SHALL become another CLI flag or configuration assignment

#### Scenario: Resume without changing the instruction

- **WHEN** an instructed Codex session resumes
- **THEN** the runner SHALL supply the same stored developer_instructions value with the same thread ID
- **AND** no instruction text SHALL be prepended to the user prompt


<!-- @trace
source: session-custom-instructions
updated: 2026-11-13
code:
  - internal/static/js/chat/ForwardModal.js
  - cmd/desktop/main_windows.go
  - internal/static/js/chat/ChatView.js
  - internal/static/js/hooks/useNewSessionForm.js
  - internal/static/js/core/core.js
  - scripts/context-menu.test.cjs
  - internal/api/open.go
  - CHANGELOG.md
  - docs/features.md
  - internal/db/busy.go
  - internal/server/server.go
  - internal/mcp/tools.go
  - cmd/desktop/desktop_windows_amd64.syso
  - README.md
  - README.zh-TW.md
  - internal/api/session.go
  - internal/api/upload.go
  - internal/ws/handler.go
  - internal/static/js/chat/chat-header.js
  - internal/static/js/ui/SessionTabs.js
  - cmd/desktop/desktop.rc
  - config.example.yaml
  - internal/agent/factory.go
  - docs/features.zh-TW.md
tests:
  - internal/mcp/kiro_readonly_test.go
  - internal/api/open_test.go
  - internal/db/busy_test.go
  - docs/spec/kiro-cli.md
  - cmd/desktop/icon_windows_amd64_test.go
  - internal/ws/kiro_readonly_test.go
  - internal/agent/factory_test.go
-->

---
### Requirement: Antigravity uses direct ACP instructions without an agent file

A non-empty instruction SHALL select agy acp within the existing antigravity runner. The runner SHALL verify agentCapabilities._meta.antigravity.systemPrompt.append before sending instructions and SHALL pass the saved string in params._meta.antigravity.systemPrompt.append on session/new and session/load. It SHALL send only user content in session/prompt. The runner SHALL preserve requested model, effort, permissions, and native customization behavior without creating a custom agent profile or weakening authorization. It SHALL fail explicitly for missing capabilities, unsupported requested settings, or instruction delivery errors instead of silently using the legacy CLI or user-message content. A configured print fallback SHALL NOT remove instructions.

#### Scenario: Create and load through ACP

- **WHEN** an instructed Antigravity session starts a new CLI process for a new or existing conversation
- **THEN** the session/new or session/load request SHALL contain the saved instruction in the Antigravity metadata append field
- **AND** a resumed request SHALL contain the existing sessionId
- **AND** the user prompt SHALL contain no instruction copy

#### Scenario: Missing instruction or load capability

- **WHEN** the server lacks the instruction append capability, or lacks load support for an existing conversation
- **THEN** the run SHALL report a specific error without executing a legacy CLI fallback or sending the user prompt

#### Scenario: Unsupported requested model or permission setting

- **WHEN** an instructed ACP run cannot honor the session's requested model, effort, or permission mode
- **THEN** the run SHALL report the unsupported setting without silently selecting a default or granting broader permission

#### Scenario: Permission request requires the existing decision path

- **WHEN** Antigravity requests tool permission
- **THEN** the runner SHALL use the caller's permission callback
- **AND** when no callback is available it SHALL cancel that permission request instead of approving it


<!-- @trace
source: session-custom-instructions
updated: 2026-11-13
code:
  - internal/static/js/chat/ForwardModal.js
  - cmd/desktop/main_windows.go
  - internal/static/js/chat/ChatView.js
  - internal/static/js/hooks/useNewSessionForm.js
  - internal/static/js/core/core.js
  - scripts/context-menu.test.cjs
  - internal/api/open.go
  - CHANGELOG.md
  - docs/features.md
  - internal/db/busy.go
  - internal/server/server.go
  - internal/mcp/tools.go
  - cmd/desktop/desktop_windows_amd64.syso
  - README.md
  - README.zh-TW.md
  - internal/api/session.go
  - internal/api/upload.go
  - internal/ws/handler.go
  - internal/static/js/chat/chat-header.js
  - internal/static/js/ui/SessionTabs.js
  - cmd/desktop/desktop.rc
  - config.example.yaml
  - internal/agent/factory.go
  - docs/features.zh-TW.md
tests:
  - internal/mcp/kiro_readonly_test.go
  - internal/api/open_test.go
  - internal/db/busy_test.go
  - docs/spec/kiro-cli.md
  - cmd/desktop/icon_windows_amd64_test.go
  - internal/ws/kiro_readonly_test.go
  - internal/agent/factory_test.go
-->

---
### Requirement: Antigravity ACP preserves lifecycle and event semantics

The instructed Antigravity path SHALL normalize streamed text, thinking/activity, tool start/completion, session identity, and errors into the existing runner event contract. It SHALL suppress session/load history replay from the current reply, finish from the session/prompt response, and emit a single success completion only after a successful turn and process cleanup. It SHALL expose authentication, JSON-RPC, EOF, timeout, and process failures. Cancellation SHALL release pending requests and terminate the child process tree with bounded waits. Unsupported blocking provider requests SHALL receive an explicit cancellation/error response rather than hanging.

#### Scenario: Resume replay does not duplicate history

- **WHEN** session/load emits previous assistant messages and session/prompt then emits a new answer
- **THEN** only the new answer SHALL be emitted as current reply text
- **AND** one successful EventDone SHALL identify the resumed conversation after completion and cleanup

#### Scenario: Stream tools and activity

- **WHEN** an instructed ACP run emits thinking/activity and tool start/completion notifications
- **THEN** clients SHALL receive their corresponding existing runner events with correlated tool identifiers

#### Scenario: Transport failure or unsupported blocking request

- **WHEN** an ACP run encounters a JSON-RPC error, unexpected EOF, timeout, missing authentication, or unsupported blocking request
- **THEN** pending waits SHALL terminate with an actionable run error
- **AND** the runner SHALL NOT emit a successful completion for the failed turn

#### Scenario: Owner cancels the run

- **WHEN** the run context is cancelled
- **THEN** pending RPC waits SHALL be released and the process tree SHALL terminate within the configured finite timeout policy
- **AND** the run SHALL NOT be reported as successful


<!-- @trace
source: session-custom-instructions
updated: 2026-11-13
code:
  - internal/static/js/chat/ForwardModal.js
  - cmd/desktop/main_windows.go
  - internal/static/js/chat/ChatView.js
  - internal/static/js/hooks/useNewSessionForm.js
  - internal/static/js/core/core.js
  - scripts/context-menu.test.cjs
  - internal/api/open.go
  - CHANGELOG.md
  - docs/features.md
  - internal/db/busy.go
  - internal/server/server.go
  - internal/mcp/tools.go
  - cmd/desktop/desktop_windows_amd64.syso
  - README.md
  - README.zh-TW.md
  - internal/api/session.go
  - internal/api/upload.go
  - internal/ws/handler.go
  - internal/static/js/chat/chat-header.js
  - internal/static/js/ui/SessionTabs.js
  - cmd/desktop/desktop.rc
  - config.example.yaml
  - internal/agent/factory.go
  - docs/features.zh-TW.md
tests:
  - internal/mcp/kiro_readonly_test.go
  - internal/api/open_test.go
  - internal/db/busy_test.go
  - docs/spec/kiro-cli.md
  - cmd/desktop/icon_windows_amd64_test.go
  - internal/ws/kiro_readonly_test.go
  - internal/agent/factory_test.go
-->

---
### Requirement: Copy and handoff preserve configuration without forwarding it as a message

Forward-to-new-session and manual handoff creation SHALL copy the source instruction into the editable creation setting when the destination runner is supported. An automatic same-runner handoff SHALL preserve the stored instruction on the new session. Forwarding into an existing session SHALL leave the destination's instruction unchanged and SHALL NOT include the source instruction in the forwarded message.

#### Scenario: Copy settings into a new session

- **WHEN** the owner selects a supported source in forward-to-new-session or opens a supported manual handoff
- **THEN** the creation draft SHALL contain the source instruction as configuration, not message body
- **AND** creating the destination SHALL persist the value selected in that draft

#### Scenario: Automatic handoff carries the role

- **WHEN** a supported instructed session creates an automatic handoff session
- **THEN** the new session SHALL store the same instruction before its first run

#### Scenario: Forward to an existing conversation

- **WHEN** a message is forwarded to an existing session with different instructions
- **THEN** the destination SHALL retain its own instructions and the forwarded body SHALL contain no injected source instruction


<!-- @trace
source: session-custom-instructions
updated: 2026-11-13
code:
  - internal/static/js/chat/ForwardModal.js
  - cmd/desktop/main_windows.go
  - internal/static/js/chat/ChatView.js
  - internal/static/js/hooks/useNewSessionForm.js
  - internal/static/js/core/core.js
  - scripts/context-menu.test.cjs
  - internal/api/open.go
  - CHANGELOG.md
  - docs/features.md
  - internal/db/busy.go
  - internal/server/server.go
  - internal/mcp/tools.go
  - cmd/desktop/desktop_windows_amd64.syso
  - README.md
  - README.zh-TW.md
  - internal/api/session.go
  - internal/api/upload.go
  - internal/ws/handler.go
  - internal/static/js/chat/chat-header.js
  - internal/static/js/ui/SessionTabs.js
  - cmd/desktop/desktop.rc
  - config.example.yaml
  - internal/agent/factory.go
  - docs/features.zh-TW.md
tests:
  - internal/mcp/kiro_readonly_test.go
  - internal/api/open_test.go
  - internal/db/busy_test.go
  - docs/spec/kiro-cli.md
  - cmd/desktop/icon_windows_amd64_test.go
  - internal/ws/kiro_readonly_test.go
  - internal/agent/factory_test.go
-->

---
### Requirement: Configuration text does not leak through transport logging or share metadata

The system SHALL NOT log instruction bodies through generated argv, configuration overrides, or ACP payload logging. Validation errors SHALL identify the field or limit without echoing its content. Share session metadata and message history SHALL NOT gain the stored instruction as an additional field or synthetic message.

#### Scenario: Inspect generated transport logs

- **WHEN** an instructed Claude, Codex, or Antigravity session runs
- **THEN** explicit transport logs SHALL use redacted values or length metadata instead of the saved instruction text

#### Scenario: Inspect visitor session data

- **WHEN** a visitor receives share session metadata or message history
- **THEN** the new stored instruction SHALL NOT be serialized as configuration or inserted as a synthetic user message

<!-- @trace
source: session-custom-instructions
updated: 2026-11-13
code:
  - internal/static/js/chat/ForwardModal.js
  - cmd/desktop/main_windows.go
  - internal/static/js/chat/ChatView.js
  - internal/static/js/hooks/useNewSessionForm.js
  - internal/static/js/core/core.js
  - scripts/context-menu.test.cjs
  - internal/api/open.go
  - CHANGELOG.md
  - docs/features.md
  - internal/db/busy.go
  - internal/server/server.go
  - internal/mcp/tools.go
  - cmd/desktop/desktop_windows_amd64.syso
  - README.md
  - README.zh-TW.md
  - internal/api/session.go
  - internal/api/upload.go
  - internal/ws/handler.go
  - internal/static/js/chat/chat-header.js
  - internal/static/js/ui/SessionTabs.js
  - cmd/desktop/desktop.rc
  - config.example.yaml
  - internal/agent/factory.go
  - docs/features.zh-TW.md
tests:
  - internal/mcp/kiro_readonly_test.go
  - internal/api/open_test.go
  - internal/db/busy_test.go
  - docs/spec/kiro-cli.md
  - cmd/desktop/icon_windows_amd64_test.go
  - internal/ws/kiro_readonly_test.go
  - internal/agent/factory_test.go
-->