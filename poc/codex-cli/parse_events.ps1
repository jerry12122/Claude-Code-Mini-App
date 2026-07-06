<#
.SYNOPSIS
  驗證 item.started type 映射（離線解析 samples/capture.jsonl）。
#>
Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$jsonl = Join-Path $PSScriptRoot "samples\capture.jsonl"
if (-not (Test-Path $jsonl)) {
    Write-Error "Missing $jsonl - run capture_thread_id.ps1 first"
    exit 1
}

$labels = @{
    command_execution = "command"
    web_search        = "search"
    file_change       = "file"
    mcp_tool_call     = "tool"
}

Write-Host "=== Parse events ===" -ForegroundColor Cyan
$counts = @{}
Get-Content $jsonl | ForEach-Object {
    $line = $_.Trim()
    if (-not $line) { return }
    $ev = $line | ConvertFrom-Json
    Write-Host "event: $($ev.type)"
    if ($ev.type -eq "item.started" -and $ev.item) {
        $t = $ev.item.type
        if (-not $counts.ContainsKey($t)) { $counts[$t] = 0 }
        $counts[$t]++
        if ($labels.ContainsKey($t)) {
            Write-Host "  activity: $($labels[$t])" -ForegroundColor Yellow
        } elseif ($t -in @("reasoning", "plan_update", "agent_message")) {
            Write-Host "  (ignored: $t)"
        } else {
            Write-Host "  (unknown: $t)"
        }
    }
}

Write-Host ""
Write-Host "Item types seen:" -ForegroundColor Green
$counts.GetEnumerator() | Sort-Object Name | ForEach-Object { Write-Host "  $($_.Key): $($_.Value)" }
exit 0
