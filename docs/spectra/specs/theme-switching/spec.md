# theme-switching Specification

## Purpose

The theme-switching capability lets each device choose between a dark theme, a light theme, or following the operating system, and applies the choice consistently to every surface of the web UI, including floating layers, native form controls, code blocks, and diagrams. It exists so that users can read the app comfortably in bright environments without losing the existing dark look.

## Requirements

### Requirement: Theme preference options and default

The system SHALL offer three theme preferences: `system`, `dark`, and `light`. The default preference, used when no preference has been stored on the device, SHALL be `dark`, and the dark theme SHALL present the application as it appeared before this capability existed.

#### Scenario: First visit uses dark theme
- **WHEN** the app loads on a device with no stored theme preference
- **THEN** the effective theme SHALL be dark and the `<html>` element SHALL carry `data-theme="dark"`

#### Scenario: Stored invalid value falls back to default
- **WHEN** the stored theme preference is a value other than `system`, `dark`, or `light`
- **THEN** the system SHALL treat the preference as `dark`

##### Example: Resolution table
| Stored preference | OS prefers | Effective theme |
|-------------------|------------|-----------------|
| (none)            | light      | dark            |
| `dark`            | light      | dark            |
| `light`           | dark       | light           |
| `system`          | light      | light           |
| `system`          | dark       | dark            |
| `garbage`         | light      | dark            |


<!-- @trace
source: theme-switch
updated: 2026-11-03
code:
  - internal/static/js/app.js
  - internal/static/js/chat/MentionMenu.js
  - internal/static/js/chat/MermaidBlock.js
  - internal/static/js/core/core.js
  - internal/static/js/session/SessionView.js
  - internal/static/js/share/ShareModal.js
  - docs/plan/todo.md
  - internal/static/js/ui/DesktopUpdateSection.js
  - internal/static/js/ui/SessionTabs.js
  - internal/static/js/share/JoinView.js
  - internal/static/js/ui/SplitLayout.js
  - internal/static/js/chat/ChatView.js
  - internal/static/index.html
  - scripts/mermaid-render.test.cjs
  - internal/static/js/ui/LogsSection.js
  - internal/static/js/chat/SelectionQuotePopover.js
  - internal/static/js/setup/SetupWizard.js
  - internal/static/js/share/SharesSection.js
  - internal/static/js/chat/chat-header.js
  - internal/static/js/ui/SettingsModal.js
  - internal/static/js/session/NewSessionComposer.js
  - internal/static/js/ui/ui-atoms.js
  - scripts/theme.test.cjs
  - internal/static/js/chat/MessageActions.js
tests:
  - internal/mcp/queue_test.go
-->

---
### Requirement: Per-device persistence without server involvement

The system SHALL persist the theme preference only in the local storage of the device under the key `cc_theme_v1`, and SHALL NOT send it to the server or include it in the server-synced appearance settings.

#### Scenario: Preference survives reload on the same device
- **WHEN** the user selects `light` and reloads the page
- **THEN** the effective theme SHALL be light after reload

#### Scenario: Preference does not travel across devices
- **WHEN** the user selects `light` on one device and opens the app on another device with no stored preference
- **THEN** the other device SHALL use the dark theme

#### Scenario: No appearance API payload change
- **WHEN** the user changes the theme preference
- **THEN** the system SHALL NOT issue a request to `/settings/appearance` and the stored appearance payload SHALL be unchanged


<!-- @trace
source: theme-switch
updated: 2026-11-03
code:
  - internal/static/js/app.js
  - internal/static/js/chat/MentionMenu.js
  - internal/static/js/chat/MermaidBlock.js
  - internal/static/js/core/core.js
  - internal/static/js/session/SessionView.js
  - internal/static/js/share/ShareModal.js
  - docs/plan/todo.md
  - internal/static/js/ui/DesktopUpdateSection.js
  - internal/static/js/ui/SessionTabs.js
  - internal/static/js/share/JoinView.js
  - internal/static/js/ui/SplitLayout.js
  - internal/static/js/chat/ChatView.js
  - internal/static/index.html
  - scripts/mermaid-render.test.cjs
  - internal/static/js/ui/LogsSection.js
  - internal/static/js/chat/SelectionQuotePopover.js
  - internal/static/js/setup/SetupWizard.js
  - internal/static/js/share/SharesSection.js
  - internal/static/js/chat/chat-header.js
  - internal/static/js/ui/SettingsModal.js
  - internal/static/js/session/NewSessionComposer.js
  - internal/static/js/ui/ui-atoms.js
  - scripts/theme.test.cjs
  - internal/static/js/chat/MessageActions.js
