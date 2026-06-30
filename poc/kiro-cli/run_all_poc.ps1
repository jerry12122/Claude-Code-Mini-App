<#
.SYNOPSIS
  端對端 POC 驗證：首回合 → 取得 session id → resume 第二輪

.PARAMETER WorkDir
  工作目錄（預設：目前目錄）
#>
param(
    [string]$WorkDir = $PSScriptRoot
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

Write-Host "======================================" -ForegroundColor Yellow
Write-Host " kiro-cli Integration POC Runner"      -ForegroundColor Yellow
Write-Host "======================================" -ForegroundColor Yellow
Write-Host ""

$script = Split-Path $MyInvocation.MyCommand.Path -Parent

# Step 1: First turn + session id capture
Write-Host "[1/2] First turn & session id capture..." -ForegroundColor Cyan
$sessionId = & "$script\capture_session_id.ps1" -WorkDir $WorkDir -Prompt "say hello in one word"

if (-not $sessionId) {
    Write-Host ""
    Write-Host "FAIL: Could not obtain session id" -ForegroundColor Red
    exit 1
}

Write-Host ""
Write-Host "Captured session id: $sessionId" -ForegroundColor Green

# Step 2: Resume second turn
Write-Host ""
Write-Host "[2/2] Resume second turn..." -ForegroundColor Cyan
$ok = & "$script\resume_smoke_test.ps1" -SessionId $sessionId -WorkDir $WorkDir -Prompt "now say goodbye in one word"

if (-not $ok) {
    Write-Host ""
    Write-Host "FAIL: Resume failed" -ForegroundColor Red
    exit 1
}

Write-Host ""
Write-Host "======================================" -ForegroundColor Green
Write-Host " ALL POC CHECKS PASSED"                -ForegroundColor Green
Write-Host "======================================" -ForegroundColor Green
exit 0
