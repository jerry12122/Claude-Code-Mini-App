# split-chat-layout Specification

## Purpose

Provides responsive split-pane UI containers, a resizable divider, active pane focus indication, and automatic single-pane fallback on narrow viewports. This enables comfortable side-by-side agent supervision on desktop screens without breaking mobile usability.

## Requirements

### Requirement: Split pane container layout
The UI SHALL render two chat panes either side-by-side (horizontal orientation) or stacked top-and-bottom (vertical orientation) according to the active split orientation setting.

#### Scenario: Enable horizontal split view
- **WHEN** user enables split view with horizontal orientation
- **THEN** UI renders left primary and right secondary chat panes divided by a vertical separator

#### Scenario: Enable vertical split view
- **WHEN** user enables split view with vertical orientation
- **THEN** UI renders top primary and bottom secondary chat panes divided by a horizontal separator

#### Scenario: Disable split view mode
- **WHEN** user closes the secondary pane via close button or split toggle
- **THEN** secondary pane unmounts and primary chat pane expands to fill the full content area


<!-- @trace
source: split-view-orientation
updated: 2026-10-12
code:
  - internal/static/js/chat/ChatHeader.js
  - internal/static/js/session/SessionView.js
  - CLAUDE.md
  - .spectra.yaml
  - .agents/skills/spectra-apply/SKILL.md
  - .cursorrules
  - .agents/skills/spectra-ingest/SKILL.md
  - docs/plan/done.md
  - internal/static/js/chat/ChatView.js
  - internal/static/index.html
  - .agents/skills/spectra-analyze/SKILL.md
  - README.zh-TW.md
  - .agents/skills/spectra-debug/SKILL.md
  - .agents/skills/spectra-archive/SKILL.md
  - internal/static/js/app.js
  - internal/static/js/ui/SplitLayout.js
  - .agents/skills/spectra-drift/SKILL.md
  - .agents/skills/spectra-propose/SKILL.md
  - README.md
  - .agents/skills/spectra-review/SKILL.md
  - .agents/skills/spectra-commit/SKILL.md
  - .agents/skills/spectra-discuss/SKILL.md
  - .agents/skills/spectra-audit/SKILL.md
  - .agents/skills/spectra-verify/SKILL.md
-->

---
### Requirement: Resizable pane divider
The split view divider SHALL support mouse and touch drag to adjust pane dimensions according to the active orientation, enforcing a minimum width of 320px in horizontal mode and a minimum height of 200px in vertical mode.

#### Scenario: Drag divider in horizontal orientation
- **WHEN** user drags the vertical split divider horizontally
- **THEN** left and right pane widths adjust smoothly according to pointer position while respecting minimum 320px width boundaries

#### Scenario: Drag divider in vertical orientation
- **WHEN** user drags the horizontal split divider vertically
- **THEN** top and bottom pane heights adjust smoothly according to pointer position while respecting minimum 200px height boundaries

#### Scenario: Double click divider to reset ratio
- **WHEN** user double clicks the split divider in either orientation
- **THEN** pane dimensions reset to an equal 50/50 ratio


<!-- @trace
source: split-view-orientation
updated: 2026-10-12
code:
  - internal/static/js/chat/ChatHeader.js
  - internal/static/js/session/SessionView.js
  - CLAUDE.md
  - .spectra.yaml
  - .agents/skills/spectra-apply/SKILL.md
  - .cursorrules
  - .agents/skills/spectra-ingest/SKILL.md
  - docs/plan/done.md
  - internal/static/js/chat/ChatView.js
  - internal/static/index.html
  - .agents/skills/spectra-analyze/SKILL.md
  - README.zh-TW.md
  - .agents/skills/spectra-debug/SKILL.md
  - .agents/skills/spectra-archive/SKILL.md
  - internal/static/js/app.js
  - internal/static/js/ui/SplitLayout.js
  - .agents/skills/spectra-drift/SKILL.md
  - .agents/skills/spectra-propose/SKILL.md
  - README.md
  - .agents/skills/spectra-review/SKILL.md
  - .agents/skills/spectra-commit/SKILL.md
  - .agents/skills/spectra-discuss/SKILL.md
  - .agents/skills/spectra-audit/SKILL.md
  - .agents/skills/spectra-verify/SKILL.md
-->

---
### Requirement: Active pane focus indication
The UI SHALL highlight the currently active pane and route keyboard shortcuts and quick switcher commands to the focused pane.

#### Scenario: Click pane to switch focus
- **WHEN** user clicks inside the secondary pane
- **THEN** secondary pane receives active focus highlight and primary pane border becomes inactive

#### Scenario: Keyboard shortcut pane toggle
- **WHEN** user presses `Ctrl+Alt+Left` or `Ctrl+Alt+Right`
- **THEN** focus shifts between the primary and secondary panes respectively


<!-- @trace
source: split-chat-view
updated: 2026-10-12
code:
  - CLAUDE.md
  - docs/plan/done.md
  - internal/static/js/chat/ChatView.js
  - internal/static/js/ui/SplitLayout.js
  - internal/static/js/app.js
  - internal/static/index.html
  - .agents/skills/spectra-propose/SKILL.md
  - .cursorrules
  - .agents/skills/spectra-verify/SKILL.md
  - .agents/skills/spectra-discuss/SKILL.md
  - internal/static/js/chat/ChatHeader.js
  - .spectra.yaml
  - internal/static/js/session/SessionView.js
  - README.md
  - README.zh-TW.md
  - .agents/skills/spectra-audit/SKILL.md
  - .agents/skills/spectra-drift/SKILL.md
  - .agents/skills/spectra-archive/SKILL.md
  - .agents/skills/spectra-analyze/SKILL.md
  - .agents/skills/spectra-debug/SKILL.md
  - .agents/skills/spectra-review/SKILL.md
  - .agents/skills/spectra-commit/SKILL.md
  - .agents/skills/spectra-apply/SKILL.md
  - .agents/skills/spectra-ingest/SKILL.md
