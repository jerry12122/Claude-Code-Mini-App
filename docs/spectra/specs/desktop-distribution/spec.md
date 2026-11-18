# desktop-distribution Specification

## Purpose

Defines how production desktop builds carry their own web assets and how GitHub Releases deliver compatible installation bundles and a verifiable desktop update executable. It preserves disk-based development and existing server distribution.

## Requirements

### Requirement: Production embedded assets

Production desktop builds SHALL embed the application HTML, JavaScript, and favicon in the executable. The application SHALL serve the home page, share page, and desktop icon from the same embedded asset source without depending on an adjacent internal/static directory. An adjacent stale directory SHALL NOT override embedded assets. Embedded assets MUST NOT include configuration, databases, uploads, logs, Go source files, or development documentation.

#### Scenario: Run without disk frontend

- **WHEN** a production desktop executable starts with a valid configuration and without a disk frontend directory
- **THEN** the home page, JavaScript, favicon, and share page are served from the executable

#### Scenario: Ignore stale frontend files

- **WHEN** an older internal/static directory remains beside a newer production desktop executable
- **THEN** the application serves the embedded version and ignores the stale files

#### Scenario: Preserve share page behavior

- **WHEN** a visitor opens a valid share URL with or without a trailing slash
- **THEN** the page retains its relative-asset base behavior, no-store and no-referrer headers, and existing authorization restrictions


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
  - internal/static/js/ui/desktopUpdate.js
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
### Requirement: Development disk assets

Non-production builds SHALL read web assets from the development disk directory without requiring a frontend build step. Static serving SHALL preserve API routing, correct JavaScript content types, and rejection of paths outside the public asset root.

#### Scenario: Edit frontend during development

- **WHEN** a developer changes a disk JavaScript asset and refreshes a non-production build
- **THEN** the changed asset is served without rebuilding the Go executable

#### Scenario: Preserve API routing and asset boundaries

- **WHEN** a client requests an existing API route, an unknown API route, or a path outside the public asset root
- **THEN** the existing API route keeps its behavior, the unknown route does not become an HTML success response, and no file outside the public asset root is returned


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
  - internal/static/js/ui/desktopUpdate.js
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
### Requirement: Compatible release artifacts

Each supported release SHALL retain the Windows installation ZIP and Linux server tar.gz and SHALL add exactly one Windows amd64 desktop update executable named claude-miniapp-desktop-v<VERSION>-windows-amd64.exe. The Windows ZIP desktop executable SHALL contain the same bytes as that update executable. The ZIP SHALL continue to provide the server executable and its disk frontend. The Linux package SHALL retain its server executable, configuration example, and disk frontend. The updater SHALL NOT update the standalone server.

#### Scenario: Inspect release package contents

- **WHEN** release assets for version 1.0.1 are assembled
- **THEN** claude-miniapp-v1.0.1-windows-amd64.zip, claude-miniapp-v1.0.1-linux-amd64.tar.gz, and claude-miniapp-desktop-v1.0.1-windows-amd64.exe exist, and the desktop executable in the ZIP is byte-identical to the standalone desktop asset


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
  - internal/static/js/ui/desktopUpdate.js
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
### Requirement: Release identity and integrity

Release generation MUST reject a mismatch between the tag, root VERSION, and version.Version. Production desktop build metadata MUST expose the commit referenced by the triggering tag. The release SHALL contain checksums.txt in SHA256SUMS format covering the installation ZIP, Linux tar.gz, and standalone desktop executable.

#### Scenario: Reject inconsistent version inputs

- **WHEN** the tag is v1.0.1 but VERSION or version.Version is 1.0.0
- **THEN** release generation fails before publishing a public release

#### Scenario: Verify build identity and checksums

- **WHEN** release assets for a tag are inspected
- **THEN** the desktop reports the matching version and tag commit SHA, and every downloadable program asset has a matching SHA256 entry in checksums.txt


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
  - internal/static/js/ui/desktopUpdate.js
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
### Requirement: Complete release publication and bootstrap

