# desktop-self-update Specification

## Purpose

Provides Windows desktop users with checks for stable GitHub Releases, explicit download and restart actions, and verified executable replacement through the existing Wails updater. It preserves running work and user data and exposes no remote installation or restart API.

## Requirements

### Requirement: Desktop update entry points

The desktop SHALL provide Check for Updates in the system tray and beside the existing version and SHA display in settings. Both SHALL open the same update section showing current identity, available version, release notes (the changelog part, rendered through a minimal built-in Markdown subset), progress, and applicable download, later, retry, and restart actions. Browser, Telegram, guest, and standalone server contexts SHALL NOT expose desktop update controls.

#### Scenario: Open updates from either entry point

- **WHEN** the desktop user invokes Check for Updates from the tray or settings
- **THEN** the main window shows the same update section and starts a manual check without starting a second concurrent operation

#### Scenario: Indicate an available desktop update

- **WHEN** the desktop has an available update
- **THEN** the bottom-left settings entry shows a purple dot, including in the collapsed sidebar, and opens the update section when clicked without a popup or automatic download

#### Scenario: Notify browser owners without installation controls

- **WHEN** an authenticated owner uses a browser or Telegram
- **THEN** the page checks public stable release metadata at startup and hourly while visible, catches up when returning to the foreground, and notifies at most once per version per day
- **AND** a host version or revision change prompts a manual refresh without reloading the page automatically
- **AND** version information is available without any download, install, or host restart controls; guests perform no release checks

#### Scenario: Read untrusted release notes

- **WHEN** release notes contain HTML or script markup
- **THEN** the update section displays it as text without executing it
- **AND** only headings, lists, emphasis, inline code, code blocks, quotes and rules are rendered as formatting, every other character is escaped, and links and images show their text only

#### Scenario: Show only the changelog

- **WHEN** the release body starts with download and first-time-setup sections followed by a "## 更新內容" heading
- **THEN** the update section shows only the content after that heading, and shows the whole body when the heading is absent

#### Scenario: Open settings outside desktop

- **WHEN** settings are opened in a browser, Telegram, or guest context
- **THEN** desktop update controls are absent while permitted existing settings retain their behavior


<!-- @trace
source: desktop-self-update
updated: 2026-10-16
code:
  - internal/desktopupdate/manager.go
  - internal/desktopupdate/script.go
  - README.zh-TW.md
  - internal/ws/handler.go
  - internal/server/server.go
  - scripts/e2e/desktop_driver.py
  - internal/desktopupdate/endpoint_default.go
  - CHANGELOG.md
  - internal/static/js/ui/SettingsModal.js
  - internal/static/js/wails-runtime-ready.js
  - scripts/release/package.sh
  - docs/features.md
  - internal/db/busy.go
  - internal/static/js/ui/DesktopUpdateSection.js
  - internal/static/AGENT.md
  - internal/static/assets_dev.go
  - go.mod
  - docs/getting-started.zh-TW.md
  - internal/desktopupdate/schedule.go
  - internal/static/js/app.js
  - internal/desktopupdate/endpoint_e2e.go
  - cmd/desktop/e2e_default_windows.go
  - docs/getting-started.md
  - internal/desktopupdate/bridge.go
  - scripts/desktop-update.test.cjs
  - scripts/e2e/README.md
  - scripts/release/release_state.py
  - scripts/wails-runtime-ready.test.cjs
  - cmd/desktop/main_windows.go
  - build.bat
  - internal/ws/admission.go
  - .github/release-header.md
  - internal/api/share.go
  - internal/desktopupdate/provider.go
  - internal/ws/shell.go
  - internal/static/index.html
  - docs/features.zh-TW.md
  - internal/static/assets.go
  - internal/static/js/ui/desktop-update.js
  - scripts/e2e/run_e2e.py
  - scripts/release/check-version.sh
  - README.md
  - internal/static/assets_production.go
  - scripts/release/publish.sh
  - scripts/e2e/fixture_server.py
  - .github/workflows/release.yml
  - cmd/desktop/e2e_hooks_windows.go
  - internal/static/js/hooks/useDesktopUpdate.js
  - scripts/e2e/build_fixtures.py
