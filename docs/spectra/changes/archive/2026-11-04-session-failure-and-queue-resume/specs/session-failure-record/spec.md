## Purpose

The session-failure-record capability persists the most recent agent run failure of each session, with its raw error text and time, and exposes it through the MCP `list_sessions` tool. It exists so that external agents can learn why a session stopped without depending on a live websocket connection or in-memory state.

## ADDED Requirements

### Requirement: Persist the latest run failure
When an agent run fails and was not cancelled by the user, the system SHALL store the raw error text and the failure time on the session record, replacing any previous failure. The stored error text SHALL be truncated to 4000 characters.

#### Scenario: Run fails with a usage limit error
- **WHEN** a codex run for a session ends with a usage limit error that says to try again at 4:42 PM
- **THEN** the session record SHALL hold that error text and the failure time

#### Scenario: A newer failure replaces the older one
- **WHEN** a session that already has a stored failure fails again
- **THEN** the stored error text and time SHALL be replaced by the newer failure

#### Scenario: Cancelled run
- **WHEN** a run ends because the user cancelled it
- **THEN** the system SHALL NOT store a failure and SHALL NOT clear an existing one

#### Scenario: Oversized error text
- **WHEN** a failed run reports an error text longer than 4000 characters
- **THEN** the stored error text SHALL be truncated to 4000 characters

### Requirement: Clear the failure after a successful run
When a run for a session completes successfully, the system SHALL clear the stored failure for that session.

#### Scenario: Session recovers
- **WHEN** a session with a stored failure completes a run successfully
- **THEN** its stored error text and time SHALL be empty

### Requirement: Failure state survives restart
The stored failure SHALL be persisted in the database and SHALL NOT depend on any websocket connection or in-memory cache.

#### Scenario: Server restart
- **WHEN** the server restarts after a session failed
- **THEN** `list_sessions` SHALL still report that session's failure

### Requirement: Report failure and queue state in list_sessions
The MCP `list_sessions` tool SHALL include for every session `queue_paused` and `queue_length`, and SHALL include `last_error` and `last_error_at` when a failure is stored.

#### Scenario: Session with a stored failure and a paused queue
- **WHEN** a session has a stored failure, two queued messages and a paused queue
- **THEN** its `list_sessions` entry SHALL contain the error text, the failure time, `queue_paused` true and `queue_length` 2

##### Example: Codex quota stall
- **GIVEN** session "cloudserver" failed at 2026-11-02T08:17:43Z with a usage limit error and has queue items 244 and 248 paused
- **WHEN** an agent calls `list_sessions`
- **THEN** the entry has `last_error_at` "2026-11-02T08:17:43Z", the error text, `queue_paused` true and `queue_length` 2

#### Scenario: Session without a failure
- **WHEN** a session has no stored failure
- **THEN** its entry SHALL omit `last_error` and `last_error_at` and SHALL still report `queue_paused` and `queue_length`

### Requirement: Filter sessions by failure time
The `list_sessions` tool SHALL accept an optional `failed_after` ISO8601 timestamp and, when given, SHALL return only sessions whose `last_error_at` is later than it.

#### Scenario: Only newer failures
- **WHEN** an agent calls `list_sessions` with `failed_after` set to a time between two sessions' failure times
- **THEN** only the session that failed after that time SHALL be returned

#### Scenario: Sessions without failures are excluded
- **WHEN** an agent calls `list_sessions` with `failed_after`
- **THEN** sessions with no stored failure SHALL NOT be returned

#### Scenario: Combined with since
- **WHEN** an agent supplies both `since` and `failed_after`
- **THEN** a session SHALL be returned only if it satisfies both filters

#### Scenario: Invalid timestamp
- **WHEN** an agent supplies a `failed_after` that is not a valid timestamp
- **THEN** the tool SHALL return an error describing the invalid time
