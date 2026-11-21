## ADDED Requirements

### Requirement: Consistent status indicators across tabs and collapsed sidebar

For the same session state, its tab and collapsed-sidebar icon SHALL use the same running or attention status meaning, label and animation policy. Running SHALL display the existing pulsing status dot on both surfaces when motion is allowed. Awaiting confirmation, input or shell confirmation SHALL display the existing attention indicator instead of the running pulse. Idle and unknown states SHALL NOT display a running or attention indicator. Updates SHALL derive from the existing shared session state without new polling or independent status timers. Active and background sessions SHALL follow the same rule.

#### Scenario: Running starts and finishes

- **WHEN** session A changes from idle to running and later returns to idle
- **THEN** its tab and collapsed-sidebar icon SHALL both show the running indicator during running and remove it after idle is received
- **AND** session B SHALL retain its own independent status

##### Example: Running session with an unread result

- **GIVEN** A is running with unread_count 3 and B is idle
- **WHEN** the collapsed sidebar and tab bar render their shared session data
- **THEN** A SHALL have running indicators on both surfaces and retain its numeric unread badge 3
- **AND** B SHALL have no running indicator

#### Scenario: Attention replaces running

- **WHEN** session A transitions from running to awaiting_confirm, awaiting_input or awaiting_shell_confirm
- **THEN** both surfaces SHALL replace the running indicator with the corresponding existing attention label and indicator
- **AND** returning to running SHALL restore the running indicator on both surfaces

### Requirement: Status parity preserves accessibility and navigation

The collapsed-sidebar status SHALL remain distinguishable alongside the selected-session outline, agent icon and unread badge without covering the unread count or blocking session controls. Its accessible description SHALL identify the session status. With prefers-reduced-motion set to reduce, running indicators on both surfaces SHALL remain visible and static. Clicking, keyboard activation, context menus and drag behavior SHALL retain their existing semantics; a status update SHALL NOT activate or reorder a session or change unread state.

#### Scenario: Reduced motion retains status

- **WHEN** reduced motion is enabled while session A is running
- **THEN** both surfaces SHALL show a static running indicator with a readable status description and no status animation

#### Scenario: Status coexists with navigation

- **WHEN** a running selected session also has unread messages and the owner activates its sidebar icon with keyboard or pointer
- **THEN** selection and unread indicators SHALL remain distinguishable and the existing navigation action SHALL execute
- **AND** rendering its status SHALL NOT acknowledge unread messages or switch any other session

### Requirement: Compact pinned session icon

In the expanded sidebar, a pinned session row SHALL show the existing pin glyph instead of the visible pinned text label. The marker SHALL expose the label 已釘選 through a hover tooltip and an accessible name or description. Unpinned rows SHALL NOT show this marker. The marker SHALL remain informational: existing pin and unpin menu actions, group placement, session activation, selection, unread badges and running indicators SHALL retain their behavior.

#### Scenario: Pinned icon replaces text without changing controls

- **WHEN** the owner views a pinned session in the expanded sidebar
- **THEN** the row SHALL show the pin glyph without the visible 釘選 text label
- **AND** hover and assistive technology SHALL identify it as 已釘選
- **AND** using the existing unpin menu action SHALL remove the marker and update grouping normally

#### Scenario: Other indicators remain visible

- **WHEN** a pinned session is selected, running and has three unread messages
- **THEN** the pin marker, selection, running indicator and unread badge 3 SHALL remain distinguishable
- **AND** clicking the informational pin marker SHALL retain the existing row activation behavior without toggling pin state