The release pipeline SHALL upload and verify all required assets on a draft release before publishing it as a stable latest release. It SHALL reuse only an existing draft for the same tag, SHALL NOT overwrite assets of an already published release, and SHALL leave an incomplete release unpublished. Distribution documentation SHALL explain the one-time manual upgrade needed to acquire the updater, writable installation-directory requirements, retained user data, and manual recovery of the desktop executable.

#### Scenario: Asset upload fails

- **WHEN** an asset upload or checksum verification fails during release preparation
- **THEN** the release remains a draft and is not advertised as a latest stable update

#### Scenario: Retry an interrupted release

- **WHEN** the pipeline is retried for a tag that already has a draft release
- **THEN** it completes and verifies that draft before publication without creating a duplicate release

#### Scenario: Reject a published release rewrite

- **WHEN** the pipeline is rerun for a tag whose release is already public
- **THEN** it stops without replacing published assets

#### Scenario: Upgrade from a version without an updater

- **WHEN** a user follows the release documentation from a desktop version without update support
- **THEN** the instructions provide a manual upgrade that retains configuration and data and enables subsequent in-app updates

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
  - internal/static/js/ui/desktopUpdate.js
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
### Requirement: Current-user Windows installer

The release SHALL include claude-miniapp-v<VERSION>-windows-amd64-setup.exe alongside the existing ZIP, Linux archive and desktop update executable. All four program assets MUST have SHA256 entries and all five release assets MUST be verified on a draft before publication. Setup MUST install the exact desktop bytes used by the ZIP and update executable without rebuilding them.

#### Scenario: Assemble and publish installation assets
- **WHEN** release assets are assembled
- **THEN** Setup, ZIP, Linux archive, desktop update executable and checksums.txt exist and are verified before publication

#### Scenario: Missing or corrupt Setup
- **WHEN** Setup is missing locally or differs after upload
- **THEN** publication fails and no incomplete release becomes latest


