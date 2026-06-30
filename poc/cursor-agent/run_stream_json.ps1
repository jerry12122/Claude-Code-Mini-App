<#
.SYNOPSIS
  Live POC：以與 Go runner 相同參數執行 agent CLI，逐行解析 stream-json。

.PARAMETER Prompt
  測試 prompt（預設：say hello in one word）

.PARAMETER WorkDir
  工作目錄

.PARAMETER UsePartial
  是否加 --stream-partial-output（預設 true）
#>
param(
    [string]$Prompt = "say hello in one word only",
    [string]$WorkDir = (Get-Location).Path,
    [switch]$UsePartial = $true
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

Write-Host "=== Cursor Agent stream-json POC ===" -ForegroundColor Cyan
Write-Host "Version: $(agent --version 2>&1)"
Write-Host "Status : $(agent status 2>&1 | Out-String)"
Write-Host "WorkDir: $WorkDir"
Write-Host "Prompt : $Prompt"
if ($env:CURSOR_API_KEY) {
    Write-Host "CURSOR_API_KEY: set (len=$($env:CURSOR_API_KEY.Length))"
} else {
    Write-Host "CURSOR_API_KEY: not set"
}
Write-Host ""

$cliArgs = @("--print", "--output-format", "stream-json", "--trust")
if ($UsePartial) { $cliArgs += "--stream-partial-output" }
$cliArgs += $Prompt

$outFile = [System.IO.Path]::GetTempFileName()
$errFile = [System.IO.Path]::GetTempFileName()

try {
    Push-Location $WorkDir
    $prevEap = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    & agent @cliArgs 1> $outFile 2> $errFile
    $exitCode = $LASTEXITCODE
    $ErrorActionPreference = $prevEap

    Write-Host "Exit code: $exitCode"
    $stderr = Get-Content $errFile -Raw -ErrorAction SilentlyContinue
    if ($stderr) {
        Write-Host "--- STDERR ---" -ForegroundColor Yellow
        Write-Host $stderr
    }

    $lines = @(Get-Content $outFile -ErrorAction SilentlyContinue)
    Write-Host "--- STDOUT ($($lines.Count) lines) ---" -ForegroundColor Green

    if ($lines.Count -eq 0) {
        Write-Host "(empty stdout)"
        if ($stderr -match 'Authentication required') {
            Write-Host ""
            Write-Host "DIAGNOSIS: Headless auth failure." -ForegroundColor Red
            Write-Host "Fix: export CURSOR_API_KEY before starting the server, or verify 'agent -p hi' works in this shell."
            exit 2
        }
        exit 1
    }

    $i = 0
    foreach ($line in $lines) {
        try {
            $obj = $line | ConvertFrom-Json
            $extra = ""
            if ($obj.type -eq "assistant") {
                $hasTs = $null -ne $obj.PSObject.Properties["timestamp_ms"]
                $hasMc = $null -ne $obj.PSObject.Properties["model_call_id"]
                $extra = " ts=$hasTs mcid=$hasMc"
            }
            Write-Host ("[{0}] type={1} subtype={2}{3}" -f $i, $obj.type, $obj.subtype, $extra)
            if ($obj.session_id) { Write-Host "       session_id=$($obj.session_id)" }
        } catch {
            Write-Host ("[{0}] (invalid json) {1}" -f $i, $line)
        }
        $i++
    }

    Write-Host ""
    Write-Host "PASS: received stream-json events" -ForegroundColor Green
    exit 0

} finally {
    Pop-Location -ErrorAction SilentlyContinue
    Remove-Item $outFile, $errFile -ErrorAction SilentlyContinue
}
