## Purpose

Provide a compact, usable conversation header on landscape tablets while preserving host safety clearances, touch interactions, and conversation space across viewport changes.

## ADDED Requirements

### Requirement: Compact landscape header preserves conversation space

The system SHALL reduce avoidable application header space on landscape tablets and make the recovered space available to the conversation, while preserving session identity, state, and existing header actions. Moving the same empty space into another application container SHALL NOT satisfy this requirement.

#### Scenario: Landscape conversation remains usable

- **WHEN** an owner views a conversation on a landscape tablet with the same content and host clearances before and after the change
- **THEN** the conversation has more visible space, header information and actions remain available, and the composer remains operable

### Requirement: Touch targets and host clearance remain usable

The system SHALL keep application controls clear of operating-system and Telegram host controls, preserve usable touch targets, and retain normal layouts when host inset APIs are absent. The system SHALL respond to orientation, host inset, fullscreen, and keyboard changes without retaining obsolete offsets.

#### Scenario: Host clearance changes after rotation

- **WHEN** a Telegram tablet rotates or reports updated content safe-area insets
- **THEN** application controls remain outside required host clearances, the conversation and composer remain operable, and obsolete spacing is removed

#### Scenario: Browser without Telegram APIs

- **WHEN** the conversation opens in a normal browser or a host without content-safe-area APIs
- **THEN** header actions remain operable without script errors or unnecessary host-only spacing

#### Scenario: Other layouts retain access

- **WHEN** the user changes between tablet portrait, phone, desktop, split-pane, or an open-keyboard layout
- **THEN** session navigation, header actions, conversation scrolling, and composer access remain available without new clipping or page-level horizontal scrolling