<!-- @trace
source: windows-installer
updated: 2026-11-18
code:
  - internal/static/js/session/SessionView.js
  - docs/plan/todo/system-monitor.md
  - internal/server/server.go
  - README.zh-TW.md
  - docs/reviews/2026-11-17-browserstack-ui.md
  - internal/db/attachment.go
  - internal/static/js/hooks/useRoomTimeline.js
  - internal/static/js/ui/SessionTabs.js
  - go.mod
  - docs/plan/todo/telegram-optional.md
  - internal/db/room_workflow.go
  - scripts/browser/requirements.txt
  - scripts/file-browser.test.cjs
  - scripts/browser/requirements-visual.txt
  - internal/static/js/core/core.js
  - scripts/system-stats.test.cjs
  - internal/mcp/tools.go
  - docs/plan/todo/browserstack-automation.md
  - internal/db/session.go
  - internal/static/js/share/ShareModal.js
  - scripts/console-usability.test.cjs
  - internal/static/js/chat/GitControls.js
  - scripts/rooms.test.cjs
  - go.sum
  - internal/server/room.go
  - internal/static/js/chat/ChatView.js
  - scripts/release/prepare-webview2.ps1
  - internal/static/js/session/RoomMessageBubble.js
  - cmd/desktop/install_windows.go
  - internal/api/system.go
  - docs/plan/todo.md
  - scripts/browser/check_fixtures.py
  - docs/plan/todo/turn-token-usage.md
  - internal/static/js/session/RoomFormModal.js
  - scripts/tab-pinning.test.py
  - docs/getting-started.zh-TW.md
  - internal/mcp/room.go
  - internal/fsbrowse/platform_other.go
  - internal/api/settings.go
  - .github/workflows/release.yml
  - internal/gitinfo/gitops.go
  - build.bat
  - docs/plan/todo/ux-feedback.md
  - cmd/desktop/main_windows.go
  - docs/reviews/2026-11-18-windows-installer.md
  - internal/api/files.go
  - internal/db/room_timeline.go
  - internal/sysmon/procfs.go
  - internal/static/js/session/FileBrowserModal.js
  - internal/sysmon/sysmon_windows.go
  - internal/ws/instruction.go
  - .github/workflows/browser-tests.yml
  - scripts/browser/conftest.py
  - internal/static/js/session/RoomView.js
  - internal/db/global_instruction.go
  - scripts/release/windows-installer.iss
  - README.md
  - scripts/browser/requirements-browserstack.txt
  - scripts/browser/tls_proxy.go
  - internal/api/rooms.go
  - browserstack.yml
  - config.example.yaml
  - docs/plan/todo/windows-installer.md
  - cmd/desktop/desktop_windows_amd64.syso
  - internal/static/js/hooks/systemStats.js
  - internal/sysmon/sysmon.go
  - .github/workflows/ci.yml
  - internal/ws/handoff.go
  - internal/mcp/server.go
  - internal/fsbrowse/fsbrowse.go
  - internal/static/js/ui/SystemStatsSection.js
  - internal/db/db.go
  - internal/static/index.html
  - internal/ws/quota_guard.go
  - scripts/browser/README.md
  - internal/db/room.go
  - scripts/release/version-resource.py
  - scripts/mobile-settings.test.py
  - internal/api/session.go
  - internal/cursor/runner.go
  - internal/static/js/session/RoomControls.js
  - internal/config/config.go
  - docs/plan/todo/file-explorer.md
  - internal/static/js/ui/SettingsModal.js
  - internal/ws/claudeperm.go
  - internal/ws/handler.go
  - internal/static/js/hooks/fileBrowser.js
  - internal/sysmon/sysmon_linux.go
  - internal/db/input_receipt.go
  - internal/ws/dispatch.go
  - docs/plan/todo/supervisor-room.md
  - internal/static/js/hooks/messageRows.js
  - internal/static/js/session/RoomsSection.js
  - cmd/desktop/desktop.rc
  - internal/static/js/hooks/roomPoller.js
  - internal/static/js/hooks/rooms.js
  - .github/release-header.md
  - internal/sysmon/sysmon_other.go
  - internal/static/js/chat/ChatHeader.js
  - scripts/release/package.sh
  - docs/plan/roadmap.md
  - internal/static/js/chat/AttachmentCards.js
  - internal/fsbrowse/platform_windows.go
  - internal/static/js/app.js
  - docs/getting-started.md
  - internal/agent/runner.go
  - scripts/release/publish.sh
tests:
  - internal/gitinfo/gitops_test.go
  - internal/fsbrowse/fsbrowse_test.go
  - internal/desktopupdate/release_contract_test.go
  - scripts/release/test/check_workflow.py
  - internal/desktopupdate/helper_startup_windows_test.go
  - internal/fsbrowse/fsbrowse_windows_test.go
  - scripts/browser/test_ui_review.py
  - internal/sysmon/sysmon_test.go
  - internal/server/guest_test.go
  - scripts/release/test/test-release.sh
  - cmd/desktop/install_windows_test.go
  - internal/api/git_test.go
  - internal/db/room_test.go
  - internal/api/system_test.go
  - scripts/browser/test_regression.py
  - internal/arch/arch_test.go
  - internal/server/rooms_test.go
  - internal/db/room_workflow_test.go
  - internal/sysmon/sysmon_windows_test.go
  - internal/api/rooms_test.go
  - internal/ws/session_instruction_test.go
  - scripts/release/test/installer-smoke.ps1
  - internal/api/session_instruction_test.go
  - internal/ws/quota_guard_test.go
  - cmd/desktop/icon_windows_amd64_test.go
  - internal/server/files_test.go
  - internal/config/config_test.go
  - internal/ws/claudeperm_test.go
  - internal/api/files_test.go
  - internal/server/system_stats_test.go
-->

---
### Requirement: Installed desktop data isolation

An installed.flag beside the desktop executable SHALL select installed mode. Installed mode MUST use %LOCALAPPDATA%\ClaudeMiniApp\data as working directory before configuration, logging and DB initialization, and its webview2 subdirectory as a stable WebView2 profile. Missing LOCALAPPDATA or filesystem failures MUST stop startup with an error. Without the flag the existing portable and development behavior SHALL remain unchanged.

