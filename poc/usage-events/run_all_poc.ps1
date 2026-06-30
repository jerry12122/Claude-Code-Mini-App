<#
.SYNOPSIS
  Usage POC：live 擷取 + Go parser 單元測試。
#>
Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$root = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
Push-Location $root

Write-Host "=== [1/2] Live usage capture ===" -ForegroundColor Cyan
& "$PSScriptRoot\probe_usage.ps1"
$liveCode = $LASTEXITCODE

Write-Host ""
Write-Host "=== [2/2] Go parser tests ===" -ForegroundColor Cyan
go test ./internal/usage/... -v
$testCode = $LASTEXITCODE

Pop-Location
if ($liveCode -ne 0 -or $testCode -ne 0) { exit 1 }
exit 0
