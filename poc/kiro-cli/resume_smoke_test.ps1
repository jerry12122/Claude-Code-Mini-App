<#
.SYNOPSIS
  以指定 session id 延續對話，驗證 resume 機制是否正常。

.PARAMETER SessionId
  要延續的 session id（UUID 格式）

.PARAMETER WorkDir
  工作目錄（預設：目前目錄）

.PARAMETER Prompt
  第二輪提示詞（預設：now say goodbye in one word）

.EXAMPLE
  .\resume_smoke_test.ps1 -SessionId "bd27df3b-..." -WorkDir "C:\myproject"
#>
param(
    [Parameter(Mandatory = $true)]
    [string]$SessionId,
    [string]$WorkDir = $PSScriptRoot,
    [string]$Prompt = "now say goodbye in one word"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

Write-Host "=== Resume smoke test ===" -ForegroundColor Cyan
Write-Host "SessionId: $SessionId"
Write-Host "WorkDir  : $WorkDir"
Write-Host "Prompt   : $Prompt"

$outFile = [System.IO.Path]::GetTempFileName()
$errFile = [System.IO.Path]::GetTempFileName()

try {
    $escapedPrompt = $Prompt.Replace('"', '\"')
    $p = Start-Process -FilePath "kiro-cli" `
        -ArgumentList "chat --no-interactive --trust-all-tools --resume-id $SessionId `"$escapedPrompt`"" `
        -WorkingDirectory $WorkDir `
        -RedirectStandardOutput $outFile `
        -RedirectStandardError  $errFile `
        -NoNewWindow -Wait -PassThru

    $stdout = Get-Content $outFile -Raw -ErrorAction SilentlyContinue
    $stderr = Get-Content $errFile -Raw -ErrorAction SilentlyContinue

    Write-Host "Exit code: $($p.ExitCode)"
    Write-Host "--- stdout ---"
    Write-Host $stdout
    Write-Host "--- stderr ---"
    Write-Host $stderr

    if ($p.ExitCode -ne 0) {
        Write-Error "Resume failed with exit code $($p.ExitCode)"
        return $false
    }

    $response = ($stdout -split "`n" | ForEach-Object {
        if ($_ -match '^\> (.*)') { $Matches[1] } else { $_ }
    }) -join "`n"

    Write-Host ""
    Write-Host "=== Resume SUCCESS ===" -ForegroundColor Green
    Write-Host "Response: $response"
    return $true

} finally {
    Remove-Item $outFile, $errFile -ErrorAction SilentlyContinue
}
