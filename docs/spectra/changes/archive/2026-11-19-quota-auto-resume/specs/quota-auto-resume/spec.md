## Purpose

The quota-auto-resume capability lets the server automatically continue a session that stopped because its 5-hour usage window was exhausted, once the window has reset. It is controlled by a single global switch that is off by default, and its lifecycle is stored as an explicit persisted state machine so that it survives restarts and never triggers twice.

## ADDED Requirements

### Requirement: Global auto-resume switch
The system SHALL store a boolean general setting `autoResumeOnQuota` on the server, defaulting to `false` when no value has been saved. The settings page "General" section SHALL expose this switch and persist changes immediately. The server SHALL read the setting each time it evaluates a failed run and each time the resume loop ticks, so a change takes effect without a restart.

#### Scenario: Default is off
- **WHEN** the general settings have never been saved
- **THEN** `autoResumeOnQuota` SHALL read as `false`

#### Scenario: Switch change applies without restart
- **WHEN** the owner turns the switch on in the settings page and a run later fails because the 5-hour window is exhausted
- **THEN** the system SHALL arm auto-resume for that session without restarting the server

### Requirement: Persisted auto-resume state machine
The system SHALL persist, for every session, an auto-resume state with exactly one of the values `none`, `waiting`, `resuming`, `exhausted`, defaulting to `none`, together with the planned resume time and the consecutive automatic resume count. Every state change SHALL be a single conditional database update that names the state it expects to leave, so that two concurrent actors can never both succeed in the same transition. The only allowed transitions SHALL be those in the table below, and an attempted transition from any other state SHALL leave the stored state unchanged.

##### Example: allowed transitions
| From | To | Trigger |
|------|----|---------|
| `none` | `waiting` | Failure detected as quota exhaustion while the switch is on and the count is below 3 |
| `none` | `exhausted` | Failure detected as quota exhaustion while the switch is on and the count is already 3 |
| `waiting` | `waiting` | Resume time passed but the `session` window is still full; the resume time moves to the new reset time |
| `waiting` | `resuming` | Resume time passed, the `session` window is below 100%, and this tick won the conditional update; the count increases by 1 |
| `resuming` | `waiting` | The automatic run failed by quota exhaustion again and the count is below 3; or the server restarted while resuming (the resume time becomes the restart time) |
| `resuming` | `waiting` | The resume message could not be delivered; the count returns to its previous value |
| `resuming` | `exhausted` | The automatic run failed by quota exhaustion again and the count is 3 |
| `resuming` | `none` | The automatic run succeeded, failed for another reason, or was cancelled; the count becomes 0 |
| `waiting`, `resuming`, `exhausted` | `none` | Any cancel trigger listed in the cancelling requirement; the count becomes 0 |

#### Scenario: Concurrent ticks start a session once
- **WHEN** two resume loop ticks both find the same session in `waiting` with an elapsed resume time and quota recovered
- **THEN** exactly one tick SHALL change the state to `resuming` and send `quota_resume`, and the other SHALL send nothing

#### Scenario: Disallowed transition is ignored
- **WHEN** a transition from `none` to `resuming` is requested
- **THEN** the stored state SHALL remain `none`

#### Scenario: Restart while resuming
- **WHEN** the server restarts while a session is in `resuming` and its run was interrupted
- **THEN** on startup the session SHALL be set to `waiting` with a resume time equal to the startup time, keeping its count, so the next loop tick re-checks quota and continues without user action

#### Scenario: State survives restart
- **WHEN** the server restarts while a session is in `waiting` with a future resume time
- **THEN** the session SHALL remain in `waiting` with the same resume time and SHALL be resumed after that time

### Requirement: Detect quota exhaustion from the quota snapshot
When the switch is on and a run fails, is not cancelled by the user, and is not waiting for a permission decision, the system SHALL refresh the quota snapshot of that session provider and SHALL treat the failure as quota exhaustion only if a window with key `session` has `percent` greater than or equal to 100 and a `resets_at` value that parses as a time. The system SHALL NOT decide quota exhaustion by matching the error text. When the snapshot is missing, stale, has no `session` window, or lacks a parsable `resets_at`, the system SHALL NOT arm auto-resume and SHALL keep the existing behavior.

