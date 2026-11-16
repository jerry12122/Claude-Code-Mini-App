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