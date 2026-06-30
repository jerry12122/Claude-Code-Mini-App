<#
.SYNOPSIS
  若 agy 支援 --output-format stream-json，逐行解析 NDJSON（對齊 internal/antigravity）
#>
param(
    [string]$Prompt = "Say hello in one word",
    [string]$WorkDir = (Get-Location).Path,
    [string]$AgyBin = $(if ($env:CC_AGY_BIN) { $env:CC_AGY_BIN } else { "agy" })
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

Write-Host "=== Antigravity stream-json probe ===" -ForegroundColor Cyan

if (-not (Get-Command $AgyBin -ErrorAction SilentlyContinue)) {
    Write-Host "SKIP: agy not installed" -ForegroundColor Yellow
    exit 2
}

$help = (& $AgyBin --help 2>&1 | Out-String)
if ($help -notmatch 'output-format|stream-json|stream\.json') {
    Write-Host "SKIP: --output-format stream-json not supported in this agy version" -ForegroundColor Yellow
    exit 3
}

$outFile = [System.IO.Path]::GetTempFileName()
$errFile = [System.IO.Path]::GetTempFileName()

try {
    Push-Location $WorkDir
    $prevEap = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    $Prompt | & $AgyBin --output-format stream-json --dangerously-skip-permissions 1> $outFile 2> $errFile
    $exitCode = $LASTEXITCODE
    $ErrorActionPreference = $prevEap

    $stderr = Get-Content $errFile -Raw -ErrorAction SilentlyContinue
    if ($stderr -match 'not defined|unknown flag|output-format') {
        Write-Host "SKIP: --output-format stream-json not supported in this agy version" -ForegroundColor Yellow
        Write-Host $stderr
        exit 3
    }

    Write-Host "Exit code: $exitCode"
    if ($stderr) { Write-Host "STDERR: $stderr" -ForegroundColor Yellow }

    $lines = @(Get-Content $outFile -ErrorAction SilentlyContinue)
    Write-Host "Lines: $($lines.Count)"
    $i = 0
    foreach ($line in $lines) {
        try {
            $obj = $line | ConvertFrom-Json
            Write-Host ("[{0}] type={1}" -f $i, $obj.type)
            if ($obj.session_id) { Write-Host "       session_id=$($obj.session_id)" }
        } catch {
            Write-Host ("[{0}] invalid json" -f $i)
        }
        $i++
    }

    if ($lines.Count -gt 0) {
        Write-Host "PASS" -ForegroundColor Green
        exit 0
    }
    Write-Host "FAIL: no NDJSON lines" -ForegroundColor Red
    exit 1
} finally {
    Pop-Location -ErrorAction SilentlyContinue
    Remove-Item $outFile, $errFile -ErrorAction SilentlyContinue
}
