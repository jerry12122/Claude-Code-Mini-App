<#
.SYNOPSIS
  探測 Codex 帳戶額度來源（/status、/usage slash commands）。
#>
param(
    [string]$WorkDir = $PSScriptRoot,
    [int]$TimeoutSec = 120
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
. (Join-Path $PSScriptRoot "common.ps1")

$samplesDir = Join-Path $PSScriptRoot "samples"
New-Item -ItemType Directory -Force -Path $samplesDir | Out-Null

function Invoke-CodexSlash {
    param([string]$SlashCmd, [string]$Label)
    $outJson = Join-Path $samplesDir "$Label.jsonl"
    $errFile = Join-Path $samplesDir "$Label.stderr"
    $args = @(
        "exec", "--json", "--skip-git-repo-check", "-C", $WorkDir,
        "-s", "read-only", "-c", 'approval_policy="never"',
        $SlashCmd
    )
    Write-Host ""
    Write-Host "=== Probe: $SlashCmd ===" -ForegroundColor Cyan
    $exit = Invoke-CodexExec -CodexArgs $args -WorkDir $WorkDir -OutFile $outJson -ErrFile $errFile
    Write-Host "Exit: $exit"
    $result = Get-AgentMessageFromJsonl -Path $outJson
    if ($result.Text) {
        Write-Host "agent_message:" -ForegroundColor Green
        Write-Host $result.Text
        return $result.Text
    }
    if (Test-Path $errFile) {
        $e = Get-Content $errFile -Raw -ErrorAction SilentlyContinue
        if ($e) { Write-Host "stderr: $e" }
    }
    return ""
}

$statusText = Invoke-CodexSlash -SlashCmd "/status" -Label "quota-status"
$usageText  = Invoke-CodexSlash -SlashCmd "/usage" -Label "quota-usage"

$report = @{
    status_ok = ($statusText -ne "")
    usage_ok  = ($usageText -ne "")
    status_preview = if ($statusText.Length -gt 200) { $statusText.Substring(0, 200) + "..." } else { $statusText }
    usage_preview  = if ($usageText.Length -gt 200) { $usageText.Substring(0, 200) + "..." } else { $usageText }
}
$reportPath = Join-Path $samplesDir "quota-report.json"
$report | ConvertTo-Json | Set-Content $reportPath -Encoding UTF8
Write-Host ""
Write-Host "Report: $reportPath" -ForegroundColor Yellow

if ($statusText -or $usageText) {
    Write-Host "PASS: at least one quota source works" -ForegroundColor Green
    exit 0
}
Write-Host "WARN: no quota text from slash commands; fallback to turn.completed.usage" -ForegroundColor Yellow
exit 0
