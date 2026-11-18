; Inno Setup 6.7.1. Inputs are supplied by package.sh; no desktop rebuild occurs here.
#ifndef AppVersion
  #error AppVersion is required
#endif
#ifndef SourceDir
  #error SourceDir is required
#endif
#ifndef OutputDir
  #error OutputDir is required
#endif
#ifndef BootstrapperFile
  #error BootstrapperFile is required
#endif
#ifdef SmokeTestRoot
  #ifndef SmokeTestId
    #error SmokeTestId is required with SmokeTestRoot
  #endif
  #define ProductId SmokeTestId
  #define InstallDir SmokeTestRoot + "\app"
  #define DesktopMutex "Local\" + SmokeTestId + ".Desktop"
  #define LegacyDesktopMutex "Local\" + SmokeTestId + ".LegacyDesktop"
  #define MaintenanceMutex "Local\" + SmokeTestId + ".Setup"
  #define RuntimeKey "Software\" + SmokeTestId + "\WebView2"
#else
  #define ProductId "ClaudeMiniApp"
  #define InstallDir "{localappdata}\Programs\ClaudeMiniApp"
  #define DesktopMutex "Local\ClaudeMiniApp.Desktop"
  ; Wails beta.26's SingleInstance.UniqueID mutex also covers older portable builds.
  #define LegacyDesktopMutex "wails-app-com.jerry12122.claude-miniapp-sim"
  #define MaintenanceMutex "Local\ClaudeMiniApp.Setup"
  #define RuntimeKey "Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}"
#endif
#define DesktopExe "claude-miniapp-desktop.exe"
#define SourceExe SourceDir + "\" + DesktopExe
#if GetVersionNumbersString(SourceExe) != AppVersion + ".0"
  #error Desktop executable file version must match AppVersion.0
#endif

