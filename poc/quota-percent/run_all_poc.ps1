<#
.SYNOPSIS
  帳戶用量 % POC：live 擷取 + Go parser 測試。
#>
Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$root = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
Push-Location $root

Write-Host "=== [1/2] Live quota percent POC ===" -ForegroundColor Cyan
& "$PSScriptRoot\probe_quota.ps1"
$live = $LASTEXITCODE

Write-Host ""
Write-Host "=== [2/2] Go parser tests ===" -ForegroundColor Cyan
go test ./internal/usage/... -v -run "FromClaude|FromCursor|FromKiro|Quota"
$test = $LASTEXITCODE

Pop-Location
if ($test -ne 0) { exit 1 }
if ($live -ne 0) {
    Write-Host ""
    Write-Host "WARN: live capture incomplete ($live) - OAuth 429 or CLI output format may cause transient Claude failures" -ForegroundColor Yellow
}
exit 0