#### Scenario: First installed startup
- **WHEN** the marked executable starts from an unrelated directory
- **THEN** setup creates configuration in the fixed data directory and profile storage is independent of the executable version

#### Scenario: Invalid data location
- **WHEN** the installed data directory cannot be created or entered
- **THEN** startup reports failure without writing configuration or opening a DB in the launch directory

#### Scenario: Portable startup
- **WHEN** the executable has no installed.flag
- **THEN** existing configuration selection and development startup remain unchanged


<!-- @trace
source: windows-installer
updated: 2026-11-18
code:
  - internal/static/js/session/SessionView.js
  - docs/plan/todo/system-monitor.md
  - internal/server/server.go
  - README.zh-TW.md
  - docs/reviews/2026-11-17-browserstack-ui.md
  - internal/db/attachment.go
  - internal/static/js/hooks/useRoomTimeline.js
  - internal/static/js/ui/SessionTabs.js
  - go.mod
  - docs/plan/todo/telegram-optional.md
  - internal/db/room_workflow.go
  - scripts/browser/requirements.txt
  - scripts/file-browser.test.cjs
  - scripts/browser/requirements-visual.txt
  - internal/static/js/core/core.js
  - scripts/system-stats.test.cjs
  - internal/mcp/tools.go
  - docs/plan/todo/browserstack-automation.md
  - internal/db/session.go
  - internal/static/js/share/ShareModal.js
  - scripts/console-usability.test.cjs
  - internal/static/js/chat/GitControls.js
  - scripts/rooms.test.cjs
  - go.sum
  - internal/server/room.go
  - internal/static/js/chat/ChatView.js
  - scripts/release/prepare-webview2.ps1
  - internal/static/js/session/RoomMessageBubble.js
  - cmd/desktop/install_windows.go
  - internal/api/system.go
  - docs/plan/todo.md
  - scripts/browser/check_fixtures.py
  - docs/plan/todo/turn-token-usage.md
  - internal/static/js/session/RoomFormModal.js
  - scripts/tab-pinning.test.py
  - docs/getting-started.zh-TW.md
  - internal/mcp/room.go
  - internal/fsbrowse/platform_other.go
  - internal/api/settings.go
  - .github/workflows/release.yml
  - internal/gitinfo/gitops.go
  - build.bat
  - docs/plan/todo/ux-feedback.md
  - cmd/desktop/main_windows.go
  - docs/reviews/2026-11-18-windows-installer.md
  - internal/api/files.go
  - internal/db/room_timeline.go
  - internal/sysmon/procfs.go
  - internal/static/js/session/FileBrowserModal.js
  - internal/sysmon/sysmon_windows.go
  - internal/ws/instruction.go
  - .github/workflows/browser-tests.yml
  - scripts/browser/conftest.py
  - internal/static/js/session/RoomView.js
  - internal/db/global_instruction.go
  - scripts/release/windows-installer.iss
  - README.md
  - scripts/browser/requirements-browserstack.txt
  - scripts/browser/tls_proxy.go
  - internal/api/rooms.go
  - browserstack.yml
  - config.example.yaml
  - docs/plan/todo/windows-installer.md
  - cmd/desktop/desktop_windows_amd64.syso
  - internal/static/js/hooks/systemStats.js
  - internal/sysmon/sysmon.go
  - .github/workflows/ci.yml
  - internal/ws/handoff.go
  - internal/mcp/server.go
  - internal/fsbrowse/fsbrowse.go
  - internal/static/js/ui/SystemStatsSection.js
  - internal/db/db.go
  - internal/static/index.html
  - internal/ws/quota_guard.go
  - scripts/browser/README.md
  - internal/db/room.go
  - scripts/release/version-resource.py
  - scripts/mobile-settings.test.py
  - internal/api/session.go
  - internal/cursor/runner.go
  - internal/static/js/session/RoomControls.js
  - internal/config/config.go
  - docs/plan/todo/file-explorer.md
  - internal/static/js/ui/SettingsModal.js
  - internal/ws/claudeperm.go
  - internal/ws/handler.go
  - internal/static/js/hooks/fileBrowser.js
  - internal/sysmon/sysmon_linux.go
  - internal/db/input_receipt.go
  - internal/ws/dispatch.go
  - docs/plan/todo/supervisor-room.md
  - internal/static/js/hooks/messageRows.js
  - internal/static/js/session/RoomsSection.js
  - cmd/desktop/desktop.rc
  - internal/static/js/hooks/roomPoller.js
  - internal/static/js/hooks/rooms.js
  - .github/release-header.md
  - internal/sysmon/sysmon_other.go
  - internal/static/js/chat/ChatHeader.js
  - scripts/release/package.sh
  - docs/plan/roadmap.md
  - internal/static/js/chat/AttachmentCards.js
  - internal/fsbrowse/platform_windows.go
  - internal/static/js/app.js
  - docs/getting-started.md
  - internal/agent/runner.go
  - scripts/release/publish.sh
