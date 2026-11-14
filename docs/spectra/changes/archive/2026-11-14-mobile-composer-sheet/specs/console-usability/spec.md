## MODIFIED Requirements

### Requirement: Compact controls and zoom retain access to controls

The desktop composer SHALL preserve its existing compact control sizing and action order. Compact mobile layouts SHALL expose a Plus entry beside the input and send or interrupt actions and SHALL place attachment and supported setting controls in a bottom sheet. Mobile entry and sheet controls SHALL provide touch-operable targets and SHALL remain reachable when space is constrained. Browser zoom SHALL remain enabled. Permission, model and effort selectors SHALL remain available according to the session's existing agent and role rules. Mobile textarea sizing SHALL follow content and available space without changing its height limit solely because focus leaves the composer.

#### Scenario: Use supported selectors on a phone

- **WHEN** a session owner opens a Claude agent-mode chat on a phone and activates Plus
- **THEN** the supported permission, model, effort and attachment actions are reachable in the sheet, including by internal scrolling, without horizontal page scrolling

#### Scenario: Enlarge a narrow interface

- **WHEN** the user enlarges text or uses browser zoom
- **THEN** message input, Plus, sheet dismissal, and important actions remain reachable and the page does not disable zoom through viewport restrictions

#### Scenario: Preserve desktop controls

- **WHEN** the user views an editable chat using the desktop layout
- **THEN** the composer retains its inline tools, existing compact sizing and action order

#### Scenario: Read a multiline draft after blur

- **WHEN** a mobile user leaves a textarea containing a multiline draft to operate Plus or a sheet control
- **THEN** blur alone does not collapse the textarea or hide its attachment chips
