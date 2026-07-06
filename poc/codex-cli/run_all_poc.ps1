<#
.SYNOPSIS
  端對端 Codex 整合 POC runner。
#>
param(
    [string]$WorkDir = $PSScriptRoot
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

Write-Host "======================================" -ForegroundColor Yellow
Write-Host " codex-cli Integration POC Runner"     -ForegroundColor Yellow
Write-Host "======================================" -ForegroundColor Yellow
Write-Host ""

$script = $PSScriptRoot

Write-Host "[0/5] Auth probe..." -ForegroundColor Cyan
& "$script\probe_auth.ps1"
if ($LASTEXITCODE -ne 0) { exit 1 }

Write-Host ""
Write-Host "[1/5] First turn & thread_id capture..." -ForegroundColor Cyan
$threadId = & "$script\capture_thread_id.ps1" -WorkDir $WorkDir -Prompt "reply with exactly: POC_OK"
if (-not $threadId) {
    Write-Host "FAIL: Could not obtain thread_id" -ForegroundColor Red
    exit 1
}
Write-Host "Captured thread_id: $threadId" -ForegroundColor Green

Write-Host ""
Write-Host "[2/5] Resume second turn..." -ForegroundColor Cyan
$ok = & "$script\resume_smoke_test.ps1" -ThreadId $threadId -WorkDir $WorkDir
if (-not $ok) {
    Write-Host "FAIL: Resume failed" -ForegroundColor Red
    exit 1
}

Write-Host ""
Write-Host "[3/5] Parse events..." -ForegroundColor Cyan
& "$script\parse_events.ps1"

Write-Host ""
Write-Host "[4/5] Quota probe..." -ForegroundColor Cyan
& "$script\probe_quota.ps1" -WorkDir $WorkDir

Write-Host ""
Write-Host "======================================" -ForegroundColor Green
Write-Host " ALL POC CHECKS PASSED"                -ForegroundColor Green
Write-Host "======================================" -ForegroundColor Green
exit 0
