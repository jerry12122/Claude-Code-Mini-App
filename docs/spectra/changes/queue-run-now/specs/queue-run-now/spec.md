## Purpose

The queue-run-now capability lets a user take a message already waiting in a busy session's queue and run it immediately by interrupting the current agent turn. It exists because the only other way to intervene mid-turn is the stop action, which pauses the whole queue and discards the user's planned follow-ups.

## ADDED Requirements

### Requirement: Run a queued message immediately
The system SHALL accept a `queue_run_now` WebSocket message carrying the id of a queued message. When the session has an active agent task of a supported agent type, the system SHALL terminate that task, wait until the terminated task has fully finished its wrap-up, and then start the identified queued message as a new turn that resumes the same agent session.

#### Scenario: Run a queued message while a turn is running
- **WHEN** a session of agent type `claude` is running a turn and has queued messages B and C, and the client sends `queue_run_now` for B
- **THEN** the running turn SHALL be terminated, message B SHALL be removed from the queue and recorded as a user message, a new turn for B SHALL start resuming the same agent session, and message C SHALL remain queued

#### Scenario: Run a queued message that is not the first
- **WHEN** a session is running a turn and has queued messages B and C, and the client sends `queue_run_now` for C
- **THEN** message C SHALL run next, and message B SHALL remain queued in its original position

#### Scenario: Client retries after the message was already taken
- **WHEN** the client sends `queue_run_now` for a queued message id that no longer exists in the session's queue
- **THEN** the system SHALL reject the request with an error message and SHALL NOT interrupt the running turn

### Requirement: Old turn finishes before the new turn starts
The system MUST NOT start the new turn until the terminated task's process has exited and its wrap-up (finalizing its chat message and writing the session status back) has completed. The recorded conversation order SHALL be the earlier user message, the interrupted assistant message with its partial content preserved, then the run-now user message.

#### Scenario: Ordering of recorded messages
- **WHEN** user message A is being answered and a run-now request for queued message B interrupts the turn after partial output was produced
- **THEN** the session history SHALL contain user message A, then the assistant message holding the partial output, then user message B, and the old turn's run SHALL have returned before the new turn's run begins

#### Scenario: Old turn does not finish in time
- **WHEN** the terminated task has not completed its wrap-up within 15 seconds
- **THEN** the system SHALL NOT start the new turn, SHALL report an error to the client, and the identified message SHALL remain in the queue

### Requirement: Run-now does not pause the queue
Unlike the stop action, a run-now interruption SHALL NOT pause the session's queue and SHALL NOT send a task-cancelled notification. After the new turn succeeds, the remaining queued messages SHALL continue to run in order.

#### Scenario: Remaining queue continues after run-now
- **WHEN** a run-now turn for message B completes successfully and message C is still queued
- **THEN** the system SHALL start message C automatically, and the queue SHALL NOT have been marked paused at any point during the run-now sequence

#### Scenario: Paused flag is untouched
- **WHEN** the queue is already paused and a run-now request starts one message while the session is idle
- **THEN** that single message SHALL run, and the queue's paused flag SHALL remain paused

### Requirement: Interrupted turn is disclosed to the agent
The prompt of the run-now turn SHALL begin with a fixed line stating that the agent's previous reply was interrupted by the user and that the following text is the new instruction. The user message shown in the chat history SHALL contain only the user's original text without that line.

#### Scenario: Prompt carries the interruption notice
- **WHEN** a run-now turn starts after an interrupted turn
- **THEN** the prompt passed to the runner SHALL start with the interruption notice followed by the queued message content, and the stored user message content SHALL equal the original queued content

### Requirement: Unsupported situations are rejected without side effects
The system SHALL reject `queue_run_now` with an error message, leaving the running task and the queue unchanged, when any of the following holds: the session's agent type is not `claude`, `codex`, or `cursor`; the session has a pending permission request or a pending user question; a direct shell task is running or a shell command awaits confirmation; the server is in maintenance mode.

#### Scenario: Unsupported agent type
- **WHEN** the client sends `queue_run_now` in a session whose agent type is `kiroacp`
- **THEN** the system SHALL reply with an error message and SHALL NOT terminate the running task

#### Scenario: Awaiting permission
- **WHEN** the client sends `queue_run_now` while the session is awaiting a permission decision
- **THEN** the system SHALL reply with an error message and the pending permission request SHALL remain unchanged

#### Scenario: Shell task running
- **WHEN** the client sends `queue_run_now` while a direct shell task is running in the session
- **THEN** the system SHALL reply with an error message and the shell task SHALL keep running

### Requirement: Run-now when no task is active
If the session has no active agent task when `queue_run_now` is processed, the system SHALL start the identified queued message directly without terminating anything.

#### Scenario: Turn finished just before the request
- **WHEN** the previous turn completed before the server processed `queue_run_now` and the identified message is still queued
- **THEN** the system SHALL run that message exactly once as a normal turn

#### Scenario: Turn is lingering after its result
- **WHEN** a turn has already delivered its final result but its process is still alive, and the client sends `queue_run_now`
- **THEN** the system SHALL terminate the lingering process and start the identified message as a new turn

### Requirement: Queue view offers an explicit run-now action
The chat UI SHALL show a "run now" action on each queued message while the session has an active task of a supported agent type, separate from the send button. The send action SHALL keep its existing behavior of queueing the message when the session is busy.

#### Scenario: Run-now button is a separate control
- **WHEN** a session of agent type `claude` is running and has a queued message
- **THEN** the queued message row SHALL show a "run now" control distinct from the remove control and the send button

#### Scenario: Send still queues
- **WHEN** the user submits a new message while the session is running
- **THEN** the message SHALL be added to the queue and the running turn SHALL NOT be interrupted
