<#
.SYNOPSIS
  執行 model POC：live probe + Go parser 測試
#>
param(
    [string]$Prompt = "say hi in one word only",
    [string]$WorkDir = (Join-Path $PSScriptRoot "..\..")
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$root = Resolve-Path (Join-Path $PSScriptRoot "..\..")
Push-Location $root
try {
    Write-Host "=== 1/2 probe_model.ps1 ===" -ForegroundColor Cyan
    & (Join-Path $PSScriptRoot "probe_model.ps1") -Prompt $Prompt -WorkDir $WorkDir
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

    Write-Host ""
    Write-Host "=== 2/2 go test ./internal/model/... ===" -ForegroundColor Cyan
    go test ./internal/model/... -v
    exit $LASTEXITCODE
} finally {
    Pop-Location
}