tests:
  - internal/mcp/queue_test.go
-->

---
### Requirement: Following the operating system theme

When the preference is `system`, the system SHALL resolve the effective theme from the `prefers-color-scheme` media query and SHALL update the effective theme live when the operating system setting changes, without a page reload.

#### Scenario: Live update while in system mode
- **WHEN** the preference is `system` and the operating system switches from light to dark
- **THEN** the effective theme SHALL change to dark without reloading the page

#### Scenario: Explicit preference ignores system changes
- **WHEN** the preference is `light` and the operating system switches to dark
- **THEN** the effective theme SHALL remain light


<!-- @trace
source: theme-switch
updated: 2026-11-03
code:
  - internal/static/js/app.js
  - internal/static/js/chat/MentionMenu.js
  - internal/static/js/chat/MermaidBlock.js
  - internal/static/js/core/core.js
  - internal/static/js/session/SessionView.js
  - internal/static/js/share/ShareModal.js
  - docs/plan/todo.md
  - internal/static/js/ui/DesktopUpdateSection.js
  - internal/static/js/ui/SessionTabs.js
  - internal/static/js/share/JoinView.js
  - internal/static/js/ui/SplitLayout.js
  - internal/static/js/chat/ChatView.js
  - internal/static/index.html
  - scripts/mermaid-render.test.cjs
  - internal/static/js/ui/LogsSection.js
  - internal/static/js/chat/SelectionQuotePopover.js
  - internal/static/js/setup/SetupWizard.js
  - internal/static/js/share/SharesSection.js
  - internal/static/js/chat/chat-header.js
  - internal/static/js/ui/SettingsModal.js
  - internal/static/js/session/NewSessionComposer.js
  - internal/static/js/ui/ui-atoms.js
  - scripts/theme.test.cjs
  - internal/static/js/chat/MessageActions.js
tests:
  - internal/mcp/queue_test.go
-->

---
### Requirement: Theme applied before first paint

The system SHALL apply the effective theme to the `<html>` element before the React application mounts, on every entry page including the main app, the guest Join page, and the setup wizard, so that no frame of the wrong theme is displayed.

#### Scenario: Reload in light theme does not flash dark
- **WHEN** the stored preference is `light` and the page is reloaded
- **THEN** the `data-theme="light"` attribute SHALL be present on `<html>` before the first React render


<!-- @trace
source: theme-switch
updated: 2026-11-03
code:
  - internal/static/js/app.js
  - internal/static/js/chat/MentionMenu.js
  - internal/static/js/chat/MermaidBlock.js
  - internal/static/js/core/core.js
  - internal/static/js/session/SessionView.js
  - internal/static/js/share/ShareModal.js
  - docs/plan/todo.md
  - internal/static/js/ui/DesktopUpdateSection.js
  - internal/static/js/ui/SessionTabs.js
  - internal/static/js/share/JoinView.js
  - internal/static/js/ui/SplitLayout.js
  - internal/static/js/chat/ChatView.js
  - internal/static/index.html
  - scripts/mermaid-render.test.cjs
  - internal/static/js/ui/LogsSection.js
  - internal/static/js/chat/SelectionQuotePopover.js
  - internal/static/js/setup/SetupWizard.js
  - internal/static/js/share/SharesSection.js
  - internal/static/js/chat/chat-header.js
  - internal/static/js/ui/SettingsModal.js
  - internal/static/js/session/NewSessionComposer.js
  - internal/static/js/ui/ui-atoms.js
  - scripts/theme.test.cjs
  - internal/static/js/chat/MessageActions.js
tests:
  - internal/mcp/queue_test.go
-->

---
### Requirement: Theme selector in settings

The appearance section of the Settings dialog SHALL present the theme preference as a three-option control at the top of the section, SHALL reflect the stored preference when opened, and SHALL apply a newly selected option immediately without requiring the Save action.

#### Scenario: Selecting an option applies immediately
- **WHEN** the user selects `light` in the theme control while the Settings dialog remains open
- **THEN** the effective theme SHALL become light immediately and the preference SHALL be stored locally without pressing Save

#### Scenario: Control reflects stored preference
- **WHEN** the preference is `system` and the user opens the Settings dialog
- **THEN** the `system` option SHALL be shown as selected


