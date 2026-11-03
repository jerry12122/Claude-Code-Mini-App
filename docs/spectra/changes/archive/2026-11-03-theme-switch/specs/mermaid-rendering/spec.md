## MODIFIED Requirements

### Requirement: Dark theme visual consistency

The system SHALL initialize Mermaid with theme settings matching the effective application theme: dark settings (`theme: 'dark'`) when the effective theme is dark or cannot be determined, and light settings (`theme: 'default'` with light application color variables) when the effective theme is light. When the effective theme changes, the system SHALL re-initialize Mermaid with the settings for the new theme and SHALL re-render every diagram that is currently displayed.

#### Scenario: Dark theme configuration applied
- **WHEN** the Mermaid library is initialized while the effective theme is dark
- **THEN** the system SHALL apply dark mode with background, line color, and text colors matching the design system

##### Example: Theme initialization parameters
- **GIVEN** application theme variables with dark background `oklch(0.16 0.02 264)`
- **WHEN** `mermaid.initialize()` is executed
- **THEN** the configuration includes `theme: 'dark'` and `securityLevel: 'strict'`

#### Scenario: Light theme configuration applied
- **WHEN** the Mermaid library is initialized while the effective theme is light
- **THEN** the configuration SHALL include `theme: 'default'`, `themeVariables.darkMode` equal to `false`, a light background, and `securityLevel: 'strict'`

#### Scenario: Unknown theme defaults to dark
- **WHEN** the configuration is built in an environment where no effective theme can be read
- **THEN** the configuration SHALL be the dark configuration

#### Scenario: Displayed diagrams redraw on theme change
- **WHEN** a rendered Mermaid diagram is visible and the user switches the theme from dark to light
- **THEN** the system SHALL re-render that diagram with the light configuration, and the diagram toolbar and container SHALL use light-theme colors

##### Example: Streaming diagram is not rendered by a theme change
- **GIVEN** a Mermaid block whose message is still streaming
- **WHEN** the theme changes
- **THEN** the system SHALL NOT invoke SVG rendering for that block until streaming completes
