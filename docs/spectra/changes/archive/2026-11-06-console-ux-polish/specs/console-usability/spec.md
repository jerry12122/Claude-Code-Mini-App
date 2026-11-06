## Purpose

Console usability defines the shared interaction and readability baseline for the existing Agent Console. It keeps desktop workspaces and mobile remote control operable without replacing the established visual identity or transport behavior.

## ADDED Requirements

### Requirement: Keyboard-visible and named shared controls

Shared icon actions and form controls SHALL expose accessible names. Keyboard focus SHALL remain visible in both supported themes, including inside the composer and on controls revealed through focus rather than pointer hover. Important actions SHALL have a click or keyboard alternative to drag and hover.

#### Scenario: Reach a shared icon action without a mouse

- **WHEN** a keyboard user focuses a header or composer icon action in either theme
- **THEN** the action exposes its purpose and a visible focus indicator without requiring hover

#### Scenario: Composer receives keyboard focus

- **WHEN** the message textarea receives keyboard focus
- **THEN** the composer identifies the focused input and its focused boundary remains visible

### Requirement: Compact controls and zoom retain access to controls

The composer SHALL preserve its existing 32 CSS pixel select and button height and existing action order. This change SHALL NOT enlarge controls or introduce a new tool-row wrapping policy. Browser zoom SHALL remain enabled. Permission, model and effort selectors SHALL remain available according to the session's existing agent and role rules when controls wrap on a narrow screen.

#### Scenario: Use supported selectors on a phone

- **WHEN** a session owner opens a Claude agent-mode chat at a 390 CSS pixel viewport width
- **THEN** the supported permission, model, effort and send controls remain reachable with their existing compact arrangement without horizontal page scrolling
- **AND** composer selectors and action buttons retain their existing 32 CSS pixel height

#### Scenario: Enlarge a narrow interface

- **WHEN** the user enlarges text or uses browser zoom
- **THEN** message input and important actions remain reachable and the page does not disable zoom through viewport restrictions

### Requirement: Long content remains readable in both themes

The chat header SHALL provide a keyboard- and touch-accessible disclosure for the complete session name and work directory when the summary is truncated. Expanded identity text, prose and long unbroken tokens SHALL wrap within the available chat width. Code blocks SHALL preserve source formatting and support local horizontal scrolling without causing page-wide overflow. Usage information and state labels SHALL remain readable in both themes, with normal text meeting a 4.5:1 foreground/background contrast ratio.

#### Scenario: Read a long work directory

- **WHEN** a path is truncated in a narrow chat header
- **THEN** the user can expose the complete path without relying exclusively on a pointer tooltip

#### Scenario: Read a long code line in a split group

- **WHEN** a code line exceeds its chat group's width
- **THEN** the code block supports local horizontal scrolling and the workspace remains within the viewport

#### Scenario: Read usage and state across themes

- **WHEN** the user switches between the existing light and dark themes
- **THEN** usage values and meaningful state text satisfy the text contrast contract and state meaning is available as text rather than color alone

### Requirement: Failure feedback preserves recoverable input

Visual refinements SHALL preserve the existing authorization, queue and transport semantics. A disconnected composer SHALL keep its draft editable and explain unavailable delivery. Failed send or authorization actions SHALL preserve recoverable input and expose a recovery instruction. Streaming content SHALL NOT be announced token by token through a live region. Feedback SHALL NOT delay high-frequency keyboard actions or require motion to convey state.

#### Scenario: Edit a draft while disconnected

- **WHEN** the chat loses its connection while a draft exists
- **THEN** the draft remains editable and delivery unavailability is explained without discarding the draft

#### Scenario: An authorization action cannot be delivered

- **WHEN** an authorization action fails to reach the existing transport
- **THEN** its controls and pending request remain available and feedback explains how the user can retry

#### Scenario: Reduced motion keeps state understandable

- **WHEN** reduced motion is enabled and an operation changes state
- **THEN** text and control state communicate the change without waiting for animation completion
