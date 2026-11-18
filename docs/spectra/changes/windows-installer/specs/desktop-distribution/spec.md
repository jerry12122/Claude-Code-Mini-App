## ADDED Requirements

### Requirement: Current-user Windows installer

The release SHALL include claude-miniapp-v<VERSION>-windows-amd64-setup.exe alongside the existing ZIP, Linux archive and desktop update executable. All four program assets MUST have SHA256 entries and all five release assets MUST be verified on a draft before publication. Setup MUST install the exact desktop bytes used by the ZIP and update executable without rebuilding them.

#### Scenario: Assemble and publish installation assets
- **WHEN** release assets are assembled
- **THEN** Setup, ZIP, Linux archive, desktop update executable and checksums.txt exist and are verified before publication

#### Scenario: Missing or corrupt Setup
- **WHEN** Setup is missing locally or differs after upload
- **THEN** publication fails and no incomplete release becomes latest

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