tests:
  - internal/desktopupdate/flow_test.go
  - scripts/release/test/check_workflow.py
  - internal/server/guest_test.go
  - internal/desktopupdate/release_contract_test.go
  - internal/ws/admission_test.go
  - scripts/release/test/fake_gh.py
  - internal/static/assets_test.go
  - scripts/release/test/state_check.py
  - internal/desktopupdate/selection_test.go
  - internal/desktopupdate/fixture_test.go
  - internal/db/busy_test.go
  - internal/api/share_page_test.go
  - scripts/release/test/test-release.sh
  - internal/desktopupdate/helper_startup_windows_test.go
-->

---
### Requirement: Restricted native update commands

Native update commands SHALL accept only check, download, restart, and status from the trusted desktop main frame at the configured local owner-page origin. Native handlers MUST validate the sending window, main-frame identity, Origin, and TopOrigin. They MUST reject unknown commands and untrusted navigation, MUST NOT accept caller-selected repositories, download URLs, paths, or executable commands, and SHALL expose no installation or restart HTTP endpoint.

#### Scenario: Reject an untrusted native command

- **WHEN** a command comes from another window, a child frame, an empty or foreign origin, a guest page, or uses an unknown action
- **THEN** no update operation starts and no executable is downloaded or replaced

#### Scenario: Navigate to an external or share URL

- **WHEN** the desktop main window is asked to open an external page or guest share link
- **THEN** that link opens in the external browser without receiving the desktop update bridge


<!-- @trace
source: desktop-self-update
updated: 2026-10-16
code:
  - internal/desktopupdate/manager.go
  - internal/desktopupdate/script.go
  - README.zh-TW.md
  - internal/ws/handler.go
  - internal/server/server.go
  - scripts/e2e/desktop_driver.py
  - internal/desktopupdate/endpoint_default.go
  - CHANGELOG.md
  - internal/static/js/ui/SettingsModal.js
  - internal/static/js/wails-runtime-ready.js
  - scripts/release/package.sh
  - docs/features.md
  - internal/db/busy.go
  - internal/static/js/ui/DesktopUpdateSection.js
  - internal/static/AGENT.md
  - internal/static/assets_dev.go
  - go.mod
  - docs/getting-started.zh-TW.md
  - internal/desktopupdate/schedule.go
  - internal/static/js/app.js
  - internal/desktopupdate/endpoint_e2e.go
  - cmd/desktop/e2e_default_windows.go
  - docs/getting-started.md
  - internal/desktopupdate/bridge.go
  - scripts/desktop-update.test.cjs
  - scripts/e2e/README.md
  - scripts/release/release_state.py
  - scripts/wails-runtime-ready.test.cjs
  - cmd/desktop/main_windows.go
  - build.bat
  - internal/ws/admission.go
  - .github/release-header.md
  - internal/api/share.go
  - internal/desktopupdate/provider.go
  - internal/ws/shell.go
  - internal/static/index.html
  - docs/features.zh-TW.md
  - internal/static/assets.go
  - internal/static/js/ui/desktop-update.js
  - scripts/e2e/run_e2e.py
  - scripts/release/check-version.sh
  - README.md
  - internal/static/assets_production.go
  - scripts/release/publish.sh
  - scripts/e2e/fixture_server.py
  - .github/workflows/release.yml
  - cmd/desktop/e2e_hooks_windows.go
  - internal/static/js/hooks/useDesktopUpdate.js
  - scripts/e2e/build_fixtures.py
tests:
  - internal/desktopupdate/flow_test.go
  - scripts/release/test/check_workflow.py
  - internal/server/guest_test.go
  - internal/desktopupdate/release_contract_test.go
  - internal/ws/admission_test.go
  - scripts/release/test/fake_gh.py
  - internal/static/assets_test.go
  - scripts/release/test/state_check.py
  - internal/desktopupdate/selection_test.go
  - internal/desktopupdate/fixture_test.go
  - internal/db/busy_test.go
  - internal/api/share_page_test.go
  - scripts/release/test/test-release.sh
  - internal/desktopupdate/helper_startup_windows_test.go
-->

---
### Requirement: Stable release checks

