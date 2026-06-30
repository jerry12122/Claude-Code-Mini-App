<#
.SYNOPSIS
  驗證 kiro-cli 首回合無互動模式，並以 before/after 快照 diff 取得 session id。

.PARAMETER WorkDir
  工作目錄（預設：目前目錄）

.PARAMETER Prompt
  要傳送的提示詞（預設：say hello in one word）
#>
param(
    [string]$WorkDir = $PSScriptRoot,
    [string]$Prompt = "say hello in one word"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Get-Sessions([string]$dir) {
    $listOut = [System.IO.Path]::GetTempFileName()
    $listErr = [System.IO.Path]::GetTempFileName()
    $lp = Start-Process -FilePath "kiro-cli" `
        -ArgumentList "chat --list-sessions" `
        -WorkingDirectory $dir `
        -RedirectStandardOutput $listOut `
        -RedirectStandardError $listErr `
        -NoNewWindow -Wait -PassThru
    if ($lp.ExitCode -ne 0) { return @() }
    $raw = (Get-Content $listErr -Raw) -replace '\x1b\[[0-9;?]*[A-Za-z]','' -replace '\x1b[A-Za-z]',''
    $sessions = @()
    $pendingId = $null
    foreach ($line in ($raw -split "`n")) {
        if ($line -match 'Chat SessionId:\s+([0-9a-f\-]{36})') {
            $pendingId = $Matches[1]
        } elseif ($pendingId -and $line -match '\|\s*(.+?)\s*\|\s*\d+\s+msgs') {
            $sessions += [PSCustomObject]@{ Id = $pendingId; Title = $Matches[1].Trim() }
            $pendingId = $null
        }
    }
    Remove-Item $listOut, $listErr -ErrorAction SilentlyContinue
    return $sessions
}

function Resolve-SessionId($before, $after, [string]$prompt) {
    $beforeIds = @{}
    foreach ($s in $before) { $beforeIds[$s.Id] = $true }
    $newOnes = @($after | Where-Object { -not $beforeIds.ContainsKey($_.Id) })

    if ($newOnes.Count -eq 1) { return $newOnes[0].Id }

    if ($newOnes.Count -gt 1) {
        foreach ($s in $newOnes) {
            $p = $prompt.ToLower().Trim()
            $t = $s.Title.ToLower().Trim()
            if ($p.StartsWith($t) -or $t.StartsWith($p)) { return $s.Id }
        }
        return $newOnes[0].Id
    }

    foreach ($s in $after) {
        $p = $prompt.ToLower().Trim()
        $t = $s.Title.ToLower().Trim()
        if ($p.StartsWith($t) -or $t.StartsWith($p)) { return $s.Id }
    }
    return ""
}

Write-Host "=== Step 1: Before snapshot ===" -ForegroundColor Cyan
Write-Host "WorkDir: $WorkDir"
Write-Host "Prompt : $Prompt"

$outFile = [System.IO.Path]::GetTempFileName()
$errFile = [System.IO.Path]::GetTempFileName()

try {
    $before = Get-Sessions $WorkDir
    Write-Host "Before: $($before.Count) sessions"

    Write-Host ""
    Write-Host "=== Step 2: Run kiro-cli --no-interactive ===" -ForegroundColor Cyan
    $escapedPrompt = $Prompt.Replace('"', '\"')
    $p = Start-Process -FilePath "kiro-cli" `
        -ArgumentList "chat --no-interactive --trust-all-tools `"$escapedPrompt`"" `
        -WorkingDirectory $WorkDir `
        -RedirectStandardOutput $outFile `
        -RedirectStandardError $errFile `
        -NoNewWindow -Wait -PassThru

    if ($p.ExitCode -ne 0) {
        Write-Error "kiro-cli failed with exit code $($p.ExitCode)"
        return ""
    }

    Write-Host ""
    Write-Host "=== Step 3: After snapshot + diff ===" -ForegroundColor Cyan
    $after = Get-Sessions $WorkDir
    Write-Host "After: $($after.Count) sessions"

    $sessionId = Resolve-SessionId $before $after $Prompt
    if ($sessionId -eq "") {
        Write-Warning "Could not resolve session id"
        return ""
    }

    Write-Host ""
    Write-Host "=== SUCCESS ===" -ForegroundColor Green
    Write-Host "Session ID: $sessionId"
    return $sessionId

} finally {
    Remove-Item $outFile, $errFile -ErrorAction SilentlyContinue
}
