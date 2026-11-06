## ADDED Requirements

### Requirement: Keyboard navigation and focus within a tab group

A desktop SessionTabBar SHALL expose exactly one session-tab entry in the Tab sequence: its active tab. ArrowLeft and ArrowRight SHALL activate and focus the previous or next open tab in that group with wraparound; Home and End SHALL activate and focus the first or last open tab. Navigation SHALL keep the target tab visible and SHALL NOT delay switching for animation. Enter and Space SHALL retain native tab activation behavior. Tab order SHALL expose the active tab's close action, while inactive close actions remain usable by pointer without adding Tab stops.

#### Scenario: Navigate a tab group with wraparound

- **WHEN** the user presses ArrowRight on the last of three open tabs
- **THEN** the first tab becomes active, receives visible keyboard focus and is brought into view
- **AND** no duplicate session tab is created

##### Example: Navigation target selection

| Open tabs | Active tab | Key | New active and focused tab |
| --- | --- | --- | --- |
| A, B, C | C | ArrowRight | A |
| A, B, C | A | ArrowLeft | C |
| A, B, C | B | Home | A |
| A, B, C | B | End | C |
| A | A | ArrowRight | A |

#### Scenario: Leave a tab group using Tab

- **WHEN** the user presses Tab from the active session tab
- **THEN** keyboard focus can reach its visibly revealed close control and continue to the next interface control without visiting every inactive tab or its close action

### Requirement: Closing a tab preserves a valid keyboard destination

After a keyboard user closes the active tab, focus SHALL move to the tab selected by the existing close-selection rules in that group. If closing removes the group or its final tab, focus SHALL move to the surviving active group or the session-navigation entry when no group remains. Inactive close controls SHALL become visible on group focus as well as hover. Existing middle-click, drag, persistence, mount-count and narrow-screen behavior SHALL remain intact.

#### Scenario: Close an active tab with siblings

- **WHEN** the user activates the active tab's close control and sibling tabs remain
- **THEN** the newly active sibling receives visible focus and remains the sole session-tab entry in that group's Tab sequence

#### Scenario: Close the final open tab

- **WHEN** the user closes the final tab with the keyboard
- **THEN** focus reaches session navigation instead of being left on a removed element