#### Scenario: Session window is full
- **WHEN** a Claude run fails and the refreshed snapshot has a `session` window at 100% with `resets_at` 2026-11-18T10:00:00Z
- **THEN** the system SHALL arm auto-resume for that session

##### Example: detection boundaries
| Switch | Failure kind | `session` window | Armed |
|--------|--------------|------------------|-------|
| on | run error | 100% with parsable `resets_at` | yes |
| on | run error | 99% with parsable `resets_at` | no |
| on | run error | 100% without `resets_at` | no |
| on | run error | only `weekly` at 100% | no |
| on | snapshot stale or missing | not usable | no |
| on | user cancelled | 100% with parsable `resets_at` | no |
| on | waiting for permission | 100% with parsable `resets_at` | no |
| off | run error | 100% with parsable `resets_at` | no |

### Requirement: Arming preserves the interrupted message
When auto-resume is armed, the system SHALL move the session to `waiting`, record the resume time as the window `resets_at` plus 60 seconds, SHALL put the interrupted user message (its text and attachment references) at the front of the session queue ahead of any already queued messages, and SHALL pause the queue even when no other message was queued. The system SHALL broadcast the state and resume time to the connected clients of the session. This applies equally to a run that was itself started by auto-resume: its message left the queue when the run started, so it SHALL be placed at the front again.

#### Scenario: Failed message with no queue
- **WHEN** a session with an empty queue fails on message "refactor X" because the 5-hour window is exhausted and auto-resume is armed
- **THEN** the queue SHALL contain "refactor X" as its first entry and SHALL be paused

#### Scenario: Failed message with a queue
- **WHEN** a session with queued messages "B" and "C" fails on message "A" because the 5-hour window is exhausted and auto-resume is armed
- **THEN** the queue order SHALL be "A", "B", "C" and the queue SHALL be paused

### Requirement: Resume loop re-checks quota before continuing
The server SHALL run a loop that checks every 30 seconds for sessions in `waiting` whose resume time has passed. For each such session it SHALL refresh the quota snapshot first. When the `session` window is below 100%, the system SHALL win the conditional transition `waiting` to `resuming` and then continue the session with the same behavior as the user pressing the queue "Continue" action, bypassing the quota guard. When the window is still at 100% or higher, the system SHALL move the resume time to the new `resets_at` plus 60 seconds, keep the state `waiting`, and SHALL NOT start the session. When the snapshot cannot be refreshed, the system SHALL leave the session in `waiting` and retry on a later tick. While the switch is off, the loop SHALL NOT start any session.

#### Scenario: Due and quota recovered
- **WHEN** the resume time of a `waiting` session has passed and the refreshed `session` window is at 40%
- **THEN** the system SHALL change the state to `resuming`, resume the queue, and run the front message

#### Scenario: Due but quota still full
- **WHEN** the resume time has passed and the refreshed `session` window is still at 100% with a later `resets_at`
- **THEN** the system SHALL NOT run any message, SHALL keep the state `waiting`, and SHALL set the resume time to the new `resets_at` plus 60 seconds

#### Scenario: Not yet due
- **WHEN** the resume time is still in the future
- **THEN** the loop SHALL NOT refresh quota for that session and SHALL NOT start it

#### Scenario: Session already resuming is skipped
- **WHEN** a session is in `resuming`
- **THEN** the loop SHALL NOT send `quota_resume` for it again

### Requirement: Bounded consecutive retries
The system SHALL count automatic resumes per session, increasing the count when the state moves from `waiting` to `resuming`, and SHALL reset the count to zero whenever the state returns to `none`. When a run started by auto-resume fails by quota exhaustion again and the count is already 3, the system SHALL move the session to `exhausted`, SHALL NOT arm again, and SHALL leave the queue paused for manual continuation with the failure shown to the user.