tests:
  - internal/gitinfo/gitops_test.go
  - internal/fsbrowse/fsbrowse_test.go
  - internal/desktopupdate/release_contract_test.go
  - scripts/release/test/check_workflow.py
  - internal/desktopupdate/helper_startup_windows_test.go
  - internal/fsbrowse/fsbrowse_windows_test.go
  - scripts/browser/test_ui_review.py
  - internal/sysmon/sysmon_test.go
  - internal/server/guest_test.go
  - scripts/release/test/test-release.sh
  - cmd/desktop/install_windows_test.go
  - internal/api/git_test.go
  - internal/db/room_test.go
  - internal/api/system_test.go
  - scripts/browser/test_regression.py
  - internal/arch/arch_test.go
  - internal/server/rooms_test.go
  - internal/db/room_workflow_test.go
  - internal/sysmon/sysmon_windows_test.go
  - internal/api/rooms_test.go
  - internal/ws/session_instruction_test.go
  - scripts/release/test/installer-smoke.ps1
  - internal/api/session_instruction_test.go
  - internal/ws/quota_guard_test.go
  - cmd/desktop/icon_windows_amd64_test.go
  - internal/server/files_test.go
  - internal/config/config_test.go
  - internal/ws/claudeperm_test.go
  - internal/api/files_test.go
  - internal/server/system_stats_test.go
-->

---
### Requirement: Safe installer maintenance

Setup SHALL install for the current user without elevation in %LOCALAPPDATA%\Programs\ClaudeMiniApp, create a start-menu shortcut, offer an optional desktop shortcut and register an uninstaller with a stable AppId. Setup MUST refuse downgrade of a newer installed desktop and refuse replacement if an existing executable version cannot be determined. Setup and uninstall MUST refuse maintenance while a desktop process is active or managed files are locked. Desktop startup MUST refuse while maintenance holds its mutex. Maintenance MUST NOT kill workloads or schedule replacement on reboot. Uninstall MUST retain the separate data directory and unknown files.

#### Scenario: Install and remove
- **WHEN** a user installs and subsequently uninstalls Setup
- **THEN** program files and shortcuts are removed while configuration, DB, attachments and profile remain

#### Scenario: Running or newer application
- **WHEN** maintenance encounters an active desktop or locked files, or Setup encounters an unknown executable version or a newer version
- **THEN** the conflicting operation is refused without replacing the desktop or changing user data

#### Scenario: Launch during maintenance
- **WHEN** desktop startup detects the maintenance mutex
- **THEN** startup stops before initializing logging or opening the DB


