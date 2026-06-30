<#
.SYNOPSIS
  POC：驗證 kiro-cli stdout 的思考鏈 vs 最終回覆分類規則。

  規則（狀態機）：
  - 在首個 "> " 行之前：thinking（工具執行、shell 輸出、Completed 等）
  - 首個 "> " 行之後：response（含後續無 "> " 的多行延續，如 markdown）

.PARAMETER WorkDir
  工作目錄

.PARAMETER Prompt
  觸發工具使用的提示詞
#>
param(
    [string]$WorkDir = $PSScriptRoot,
    [string]$Prompt = "what git branch am I on? only answer the branch name"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Strip-Ansi([string]$s) {
    return ($s -replace '\x1b\[[0-9;?]*[A-Za-z]', '' -replace '\x1b[A-Za-z]', '')
}

function Is-ResponseLine([string]$raw) {
    $clean = (Strip-Ansi $raw).TrimEnd("`r")
    return $clean.StartsWith('> ')
}

function Strip-KiroPrefix([string]$raw) {
    $clean = (Strip-Ansi $raw).TrimEnd("`r")
    if ($clean.StartsWith('> ')) { return $clean.Substring(2) }
    return $clean
}

Write-Host "=== classify_output POC ===" -ForegroundColor Cyan
Write-Host "Prompt: $Prompt"
Write-Host ""

$outFile = [System.IO.Path]::GetTempFileName()
$errFile = [System.IO.Path]::GetTempFileName()

try {
    $escaped = $Prompt.Replace('"', '\"')
    $p = Start-Process -FilePath "kiro-cli" `
        -ArgumentList "chat --no-interactive --trust-all-tools `"$escaped`"" `
        -WorkingDirectory $WorkDir `
        -RedirectStandardOutput $outFile `
        -RedirectStandardError $errFile `
        -NoNewWindow -Wait -PassThru

    if ($p.ExitCode -ne 0) {
        Write-Error "kiro-cli exit $($p.ExitCode)"
        exit 1
    }

    $lines = Get-Content $outFile
    $responseStarted = $false
    $thinkingLines = [System.Collections.Generic.List[string]]::new()
    $responseLines = [System.Collections.Generic.List[string]]::new()

    foreach ($raw in $lines) {
        $clean = (Strip-Ansi $raw).TrimEnd("`r")
        if (-not $clean.Trim()) { continue }

        if (-not $responseStarted) {
            if (Is-ResponseLine $raw) {
                $responseStarted = $true
                $responseLines.Add((Strip-KiroPrefix $raw))
            } else {
                $thinkingLines.Add((Strip-KiroPrefix $raw))
            }
        } else {
            $responseLines.Add((Strip-KiroPrefix $raw))
        }
    }

    Write-Host "--- THINKING ($($thinkingLines.Count) lines) ---" -ForegroundColor Yellow
    foreach ($l in $thinkingLines) {
        Write-Host "  | $l"
    }

    Write-Host ""
    Write-Host "--- RESPONSE ($($responseLines.Count) lines) ---" -ForegroundColor Green
    foreach ($l in $responseLines) {
        Write-Host "  | $l"
    }

    Write-Host ""
    if ($thinkingLines.Count -gt 0 -and $responseLines.Count -gt 0) {
        Write-Host "PASS: thinking/response split works" -ForegroundColor Green
        exit 0
    }
    if ($thinkingLines.Count -eq 0 -and $responseLines.Count -gt 0) {
        Write-Host "PASS: text-only response (no tools)" -ForegroundColor Green
        exit 0
    }
    Write-Host "FAIL: could not classify output" -ForegroundColor Red
    exit 1

} finally {
    Remove-Item $outFile, $errFile -ErrorAction SilentlyContinue
}