The desktop SHALL check the fixed public repository jerry12122/Claude-Code-Mini-App after initialization and once every hour while running, and SHALL support manual checks. One hour is the configured product schedule. It SHALL offer only a strictly newer stable semantic version with the exact desktop executable for the running supported platform. It MUST NOT select a draft, prerelease, same or older version, server executable, or installation ZIP. The shipped application SHALL contain no GitHub personal access token.

#### Scenario: Check at startup and on schedule

- **WHEN** initialization completes and the configured interval subsequently elapses
- **THEN** the desktop checks once at each trigger, updates its status silently, and does not automatically download or restart

#### Scenario: Select a newer desktop release

- **WHEN** version 1.0.0 checks a stable v1.0.1 release containing the legacy Windows ZIP and the exact desktop EXE
- **THEN** only the desktop EXE is offered with version 1.0.1 and its release notes

#### Scenario: Exclude unsupported release candidates

- **WHEN** candidates are a draft, a prerelease, v1.0.0, or v0.9.9 for current version 1.0.0
- **THEN** none is offered as an update

#### Scenario: Required desktop asset is missing

- **WHEN** the latest stable release is newer but has no exact matching desktop asset
- **THEN** the desktop reports asset_missing and retains the installed version


<!-- @trace
source: desktop-self-update
updated: 2026-10-16
code:
  - internal/desktopupdate/manager.go
  - internal/desktopupdate/script.go
  - README.zh-TW.md
  - internal/ws/handler.go
  - internal/server/server.go
  - scripts/e2e/desktop_driver.py
  - internal/desktopupdate/endpoint_default.go
  - CHANGELOG.md
  - internal/static/js/ui/SettingsModal.js
  - internal/static/js/wails-runtime-ready.js
  - scripts/release/package.sh
  - docs/features.md
  - internal/db/busy.go
  - internal/static/js/ui/DesktopUpdateSection.js
  - internal/static/AGENT.md
  - internal/static/assets_dev.go
  - go.mod
  - docs/getting-started.zh-TW.md
  - internal/desktopupdate/schedule.go
  - internal/static/js/app.js
  - internal/desktopupdate/endpoint_e2e.go
  - cmd/desktop/e2e_default_windows.go
  - docs/getting-started.md
  - internal/desktopupdate/bridge.go
  - scripts/desktop-update.test.cjs
  - scripts/e2e/README.md
  - scripts/release/release_state.py
  - scripts/wails-runtime-ready.test.cjs
  - cmd/desktop/main_windows.go
  - build.bat
  - internal/ws/admission.go
  - .github/release-header.md
  - internal/api/share.go
  - internal/desktopupdate/provider.go
  - internal/ws/shell.go
  - internal/static/index.html
  - docs/features.zh-TW.md
  - internal/static/assets.go
  - internal/static/js/ui/desktop-update.js
  - scripts/e2e/run_e2e.py
  - scripts/release/check-version.sh
  - README.md
  - internal/static/assets_production.go
  - scripts/release/publish.sh
  - scripts/e2e/fixture_server.py
  - .github/workflows/release.yml
  - cmd/desktop/e2e_hooks_windows.go
  - internal/static/js/hooks/useDesktopUpdate.js
  - scripts/e2e/build_fixtures.py
tests:
  - internal/desktopupdate/flow_test.go
  - scripts/release/test/check_workflow.py
  - internal/server/guest_test.go
  - internal/desktopupdate/release_contract_test.go
  - internal/ws/admission_test.go
  - scripts/release/test/fake_gh.py
  - internal/static/assets_test.go
  - scripts/release/test/state_check.py
  - internal/desktopupdate/selection_test.go
  - internal/desktopupdate/fixture_test.go
  - internal/db/busy_test.go
  - internal/api/share_page_test.go
  - scripts/release/test/test-release.sh
  - internal/desktopupdate/helper_startup_windows_test.go
-->

---
### Requirement: Explicit serialized download and verification

The desktop MUST require the user's download action before downloading an update. It MUST require a valid SHA256 checksum for the exact executable in checksums.txt and MUST refuse missing, malformed, or mismatching checksum data. It SHALL use Wails staging and verification, report progress, and enter ready only after successful verification. Repeated or scheduled operations SHALL NOT create concurrent downloads or helpers or invalidate a ready update.

