<#
.SYNOPSIS
  模擬 Go exec stdout pipe：agy --print 在非 TTY 下是否有輸出（Issue #76）
#>
param(
    [string]$Prompt = "Reply with exactly one word: PONG",
    [string]$AgyBin = $(if ($env:CC_AGY_BIN) { $env:CC_AGY_BIN } else { "agy" }),
    [string]$PrintTimeout = "45s"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

Write-Host "=== Antigravity headless pipe probe ===" -ForegroundColor Cyan
Write-Host "Prompt: $Prompt"

if (-not (Get-Command $AgyBin -ErrorAction SilentlyContinue)) {
    Write-Host "SKIP: agy not installed" -ForegroundColor Yellow
    exit 2
}

$outFile = [System.IO.Path]::GetTempFileName()
$errFile = [System.IO.Path]::GetTempFileName()

try {
    $prevEap = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    # stdin 餵 prompt（對齊 Go runner）
    $Prompt | & $AgyBin --print --print-timeout $PrintTimeout --dangerously-skip-permissions 1> $outFile 2> $errFile
    $exitCode = $LASTEXITCODE
    $ErrorActionPreference = $prevEap

    $stdout = Get-Content $outFile -Raw -ErrorAction SilentlyContinue
    $stderr = Get-Content $errFile -Raw -ErrorAction SilentlyContinue
    if ($null -eq $stdout) { $stdout = "" }

    Write-Host "Exit code: $exitCode"
    Write-Host "Stdout bytes: $($stdout.Length)"
    if ($stderr) {
        Write-Host "--- STDERR ---" -ForegroundColor Yellow
        Write-Host $stderr
    }
    if ($stdout) {
        Write-Host "--- STDOUT ---" -ForegroundColor Green
        Write-Host $stdout
        Write-Host "PASS: headless stdout non-empty" -ForegroundColor Green
        exit 0
    }

    Write-Host "FAIL: empty stdout (likely Issue #76 non-TTY drop)" -ForegroundColor Red
    Write-Host "See: https://github.com/google-antigravity/antigravity-cli/issues/76"
    exit 1
} finally {
    Remove-Item $outFile, $errFile -ErrorAction SilentlyContinue
}