<!-- @trace
source: theme-switch
updated: 2026-11-03
code:
  - internal/static/js/app.js
  - internal/static/js/chat/MentionMenu.js
  - internal/static/js/chat/MermaidBlock.js
  - internal/static/js/core/core.js
  - internal/static/js/session/SessionView.js
  - internal/static/js/share/ShareModal.js
  - docs/plan/todo.md
  - internal/static/js/ui/DesktopUpdateSection.js
  - internal/static/js/ui/SessionTabs.js
  - internal/static/js/share/JoinView.js
  - internal/static/js/ui/SplitLayout.js
  - internal/static/js/chat/ChatView.js
  - internal/static/index.html
  - scripts/mermaid-render.test.cjs
  - internal/static/js/ui/LogsSection.js
  - internal/static/js/chat/SelectionQuotePopover.js
  - internal/static/js/setup/SetupWizard.js
  - internal/static/js/share/SharesSection.js
  - internal/static/js/chat/chat-header.js
  - internal/static/js/ui/SettingsModal.js
  - internal/static/js/session/NewSessionComposer.js
  - internal/static/js/ui/ui-atoms.js
  - scripts/theme.test.cjs
  - internal/static/js/chat/MessageActions.js
tests:
  - internal/mcp/queue_test.go
-->

---
### Requirement: Complete themed surface coverage

In both themes, every UI surface SHALL derive its background, text, border, and focus colors from theme-aware tokens, so that text remains readable against its background. This coverage SHALL include the main chat, sidebar, session tabs, Settings, Logs, Share, Forward, Join, and setup screens, and the following floating or easily missed layers: the quick switcher opened with Ctrl+P (Cmd+P on macOS), every dropdown including the native `<select>` option list, the `@` mention menu, the message action menu, the selection quote popover, the image lightbox, and modal dialogs.

#### Scenario: Quick switcher is readable in light theme
- **WHEN** the effective theme is light and the user presses Ctrl+P
- **THEN** the quick switcher panel, its input, list items, selected item, and hints SHALL render with a light panel background and dark readable text

#### Scenario: Dropdowns follow the theme
- **WHEN** the effective theme is light and the user opens any dropdown, including the permission mode select, the model select, and the log level select
- **THEN** the closed control and the opened option list SHALL both render in light colors

#### Scenario: Floating layers follow the theme
- **WHEN** the effective theme is light and the user opens the mention menu, the message action menu, the selection quote popover, the image lightbox, or a modal dialog
- **THEN** each layer SHALL render with light-theme surface and text colors

#### Scenario: Solid action buttons keep contrast
- **WHEN** the effective theme is light and a confirmation dialog shows its primary, caution, and destructive action buttons
- **THEN** the label of each button SHALL remain readable against the background of that button


<!-- @trace
source: theme-switch
updated: 2026-11-03
code:
  - internal/static/js/app.js
  - internal/static/js/chat/MentionMenu.js
  - internal/static/js/chat/MermaidBlock.js
  - internal/static/js/core/core.js
  - internal/static/js/session/SessionView.js
  - internal/static/js/share/ShareModal.js
  - docs/plan/todo.md
  - internal/static/js/ui/DesktopUpdateSection.js
  - internal/static/js/ui/SessionTabs.js
  - internal/static/js/share/JoinView.js
  - internal/static/js/ui/SplitLayout.js
  - internal/static/js/chat/ChatView.js
  - internal/static/index.html
  - scripts/mermaid-render.test.cjs
  - internal/static/js/ui/LogsSection.js
  - internal/static/js/chat/SelectionQuotePopover.js
  - internal/static/js/setup/SetupWizard.js
  - internal/static/js/share/SharesSection.js
  - internal/static/js/chat/chat-header.js
  - internal/static/js/ui/SettingsModal.js
  - internal/static/js/session/NewSessionComposer.js
  - internal/static/js/ui/ui-atoms.js
  - scripts/theme.test.cjs
  - internal/static/js/chat/MessageActions.js
tests:
  - internal/mcp/queue_test.go
-->

---
### Requirement: Native controls and scrollbars follow the theme

The system SHALL set the CSS `color-scheme` property to match the effective theme so that native controls, including `<select>` option lists, checkboxes, color inputs, range inputs, and scrollbars, render in the matching scheme.

#### Scenario: color-scheme matches theme
- **WHEN** the effective theme is light
- **THEN** the computed `color-scheme` of the document root SHALL be `light`; and when the effective theme is dark it SHALL be `dark`


