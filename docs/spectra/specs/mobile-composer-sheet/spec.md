# mobile-composer-sheet Specification

## Purpose

The mobile composer provides a compact input row and an independently controlled bottom sheet for attachments and session input settings. It keeps tools usable across text-field blur, keyboard changes, and native file selection without changing upload or authorization contracts.

## Requirements

### Requirement: Explicit mobile tools entry

On compact mobile layouts, an editable chat composer SHALL display a named Plus button beside the message input and existing send or interrupt actions. Its attachment and setting controls SHALL be reachable through a bottom sheet opened only by activating Plus. Textarea focus or blur MUST NOT open or close the sheet, reveal an inline tools row, or impose a different content-height limit. Desktop layouts SHALL retain their inline tools. Read-only chats and composer states that currently replace input with an interrupt-only bar SHALL NOT acquire an editable composer through this entry.

#### Scenario: Open from an unfocused composer

- **WHEN** the user activates Plus while the mobile message input is unfocused
- **THEN** the tools sheet opens without requiring text input or displaying the keyboard

#### Scenario: Type and leave the message input

- **WHEN** the user focuses and then blurs a mobile textarea containing a multiline draft
- **THEN** the tool entry stays in the same arrangement and the textarea retains its content-based height limit without a blur-driven collapse

#### Scenario: Desktop and non-editable chats

- **WHEN** a desktop editable chat, a read-only shared chat, an ended share, an unsupported read-only agent session, or an interrupt-only composer is displayed
- **THEN** desktop tools remain inline and the non-editable or interrupt-only surfaces expose no mobile tools entry


<!-- @trace
source: mobile-composer-sheet
updated: 2026-11-14
code:
  - internal/static/js/session/SessionView.js
  - internal/server/server.go
  - scripts/mobile-composer-mock.test.py
  - internal/db/busy.go
  - internal/static/sw.js
  - internal/static/assets.go
  - scripts/telegram-chat.test.cjs
  - README.md
  - internal/db/session.go
  - internal/static/js/hooks/useNewSessionForm.js
  - internal/quota/antigravity.go
  - internal/db/db.go
  - internal/static/js/hooks/useChatSocket.js
  - internal/static/manifest.webmanifest
  - README.zh-TW.md
  - internal/static/AGENT.md
  - internal/static/js/chat/ChatView.js
  - internal/ws/handler.go
  - internal/static/index.html
  - scripts/console-usability.test.cjs
  - internal/static/js/app.js
  - scripts/composer-attachment.test.py
  - docs/prototypes/mobile-composer-sheet.dc.html
  - internal/static/js/ui/ui-atoms.js
  - internal/quota/codex.go
  - internal/agent/factory.go
  - internal/api/upload.go
  - internal/static/js/ui/SessionTabs.js
  - internal/static/assets_production.go
  - internal/static/js/chat/ComposerTools.js
  - internal/mcp/tools.go
  - internal/static/robots.txt
  - scripts/context-menu.test.cjs
  - scripts/service-worker.test.cjs
  - internal/usage/quota.go
  - internal/api/session.go
  - CHANGELOG.md
  - internal/static/js/chat/ForwardModal.js
tests:
  - internal/ws/unread_test.go
  - internal/quota/display_test.go
  - internal/db/busy_test.go
  - internal/usage/quota_test.go
  - docs/spec/codex-cli.md
  - docs/spec/antigravity-cli.md
  - internal/db/unread_smoke_test.go
  - docs/spec/kiro-cli.md
  - internal/server/crawler_guard_test.go
  - internal/server/start_helper_test.go
  - internal/quota/codex_test.go
  - internal/ws/kiro_readonly_test.go
  - internal/mcp/kiro_readonly_test.go
  - internal/quota/windows_test.go
  - internal/api/session_test.go
  - internal/agent/factory_test.go
-->

---
### Requirement: Sheet state survives focus and viewport changes

An open sheet SHALL remain open when the textarea blurs, the keyboard is dismissed, a settings choice page is used, or a native file picker is opened and cancelled. Keyboard-driven viewport resizing and rotation within mobile layouts SHALL only adjust available sheet space. Switching to a desktop layout SHALL close the sheet and preserve draft text, attachments, and setting values. The sheet SHALL keep all its controls reachable through internal scrolling without scrolling the background conversation.

#### Scenario: Blur and dismiss the keyboard

- **WHEN** the user opens tools while typing and the keyboard subsequently disappears or the visual viewport height changes
- **THEN** the sheet remains open and its controls remain operable

#### Scenario: Change a setting inside the sheet