<!-- @trace
source: windows-installer
updated: 2026-11-18
code:
  - internal/static/js/session/SessionView.js
  - docs/plan/todo/system-monitor.md
  - internal/server/server.go
  - README.zh-TW.md
  - docs/reviews/2026-11-17-browserstack-ui.md
  - internal/db/attachment.go
  - internal/static/js/hooks/useRoomTimeline.js
  - internal/static/js/ui/SessionTabs.js
  - go.mod
  - docs/plan/todo/telegram-optional.md
  - internal/db/room_workflow.go
  - scripts/browser/requirements.txt
  - scripts/file-browser.test.cjs
  - scripts/browser/requirements-visual.txt
  - internal/static/js/core/core.js
  - scripts/system-stats.test.cjs
  - internal/mcp/tools.go
  - docs/plan/todo/browserstack-automation.md
  - internal/db/session.go
  - internal/static/js/share/ShareModal.js
  - scripts/console-usability.test.cjs
  - internal/static/js/chat/GitControls.js
  - scripts/rooms.test.cjs
  - go.sum
  - internal/server/room.go
  - internal/static/js/chat/ChatView.js
  - scripts/release/prepare-webview2.ps1
  - internal/static/js/session/RoomMessageBubble.js
  - cmd/desktop/install_windows.go
  - internal/api/system.go
  - docs/plan/todo.md
  - scripts/browser/check_fixtures.py
  - docs/plan/todo/turn-token-usage.md
  - internal/static/js/session/RoomFormModal.js
  - scripts/tab-pinning.test.py
  - docs/getting-started.zh-TW.md
  - internal/mcp/room.go
  - internal/fsbrowse/platform_other.go
  - internal/api/settings.go
  - .github/workflows/release.yml
  - internal/gitinfo/gitops.go
  - build.bat
  - docs/plan/todo/ux-feedback.md
  - cmd/desktop/main_windows.go
  - docs/reviews/2026-11-18-windows-installer.md
  - internal/api/files.go
  - internal/db/room_timeline.go
  - internal/sysmon/procfs.go
  - internal/static/js/session/FileBrowserModal.js
  - internal/sysmon/sysmon_windows.go
  - internal/ws/instruction.go
  - .github/workflows/browser-tests.yml
  - scripts/browser/conftest.py
  - internal/static/js/session/RoomView.js
  - internal/db/global_instruction.go
  - scripts/release/windows-installer.iss
  - README.md
  - scripts/browser/requirements-browserstack.txt
  - scripts/browser/tls_proxy.go
  - internal/api/rooms.go
  - browserstack.yml
  - config.example.yaml
  - docs/plan/todo/windows-installer.md
  - cmd/desktop/desktop_windows_amd64.syso
  - internal/static/js/hooks/systemStats.js
  - internal/sysmon/sysmon.go
  - .github/workflows/ci.yml
  - internal/ws/handoff.go
  - internal/mcp/server.go
  - internal/fsbrowse/fsbrowse.go
  - internal/static/js/ui/SystemStatsSection.js
  - internal/db/db.go
  - internal/static/index.html
  - internal/ws/quota_guard.go
  - scripts/browser/README.md
  - internal/db/room.go
  - scripts/release/version-resource.py
  - scripts/mobile-settings.test.py
  - internal/api/session.go
  - internal/cursor/runner.go
  - internal/static/js/session/RoomControls.js
  - internal/config/config.go
  - docs/plan/todo/file-explorer.md
  - internal/static/js/ui/SettingsModal.js
  - internal/ws/claudeperm.go
  - internal/ws/handler.go
  - internal/static/js/hooks/fileBrowser.js
  - internal/sysmon/sysmon_linux.go
  - internal/db/input_receipt.go
  - internal/ws/dispatch.go
  - docs/plan/todo/supervisor-room.md
  - internal/static/js/hooks/messageRows.js
  - internal/static/js/session/RoomsSection.js
  - cmd/desktop/desktop.rc
  - internal/static/js/hooks/roomPoller.js
  - internal/static/js/hooks/rooms.js
  - .github/release-header.md
  - internal/sysmon/sysmon_other.go
  - internal/static/js/chat/ChatHeader.js
  - scripts/release/package.sh
  - docs/plan/roadmap.md
  - internal/static/js/chat/AttachmentCards.js
  - internal/fsbrowse/platform_windows.go
  - internal/static/js/app.js
  - docs/getting-started.md
  - internal/agent/runner.go
  - scripts/release/publish.sh
