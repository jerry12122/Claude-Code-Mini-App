<#
.SYNOPSIS
  執行 Cursor Agent CLI 全部 POC（離線單元測試 + live stream-json）。
#>
Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$root = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
Push-Location $root

Write-Host "=== [1/2] Go unit tests (offline protocol) ===" -ForegroundColor Cyan
go test ./internal/cursor/... -v
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

Write-Host ""
Write-Host "=== [2/2] Live stream-json POC ===" -ForegroundColor Cyan
& "$PSScriptRoot\run_stream_json.ps1"
$liveCode = $LASTEXITCODE

Pop-Location

if ($liveCode -eq 2) {
    Write-Host ""
    Write-Host "Live POC skipped auth — offline tests passed." -ForegroundColor Yellow
    exit 0
}
exit $liveCode