- **WHEN** the user opens a model, effort, or permission choice page and selects a value or returns without changing it
- **THEN** the sheet remains open on its tools home page, displays the resulting draft selection, and focuses the original settings entry

#### Scenario: Rotate and switch layouts

- **WHEN** an open mobile tools sheet is rotated within mobile layouts and then resized to a desktop layout
- **THEN** rotation preserves the open sheet and the desktop transition closes it while retaining the same composer draft and setting values

#### Scenario: Scroll a constrained sheet

- **WHEN** available mobile height cannot display every tool at once
- **THEN** the tool content scrolls within the sheet, the close action remains reachable, and the conversation behind it retains its scroll position


<!-- @trace
source: mobile-composer-sheet
updated: 2026-11-14
code:
  - internal/static/js/session/SessionView.js
  - internal/server/server.go
  - scripts/mobile-composer-mock.test.py
  - internal/db/busy.go
  - internal/static/sw.js
  - internal/static/assets.go
  - scripts/telegram-chat.test.cjs
  - README.md
  - internal/db/session.go
  - internal/static/js/hooks/useNewSessionForm.js
  - internal/quota/antigravity.go
  - internal/db/db.go
  - internal/static/js/hooks/useChatSocket.js
  - internal/static/manifest.webmanifest
  - README.zh-TW.md
  - internal/static/AGENT.md
  - internal/static/js/chat/ChatView.js
  - internal/ws/handler.go
  - internal/static/index.html
  - scripts/console-usability.test.cjs
  - internal/static/js/app.js
  - scripts/composer-attachment.test.py
  - docs/prototypes/mobile-composer-sheet.dc.html
  - internal/static/js/ui/ui-atoms.js
  - internal/quota/codex.go
  - internal/agent/factory.go
  - internal/api/upload.go
  - internal/static/js/ui/SessionTabs.js
  - internal/static/assets_production.go
  - internal/static/js/chat/ComposerTools.js
  - internal/mcp/tools.go
  - internal/static/robots.txt
  - scripts/context-menu.test.cjs
  - scripts/service-worker.test.cjs
  - internal/usage/quota.go
  - internal/api/session.go
  - CHANGELOG.md
  - internal/static/js/chat/ForwardModal.js
tests:
  - internal/ws/unread_test.go
  - internal/quota/display_test.go
  - internal/db/busy_test.go
  - internal/usage/quota_test.go
  - docs/spec/codex-cli.md
  - docs/spec/antigravity-cli.md
  - internal/db/unread_smoke_test.go
  - docs/spec/kiro-cli.md
  - internal/server/crawler_guard_test.go
  - internal/server/start_helper_test.go
  - internal/quota/codex_test.go
  - internal/ws/kiro_readonly_test.go
  - internal/mcp/kiro_readonly_test.go
  - internal/quota/windows_test.go
  - internal/api/session_test.go
  - internal/agent/factory_test.go
-->

---
### Requirement: Native picker lifecycle preserves attachment drafts

The composer SHALL keep one file input mounted independently of sheet visibility and SHALL open it synchronously from the user's attachment action. A cancellation or empty file list SHALL preserve the open sheet and all composer drafts. A nonempty list accepted by the existing upload entry guards SHALL create the existing attachment placeholders, close the sheet before uploads finish, and continue the existing ordered upload flow. Upload errors SHALL retain removable and retryable attachment chips. Mobile sheet file selection SHALL NOT automatically focus the textarea or send a message. A picker result belonging to a different session or to a composer that no longer permits attachments MUST NOT be added or uploaded.

#### Scenario: Cancel and choose again

- **WHEN** the user opens the native picker from the sheet, cancels it, and then selects a file from the same sheet
- **THEN** cancellation keeps tools and drafts intact and the later accepted selection adds its attachment chip and closes the sheet

#### Scenario: Choose multiple files with an upload failure

- **WHEN** the user selects two files and the second upload fails
- **THEN** both placeholders appear in selection order before upload completion, the sheet closes, and the failed attachment retains the existing error, retry, and remove actions without losing the text draft

#### Scenario: Reattach the same file

- **WHEN** the user selects a file, removes its chip, reopens Plus, and selects that same file again
- **THEN** the new selection is processed and no duplicate file input is required

#### Scenario: Return to a different or non-writable composer

- **WHEN** the active session changes or attachment permission is lost while a native picker is open
- **THEN** its eventual file result is discarded without uploading into the new or non-writable session


