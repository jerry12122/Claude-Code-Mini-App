<#
.SYNOPSIS
  執行 Antigravity CLI 全部 POC 探測
#>
Set-StrictMode -Version Latest
$ErrorActionPreference = "Continue"
$root = Split-Path -Parent $MyInvocation.MyCommand.Path

Write-Host "`n========== Antigravity CLI POC ==========" -ForegroundColor Cyan

$results = @()

function Invoke-Step($name, $script) {
    Write-Host "`n--- $name ---" -ForegroundColor White
    & $script
    $code = $LASTEXITCODE
    $script:results += [PSCustomObject]@{ Step = $name; Exit = $code }
    return $code
}

Invoke-Step "probe_flags" (Join-Path $root "probe_flags.ps1") | Out-Null
Invoke-Step "probe_headless" (Join-Path $root "probe_headless.ps1") | Out-Null
Invoke-Step "probe_stream_json" (Join-Path $root "probe_stream_json.ps1") | Out-Null

function Get-StepStatus($code) {
    switch ($code) {
        0 { "PASS" }
        1 { "FAIL" }
        2 { "SKIP (no agy)" }
        3 { "SKIP (unsupported)" }
        default { "EXIT $code" }
    }
}

$summary = $results | ForEach-Object {
    [PSCustomObject]@{
        Step   = $_.Step
        Exit   = $_.Exit
        Status = Get-StepStatus $_.Exit
    }
}

Write-Host "`n========== Summary ==========" -ForegroundColor Cyan
$summary | Format-Table -AutoSize

$samples = Join-Path $root "samples"
New-Item -ItemType Directory -Force -Path $samples | Out-Null
$report = @(
    "Antigravity POC $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')",
    "CC_AGY_BIN=$($env:CC_AGY_BIN)",
    ""
) + ($summary | ForEach-Object { "$($_.Step): $($_.Status) (exit $($_.Exit))" })
$reportPath = Join-Path $samples "poc-latest.txt"
$report -join "`n" | Set-Content -Path $reportPath -Encoding UTF8
Write-Host "Wrote $reportPath" -ForegroundColor DarkGray

$hardFail = @($results | Where-Object { $_.Exit -eq 2 }).Count -gt 0
if ($hardFail) { exit 2 }

# exit 1 = headless 已知阻塞（#76），仍回 0 讓 CI/腳本可記錄完整報告
$headlessFail = @($results | Where-Object { $_.Step -eq "probe_headless" -and $_.Exit -eq 1 }).Count -gt 0
if ($headlessFail) {
    Write-Host "`nBLOCKER: probe_headless failed (Issue #76). See samples/poc-results.md" -ForegroundColor Yellow
}
exit 0
