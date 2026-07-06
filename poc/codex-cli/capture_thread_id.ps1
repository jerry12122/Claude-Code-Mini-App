<#
.SYNOPSIS
  首回合 codex exec --json，從 JSONL 擷取 thread_id。
#>
param(
    [string]$WorkDir = $PSScriptRoot,
    [string]$Prompt = "reply with exactly: POC_OK",
    [int]$TimeoutSec = 120
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
. (Join-Path $PSScriptRoot "common.ps1")

$samplesDir = Join-Path $PSScriptRoot "samples"
New-Item -ItemType Directory -Force -Path $samplesDir | Out-Null

$outFile = Join-Path $samplesDir "capture.jsonl"
$errFile = Join-Path $samplesDir "capture.stderr"

Write-Host "=== Capture thread_id ===" -ForegroundColor Cyan
Write-Host "WorkDir: $WorkDir"
Write-Host "Prompt : $Prompt"

$args = Get-BaseExecArgs -WorkDir $WorkDir
$args += $Prompt

$exit = Invoke-CodexExec -CodexArgs $args -WorkDir $WorkDir -OutFile $outFile -ErrFile $errFile
Write-Host "Exit code: $exit"
if (Test-Path $errFile) {
    $stderr = Get-Content $errFile -Raw -ErrorAction SilentlyContinue
    if ($stderr) { Write-Host "stderr:`n$stderr" }
}
if ($exit -ne 0) {
    Write-Error "codex exec failed"
    return ""
}

$result = Get-AgentMessageFromJsonl -Path $outFile
if ($result.ThreadId -eq "") {
    Write-Warning "Could not find thread_id in JSONL"
    return ""
}

Write-Host ""
Write-Host "=== SUCCESS ===" -ForegroundColor Green
Write-Host "thread_id : $($result.ThreadId)"
Write-Host "agent_msg : $($result.Text)"
return $result.ThreadId