<!-- @trace
source: mobile-composer-sheet
updated: 2026-11-14
code:
  - internal/static/js/session/SessionView.js
  - internal/server/server.go
  - scripts/mobile-composer-mock.test.py
  - internal/db/busy.go
  - internal/static/sw.js
  - internal/static/assets.go
  - scripts/telegram-chat.test.cjs
  - README.md
  - internal/db/session.go
  - internal/static/js/hooks/useNewSessionForm.js
  - internal/quota/antigravity.go
  - internal/db/db.go
  - internal/static/js/hooks/useChatSocket.js
  - internal/static/manifest.webmanifest
  - README.zh-TW.md
  - internal/static/AGENT.md
  - internal/static/js/chat/ChatView.js
  - internal/ws/handler.go
  - internal/static/index.html
  - scripts/console-usability.test.cjs
  - internal/static/js/app.js
  - scripts/composer-attachment.test.py
  - docs/prototypes/mobile-composer-sheet.dc.html
  - internal/static/js/ui/ui-atoms.js
  - internal/quota/codex.go
  - internal/agent/factory.go
  - internal/api/upload.go
  - internal/static/js/ui/SessionTabs.js
  - internal/static/assets_production.go
  - internal/static/js/chat/ComposerTools.js
  - internal/mcp/tools.go
  - internal/static/robots.txt
  - scripts/context-menu.test.cjs
  - scripts/service-worker.test.cjs
  - internal/usage/quota.go
  - internal/api/session.go
  - CHANGELOG.md
  - internal/static/js/chat/ForwardModal.js
tests:
  - internal/ws/unread_test.go
  - internal/quota/display_test.go
  - internal/db/busy_test.go
  - internal/usage/quota_test.go
  - docs/spec/codex-cli.md
  - docs/spec/antigravity-cli.md
  - internal/db/unread_smoke_test.go
  - docs/spec/kiro-cli.md
  - internal/server/crawler_guard_test.go
  - internal/server/start_helper_test.go
  - internal/quota/codex_test.go
  - internal/ws/kiro_readonly_test.go
  - internal/mcp/kiro_readonly_test.go
  - internal/quota/windows_test.go
  - internal/api/session_test.go
  - internal/agent/factory_test.go
-->

---
### Requirement: Accessible dismissal consumes only the tools interaction

The sheet SHALL expose a labelled modal dialog, a named Close action, and a Plus button reporting its expanded state. Opening SHALL move focus into the dialog and constrain background interaction. Close and backdrop activation SHALL dismiss the sheet from either the tools home or a choice page while preserving the chat and drafts. Escape, browser Back, and the Telegram BackButton when supported SHALL return from a choice page to the tools home, or dismiss the sheet from the tools home. Escape handled by the sheet MUST NOT activate global chat-tab close shortcuts. Native file picker dismissal MUST NOT be interpreted as sheet dismissal. Closing SHALL restore focus to the surviving Plus button without scrolling or explicitly reopening the keyboard.

#### Scenario: Dismiss with close or backdrop

- **WHEN** the user activates Close or the backdrop outside the sheet from either the tools home or a choice page
- **THEN** only the sheet closes, draft content survives, background interaction resumes, and focus returns to Plus

#### Scenario: Dismiss with keyboard

- **WHEN** a keyboard user opens the sheet and presses Escape, from the tools home
- **THEN** the sheet closes without the same event activating the application's chat-tab close shortcut

#### Scenario: Return without leaving chat

- **WHEN** the user opens the sheet and activates browser Back or the supported Telegram BackButton from the tools home
- **THEN** the sheet closes and the current chat remains selected without leaving the page or clearing drafts

#### Scenario: Repeated opening and browser history

- **WHEN** the user repeatedly activates Plus on an already open sheet, closes and reopens it, and uses Back followed by Forward
- **THEN** each sheet opening owns at most one history marker, closing leaves no active marker or duplicate back handler, and Forward does not reopen the sheet

#### Scenario: Assistive and keyboard navigation

- **WHEN** the sheet is opened using a keyboard or assistive technology
- **THEN** it announces its name, exposes named controls with visible focus, and keeps keyboard navigation within the open dialog