[Setup]
AppId={#ProductId}
AppName=Claude MiniApp
AppVersion={#AppVersion}
VersionInfoVersion={#AppVersion}
AppPublisher=Claude MiniApp
DefaultDirName={#InstallDir}
UsePreviousAppDir=no
DisableDirPage=yes
DefaultGroupName={#ProductId}
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0
OutputDir={#OutputDir}
OutputBaseFilename=claude-miniapp-v{#AppVersion}-windows-amd64-setup
Compression=lzma2
SolidCompression=yes
CloseApplications=no
RestartApplications=no
RestartIfNeededByRun=no
AlwaysRestart=no
UninstallDisplayIcon={app}\{#DesktopExe}
LicenseFile=..\..\LICENSE

[Tasks]
Name: desktopicon; Description: "Create a desktop shortcut"; Flags: unchecked

[Files]
Source: "{#SourceExe}"; DestDir: "{app}"; Flags: ignoreversion; AfterInstall: WriteInstalledFlag
Source: "..\..\LICENSE"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#BootstrapperFile}"; DestName: "MicrosoftEdgeWebview2Setup.exe"; Flags: dontcopy

[Icons]
Name: "{userprograms}\{#ProductId}\Claude MiniApp"; Filename: "{app}\{#DesktopExe}"
Name: "{userdesktop}\{#ProductId}"; Filename: "{app}\{#DesktopExe}"; Tasks: desktopicon

[UninstallDelete]
Type: files; Name: "{app}\installed.flag"

[Code]
const
  DesktopMutex = '{#DesktopMutex}';
  LegacyDesktopMutex = '{#LegacyDesktopMutex}';
  MaintenanceMutex = '{#MaintenanceMutex}';
  RuntimeKey = '{#RuntimeKey}';
var
  MaintenanceHandle: THandle;

function CreateMutexW(Attributes: Integer; InitialOwner: Boolean; Name: String): THandle;
  external 'CreateMutexW@kernel32.dll stdcall';
function OpenMutexW(Access: Cardinal; Inherit: Boolean; Name: String): THandle;
  external 'OpenMutexW@kernel32.dll stdcall';
function CloseHandle(Handle: THandle): Boolean;
  external 'CloseHandle@kernel32.dll stdcall';
function GetLastError: Cardinal;
  external 'GetLastError@kernel32.dll stdcall';
function CreateFileW(Name: String; Access, Share: Cardinal; Security: Integer;
  Creation, Flags: Cardinal; Template: THandle): THandle;
  external 'CreateFileW@kernel32.dll stdcall';

function MutexAbsent(const Name: String): Boolean;
var Handle: THandle; ErrorCode: Cardinal;
begin
  Handle := OpenMutexW($00100000, False, Name);
  ErrorCode := GetLastError;
  if Handle <> 0 then begin
    CloseHandle(Handle);
    Result := False;
  end else
    Result := ErrorCode = 2; { Only FILE_NOT_FOUND proves absence. }
end;

function DesktopIsStopped: Boolean;
begin
  Result := MutexAbsent(DesktopMutex) and MutexAbsent(LegacyDesktopMutex);
end;

function BeginMaintenance: Boolean;
var ErrorCode: Cardinal;
begin
  MaintenanceHandle := CreateMutexW(0, False, MaintenanceMutex);
  ErrorCode := GetLastError;
  Result := (MaintenanceHandle <> 0) and (ErrorCode = 0);
  if Result then Result := DesktopIsStopped;
  if not Result then begin
    if MaintenanceHandle <> 0 then begin CloseHandle(MaintenanceHandle); MaintenanceHandle := 0; end;
    SuppressibleMsgBox('Close Claude MiniApp and any other installer, then try again. Maintenance could not safely begin.', mbError, MB_OK, IDOK);
  end;
end;

function FileUnlocked(const Name: String): Boolean;
var Handle: THandle;
begin
  Result := False;
  if DirExists(Name) then Exit;
  if not FileExists(Name) then begin Result := True; Exit; end;
  Handle := CreateFileW(Name, $C0000000, 0, 0, 3, $80, 0);
  Result := Handle <> THandle(-1);
  if Result then CloseHandle(Handle);
end;

function ManagedFilesUnlocked(const IsUninstall: Boolean): Boolean;
begin
  Result := FileUnlocked(ExpandConstant('{app}\{#DesktopExe}')) and
    FileUnlocked(ExpandConstant('{app}\LICENSE')) and
    FileUnlocked(ExpandConstant('{app}\installed.flag'));
  { Uninstall already has its own engine/data open; Inno validates those locks. }
  if Result and not IsUninstall then
    Result := FileUnlocked(ExpandConstant('{app}\unins000.exe')) and
      FileUnlocked(ExpandConstant('{app}\unins000.dat'));
end;

function ExistingVersionAllowed: Boolean;
var
  A, B, C, D, NA, NB, NC, ND: Word;
  Incoming: Int64;
  Name: String;
begin
  Name := ExpandConstant('{app}\{#DesktopExe}');
  Result := not FileExists(Name) and not DirExists(Name);
  if Result then Exit;
  Result := GetVersionComponents(Name, A, B, C, D) and
    StrToVersion('{#GetVersionNumbersString(SourceExe)}', Incoming);
  if not Result then Exit;
  UnpackVersionComponents(Incoming, NA, NB, NC, ND);
  { Compare components to avoid signed packed-version ordering above major 32767. }
  if (A = 0) and (B = 0) and (C = 0) and (D = 0) then Result := False
  else if A <> NA then Result := A < NA
  else if B <> NB then Result := B < NB
  else if C <> NC then Result := C < NC
  else Result := D <= ND;
end;
function ValidRuntimeVersion(const Version: String): Boolean;
var Packed: Int64; I, Dots: Integer;
begin
  Result := False;
  Dots := 0;
  for I := 1 to Length(Version) do begin
    if Version[I] = '.' then Dots := Dots + 1
    else if (Version[I] < '0') or (Version[I] > '9') then Exit;
  end;
  if Dots <> 3 then Exit;
  if StrToVersion(Version, Packed) then Result := Packed <> 0;
end;

function RuntimePresent: Boolean;
var Version: String;
begin
  Result := RegQueryStringValue(HKCU32, RuntimeKey, 'pv', Version);
  if Result then Result := ValidRuntimeVersion(Version);
  if Result then Exit;
  Result := RegQueryStringValue(HKLM32, RuntimeKey, 'pv', Version);
  if Result then Result := ValidRuntimeVersion(Version);
end;
function InitializeSetup: Boolean;
begin
  Result := BeginMaintenance;
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
var ExitCode: Integer;
begin
  Result := '';
  if CompareText(ExpandConstant('{app}'), ExpandConstant('{#InstallDir}')) <> 0 then
    Result := 'The installation directory is fixed for this per-user installation.'
  else if not DesktopIsStopped or not ManagedFilesUnlocked(False) then
    Result := 'Close Claude MiniApp and any program using its installed files, then try again.'
  else if not ExistingVersionAllowed then
    Result := 'The existing executable is newer or its file version is unknown. Back it up and remove it manually before installing.';
  if Result <> '' then Exit;
  if not RuntimePresent then begin
    ExtractTemporaryFile('MicrosoftEdgeWebview2Setup.exe');
    if not Exec(ExpandConstant('{tmp}\MicrosoftEdgeWebview2Setup.exe'), '/silent /install', '', SW_HIDE, ewWaitUntilTerminated, ExitCode) then
      Result := 'WebView2 Runtime installation could not start. Install Microsoft Evergreen WebView2 Runtime and retry.'
    else if (ExitCode <> 0) or not RuntimePresent then
      Result := 'WebView2 Runtime is still unavailable. Check your network, install Microsoft Evergreen WebView2 Runtime and retry.';
  end;
  if (Result = '') and (not DesktopIsStopped or not ManagedFilesUnlocked(False)) then
    Result := 'Close Claude MiniApp and any program using its installed files, then try again.';
end;

procedure WriteInstalledFlag;
begin
  if not SaveStringToFile(ExpandConstant('{app}\installed.flag'), '', False) then
    RaiseException('Could not create installed.flag.');
end;

function InitializeUninstall: Boolean;
begin
  Result := BeginMaintenance;
  if Result then begin
    Result := ManagedFilesUnlocked(True);
    if not Result then
      SuppressibleMsgBox('Close all programs using the installed files, then try uninstalling again.', mbError, MB_OK, IDOK);
  end;
end;

procedure DeinitializeSetup;
begin
  if MaintenanceHandle <> 0 then CloseHandle(MaintenanceHandle);
end;

procedure DeinitializeUninstall;
begin
  if MaintenanceHandle <> 0 then CloseHandle(MaintenanceHandle);
end;