#### Scenario: Download after confirmation

- **WHEN** the user chooses Download for an available release with valid checksum data
- **THEN** the desktop downloads the executable, reports progress, verifies its bytes, and offers restart while the current application continues running

#### Scenario: Refuse checksum failures

- **WHEN** checksums.txt is missing, the matching filename entry is absent, its hash is malformed, or downloaded bytes fail the hash check
- **THEN** the desktop reports checksum_missing or checksum_mismatch, does not enter ready, and leaves the installed executable unchanged

#### Scenario: Repeat commands during an operation

- **WHEN** a download is active or a verified update is ready and a timer or duplicate user command arrives
- **THEN** no additional download or helper starts and the active operation or ready update remains intact


<!-- @trace
source: desktop-self-update
updated: 2026-10-16
code:
  - internal/desktopupdate/manager.go
  - internal/desktopupdate/script.go
  - README.zh-TW.md
  - internal/ws/handler.go
  - internal/server/server.go
  - scripts/e2e/desktop_driver.py
  - internal/desktopupdate/endpoint_default.go
  - CHANGELOG.md
  - internal/static/js/ui/SettingsModal.js
  - internal/static/js/wails-runtime-ready.js
  - scripts/release/package.sh
  - docs/features.md
  - internal/db/busy.go
  - internal/static/js/ui/DesktopUpdateSection.js
  - internal/static/AGENT.md
  - internal/static/assets_dev.go
  - go.mod
  - docs/getting-started.zh-TW.md
  - internal/desktopupdate/schedule.go
  - internal/static/js/app.js
  - internal/desktopupdate/endpoint_e2e.go
  - cmd/desktop/e2e_default_windows.go
  - docs/getting-started.md
  - internal/desktopupdate/bridge.go
  - scripts/desktop-update.test.cjs
  - scripts/e2e/README.md
  - scripts/release/release_state.py
  - scripts/wails-runtime-ready.test.cjs
  - cmd/desktop/main_windows.go
  - build.bat
  - internal/ws/admission.go
  - .github/release-header.md
  - internal/api/share.go
  - internal/desktopupdate/provider.go
  - internal/ws/shell.go
  - internal/static/index.html
  - docs/features.zh-TW.md
  - internal/static/assets.go
  - internal/static/js/ui/desktop-update.js
  - scripts/e2e/run_e2e.py
  - scripts/release/check-version.sh
  - README.md
  - internal/static/assets_production.go
  - scripts/release/publish.sh
  - scripts/e2e/fixture_server.py
  - .github/workflows/release.yml
  - cmd/desktop/e2e_hooks_windows.go
  - internal/static/js/hooks/useDesktopUpdate.js
  - scripts/e2e/build_fixtures.py
tests:
  - internal/desktopupdate/flow_test.go
  - scripts/release/test/check_workflow.py
  - internal/server/guest_test.go
  - internal/desktopupdate/release_contract_test.go
  - internal/ws/admission_test.go
  - scripts/release/test/fake_gh.py
  - internal/static/assets_test.go
  - scripts/release/test/state_check.py
  - internal/desktopupdate/selection_test.go
  - internal/desktopupdate/fixture_test.go
  - internal/db/busy_test.go
  - internal/api/share_page_test.go
  - scripts/release/test/test-release.sh
  - internal/desktopupdate/helper_startup_windows_test.go
-->

---
### Requirement: Helper startup precedes application side effects

A helper-mode process MUST handle the Wails update protocol before changing working directory, reading configuration, initializing logs, opening the database, binding the HTTP port, or entering single-instance handling. A normal process SHALL retain the established application startup behavior.

#### Scenario: Start a helper while the application owns its port

- **WHEN** the updater spawns its helper while the current application is running
- **THEN** the helper acknowledges readiness without initializing application data or trying to bind the occupied port