<!-- @trace
source: mobile-composer-sheet
updated: 2026-11-14
code:
  - internal/static/js/session/SessionView.js
  - internal/server/server.go
  - scripts/mobile-composer-mock.test.py
  - internal/db/busy.go
  - internal/static/sw.js
  - internal/static/assets.go
  - scripts/telegram-chat.test.cjs
  - README.md
  - internal/db/session.go
  - internal/static/js/hooks/useNewSessionForm.js
  - internal/quota/antigravity.go
  - internal/db/db.go
  - internal/static/js/hooks/useChatSocket.js
  - internal/static/manifest.webmanifest
  - README.zh-TW.md
  - internal/static/AGENT.md
  - internal/static/js/chat/ChatView.js
  - internal/ws/handler.go
  - internal/static/index.html
  - scripts/console-usability.test.cjs
  - internal/static/js/app.js
  - scripts/composer-attachment.test.py
  - docs/prototypes/mobile-composer-sheet.dc.html
  - internal/static/js/ui/ui-atoms.js
  - internal/quota/codex.go
  - internal/agent/factory.go
  - internal/api/upload.go
  - internal/static/js/ui/SessionTabs.js
  - internal/static/assets_production.go
  - internal/static/js/chat/ComposerTools.js
  - internal/mcp/tools.go
  - internal/static/robots.txt
  - scripts/context-menu.test.cjs
  - scripts/service-worker.test.cjs
  - internal/usage/quota.go
  - internal/api/session.go
  - CHANGELOG.md
  - internal/static/js/chat/ForwardModal.js
tests:
  - internal/ws/unread_test.go
  - internal/quota/display_test.go
  - internal/db/busy_test.go
  - internal/usage/quota_test.go
  - docs/spec/codex-cli.md
  - docs/spec/antigravity-cli.md
  - internal/db/unread_smoke_test.go
  - docs/spec/kiro-cli.md
  - internal/server/crawler_guard_test.go
  - internal/server/start_helper_test.go
  - internal/quota/codex_test.go
  - internal/ws/kiro_readonly_test.go
  - internal/mcp/kiro_readonly_test.go
  - internal/quota/windows_test.go
  - internal/api/session_test.go
  - internal/agent/factory_test.go
-->

---
### Requirement: Sheet lifecycle retains existing control restrictions

The sheet SHALL reuse the existing agent, role, input-mode, busy-state, sending, connection, draft-restoration, and pending-send restrictions for each tool. Model, effort, and permission changes SHALL remain draft selections applied on the next send. Shell mode SHALL hide attachments and agent selectors while retaining the allowed mode switch. Switching input mode through the sheet SHALL return to its tools home page. Session changes, loss of write access, replacement by an interrupt-only bar, transition to desktop, and unmount SHALL release modal state, background scroll locks, and owned history and Telegram handlers without transferring drafts or focus to another session.

#### Scenario: Supported and restricted tools

- **WHEN** an owner in Claude, Codex, or Cursor agent mode, a writable guest, or an owner in shell mode opens mobile tools
- **THEN** only the tools permitted by the existing agent, role, and mode are displayed, their current values are shown, and disabled actions remain disabled

#### Scenario: Settings wait for send

- **WHEN** an idle owner changes a supported model, effort, or permission value and closes the sheet
- **THEN** the draft retains that value and no corresponding setting message is sent until the next message submission

#### Scenario: Change mode from tools

- **WHEN** an idle owner changes between agent and shell mode through the sheet
- **THEN** the sheet returns to its tools home page and the existing mode-switch and draft-preservation rules apply

#### Scenario: Release an obsolete sheet

- **WHEN** a sheet's session changes, write access ends, the composer becomes interrupt-only, the layout becomes desktop, or ChatView unmounts
- **THEN** its modal, scroll lock, owned history marker, and Telegram handlers are cleaned up without clearing recoverable drafts or focusing a removed control


<!-- @trace
source: mobile-composer-sheet
updated: 2026-11-14
code:
  - internal/static/js/session/SessionView.js
  - internal/server/server.go
  - scripts/mobile-composer-mock.test.py
  - internal/db/busy.go
  - internal/static/sw.js
  - internal/static/assets.go
  - scripts/telegram-chat.test.cjs
  - README.md
  - internal/db/session.go
  - internal/static/js/hooks/useNewSessionForm.js
  - internal/quota/antigravity.go
  - internal/db/db.go
  - internal/static/js/hooks/useChatSocket.js
  - internal/static/manifest.webmanifest
  - README.zh-TW.md
  - internal/static/AGENT.md
  - internal/static/js/chat/ChatView.js
  - internal/ws/handler.go
  - internal/static/index.html
  - scripts/console-usability.test.cjs
  - internal/static/js/app.js
  - scripts/composer-attachment.test.py
  - docs/prototypes/mobile-composer-sheet.dc.html
  - internal/static/js/ui/ui-atoms.js
  - internal/quota/codex.go
  - internal/agent/factory.go
  - internal/api/upload.go
  - internal/static/js/ui/SessionTabs.js
  - internal/static/assets_production.go
  - internal/static/js/chat/ComposerTools.js
  - internal/mcp/tools.go
  - internal/static/robots.txt
  - scripts/context-menu.test.cjs
  - scripts/service-worker.test.cjs
  - internal/usage/quota.go
  - internal/api/session.go
  - CHANGELOG.md
  - internal/static/js/chat/ForwardModal.js
