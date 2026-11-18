## ADDED Requirements

### Requirement: Global instruction is configured in Settings

The system SHALL offer a multiline 全域指令 field and an injection mode under Settings → General, persisted through `GET /settings/global-instruction` and `PUT /settings/global-instruction`. The modes SHALL be 會話第一則訊息 (`first`), 每則訊息 (`every`), and 僅原生支援 (`native`); the default mode SHALL be `native` with empty text. A saved text SHALL be trimmed, SHALL NOT exceed 16384 UTF-8 bytes, and SHALL NOT contain U+0000. Validation errors SHALL NOT echo the submitted text.

#### Scenario: Save and read the setting

- **WHEN** the owner saves text `  全域\n第二行  ` with mode `native`
- **THEN** a later read SHALL return the text trimmed and the mode `native`
- **AND** the response SHALL NOT be cached

#### Scenario: Reject invalid payloads

- **WHEN** a request body is `null`, omits text or mode, uses an unknown mode, or contains U+0000 in the text
- **THEN** the system SHALL reject it with HTTP 400
- **AND** the error message SHALL NOT contain the submitted text

#### Scenario: Edit the mode with a keyboard

- **WHEN** the owner focuses the selected mode radio and presses ArrowRight
- **THEN** the next mode SHALL become selected
- **AND** saving SHALL send the text and the selected mode in one request

### Requirement: Sessions snapshot the global instruction at creation

When an agent-mode session is created through New Session, forward-to-new-session, automatic handoff, MCP `create_session`, or room worker creation, the system SHALL store the global text and mode current at that moment on the session. Later changes to the setting SHALL NOT affect existing sessions. Telegram sessions and shell-mode sessions SHALL NOT store a global instruction. A session whose stored text is empty SHALL send messages unchanged.

#### Scenario: Setting changes after creation

- **WHEN** a session is created while the mode is `every` and the setting is then changed to empty text with mode `native`
- **THEN** the existing session SHALL keep using its stored text and mode

#### Scenario: Excluded session kinds

- **WHEN** a Telegram session or a shell-mode session is created
- **THEN** it SHALL store an empty global instruction

#### Scenario: Creation response does not leak the global text

- **WHEN** a session is created through the HTTP API while a global instruction exists
- **THEN** the response body SHALL NOT contain the global text

### Requirement: Native delivery merges global and session instructions

When the stored mode is `native` and the session's agent is Claude, Codex, or Antigravity, the native instruction channel SHALL receive the global text, then a blank line, then the session instruction; when the session instruction is empty it SHALL receive only the global text. The session instruction SHALL always use the native channel. The merged value SHALL NOT exceed 16384 UTF-8 bytes, otherwise creation SHALL be rejected. When the mode is `native` and the agent has no native instruction channel, messages SHALL be sent unchanged.

#### Scenario: Merge with a session instruction

- **GIVEN** global text `G` with mode `native` and session instruction `P` on a Claude session
- **WHEN** the owner sends a message
- **THEN** the native instruction SHALL be `G`, a blank line, then `P`
- **AND** the message sent to the model SHALL be unchanged

#### Scenario: Merge exceeds the limit

- **WHEN** the global text and session instruction merge to more than 16384 UTF-8 bytes in native mode
- **THEN** session creation SHALL fail with a message naming the combined limit

#### Scenario: Agent without native channel

- **WHEN** the mode is `native` and the agent is not Claude, Codex, or Antigravity
- **THEN** every message SHALL be sent to the agent unchanged

### Requirement: Chat injection prefixes the user message with the global instruction

When the stored mode is `first` or `every` and the turn qualifies, the model input SHALL be the line 全域指令, the global text, a blank line, the line 使用者訊息, then the original message, with all original line breaks preserved. The session instruction SHALL remain on the native channel. Stored chat history, queued messages, and forwarded content SHALL keep the original message. Mode `every` SHALL qualify every turn; mode `first` SHALL qualify turns until delivery has been confirmed.

#### Scenario: Every message with a session instruction

- **GIVEN** global text `G`, mode `every`, and session instruction `P` on a Claude session
- **WHEN** the owner sends `hello` and then `next`
- **THEN** the model inputs SHALL be the injected form of `hello` and the injected form of `next`
- **AND** the native instruction SHALL be `P` only
- **AND** the stored user history SHALL be `hello` and `next`

#### Scenario: First message only

- **GIVEN** mode `first`
- **WHEN** the owner sends `hello` and then `next` and `hello` reached the model
- **THEN** the model input for `hello` SHALL be the injected form and the model input for `next` SHALL be `next`

##### Example: delivery combinations

| Mode | Agent | Session instruction | Native instruction | First model input | Second model input |
|------|-------|---------------------|--------------------|-------------------|--------------------|
| native | Claude | `P` | `G` blank line `P` | `hello` | `next` |
| every | Claude | `P` | `P` | injected `hello` | injected `next` |
| first | no native channel | empty | empty | injected `hello` | `next` |
| native | no native channel | empty | empty | `hello` | `next` |

### Requirement: First-message delivery is confirmed by the model

In mode `first`, the system SHALL record delivery on the original user message only after the agent emits a model response event (text, thinking, tool start, permission denial, or completion). Starting the process or failing before any model event SHALL NOT count as delivery, so the next attempt SHALL inject again. Resetting the context SHALL clear the history so that the next message injects again.

#### Scenario: Failure before any model event

- **WHEN** the first message fails before the model responds and the owner resumes the paused queue
- **THEN** the retried message SHALL contain the injected global text

#### Scenario: Reset context re-arms first-message injection

- **WHEN** the owner resets the context after the first message was delivered and then sends a new message
- **THEN** the new message SHALL contain the injected global text

### Requirement: Native instruction conflicts with CLI system-prompt flags are rejected

When a Claude session has a non-empty native instruction (global, session, or merged), the system SHALL reject creation and CLI extra-argument updates that contain `--system-prompt`, `--system-prompt-file`, `--append-system-prompt`, or `--append-system-prompt-file`, including the equals-value form.

#### Scenario: Conflict at creation

- **WHEN** a Claude creation request carries `--append-system-prompt=legacy` while the global text is non-empty in native mode
- **THEN** the request SHALL be rejected with HTTP 400

#### Scenario: Conflict when patching CLI arguments

- **WHEN** the owner patches `cli_extra_args` of such a session to include a system-prompt flag
- **THEN** the request SHALL be rejected with HTTP 400 and the stored arguments SHALL remain unchanged
