## MODIFIED Requirements

### Requirement: Composer action row hosts model, effort, and permission controls

For a session owner in agent input mode, the chat composer SHALL expose the permission-mode, model, and effort selectors supported by that session's agent type. Desktop layouts SHALL render these controls in the action row immediately after the attachment button. Compact mobile layouts SHALL expose them inside the tools sheet reached by the composer's Plus button, with the current selection visible for each supported control. The sheet SHALL also retain the owner's allowed input-mode switch. Agent-type availability, busy-state disabling, dangerous permission emphasis, and next-send application SHALL retain their existing behavior.

#### Scenario: Controls visible in agent mode

- **WHEN** the session owner views a Claude session with the composer in agent input mode
- **THEN** desktop shows the permission-mode, model, and effort selectors after the attachment button and compact mobile exposes them through Plus in the tools sheet

#### Scenario: Controls hidden in shell mode

- **WHEN** the composer is switched to shell input mode
- **THEN** none of the three selectors is rendered in either the desktop action row or the mobile sheet and the owner's allowed mode switch remains reachable

#### Scenario: Controls hidden for guests

- **WHEN** a guest opens a shared chat
- **THEN** none of the three selectors or the owner-only mode switch is rendered and a writable guest retains the existing attachment action through the mobile sheet

### Requirement: Chat header no longer duplicates the controls

The chat header SHALL NOT render model, effort, or permission-mode selectors, nor a toggle for those selectors, on desktop or mobile. Existing header identity, model labels, quota information, and session-details disclosure SHALL remain governed by their current behavior. Desktop selectors SHALL remain inline in the composer and compact mobile selectors SHALL be reachable by activating the composer's Plus button without requiring textarea focus.

#### Scenario: Desktop header

- **WHEN** a session owner views a chat using the desktop layout
- **THEN** the header shows no model, effort, or permission selectors while its existing identity, model label, and quota information remain available

#### Scenario: Mobile header

- **WHEN** a session owner views a chat using the compact mobile layout
- **THEN** the header does not duplicate permission, model, or effort controls and Plus provides access to supported selectors without focusing the message input or expanding header details