tests:
  - internal/ws/unread_test.go
  - internal/quota/display_test.go
  - internal/db/busy_test.go
  - internal/usage/quota_test.go
  - docs/spec/codex-cli.md
  - docs/spec/antigravity-cli.md
  - internal/db/unread_smoke_test.go
  - docs/spec/kiro-cli.md
  - internal/server/crawler_guard_test.go
  - internal/server/start_helper_test.go
  - internal/quota/codex_test.go
  - internal/ws/kiro_readonly_test.go
  - internal/mcp/kiro_readonly_test.go
  - internal/quota/windows_test.go
  - internal/api/session_test.go
  - internal/agent/factory_test.go
-->

---
### Requirement: Settings use in-sheet single-choice pages

Mobile settings SHALL expose full-row named entries with current values. Activating an entry SHALL replace the tools home with a labelled single-choice page inside the same dialog. The current value SHALL be checked, including values absent from a newly loaded option list. Choosing a value, including the current value, SHALL return to the tools home without closing the sheet; Back or Escape SHALL return without changing the value. Busy-state changes SHALL disable both the entry and its choice inputs. Model-option failures SHALL preserve the draft value and the default choice. Desktop selectors SHALL retain their native presentation.

#### Scenario: Select and return to the setting

- **WHEN** an owner opens the model entry, selects a different model, and opens it again
- **THEN** the tools home shows the new value and its choice is checked without sending a setting message

#### Scenario: Back without a change

- **WHEN** an owner opens effort choices and activates Back, Escape, browser Back, or Telegram BackButton
- **THEN** the same dialog returns to its tools home, preserves effort, and focuses the effort entry

#### Scenario: Options fail or controls become disabled

- **WHEN** model options fail to load or the agent becomes busy while a choice page is open
- **THEN** a failed option load preserves the default and current draft choices and busy controls cannot change the draft

<!-- @trace
source: mobile-composer-sheet
updated: 2026-11-14
code:
  - internal/static/js/session/SessionView.js
  - internal/server/server.go
  - scripts/mobile-composer-mock.test.py
  - internal/db/busy.go
  - internal/static/sw.js
  - internal/static/assets.go
  - scripts/telegram-chat.test.cjs
  - README.md
  - internal/db/session.go
  - internal/static/js/hooks/useNewSessionForm.js
  - internal/quota/antigravity.go
  - internal/db/db.go
  - internal/static/js/hooks/useChatSocket.js
  - internal/static/manifest.webmanifest
  - README.zh-TW.md
  - internal/static/AGENT.md
  - internal/static/js/chat/ChatView.js
  - internal/ws/handler.go
  - internal/static/index.html
  - scripts/console-usability.test.cjs
  - internal/static/js/app.js
  - scripts/composer-attachment.test.py
  - docs/prototypes/mobile-composer-sheet.dc.html
  - internal/static/js/ui/ui-atoms.js
  - internal/quota/codex.go
  - internal/agent/factory.go
  - internal/api/upload.go
  - internal/static/js/ui/SessionTabs.js
  - internal/static/assets_production.go
  - internal/static/js/chat/ComposerTools.js
  - internal/mcp/tools.go
  - internal/static/robots.txt
  - scripts/context-menu.test.cjs
  - scripts/service-worker.test.cjs
  - internal/usage/quota.go
  - internal/api/session.go
  - CHANGELOG.md
  - internal/static/js/chat/ForwardModal.js
tests:
  - internal/ws/unread_test.go
  - internal/quota/display_test.go
  - internal/db/busy_test.go
  - internal/usage/quota_test.go
  - docs/spec/codex-cli.md
  - docs/spec/antigravity-cli.md
  - internal/db/unread_smoke_test.go
  - docs/spec/kiro-cli.md
  - internal/server/crawler_guard_test.go
  - internal/server/start_helper_test.go
  - internal/quota/codex_test.go
  - internal/ws/kiro_readonly_test.go
  - internal/mcp/kiro_readonly_test.go
  - internal/quota/windows_test.go
  - internal/api/session_test.go
  - internal/agent/factory_test.go
-->