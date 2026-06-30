<#
.SYNOPSIS
  Live POC：擷取 Claude / Cursor / Kiro 單回合 usage，輸出正規化 JSON。

.PARAMETER Prompt
  測試 prompt

.PARAMETER WorkDir
  工作目錄

.PARAMETER OutDir
  輸出目錄（samples + usage-report.json）
#>
param(
    [string]$Prompt = "say hi in one word only",
    [string]$WorkDir = (Get-Location).Path,
    [string]$OutDir = (Join-Path $PSScriptRoot "samples")
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Write-Utf8NoBom([string]$Path, [string]$Content) {
    $enc = New-Object System.Text.UTF8Encoding $false
    [System.IO.File]::WriteAllText($Path, $Content, $enc)
}

function Strip-Ansi([string]$s) {
    if (-not $s) { return "" }
    return ($s -replace '\x1b\[[0-9;?]*[A-Za-z]', '' -replace '\x1b[A-Za-z]', '')
}

function New-UsageObj([string]$Provider) {
    return [ordered]@{
        provider           = $Provider
        ok                 = $false
        cost_usd           = $null
        credits            = $null
        input_tokens       = $null
        output_tokens      = $null
        cache_read_tokens  = $null
        cache_write_tokens = $null
        duration_ms        = $null
        duration_text      = $null
        error              = $null
        raw_source         = $null
    }
}

function Probe-Claude {
    $u = New-UsageObj "claude"
    $outFile = [System.IO.Path]::GetTempFileName()
    try {
        Push-Location $WorkDir
        $prev = $ErrorActionPreference
        $ErrorActionPreference = 'Continue'
        claude -p $Prompt --output-format stream-json --verbose 1> $outFile 2>&1
        $code = $LASTEXITCODE
        $ErrorActionPreference = $prev

        $resultLine = Get-Content $outFile -ErrorAction SilentlyContinue |
            Where-Object { $_ -match '"type"\s*:\s*"result"' } | Select-Object -Last 1
        if (-not $resultLine) {
            $u.error = "no result line (exit=$code)"
            return $u
        }

        New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
        Write-Utf8NoBom (Join-Path $OutDir "claude-result.ndjson") $resultLine
        $u.raw_source = "stdout NDJSON type=result"

        $obj = $resultLine | ConvertFrom-Json
        $u.ok = $true
        $u.cost_usd = $obj.total_cost_usd
        $u.duration_ms = $obj.duration_ms
        if ($obj.usage) {
            $u.input_tokens = $obj.usage.input_tokens
            $u.output_tokens = $obj.usage.output_tokens
            $u.cache_read_tokens = $obj.usage.cache_read_input_tokens
            $u.cache_write_tokens = $obj.usage.cache_creation_input_tokens
        }
        return $u
    } finally {
        Pop-Location -ErrorAction SilentlyContinue
        Remove-Item $outFile -ErrorAction SilentlyContinue
    }
}

function Probe-Cursor {
    $u = New-UsageObj "cursor"
    $outFile = [System.IO.Path]::GetTempFileName()
    $errFile = [System.IO.Path]::GetTempFileName()
    try {
        Push-Location $WorkDir
        $prev = $ErrorActionPreference
        $ErrorActionPreference = 'Continue'
        agent -p $Prompt --output-format stream-json --trust 1> $outFile 2> $errFile
        $code = $LASTEXITCODE
        $ErrorActionPreference = $prev

        $stderr = Get-Content $errFile -Raw -ErrorAction SilentlyContinue
        if ($code -ne 0 -and $stderr -match 'Authentication required') {
            $u.error = "authentication required"
            return $u
        }

        $resultLine = Get-Content $outFile -ErrorAction SilentlyContinue |
            Where-Object { $_ -match '"type"\s*:\s*"result"' } | Select-Object -Last 1
        if (-not $resultLine) {
            $u.error = "no result line (exit=$code)"
            return $u
        }

        New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
        $cleanCursor = (@{
            type        = "result"
            duration_ms = $u.duration_ms
            usage       = @{
                inputTokens      = $u.input_tokens
                outputTokens     = $u.output_tokens
                cacheReadTokens  = $u.cache_read_tokens
                cacheWriteTokens = $u.cache_write_tokens
            }
        } | ConvertTo-Json -Compress -Depth 4)
        Write-Utf8NoBom (Join-Path $OutDir "cursor-result.ndjson") $cleanCursor
        $u.raw_source = "stdout NDJSON type=result"

        if ($resultLine -match '"duration_ms"\s*:\s*(\d+)') { $u.duration_ms = [int64]$Matches[1] }
        if ($resultLine -match '"inputTokens"\s*:\s*(\d+)') { $u.input_tokens = [int64]$Matches[1] }
        if ($resultLine -match '"outputTokens"\s*:\s*(\d+)') { $u.output_tokens = [int64]$Matches[1] }
        if ($resultLine -match '"cacheReadTokens"\s*:\s*(\d+)') { $u.cache_read_tokens = [int64]$Matches[1] }
        if ($resultLine -match '"cacheWriteTokens"\s*:\s*(\d+)') { $u.cache_write_tokens = [int64]$Matches[1] }
        $u.ok = $true
        return $u
    } finally {
        Pop-Location -ErrorAction SilentlyContinue
        Remove-Item $outFile, $errFile -ErrorAction SilentlyContinue
    }
}

function Probe-Kiro {
    $u = New-UsageObj "kiro"
    $outFile = [System.IO.Path]::GetTempFileName()
    $errFile = [System.IO.Path]::GetTempFileName()
    try {
        Push-Location $WorkDir
        $prev = $ErrorActionPreference
        $ErrorActionPreference = 'Continue'
        kiro-cli chat --no-interactive --trust-all-tools $Prompt 1> $outFile 2> $errFile
        $code = $LASTEXITCODE
        $ErrorActionPreference = $prev

        if ($code -ne 0) {
            $u.error = "exit=$code"
            return $u
        }

        $stderrRaw = Get-Content $errFile -Raw -ErrorAction SilentlyContinue
        New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
        Write-Utf8NoBom (Join-Path $OutDir "kiro-stderr.txt") $stderrRaw
        $u.raw_source = "stderr plain text"

        $stderr = Strip-Ansi $stderrRaw
        $line = ($stderr -split "`n" | Where-Object { $_ -match 'Credits\s*:' } | Select-Object -Last 1)
        if ($line -match 'Credits:\s*([\d.]+).*Time:\s*(\S+)') {
            $u.credits = [double]$Matches[1]
            $u.duration_text = $Matches[2]
            $u.ok = $true
        } else {
            $u.error = "Credits line not found"
        }
        return $u
    } finally {
        Pop-Location -ErrorAction SilentlyContinue
        Remove-Item $outFile, $errFile -ErrorAction SilentlyContinue
    }
}

Write-Host "=== Usage POC (Claude / Cursor / Kiro) ===" -ForegroundColor Cyan
Write-Host "Prompt : $Prompt"
Write-Host "OutDir : $OutDir"
Write-Host ""

$report = [ordered]@{
    prompt    = $Prompt
    work_dir  = $WorkDir
    captured  = (Get-Date).ToString("o")
    providers = [ordered]@{
        claude = Probe-Claude
        cursor = Probe-Cursor
        kiro   = Probe-Kiro
    }
}

foreach ($name in @("claude", "cursor", "kiro")) {
    $p = $report.providers[$name]
    Write-Host "--- $($name.ToUpper()) ---" -ForegroundColor $(if ($p.ok) { "Green" } else { "Yellow" })
    if ($p.ok) {
        if ($null -ne $p.cost_usd) { Write-Host "  cost_usd          : $($p.cost_usd)" }
        if ($null -ne $p.credits) { Write-Host "  credits           : $($p.credits)" }
        if ($null -ne $p.input_tokens) { Write-Host "  input_tokens      : $($p.input_tokens)" }
        if ($null -ne $p.output_tokens) { Write-Host "  output_tokens     : $($p.output_tokens)" }
        if ($null -ne $p.cache_read_tokens) { Write-Host "  cache_read_tokens : $($p.cache_read_tokens)" }
        if ($null -ne $p.duration_ms) { Write-Host "  duration_ms       : $($p.duration_ms)" }
        if ($p.duration_text) { Write-Host "  duration_text     : $($p.duration_text)" }
        Write-Host "  source            : $($p.raw_source)"
    } else {
        Write-Host "  error: $($p.error)" -ForegroundColor Red
    }
    Write-Host ""
}

New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
$jsonPath = Join-Path $OutDir "usage-report.json"
Write-Utf8NoBom $jsonPath (($report | ConvertTo-Json -Depth 6))

Write-Host "Report: $jsonPath" -ForegroundColor Green
$okCount = @($report.providers.claude.ok, $report.providers.cursor.ok, $report.providers.kiro.ok) | Where-Object { $_ } | Measure-Object | Select-Object -ExpandProperty Count
Write-Host "PASS: $okCount/3 providers captured usage"
if ($okCount -lt 3) { exit 1 }
exit 0