<!-- @trace
source: desktop-self-update
updated: 2026-10-16
code:
  - internal/desktopupdate/manager.go
  - internal/desktopupdate/script.go
  - README.zh-TW.md
  - internal/ws/handler.go
  - internal/server/server.go
  - scripts/e2e/desktop_driver.py
  - internal/desktopupdate/endpoint_default.go
  - CHANGELOG.md
  - internal/static/js/ui/SettingsModal.js
  - internal/static/js/wails-runtime-ready.js
  - scripts/release/package.sh
  - docs/features.md
  - internal/db/busy.go
  - internal/static/js/ui/DesktopUpdateSection.js
  - internal/static/AGENT.md
  - internal/static/assets_dev.go
  - go.mod
  - docs/getting-started.zh-TW.md
  - internal/desktopupdate/schedule.go
  - internal/static/js/app.js
  - internal/desktopupdate/endpoint_e2e.go
  - cmd/desktop/e2e_default_windows.go
  - docs/getting-started.md
  - internal/desktopupdate/bridge.go
  - scripts/desktop-update.test.cjs
  - scripts/e2e/README.md
  - scripts/release/release_state.py
  - scripts/wails-runtime-ready.test.cjs
  - cmd/desktop/main_windows.go
  - build.bat
  - internal/ws/admission.go
  - .github/release-header.md
  - internal/api/share.go
  - internal/desktopupdate/provider.go
  - internal/ws/shell.go
  - internal/static/index.html
  - docs/features.zh-TW.md
  - internal/static/assets.go
  - internal/static/js/ui/desktop-update.js
  - scripts/e2e/run_e2e.py
  - scripts/release/check-version.sh
  - README.md
  - internal/static/assets_production.go
  - scripts/release/publish.sh
  - scripts/e2e/fixture_server.py
  - .github/workflows/release.yml
  - cmd/desktop/e2e_hooks_windows.go
  - internal/static/js/hooks/useDesktopUpdate.js
  - scripts/e2e/build_fixtures.py
tests:
  - internal/desktopupdate/flow_test.go
  - scripts/release/test/check_workflow.py
  - internal/server/guest_test.go
  - internal/desktopupdate/release_contract_test.go
  - internal/ws/admission_test.go
  - scripts/release/test/fake_gh.py
  - internal/static/assets_test.go
  - scripts/release/test/state_check.py
  - internal/desktopupdate/selection_test.go
  - internal/desktopupdate/fixture_test.go
  - internal/db/busy_test.go
  - internal/api/share_page_test.go
  - scripts/release/test/test-release.sh
  - internal/desktopupdate/helper_startup_windows_test.go
-->

---
### Requirement: Idle-only update restart

The desktop MUST require the user's restart confirmation and atomically acquire an update maintenance gate before spawning a replacement helper. Running agent or shell work, unfinished cancellation cleanup, pending approvals or shell confirmation, and queued work across all sessions MUST block the update restart without stopping or deleting that work. The gate SHALL serialize with all work admission paths, including WebSocket, MCP loopback, and queue dispatch. It SHALL block new work before execution side effects or queue consumption until shutdown, and SHALL be released if helper startup fails.

#### Scenario: Work prevents update restart

- **WHEN** the user requests restart while any running, cleaning-up, awaiting-confirmation, or queued work exists
- **THEN** the application reports update_busy, stays on the current version with the update ready, and does not spawn a helper or force-stop work

#### Scenario: Report the busy reason

- **WHEN** a restart is refused as busy
- **THEN** the status carries busyReason naming the blocking category: running_task, shell_task, pending_confirm, queued, or maintenance
- **AND** available session names, IDs, and blocking reasons are shown as text, including paused queue counts, without exposing queued prompts or shell commands or changing that work

#### Scenario: Queued work blocks restart even when paused

- **WHEN** any session's message queue has items while that queue is paused
- **THEN** the restart is refused with update_busy and busyReason queued, and the queued messages are kept

#### Scenario: Restart in progress

- **WHEN** the maintenance gate is acquired and the application calls Restart
- **THEN** the state is restarting, the desktop UI shows it as busy with the check, download, and restart actions unavailable, and no second helper starts

#### Scenario: Gate cannot determine work state

- **WHEN** the gate cannot read the work state, for example because a database query fails
- **THEN** the restart is cancelled with update_check_failed, no helper starts, and the verified update remains ready

#### Scenario: Race new work against restart

- **WHEN** new work admission races an otherwise idle update restart
- **THEN** exactly one wins: admitted work prevents restart, or the acquired maintenance gate rejects work with update_in_progress before execution side effects and without deleting queued messages

