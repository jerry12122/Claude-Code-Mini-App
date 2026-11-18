param(
    [string]$IsccPath = 'C:\Program Files (x86)\Inno Setup 6\ISCC.exe',
    [string]$DesktopExe,
    [string]$AppVersion = '1.2.3'
)
$ErrorActionPreference = 'Stop'
$release = Split-Path $PSScriptRoot -Parent
$iss = Join-Path $release 'windows-installer.iss'
if (!(Test-Path -LiteralPath $iss)) { throw 'Installer implementation missing' }
$root = Join-Path ([IO.Path]::GetTempPath()) ('ClaudeMiniAppSmoke-' + [guid]::NewGuid().ToString('N'))
$id = [IO.Path]::GetFileName($root)
$key = "Software\$id\WebView2"
$reg = "HKCU:\$key"
$app = Join-Path $root 'app'
$exe = Join-Path $app 'claude-miniapp-desktop.exe'
$desktopShortcut = Join-Path ([Environment]::GetFolderPath('Desktop')) "$id.lnk"
$shortcutGroup = Join-Path ([Environment]::GetFolderPath('Programs')) $id
$csc = Join-Path $env:WINDIR 'Microsoft.NET\Framework64\v4.0.30319\csc.exe'
function Assert($condition, $message) { if (!$condition) { throw $message } }
function Fixture($name, $version, $body) {
    $cs = Join-Path $root "$name.cs"
    $out = Join-Path $root "$name.exe"
    $attribute = if ($version) { "[assembly: System.Reflection.AssemblyFileVersion(`"$version`" )]" } else { '' }
    Set-Content -LiteralPath $cs -Encoding UTF8 -Value "$attribute class Fixture { static int Main() { $body } }"
    & $csc /nologo /target:exe "/out:$out" $cs
    Assert ($LASTEXITCODE -eq 0) 'C# fixture compile failed'
    return $out
}
function Run($path, $failure, $label, $extra = '') {
    $log = Join-Path $root "$label.log"
    try { $p = Start-Process -FilePath $path -ArgumentList "/VERYSILENT /SUPPRESSMSGBOXES /NORESTART /LOG=`"$log`" $extra" -Wait -PassThru -WindowStyle Hidden } catch { if ($failure -and $label -eq 'locked-uninstall-unins000.exe' -and (Test-Path -LiteralPath $path)) { return }; throw }
    Assert (($p.ExitCode -ne 0) -eq $failure) "$label unexpected exit $($p.ExitCode); see $log"
}
function Compile($bootstrapper) {
    $source = Join-Path $root 'source'
    New-Item -ItemType Directory -Path $source -Force | Out-Null
    Copy-Item -LiteralPath $script:base -Destination (Join-Path $source 'claude-miniapp-desktop.exe')
    & $IsccPath /Qp "/DAppVersion=$AppVersion" "/DSourceDir=$source" "/DOutputDir=$root" "/DBootstrapperFile=$bootstrapper" "/DSmokeTestRoot=$root" "/DSmokeTestId=$id" $iss | Out-Host
    Assert ($LASTEXITCODE -eq 0) 'Inno compile failed'
    return Join-Path $root "claude-miniapp-v$AppVersion-windows-amd64-setup.exe"
}
$passed = $false
try {
    New-Item -ItemType Directory -Path $root | Out-Null
    $base = if ($DesktopExe) { (Resolve-Path -LiteralPath $DesktopExe).Path } else { Fixture 'base' "$AppVersion.0" 'return 0;' }
    $newer = Fixture 'newer' '60000.0.0.0' 'return 0;'
    $zero = Fixture 'zero' '0.0.0.0' 'return 0;'
    $badBootstrap = Fixture 'bad-bootstrapper' '' 'return 1;'
    $noRuntime = Fixture 'no-runtime' '' 'return 0;'
    $bootstrap = Fixture 'bootstrapper' '' "Microsoft.Win32.Registry.CurrentUser.CreateSubKey(@`"$key`" ).SetValue(`"pv`", `"123.0.0.0`" ); return 0;"
    $prepare = Join-Path $release 'prepare-webview2.ps1'
    Assert (Test-Path -LiteralPath $prepare) 'Bootstrapper verification script missing'
    foreach ($candidate in @($badBootstrap, (Join-Path $env:WINDIR 'System32\cmd.exe'))) {
        $validation = Start-Process -FilePath powershell.exe -ArgumentList "-NoProfile -ExecutionPolicy Bypass -File `"$prepare`" -OutputFile `"$candidate`" -ValidateOnly" -Wait -PassThru -WindowStyle Hidden -RedirectStandardOutput (Join-Path $root 'validation.out') -RedirectStandardError (Join-Path $root 'validation.err')
        Assert ($validation.ExitCode -ne 0) 'Unsigned or wrong-signer prerequisite was accepted'
    }
    $setup = Compile $badBootstrap
    Run $setup $true 'prerequisite-failed'
    Assert (!(Test-Path -LiteralPath $exe)) 'Files installed after prerequisite failure'
    $setup = Compile $noRuntime
    Run $setup $true 'prerequisite-not-detected'
    Assert (!(Test-Path -LiteralPath $exe)) 'Files installed without runtime'
    $setup = Compile $bootstrap
    Run $setup $false 'install' '/TASKS=desktopicon'
    Assert (Test-Path -LiteralPath $desktopShortcut) 'Optional desktop shortcut missing'
    Assert (Test-Path -LiteralPath (Join-Path $shortcutGroup 'Claude MiniApp.lnk')) 'Start menu shortcut missing'
    Assert (Test-Path -LiteralPath (Join-Path $app 'installed.flag')) 'Installed marker missing'
    Assert ((Get-FileHash -LiteralPath $exe).Hash -eq (Get-FileHash -LiteralPath $base).Hash) 'Installed EXE differs'
    $uninstall = Join-Path $app 'unins000.exe'
    $unknown = Join-Path $app 'keep.txt'
    Set-Content -LiteralPath $unknown 'keep'
    $data = Join-Path $root 'data'
    New-Item -ItemType Directory -Path $data | Out-Null
    Set-Content -LiteralPath (Join-Path $data 'config.json') 'keep'
    # A present runtime must skip this failing bootstrapper.
    $setup = Compile $badBootstrap
    Run $setup $false 'runtime-present'
    Set-ItemProperty -LiteralPath $reg -Name pv -Value '0.0.0.0'
    Run $setup $true 'runtime-zero'
    Set-ItemProperty -LiteralPath $reg -Name pv -Value 'invalid'
    Run $setup $true 'runtime-invalid'
    Set-ItemProperty -LiteralPath $reg -Name pv -Value '123.0.0.0'
    Run $setup $true 'fixed-directory' ('/DIR="' + (Join-Path $root 'elsewhere') + '"')
    $desktop = [Threading.Mutex]::new($false, "Local\$id.Desktop")
    try { Run $setup $true 'running-install'; Run $uninstall $true 'running-uninstall' } finally { $desktop.Dispose() }
    $legacy = [Threading.Mutex]::new($false, "Local\$id.LegacyDesktop")
    try { Run $setup $true 'legacy-desktop-install'; Run $uninstall $true 'legacy-desktop-uninstall' } finally { $legacy.Dispose() }
    $maintenance = [Threading.Mutex]::new($false, "Local\$id.Setup")
    try { Run $setup $true 'maintenance-install'; Run $uninstall $true 'maintenance-uninstall' } finally { $maintenance.Dispose() }
    foreach ($name in @('claude-miniapp-desktop.exe', 'LICENSE', 'installed.flag', 'unins000.exe', 'unins000.dat')) {
        $lock = [IO.File]::Open((Join-Path $app $name), 'Open', 'Read', 'None')
        try { Run $setup $true ("locked-install-$name" ); Run $uninstall $true ("locked-uninstall-$name" ) } finally { $lock.Dispose() }
    }
    Copy-Item -LiteralPath $newer -Destination $exe -Force
    Run $setup $true 'downgrade'
    Assert ((Get-FileHash -LiteralPath $exe).Hash -eq (Get-FileHash -LiteralPath $newer).Hash) 'Newer EXE replaced'
    Copy-Item -LiteralPath $zero -Destination $exe -Force
    Run $setup $true 'zero-version'
    Set-Content -LiteralPath $exe 'not a versioned executable'
    Run $setup $true 'unknown-version'
    Copy-Item -LiteralPath $base -Destination $exe -Force
    Run $uninstall $false 'uninstall'
    Assert (!(Test-Path -LiteralPath $exe)) 'Managed EXE retained'
    Assert (!(Test-Path -LiteralPath $desktopShortcut)) 'Desktop shortcut retained'
    Assert (!(Test-Path -LiteralPath (Join-Path $shortcutGroup 'Claude MiniApp.lnk'))) 'Start menu shortcut retained'
    Assert (!(Test-Path -LiteralPath (Join-Path $app 'installed.flag'))) 'Managed marker retained'
    Assert (Test-Path -LiteralPath $unknown) 'Unknown file deleted'
    Assert ((Get-Content -LiteralPath (Join-Path $data 'config.json')) -eq 'keep') 'User data changed'
    $passed = $true
    Write-Host 'PASS: isolated install/uninstall, exact bytes, runtime failure/recheck/skip, unsigned prerequisite, mutexes, locks, downgrade, unknown version, data and unknown-file retention'
} finally {
    # Only the unique smoke registry namespace and temp workspace are removed.
    if (Test-Path -LiteralPath $reg) { Remove-Item -LiteralPath ("HKCU:\Software\$id") -Recurse -Force }
    $uninstallKey = "HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\$($id)_is1"
    if (Test-Path -LiteralPath $uninstallKey) { Remove-Item -LiteralPath $uninstallKey -Recurse -Force }
    if (Test-Path -LiteralPath $desktopShortcut) { Remove-Item -LiteralPath $desktopShortcut -Force }
    $programs = [IO.Path]::GetFullPath([Environment]::GetFolderPath('Programs'))
    if (([IO.Path]::GetDirectoryName([IO.Path]::GetFullPath($shortcutGroup)) -eq $programs) -and
        ([IO.Path]::GetFileName($shortcutGroup) -eq $id) -and (Test-Path -LiteralPath $shortcutGroup)) {
        Remove-Item -LiteralPath $shortcutGroup -Recurse -Force
    }
    $resolved = [IO.Path]::GetFullPath($root)
    if ($resolved.StartsWith([IO.Path]::GetFullPath([IO.Path]::GetTempPath()), [StringComparison]::OrdinalIgnoreCase) -and [IO.Path]::GetFileName($resolved) -eq $id) {
        if ($passed) { Remove-Item -LiteralPath $resolved -Recurse -Force } else { Write-Host "Smoke failure diagnostics retained: $resolved" }
    }
}

