<#
.SYNOPSIS
  以 codex exec resume 驗證第二輪對話連貫性。
#>
param(
    [Parameter(Mandatory = $true)][string]$ThreadId,
    [string]$WorkDir = $PSScriptRoot,
    [string]$Prompt = "what was my previous one-word reply? answer in one word only",
    [int]$TimeoutSec = 120
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
. (Join-Path $PSScriptRoot "common.ps1")

$samplesDir = Join-Path $PSScriptRoot "samples"
New-Item -ItemType Directory -Force -Path $samplesDir | Out-Null

$outFile = Join-Path $samplesDir "resume.jsonl"
$errFile = Join-Path $samplesDir "resume.stderr"

Write-Host "=== Resume smoke test ===" -ForegroundColor Cyan
Write-Host "thread_id: $ThreadId"

$args = @(
    "exec", "resume", $ThreadId, "--json", "--skip-git-repo-check",
    "-c", 'approval_policy="never"',
    $Prompt
)

$exit = Invoke-CodexExec -CodexArgs $args -WorkDir $WorkDir -OutFile $outFile -ErrFile $errFile
if ($exit -ne 0) {
    if (Test-Path $errFile) { Write-Host (Get-Content $errFile -Raw) }
    Write-Error "codex exec resume failed with exit $exit"
    return $false
}

$result = Get-AgentMessageFromJsonl -Path $outFile
Write-Host "Agent reply: $($result.Text)"
Write-Host "PASS: resume completed" -ForegroundColor Green
return $true