#### Scenario: Fourth consecutive exhaustion
- **WHEN** a session has been auto-resumed 3 times without success in between and the next run fails by quota exhaustion
- **THEN** the session SHALL be in `exhausted` and the queue SHALL stay paused

#### Scenario: Success resets the count
- **WHEN** an auto-resumed run succeeds
- **THEN** the session SHALL be in `none` and its count SHALL be zero

### Requirement: Cancelling a pending auto-resume
The system SHALL move a session from `waiting`, `resuming`, or `exhausted` to `none` when any of the following happens: the user resumes the queue manually, the queue is cleared or the session is deleted, the user sends a new message directly, the user presses the cancel action on the waiting banner, or the switch is turned off. The cancellation transition itself SHALL preserve the queue contents and pause flag. Banner cancellation and turning the switch off SHALL leave a paused queue paused with the interrupted message intact. A manual continue SHALL still resume the queue, direct input SHALL retain its existing dispatch behavior, clearing or deleting queue entries SHALL remove the requested entries, and deleting a session SHALL still delete that session and its queue. These explicit actions SHALL NOT retain entries that the owner requested to delete. Turning the switch off SHALL move every session to `none`.

#### Scenario: Cancel from the banner
- **WHEN** the user presses the cancel action on the banner of a `waiting` session
- **THEN** the state SHALL become `none` and the queue SHALL remain paused with its messages intact

#### Scenario: Switch turned off
- **WHEN** one session is `waiting`, another is `exhausted`, and the owner turns the switch off
- **THEN** both sessions SHALL become `none` and neither SHALL be started by the loop

#### Scenario: Manual continue also cancels the schedule
- **WHEN** the owner manually continues a paused `waiting` session with interrupted message "A" at the front
- **THEN** its automatic resume state SHALL become `none` and the existing manual continue operation SHALL run "A"

#### Scenario: Removing queued work cancels its schedule
- **WHEN** the owner deletes a queued entry or clears the queue of a `waiting` session
- **THEN** its automatic resume state SHALL become `none`, the requested entries SHALL be deleted, and the cancellation transition itself SHALL NOT resume the queue

### Requirement: Waiting banner
While a session is in `waiting`, the chat view SHALL show a banner stating that quota is exhausted and the local time of the planned automatic continuation, with a cancel action. While a session is in `exhausted`, the chat view SHALL show a notice that automatic continuation reached its limit and the queue needs a manual continue. The banner and notice SHALL also appear for a client that connects or reconnects, and SHALL disappear when the state returns to `none`.

#### Scenario: Reconnect while waiting
- **WHEN** a client opens a `waiting` session after the failure happened
- **THEN** the banner with the planned time SHALL be shown

#### Scenario: Exhausted notice
- **WHEN** a client opens an `exhausted` session
- **THEN** the notice about the limit SHALL be shown and the cancel action SHALL NOT be offered

### Requirement: Scope limits
Auto-resume SHALL apply only to exhaustion of the `session` window. The weekly window and the quota guard percentage pause SHALL NOT be armed for auto-resume. Auto-resume SHALL apply only to runs started from a user message, so runs started by the system itself (such as the internal retry prompt after a permission decision) and Telegram-type sessions SHALL NOT be armed. Automatic resume SHALL NOT send Telegram notifications.

#### Scenario: Weekly window exhausted
- **WHEN** a run fails and only the `weekly` window is at 100%
- **THEN** the queue SHALL follow the existing manual-resume behavior and the state SHALL remain `none`

#### Scenario: Quota guard pause
- **WHEN** the quota guard pauses a queue at the configured percentage before a message starts
- **THEN** the state SHALL remain `none` and the user SHALL resume manually

#### Scenario: Run not started from a user message
- **WHEN** a run started by the internal permission retry prompt fails because the 5-hour window is exhausted
- **THEN** the state SHALL remain `none` and the queue SHALL follow the existing behavior

