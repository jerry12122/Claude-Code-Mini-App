<#
.SYNOPSIS
  共用：以空 stdin 執行 codex，避免 headless 等待 stdin。
#>
function Format-ProcessArg {
    param([string]$Value)
    if ($Value -match '[\s"]') {
        return '"' + ($Value -replace '"', '\"') + '"'
    }
    return $Value
}

function Invoke-CodexExec {
    param(
        [string[]]$CodexArgs,
        [string]$WorkDir,
        [string]$OutFile,
        [string]$ErrFile
    )

    if (Test-Path $OutFile) { Remove-Item $OutFile -Force }
    if (Test-Path $ErrFile) { Remove-Item $ErrFile -Force }

    $argLine = ($CodexArgs | ForEach-Object { Format-ProcessArg $_ }) -join " "

    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = "codex"
    $psi.Arguments = $argLine
    $psi.WorkingDirectory = $WorkDir
    $psi.UseShellExecute = $false
    $psi.RedirectStandardInput = $true
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $psi.CreateNoWindow = $true
    if ($psi.EnvironmentVariables.ContainsKey("TERM")) {
        $psi.EnvironmentVariables.Remove("TERM")
    }

    $p = [System.Diagnostics.Process]::Start($psi)
    $p.StandardInput.Close()

    $stdout = $p.StandardOutput.ReadToEnd()
    $stderr = $p.StandardError.ReadToEnd()
    $p.WaitForExit()

    [System.IO.File]::WriteAllText($OutFile, $stdout, [System.Text.UTF8Encoding]::new($false))
    [System.IO.File]::WriteAllText($ErrFile, $stderr, [System.Text.UTF8Encoding]::new($false))
    return $p.ExitCode
}

function Get-BaseExecArgs {
    param([string]$WorkDir)
    return @(
        "exec", "--json", "--skip-git-repo-check", "-C", $WorkDir,
        "-s", "workspace-write",
        "-c", 'approval_policy="never"'
    )
}

function Get-AgentMessageFromJsonl {
    param([string]$Path)
    $threadId = ""
    $agentText = ""
    if (-not (Test-Path $Path)) { return @{ ThreadId = ""; Text = "" } }
    Get-Content $Path | ForEach-Object {
        $line = $_.Trim()
        if (-not $line) { return }
        try { $ev = $line | ConvertFrom-Json } catch { return }
        if ($ev.type -eq "thread.started" -and $ev.thread_id) { $threadId = $ev.thread_id }
        if ($ev.type -eq "item.completed" -and $ev.item -and $ev.item.type -eq "agent_message" -and $ev.item.text) {
            $agentText = $ev.item.text
        }
    }
    return @{ ThreadId = $threadId; Text = $agentText }
}
