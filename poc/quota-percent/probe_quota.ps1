<#
.SYNOPSIS
  Live POC: capture Claude / Cursor / Kiro account quota percent.

  Headless methods (no PTY):
  - Claude A: claude -p "/usage" plain text
  - Claude B: OAuth API api.anthropic.com/api/oauth/usage
  - Cursor: read state.vscdb token -> GetCurrentPeriodUsage
  - Kiro: kiro-cli chat "/usage" --no-interactive
#>
param(
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

function New-QuotaResult([string]$Provider) {
    return [ordered]@{
        provider        = $Provider
        ok              = $false
        plan            = $null
        source          = $null
        windows         = @()
        error           = $null
        display_message = $null
        api             = $null
    }
}

function Add-ClaudeWindow($result, [string]$Kind, [string]$Label, [double]$Percent, [string]$ResetsAt) {
    $result.windows += [ordered]@{
        kind       = $Kind
        label      = $Label
        percent    = $Percent
        resets_at  = $ResetsAt
    }
}

function Read-ProcessOutput([string]$FilePath) {
    if (-not (Test-Path -LiteralPath $FilePath)) { return "" }
    return (Get-Content -LiteralPath $FilePath -Raw -ErrorAction SilentlyContinue)
}

function Get-ClaudeCodeUserAgent {
    $ver = (claude --version 2>$null) -replace '\s*\(Claude Code\)\s*$', ''
    if (-not $ver) { $ver = "2.1.191" }
    return "claude-code/$ver"
}

function Probe-Claude {
    param([string]$OutDirPath)
    $r = New-QuotaResult "claude"
    $credPath = Join-Path $env:USERPROFILE ".claude\.credentials.json"
    $prev = $ErrorActionPreference
    $ErrorActionPreference = "Continue"

    # Method A: headless slash command
    $outFile = [System.IO.Path]::GetTempFileName()
    $errFile = [System.IO.Path]::GetTempFileName()
    try {
        claude -p "/usage" 1> $outFile 2> $errFile
        $text = Read-ProcessOutput $outFile
        if (-not $text) {
            $text = Read-ProcessOutput $errFile
        }
    }
    finally {
        Remove-Item -LiteralPath $outFile, $errFile -ErrorAction SilentlyContinue
    }
    if ($text) {
        Write-Utf8NoBom (Join-Path $OutDirPath "claude-usage.txt") $text
        if ($text -match "(?i)Current session:\s*(\d+(?:\.\d+)?)%\s*used.+?resets\s*(.+)") {
            Add-ClaudeWindow $r "session" "Current session" ([double]$Matches[1]) ($Matches[2].Trim())
        }
        if ($text -match "(?i)Current week \(all models\):\s*(\d+(?:\.\d+)?)%\s*used.+?resets\s*(.+)") {
            Add-ClaudeWindow $r "weekly" "Current week" ([double]$Matches[1]) ($Matches[2].Trim())
        }
        if ($r.windows.Count -gt 0) {
            $r.source = "claude -p /usage"
            $r.ok = $true
        }
    }

    # Method B: OAuth API when text lacks session/week windows
    if (($r.windows.Count -eq 0) -and (Test-Path -LiteralPath $credPath)) {
        try {
            $cred = Get-Content -LiteralPath $credPath -Raw | ConvertFrom-Json
            $token = $null
            if ($null -ne $cred.claudeAiOauth) {
                $token = $cred.claudeAiOauth.accessToken
            }
            if ($token) {
                $headers = @{
                    Authorization    = "Bearer $token"
                    "anthropic-beta" = "oauth-2-0-2025-04-20"
                    "Content-Type"   = "application/json"
                    "User-Agent"     = (Get-ClaudeCodeUserAgent)
                }
                $apiRaw = $null
                for ($attempt = 1; $attempt -le 2; $attempt++) {
                    try {
                        $apiRaw = Invoke-RestMethod -Uri "https://api.anthropic.com/api/oauth/usage" -Headers $headers -Method Get
                        break
                    }
                    catch {
                        if ($attempt -lt 2 -and $_.Exception.Message -match "429") {
                            Start-Sleep -Seconds 3
                            continue
                        }
                        throw
                    }
                }
                if (-not $apiRaw) { throw "oauth api: empty response" }
                Write-Utf8NoBom (Join-Path $OutDirPath "claude-oauth-usage.json") ($apiRaw | ConvertTo-Json -Depth 8)
                $fiveHour = $apiRaw.five_hour
                $sevenDay = $apiRaw.seven_day
                $r.api = [ordered]@{
                    source        = "api.anthropic.com/api/oauth/usage"
                    five_hour_pct = $fiveHour.utilization
                    seven_day_pct = $sevenDay.utilization
                    limits        = $apiRaw.limits
                }
                $r.ok = $true
                if (-not $r.source) { $r.source = "oauth api" }
                if ($null -ne $fiveHour.utilization) {
                    Add-ClaudeWindow $r "session" "five_hour" ([double]$fiveHour.utilization) $fiveHour.resets_at
                }
                if ($null -ne $sevenDay.utilization) {
                    Add-ClaudeWindow $r "weekly" "seven_day" ([double]$sevenDay.utilization) $sevenDay.resets_at
                }
            }
        }
        catch {
            if (-not $r.ok) {
                $r.error = "oauth api: $($_.Exception.Message)"
            }
        }
    }
    elseif (($r.windows.Count -eq 0) -and -not (Test-Path -LiteralPath $credPath)) {
        $r.error = "no credentials file and /usage text empty"
    }

    $ErrorActionPreference = $prev
    return $r
}

function Probe-Cursor {
    param([string]$OutDirPath)
    $r = New-QuotaResult "cursor"
    $db = Join-Path $env:APPDATA "Cursor\User\globalStorage\state.vscdb"
    if (-not (Test-Path -LiteralPath $db)) {
        $r.error = "Cursor state.vscdb not found"
        return $r
    }
    $token = (sqlite3 $db "SELECT value FROM ItemTable WHERE key='cursorAuth/accessToken';" 2>$null)
    if (-not $token) {
        $r.error = "cursorAuth/accessToken not in state.vscdb"
        return $r
    }
    $prev = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    try {
        $headers = @{
            Authorization              = "Bearer $token"
            "Connect-Protocol-Version" = "1"
            "Content-Type"             = "application/json"
        }
        $uri = "https://api2.cursor.sh/aiserver.v1.DashboardService/GetCurrentPeriodUsage"
        $resp = Invoke-RestMethod -Uri $uri -Method Post -Headers $headers -Body "{}"
        Write-Utf8NoBom (Join-Path $OutDirPath "cursor-period-usage.json") ($resp | ConvertTo-Json -Depth 8)
        $r.ok = $true
        $r.source = "api2.cursor.sh GetCurrentPeriodUsage"
        if ($resp.planUsage) {
            $pu = $resp.planUsage
            $r.windows += [ordered]@{ kind = "billing_auto"; label = "autoPercentUsed"; percent = [double]$pu.autoPercentUsed }
            $r.windows += [ordered]@{ kind = "billing_api"; label = "apiPercentUsed"; percent = [double]$pu.apiPercentUsed }
            $r.windows += [ordered]@{ kind = "billing_total"; label = "totalPercentUsed"; percent = [double]$pu.totalPercentUsed }
            if ($pu.limit) {
                $r.windows += [ordered]@{
                    kind  = "billing_spend"
                    label = "plan spend USD"
                    used  = [double]$pu.totalSpend / 100.0
                    limit = [double]$pu.limit / 100.0
                }
            }
        }
        if ($resp.displayMessage) {
            $r.display_message = $resp.displayMessage
        }
    }
    catch {
        $r.error = $_.Exception.Message
    }
    $ErrorActionPreference = $prev
    return $r
}

function Probe-Kiro {
    param([string]$OutDirPath)
    $r = New-QuotaResult "kiro"
    $errFile = [System.IO.Path]::GetTempFileName()
    try {
        $prev = $ErrorActionPreference
        $ErrorActionPreference = "Continue"
        kiro-cli chat "/usage" --no-interactive 1> $null 2> $errFile
        $ErrorActionPreference = $prev
        $raw = Read-ProcessOutput $errFile
    }
    finally {
        Remove-Item -LiteralPath $errFile -ErrorAction SilentlyContinue
    }
    $clean = Strip-Ansi $raw
    Write-Utf8NoBom (Join-Path $OutDirPath "kiro-usage.txt") $raw

    if ($clean -match "KIRO\s+(\w+)") {
        $r.plan = $Matches[1]
    }
    if ($clean -match "Credits\s*\(([\d.]+)\s+of\s+([\d.]+)") {
        $used = [double]$Matches[1]
        $limit = [double]$Matches[2]
        $pct = if ($limit -gt 0) { [math]::Round($used / $limit * 100, 2) } else { $null }
        if ($clean -match "(\d+(?:\.\d+)?)%") {
            $pct = [double]$Matches[1]
        }
        $r.ok = $true
        $r.source = "kiro-cli chat /usage --no-interactive"
        $r.windows += [ordered]@{
            kind    = "credits"
            label   = "monthly credits"
            percent = $pct
            used    = $used
            limit   = $limit
        }
    }
    else {
        $r.error = "Credits line not found in stdout"
    }
    return $r
}

function Get-WindowField($Window, [string]$Name) {
    if ($Window -is [System.Collections.IDictionary]) {
        if ($Window.Contains($Name)) { return $Window[$Name] }
        return $null
    }
    if ($Window.PSObject.Properties.Name -contains $Name) { return $Window.$Name }
    return $null
}

function Show-Provider([string]$Name, $ProviderResult) {
    Write-Host "--- $($Name.ToUpper()) ---" -ForegroundColor $(if ($ProviderResult.ok) { "Green" } else { "Yellow" })
    if ($ProviderResult.plan) { Write-Host "  plan   : $($ProviderResult.plan)" }
    if ($ProviderResult.source) { Write-Host "  source : $($ProviderResult.source)" }
    if ($ProviderResult.display_message) { Write-Host "  note   : $($ProviderResult.display_message)" }
    foreach ($w in $ProviderResult.windows) {
        $line = "  [$(Get-WindowField $w 'kind')]"
        $pct = Get-WindowField $w "percent"
        $used = Get-WindowField $w "used"
        $limit = Get-WindowField $w "limit"
        $resets = Get-WindowField $w "resets_at"
        if ($null -ne $pct) { $line += " $pct%" }
        if ($null -ne $used -and $null -ne $limit) { $line += " ($used/$limit)" }
        if ($resets) { $line += " resets $resets" }
        Write-Host $line
    }
    if ($ProviderResult.api) {
        Write-Host "  [api] five_hour=$($ProviderResult.api.five_hour_pct)% seven_day=$($ProviderResult.api.seven_day_pct)%" -ForegroundColor DarkGray
    }
    if (-not $ProviderResult.ok) { Write-Host "  error: $($ProviderResult.error)" -ForegroundColor Red }
    Write-Host ""
}

Write-Host "=== Account Quota Percent POC ===" -ForegroundColor Cyan
New-Item -ItemType Directory -Force -Path $OutDir | Out-Null

$report = [ordered]@{
    captured  = (Get-Date).ToString("o")
    providers = [ordered]@{
        claude = Probe-Claude -OutDirPath $OutDir
        cursor = Probe-Cursor -OutDirPath $OutDir
        kiro   = Probe-Kiro -OutDirPath $OutDir
    }
}

Show-Provider "claude" $report.providers.claude
Show-Provider "cursor" $report.providers.cursor
Show-Provider "kiro" $report.providers.kiro

$jsonPath = Join-Path $OutDir "quota-report.json"
Write-Utf8NoBom $jsonPath (($report | ConvertTo-Json -Depth 8))

Write-Host "Report: $jsonPath" -ForegroundColor Green
$okCount = @($report.providers.claude.ok, $report.providers.cursor.ok, $report.providers.kiro.ok) | Where-Object { $_ } | Measure-Object | Select-Object -ExpandProperty Count
Write-Host "PASS: $okCount/3 providers captured quota %"
if ($okCount -lt 3) { exit 1 }
exit 0