#### Scenario: Start confirmed idle update

- **WHEN** the user confirms restart, all work is idle, and the maintenance gate is acquired
- **THEN** the helper starts, the application shuts down its HTTP service and database, the desktop EXE is replaced at its original path, and the new application starts with its matching embedded frontend

#### Scenario: Helper startup fails

- **WHEN** the helper cannot start or acknowledge readiness
- **THEN** the maintenance gate is released, helper_failed is displayed, and the current process and ready update remain usable


<!-- @trace
source: desktop-self-update
updated: 2026-10-16
code:
  - internal/desktopupdate/manager.go
  - internal/desktopupdate/script.go
  - README.zh-TW.md
  - internal/ws/handler.go
  - internal/server/server.go
  - scripts/e2e/desktop_driver.py
  - internal/desktopupdate/endpoint_default.go
  - CHANGELOG.md
  - internal/static/js/ui/SettingsModal.js
  - internal/static/js/wails-runtime-ready.js
  - scripts/release/package.sh
  - docs/features.md
  - internal/db/busy.go
  - internal/static/js/ui/DesktopUpdateSection.js
  - internal/static/AGENT.md
  - internal/static/assets_dev.go
  - go.mod
  - docs/getting-started.zh-TW.md
  - internal/desktopupdate/schedule.go
  - internal/static/js/app.js
  - internal/desktopupdate/endpoint_e2e.go
  - cmd/desktop/e2e_default_windows.go
  - docs/getting-started.md
  - internal/desktopupdate/bridge.go
  - scripts/desktop-update.test.cjs
  - scripts/e2e/README.md
  - scripts/release/release_state.py
  - scripts/wails-runtime-ready.test.cjs
  - cmd/desktop/main_windows.go
  - build.bat
  - internal/ws/admission.go
  - .github/release-header.md
  - internal/api/share.go
  - internal/desktopupdate/provider.go
  - internal/ws/shell.go
  - internal/static/index.html
  - docs/features.zh-TW.md
  - internal/static/assets.go
  - internal/static/js/ui/desktop-update.js
  - scripts/e2e/run_e2e.py
  - scripts/release/check-version.sh
  - README.md
  - internal/static/assets_production.go
  - scripts/release/publish.sh
  - scripts/e2e/fixture_server.py
  - .github/workflows/release.yml
  - cmd/desktop/e2e_hooks_windows.go
  - internal/static/js/hooks/useDesktopUpdate.js
  - scripts/e2e/build_fixtures.py
tests:
  - internal/desktopupdate/flow_test.go
  - scripts/release/test/check_workflow.py
  - internal/server/guest_test.go
  - internal/desktopupdate/release_contract_test.go
  - internal/ws/admission_test.go
  - scripts/release/test/fake_gh.py
  - internal/static/assets_test.go
  - scripts/release/test/state_check.py
  - internal/desktopupdate/selection_test.go
  - internal/desktopupdate/fixture_test.go
  - internal/db/busy_test.go
  - internal/api/share_page_test.go
  - scripts/release/test/test-release.sh
  - internal/desktopupdate/helper_startup_windows_test.go
-->

---
### Requirement: Data preservation and bounded failure recovery

Updates SHALL replace only the currently running desktop executable and MUST NOT relocate or overwrite configuration, databases, attachments, logs, or standalone server files. Check, network, checksum, write-permission, or helper-start failures SHALL preserve the running version. Replacement or process-creation failure SHALL use Wails helper backup recovery. The application SHALL NOT promise automatic rollback after the replacement process has started successfully but later encounters an application-level failure.

#### Scenario: Preserve data across an update

- **WHEN** a confirmed idle update completes from fixture version 1.0.0 to 1.0.1
- **THEN** the process identity and displayed version and SHA change, the original port becomes available to the new process, user configuration and database records and attachment contents remain intact, existing log content is retained, and standalone server files remain unchanged

#### Scenario: Report connectivity and writable-directory failures

- **WHEN** checking or downloading fails due to connectivity or rate limiting, or the installation directory is not writable
- **THEN** the application retains its installed executable, records a diagnosable error, offers manual retry or manual-update instructions, and does not elevate privileges or report the application as up to date

