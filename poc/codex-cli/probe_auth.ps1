<#
.SYNOPSIS
  驗證 codex CLI 認證狀態。
#>
Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

Write-Host "=== Codex Auth Probe ===" -ForegroundColor Cyan

$outFile = [System.IO.Path]::GetTempFileName()
$errFile = [System.IO.Path]::GetTempFileName()

try {
    $p = Start-Process -FilePath "codex" `
        -ArgumentList "login status" `
        -RedirectStandardOutput $outFile `
        -RedirectStandardError $errFile `
        -NoNewWindow -Wait -PassThru

    $stdout = Get-Content $outFile -Raw -ErrorAction SilentlyContinue
    $stderr = Get-Content $errFile -Raw -ErrorAction SilentlyContinue
    $combined = ($stdout + "`n" + $stderr).Trim()

    Write-Host "Exit code: $($p.ExitCode)"
    if ($combined) {
        Write-Host $combined
    }

    if ($p.ExitCode -ne 0) {
        Write-Host "FAIL: not authenticated" -ForegroundColor Red
        exit 1
    }

    Write-Host "PASS: authenticated" -ForegroundColor Green
    exit 0
} finally {
    Remove-Item $outFile, $errFile -ErrorAction SilentlyContinue
}
