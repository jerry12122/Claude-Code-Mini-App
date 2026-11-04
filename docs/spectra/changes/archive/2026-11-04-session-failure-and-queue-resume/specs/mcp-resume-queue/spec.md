## Purpose

The mcp-resume-queue capability lets external agents release a session's paused message queue through MCP, matching the resume action available in the UI. It exists because a failed run pauses the queue and, while paused, messages sent through MCP are only appended behind it and never run.

## ADDED Requirements

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

### Requirement: Resume does not alter turn state
Calling `resume_queue` SHALL NOT reset the session's cached reply text or error, and SHALL NOT mark the session as running unless a queued message actually starts.

#### Scenario: Resume with an empty queue
- **WHEN** an agent calls `resume_queue` for an idle session with an empty queue
- **THEN** a following `get_status` SHALL still report the session as idle

### Requirement: Respect maintenance gating
While the server is in maintenance and refusing new work, `resume_queue` SHALL NOT take any queued message out of the queue.

#### Scenario: Resume during maintenance
- **WHEN** an agent calls `resume_queue` while the maintenance gate rejects new work
- **THEN** the queued messages SHALL remain queued