#### Scenario: Recover a failed executable replacement

- **WHEN** the helper cannot replace the executable or create the replacement process, and restoring the backup succeeds
- **THEN** the helper restores the original executable from `<exe>.bak` and relaunches it, without modifying user data

#### Scenario: Double failure when replacement and restore are both blocked

- **WHEN** the installed executable stays locked for the whole swap (all swap attempts are exhausted) and restoring the backup also fails because the same lock blocks removing the target
- **THEN** the helper does not relaunch automatically; the original executable remains unchanged on disk (same SHA256), `<exe>.bak` remains, user data is untouched, and the user recovers manually (close the process holding the file, then start the application or restore `<exe>.bak` to the executable name, as documented in the README)

#### Scenario: Clean stale replaced executables at startup

- **WHEN** the desktop starts and its executable directory contains files named exactly `<current exe name>.old.<digits>` (the previous executable that the updater renamed aside)
- **THEN** it removes all of them, including the newest, and logs each removal; no automatic rollback copy of the previous executable is kept, so users who want one MUST back up the executable before updating
- **AND** it never removes directories, symbolic links, the running executable, `<exe>.bak`, or any file whose name does not match exactly; a file that cannot be removed (for example still in use) is logged and does not stop startup

#### Scenario: No published release yet

- **WHEN** the release endpoint returns 404 because no public release exists yet
- **THEN** the application reports the installed version as up to date with no error code; this is not a failure and does not conflict with the rule above, which covers connectivity, rate-limit, and download failures that must not be reported as up to date

<!-- @trace
source: desktop-self-update
updated: 2026-10-16
code:
  - internal/desktopupdate/manager.go
  - internal/desktopupdate/script.go
  - README.zh-TW.md
  - internal/ws/handler.go
  - internal/server/server.go
  - scripts/e2e/desktop_driver.py
  - internal/desktopupdate/endpoint_default.go
  - CHANGELOG.md
  - internal/static/js/ui/SettingsModal.js
  - internal/static/js/wails-runtime-ready.js
  - scripts/release/package.sh
  - docs/features.md
  - internal/db/busy.go
  - internal/static/js/ui/DesktopUpdateSection.js
  - internal/static/AGENT.md
  - internal/static/assets_dev.go
  - go.mod
  - docs/getting-started.zh-TW.md
  - internal/desktopupdate/schedule.go
  - internal/static/js/app.js
  - internal/desktopupdate/endpoint_e2e.go
  - cmd/desktop/e2e_default_windows.go
  - docs/getting-started.md
  - internal/desktopupdate/bridge.go
  - scripts/desktop-update.test.cjs
  - scripts/e2e/README.md
  - scripts/release/release_state.py
  - scripts/wails-runtime-ready.test.cjs
  - cmd/desktop/main_windows.go
  - build.bat
  - internal/ws/admission.go
  - .github/release-header.md
  - internal/api/share.go
  - internal/desktopupdate/provider.go
  - internal/ws/shell.go
  - internal/static/index.html
  - docs/features.zh-TW.md
  - internal/static/assets.go
  - internal/static/js/ui/desktop-update.js
  - scripts/e2e/run_e2e.py
  - scripts/release/check-version.sh
  - README.md
  - internal/static/assets_production.go
  - scripts/release/publish.sh
  - scripts/e2e/fixture_server.py
  - .github/workflows/release.yml
  - cmd/desktop/e2e_hooks_windows.go
  - internal/static/js/hooks/useDesktopUpdate.js
  - scripts/e2e/build_fixtures.py
tests:
  - internal/desktopupdate/flow_test.go
  - scripts/release/test/check_workflow.py
  - internal/server/guest_test.go
  - internal/desktopupdate/release_contract_test.go
  - internal/ws/admission_test.go
  - scripts/release/test/fake_gh.py
  - internal/static/assets_test.go
  - scripts/release/test/state_check.py
  - internal/desktopupdate/selection_test.go
  - internal/desktopupdate/fixture_test.go
  - internal/db/busy_test.go
  - internal/api/share_page_test.go
  - scripts/release/test/test-release.sh
  - internal/desktopupdate/helper_startup_windows_test.go
-->