tests:
  - internal/gitinfo/gitops_test.go
  - internal/fsbrowse/fsbrowse_test.go
  - internal/desktopupdate/release_contract_test.go
  - scripts/release/test/check_workflow.py
  - internal/desktopupdate/helper_startup_windows_test.go
  - internal/fsbrowse/fsbrowse_windows_test.go
  - scripts/browser/test_ui_review.py
  - internal/sysmon/sysmon_test.go
  - internal/server/guest_test.go
  - scripts/release/test/test-release.sh
  - cmd/desktop/install_windows_test.go
  - internal/api/git_test.go
  - internal/db/room_test.go
  - internal/api/system_test.go
  - scripts/browser/test_regression.py
  - internal/arch/arch_test.go
  - internal/server/rooms_test.go
  - internal/db/room_workflow_test.go
  - internal/sysmon/sysmon_windows_test.go
  - internal/api/rooms_test.go
  - internal/ws/session_instruction_test.go
  - scripts/release/test/installer-smoke.ps1
  - internal/api/session_instruction_test.go
  - internal/ws/quota_guard_test.go
  - cmd/desktop/icon_windows_amd64_test.go
  - internal/server/files_test.go
  - internal/config/config_test.go
  - internal/ws/claudeperm_test.go
  - internal/api/files_test.go
  - internal/server/system_stats_test.go
-->

---
### Requirement: Verified WebView2 prerequisite

Setup SHALL detect the Evergreen WebView2 Runtime in HKCU and HKLM using a nonzero pv version. If absent, Setup MUST run the bundled official Microsoft bootstrapper and recheck the Runtime. Packaging MUST reject a bootstrapper lacking a valid Microsoft Corporation Authenticode signature. Failed installation or a still-missing Runtime MUST surface an actionable error.

#### Scenario: Runtime already present
- **WHEN** a valid Runtime is installed
- **THEN** Setup skips running the bootstrapper

#### Scenario: Runtime absent or installation fails
- **WHEN** no valid Runtime is present
- **THEN** Setup runs the verified bootstrapper and refuses completion if the Runtime remains unavailable

#### Scenario: Untrusted prerequisite
- **WHEN** the downloaded bootstrapper signature is invalid or its signer is not Microsoft Corporation
- **THEN** packaging fails before producing release assets