<!-- @trace
source: theme-switch
updated: 2026-11-03
code:
  - internal/static/js/app.js
  - internal/static/js/chat/MentionMenu.js
  - internal/static/js/chat/MermaidBlock.js
  - internal/static/js/core/core.js
  - internal/static/js/session/SessionView.js
  - internal/static/js/share/ShareModal.js
  - docs/plan/todo.md
  - internal/static/js/ui/DesktopUpdateSection.js
  - internal/static/js/ui/SessionTabs.js
  - internal/static/js/share/JoinView.js
  - internal/static/js/ui/SplitLayout.js
  - internal/static/js/chat/ChatView.js
  - internal/static/index.html
  - scripts/mermaid-render.test.cjs
  - internal/static/js/ui/LogsSection.js
  - internal/static/js/chat/SelectionQuotePopover.js
  - internal/static/js/setup/SetupWizard.js
  - internal/static/js/share/SharesSection.js
  - internal/static/js/chat/chat-header.js
  - internal/static/js/ui/SettingsModal.js
  - internal/static/js/session/NewSessionComposer.js
  - internal/static/js/ui/ui-atoms.js
  - scripts/theme.test.cjs
  - internal/static/js/chat/MessageActions.js
tests:
  - internal/mcp/queue_test.go
-->

---
### Requirement: Code highlighting follows the theme

The system SHALL render syntax-highlighted code with a light highlight palette in the light theme and a dark highlight palette in the dark theme, and SHALL switch between them when the effective theme changes without re-fetching message content.

#### Scenario: Code block recolors on theme change
- **WHEN** a chat message with a highlighted code block is visible and the user switches from dark to light
- **THEN** the code block text SHALL use the light highlight palette and the block background SHALL be light


<!-- @trace
source: theme-switch
updated: 2026-11-03
code:
  - internal/static/js/app.js
  - internal/static/js/chat/MentionMenu.js
  - internal/static/js/chat/MermaidBlock.js
  - internal/static/js/core/core.js
  - internal/static/js/session/SessionView.js
  - internal/static/js/share/ShareModal.js
  - docs/plan/todo.md
  - internal/static/js/ui/DesktopUpdateSection.js
  - internal/static/js/ui/SessionTabs.js
  - internal/static/js/share/JoinView.js
  - internal/static/js/ui/SplitLayout.js
  - internal/static/js/chat/ChatView.js
  - internal/static/index.html
  - scripts/mermaid-render.test.cjs
  - internal/static/js/ui/LogsSection.js
  - internal/static/js/chat/SelectionQuotePopover.js
  - internal/static/js/setup/SetupWizard.js
  - internal/static/js/share/SharesSection.js
  - internal/static/js/chat/chat-header.js
  - internal/static/js/ui/SettingsModal.js
  - internal/static/js/session/NewSessionComposer.js
  - internal/static/js/ui/ui-atoms.js
  - scripts/theme.test.cjs
  - internal/static/js/chat/MessageActions.js
tests:
  - internal/mcp/queue_test.go
-->

---
### Requirement: Custom CSS can scope by theme

The system SHALL expose the effective theme as the `data-theme` attribute on the `<html>` element and SHALL continue to apply the custom CSS of the user in both themes, so that custom rules can target one theme with a selector such as `html[data-theme="light"]`.

#### Scenario: Theme-scoped custom CSS applies only to its theme
- **WHEN** the custom CSS contains `html[data-theme="light"] body { outline: 1px solid red; }` and the effective theme is dark
- **THEN** the rule SHALL NOT apply; and when the effective theme becomes light it SHALL apply

<!-- @trace
source: theme-switch
updated: 2026-11-03
code:
  - internal/static/js/app.js
  - internal/static/js/chat/MentionMenu.js
  - internal/static/js/chat/MermaidBlock.js
  - internal/static/js/core/core.js
  - internal/static/js/session/SessionView.js
  - internal/static/js/share/ShareModal.js
  - docs/plan/todo.md
  - internal/static/js/ui/DesktopUpdateSection.js
  - internal/static/js/ui/SessionTabs.js
  - internal/static/js/share/JoinView.js
  - internal/static/js/ui/SplitLayout.js
  - internal/static/js/chat/ChatView.js
  - internal/static/index.html
  - scripts/mermaid-render.test.cjs
  - internal/static/js/ui/LogsSection.js
  - internal/static/js/chat/SelectionQuotePopover.js
  - internal/static/js/setup/SetupWizard.js
  - internal/static/js/share/SharesSection.js
  - internal/static/js/chat/chat-header.js
  - internal/static/js/ui/SettingsModal.js
  - internal/static/js/session/NewSessionComposer.js
  - internal/static/js/ui/ui-atoms.js
  - scripts/theme.test.cjs
  - internal/static/js/chat/MessageActions.js
tests:
  - internal/mcp/queue_test.go
-->