-->

---
### Requirement: Responsive narrow viewport fallback
The UI SHALL automatically collapse split view into a single pane with a tab switcher when viewport width drops below 900px or when running inside Telegram Mini App.

#### Scenario: Viewport resize below threshold
- **WHEN** window width resizes below 900px while split view is enabled
- **THEN** UI displays a single pane with top tabs to switch between the two sessions instead of side-by-side columns

#### Scenario: Viewport resize restores side-by-side
- **WHEN** window width resizes back above 900px
- **THEN** UI restores side-by-side dual pane layout automatically

<!-- @trace
source: split-chat-view
updated: 2026-10-12
code:
  - CLAUDE.md
  - docs/plan/done.md
  - internal/static/js/chat/ChatView.js
  - internal/static/js/ui/SplitLayout.js
  - internal/static/js/app.js
  - internal/static/index.html
  - .agents/skills/spectra-propose/SKILL.md
  - .cursorrules
  - .agents/skills/spectra-verify/SKILL.md
  - .agents/skills/spectra-discuss/SKILL.md
  - internal/static/js/chat/ChatHeader.js
  - .spectra.yaml
  - internal/static/js/session/SessionView.js
  - README.md
  - README.zh-TW.md
  - .agents/skills/spectra-audit/SKILL.md
  - .agents/skills/spectra-drift/SKILL.md
  - .agents/skills/spectra-archive/SKILL.md
  - .agents/skills/spectra-analyze/SKILL.md
  - .agents/skills/spectra-debug/SKILL.md
  - .agents/skills/spectra-review/SKILL.md
  - .agents/skills/spectra-commit/SKILL.md
  - .agents/skills/spectra-apply/SKILL.md
  - .agents/skills/spectra-ingest/SKILL.md
-->

---
### Requirement: Split orientation switching
The UI SHALL provide intuitive orientation toggle controls to switch between horizontal (side-by-side) and vertical (top-bottom) split view modes.

#### Scenario: Toggle orientation via header button
- **WHEN** user clicks the orientation toggle action in the split view controls
- **THEN** split layout switches between side-by-side and top-bottom while preserving the active sessions in each pane

#### Scenario: Toggle orientation via keyboard shortcut
- **WHEN** user presses `Ctrl+Alt+O`
- **THEN** split layout toggles between horizontal and vertical orientations immediately


<!-- @trace
source: split-view-orientation
updated: 2026-10-12
code:
  - internal/static/js/chat/ChatHeader.js
  - internal/static/js/session/SessionView.js
  - CLAUDE.md
  - .spectra.yaml
  - .agents/skills/spectra-apply/SKILL.md
  - .cursorrules
  - .agents/skills/spectra-ingest/SKILL.md
  - docs/plan/done.md
  - internal/static/js/chat/ChatView.js
  - internal/static/index.html
  - .agents/skills/spectra-analyze/SKILL.md
  - README.zh-TW.md
  - .agents/skills/spectra-debug/SKILL.md
  - .agents/skills/spectra-archive/SKILL.md
  - internal/static/js/app.js
  - internal/static/js/ui/SplitLayout.js
  - .agents/skills/spectra-drift/SKILL.md
  - .agents/skills/spectra-propose/SKILL.md
  - README.md
  - .agents/skills/spectra-review/SKILL.md
  - .agents/skills/spectra-commit/SKILL.md
  - .agents/skills/spectra-discuss/SKILL.md
  - .agents/skills/spectra-audit/SKILL.md
  - .agents/skills/spectra-verify/SKILL.md
-->

---
### Requirement: Split orientation persistence
The UI SHALL persist the split orientation setting in local storage and restore it upon page reload.

#### Scenario: Restore orientation on page load
- **WHEN** user reloads the application after configuring vertical split view
- **THEN** UI restores vertical split layout with top and bottom panes without reverting to horizontal default

<!-- @trace
source: split-view-orientation
updated: 2026-10-12
code:
  - internal/static/js/chat/ChatHeader.js
  - internal/static/js/session/SessionView.js
  - CLAUDE.md
  - .spectra.yaml
  - .agents/skills/spectra-apply/SKILL.md
  - .cursorrules
  - .agents/skills/spectra-ingest/SKILL.md
  - docs/plan/done.md
  - internal/static/js/chat/ChatView.js
  - internal/static/index.html
  - .agents/skills/spectra-analyze/SKILL.md
  - README.zh-TW.md
  - .agents/skills/spectra-debug/SKILL.md
  - .agents/skills/spectra-archive/SKILL.md
  - internal/static/js/app.js
  - internal/static/js/ui/SplitLayout.js
  - .agents/skills/spectra-drift/SKILL.md
  - .agents/skills/spectra-propose/SKILL.md
  - README.md
  - .agents/skills/spectra-review/SKILL.md
  - .agents/skills/spectra-commit/SKILL.md
  - .agents/skills/spectra-discuss/SKILL.md
  - .agents/skills/spectra-audit/SKILL.md
  - .agents/skills/spectra-verify/SKILL.md
-->