## ADDED Requirements

### Requirement: Installed desktop update continuity

Installed desktops SHALL retain the existing explicit EXE-only update flow and helper-first startup ordering. The updater MUST NOT select the Setup asset. A successfully started installed desktop SHALL update DisplayVersion in its existing current-user uninstall entry to the running version. A missing entry MUST NOT be recreated and registry failures MUST be logged without preventing normal use. Updating SHALL preserve installed.flag, shortcuts, the data directory and stable profile.

#### Scenario: Installed desktop self-update
- **WHEN** an installed desktop updates and successfully starts the new executable
- **THEN** the same installation is used, user data and profile remain available, and its existing uninstall entry reports the running version

#### Scenario: Registry unavailable or installation absent
- **WHEN** the uninstall entry is absent or cannot be updated
- **THEN** no entry is created and startup continues, logging an update error when present

#### Scenario: Helper process
- **WHEN** the desktop executable is invoked as an updater helper
- **THEN** helper handling precedes work-directory changes, mutex acquisition, registry writes, logging and server initialization