<!-- @trace
source: windows-installer
updated: 2026-11-18
code:
  - internal/static/js/session/SessionView.js
  - docs/plan/todo/system-monitor.md
  - internal/server/server.go
  - README.zh-TW.md
  - docs/reviews/2026-11-17-browserstack-ui.md
  - internal/db/attachment.go
  - internal/static/js/hooks/useRoomTimeline.js
  - internal/static/js/ui/SessionTabs.js
  - go.mod
  - docs/plan/todo/telegram-optional.md
  - internal/db/room_workflow.go
  - scripts/browser/requirements.txt
  - scripts/file-browser.test.cjs
  - scripts/browser/requirements-visual.txt
  - internal/static/js/core/core.js
  - scripts/system-stats.test.cjs
  - internal/mcp/tools.go
  - docs/plan/todo/browserstack-automation.md
  - internal/db/session.go
  - internal/static/js/share/ShareModal.js
  - scripts/console-usability.test.cjs
  - internal/static/js/chat/GitControls.js
  - scripts/rooms.test.cjs
  - go.sum
  - internal/server/room.go
  - internal/static/js/chat/ChatView.js
  - scripts/release/prepare-webview2.ps1
  - internal/static/js/session/RoomMessageBubble.js
  - cmd/desktop/install_windows.go
  - internal/api/system.go
  - docs/plan/todo.md
  - scripts/browser/check_fixtures.py
  - docs/plan/todo/turn-token-usage.md
  - internal/static/js/session/RoomFormModal.js
  - scripts/tab-pinning.test.py
  - docs/getting-started.zh-TW.md
  - internal/mcp/room.go
  - internal/fsbrowse/platform_other.go
  - internal/api/settings.go
  - .github/workflows/release.yml
  - internal/gitinfo/gitops.go
  - build.bat
  - docs/plan/todo/ux-feedback.md
  - cmd/desktop/main_windows.go
  - docs/reviews/2026-11-18-windows-installer.md
  - internal/api/files.go
  - internal/db/room_timeline.go
  - internal/sysmon/procfs.go
  - internal/static/js/session/FileBrowserModal.js
  - internal/sysmon/sysmon_windows.go
  - internal/ws/instruction.go
  - .github/workflows/browser-tests.yml
  - scripts/browser/conftest.py
  - internal/static/js/session/RoomView.js
  - internal/db/global_instruction.go
  - scripts/release/windows-installer.iss
  - README.md
  - scripts/browser/requirements-browserstack.txt
  - scripts/browser/tls_proxy.go
  - internal/api/rooms.go
  - browserstack.yml
  - config.example.yaml
  - docs/plan/todo/windows-installer.md
  - cmd/desktop/desktop_windows_amd64.syso
  - internal/static/js/hooks/systemStats.js
  - internal/sysmon/sysmon.go
  - .github/workflows/ci.yml
  - internal/ws/handoff.go
  - internal/mcp/server.go
  - internal/fsbrowse/fsbrowse.go
  - internal/static/js/ui/SystemStatsSection.js
  - internal/db/db.go
  - internal/static/index.html
  - internal/ws/quota_guard.go
  - scripts/browser/README.md
  - internal/db/room.go
  - scripts/release/version-resource.py
  - scripts/mobile-settings.test.py
  - internal/api/session.go
  - internal/cursor/runner.go
  - internal/static/js/session/RoomControls.js
  - internal/config/config.go
  - docs/plan/todo/file-explorer.md
  - internal/static/js/ui/SettingsModal.js
  - internal/ws/claudeperm.go
  - internal/ws/handler.go
  - internal/static/js/hooks/fileBrowser.js
  - internal/sysmon/sysmon_linux.go
  - internal/db/input_receipt.go
  - internal/ws/dispatch.go
  - docs/plan/todo/supervisor-room.md
  - internal/static/js/hooks/messageRows.js
  - internal/static/js/session/RoomsSection.js
  - cmd/desktop/desktop.rc
  - internal/static/js/hooks/roomPoller.js
  - internal/static/js/hooks/rooms.js
  - .github/release-header.md
  - internal/sysmon/sysmon_other.go
  - internal/static/js/chat/ChatHeader.js
  - scripts/release/package.sh
  - docs/plan/roadmap.md
  - internal/static/js/chat/AttachmentCards.js
  - internal/fsbrowse/platform_windows.go
  - internal/static/js/app.js
  - docs/getting-started.md
  - internal/agent/runner.go
  - scripts/release/publish.sh
tests:
  - internal/gitinfo/gitops_test.go
  - internal/fsbrowse/fsbrowse_test.go
  - internal/desktopupdate/release_contract_test.go
  - scripts/release/test/check_workflow.py
  - internal/desktopupdate/helper_startup_windows_test.go
  - internal/fsbrowse/fsbrowse_windows_test.go
  - scripts/browser/test_ui_review.py
  - internal/sysmon/sysmon_test.go
  - internal/server/guest_test.go
  - scripts/release/test/test-release.sh
  - cmd/desktop/install_windows_test.go
  - internal/api/git_test.go
  - internal/db/room_test.go
  - internal/api/system_test.go
  - scripts/browser/test_regression.py
  - internal/arch/arch_test.go
  - internal/server/rooms_test.go
  - internal/db/room_workflow_test.go
  - internal/sysmon/sysmon_windows_test.go
  - internal/api/rooms_test.go
  - internal/ws/session_instruction_test.go
  - scripts/release/test/installer-smoke.ps1
  - internal/api/session_instruction_test.go
  - internal/ws/quota_guard_test.go
  - cmd/desktop/icon_windows_amd64_test.go
  - internal/server/files_test.go
  - internal/config/config_test.go
  - internal/ws/claudeperm_test.go
  - internal/api/files_test.go
  - internal/server/system_stats_test.go
-->