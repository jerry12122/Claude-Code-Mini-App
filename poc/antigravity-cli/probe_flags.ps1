<#
.SYNOPSIS
  探測 agy CLI 旗標與版本，寫入 samples/flags.txt
#>
param(
    [string]$AgyBin = $(if ($env:CC_AGY_BIN) { $env:CC_AGY_BIN } else { "agy" })
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$samples = Join-Path $root "samples"
New-Item -ItemType Directory -Force -Path $samples | Out-Null

Write-Host "=== Antigravity CLI flags probe ===" -ForegroundColor Cyan

if (-not (Get-Command $AgyBin -ErrorAction SilentlyContinue)) {
    Write-Host "FAIL: '$AgyBin' not in PATH. Install: irm https://antigravity.google/cli/install.ps1 | iex" -ForegroundColor Red
    exit 2
}

$lines = @()
$lines += "=== version ==="
$lines += (& $AgyBin --version 2>&1 | Out-String).Trim()
$lines += ""
$lines += "=== help ==="
$help = (& $AgyBin --help 2>&1 | Out-String)
$lines += $help
$lines += ""
$lines += "=== stream-json flag present ==="
$lines += [string]($help -match 'output-format|stream-json|stream.json')

$outPath = Join-Path $samples "flags.txt"
$lines -join "`n" | Set-Content -Path $outPath -Encoding UTF8
Write-Host "Wrote $outPath"
Write-Host "PASS" -ForegroundColor Green
exit 0
