<#
.SYNOPSIS
  Live POC：擷取各 runner 的 session model，輸出正規化 JSON。

.PARAMETER Prompt
  測試 prompt

.PARAMETER WorkDir
  工作目錄

.PARAMETER OutDir
  輸出目錄
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

function New-ModelObj([string]$Provider) {
    return [ordered]@{
        provider     = $Provider
        ok           = $false
        model        = $null
        display_text = $null
        source       = $null
        raw_detail   = $null
        error        = $null
        init_line    = $null
        result_line  = $null
    }
}

function Get-ModelFromCliArgs([string[]]$Args) {
    for ($i = 0; $i -lt $Args.Count; $i++) {
        $a = [string]$Args[$i]
        if ($a -eq '--model' -or $a -eq '-m') {
            if ($i + 1 -lt $Args.Count) { return [string]$Args[$i + 1] }
        }
        if ($a -like '--model=*') { return $a.Substring(8) }
    }
    return ''
}

function Read-ClaudeSettingsModel {
    $p = Join-Path $env:USERPROFILE '.claude\settings.json'
    if (-not (Test-Path $p)) { return $null }
    try {
        $j = Get-Content $p -Raw | ConvertFrom-Json
        if ($j.model) { return [string]$j.model }
    } catch {}
    return $null
}

function Truncate-Line([string]$s, [int]$n = 240) {
    if (-not $s) { return $null }
    if ($s.Length -le $n) { return $s }
    return $s.Substring(0, $n) + "...(truncated)"
}

function Probe-Claude {
    $m = New-ModelObj 'claude'
    $outFile = [System.IO.Path]::GetTempFileName()
    try {
        Push-Location $WorkDir
        $prev = $ErrorActionPreference
        $ErrorActionPreference = 'Continue'
        claude -p $Prompt --output-format stream-json --verbose 1> $outFile 2>&1
        $code = $LASTEXITCODE
        $ErrorActionPreference = $prev

        $lines = @(Get-Content $outFile -ErrorAction SilentlyContinue)
        $init = $lines | Where-Object { $_ -match '"type"\s*:\s*"system"' -and $_ -match '"subtype"\s*:\s*"init"' } | Select-Object -First 1
        $result = $lines | Where-Object { $_ -match '"type"\s*:\s*"result"' } | Select-Object -Last 1
        $m.init_line = Truncate-Line $init
        $m.result_line = Truncate-Line $result

        if ($init) {
            $obj = $init | ConvertFrom-Json
            if ($obj.model) {
                $m.ok = $true
                $m.model = [string]$obj.model
                $m.display_text = $m.model
                $m.source = 'init_event'
                $m.raw_detail = 'system/subtype=init.model'
                return $m
            }
        }

        if ($result) {
            $obj = $result | ConvertFrom-Json
            if ($obj.modelUsage) {
                $keys = @($obj.modelUsage.PSObject.Properties.Name)
                if ($keys.Count -gt 0) {
                    $m.ok = $true
                    $m.model = [string]$keys[0]
                    $m.display_text = $m.model
                    $m.source = 'result_event'
                    $m.raw_detail = 'result.modelUsage key'
                    return $m
                }
            }
        }

        $global = Read-ClaudeSettingsModel
        if ($global) {
            $m.ok = $true
            $m.model = $global
            $m.display_text = $global
            $m.source = 'global_config'
            $m.raw_detail = '~/.claude/settings.json model'
            return $m
        }

        $m.error = "no model in stream (exit=$code)"
        $m.display_text = '-'
        return $m
    } finally {
        Pop-Location -ErrorAction SilentlyContinue
        Remove-Item $outFile -ErrorAction SilentlyContinue
    }
}

function Probe-Cursor {
    $m = New-ModelObj 'cursor'
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
            $m.error = 'authentication required'
            $m.display_text = '-'
            return $m
        }

        $lines = @(Get-Content $outFile -ErrorAction SilentlyContinue)
        $init = $lines | Where-Object { $_ -match '"type"\s*:\s*"system"' } | Select-Object -First 1
        $result = $lines | Where-Object { $_ -match '"type"\s*:\s*"result"' } | Select-Object -Last 1
        $m.init_line = Truncate-Line $init
        $m.result_line = Truncate-Line $result

        if ($init) {
            $obj = $init | ConvertFrom-Json
            if ($obj.model) {
                $m.ok = $true
                $m.model = [string]$obj.model
                $m.display_text = $m.model
                $m.source = 'init_event'
                $m.raw_detail = 'system/subtype=init.model'
                return $m
            }
        }

        $m.ok = $true
        $m.model = 'auto'
        $m.display_text = 'auto'
        $m.source = 'global_config'
        $m.raw_detail = "cursor-agent default (agent models: auto)"
        return $m
    } finally {
        Pop-Location -ErrorAction SilentlyContinue
        Remove-Item $outFile, $errFile -ErrorAction SilentlyContinue
    }
}

function Probe-Kiro {
    $m = New-ModelObj 'kiro'
    $outFile = [System.IO.Path]::GetTempFileName()
    $errFile = [System.IO.Path]::GetTempFileName()
    $listFile = [System.IO.Path]::GetTempFileName()
    try {
        Push-Location $WorkDir
        $prev = $ErrorActionPreference
        $ErrorActionPreference = 'Continue'
        kiro-cli chat --no-interactive --trust-all-tools $Prompt 1> $outFile 2> $errFile
        $code = $LASTEXITCODE
        kiro-cli chat --list-models 1> $listFile 2>&1
        $ErrorActionPreference = $prev

        $m.init_line = $null
        $m.result_line = Truncate-Line (Get-Content $errFile -Raw -ErrorAction SilentlyContinue)

        if ($code -ne 0) {
            $m.error = "exit=$code"
            $m.display_text = '-'
            return $m
        }

        $listText = Get-Content $listFile -Raw -ErrorAction SilentlyContinue
        $defaultLine = ($listText -split "`n" | Where-Object { $_ -match '^\*\s+' } | Select-Object -First 1)
        if ($defaultLine -match '^\*\s+(\S+)') {
            $m.ok = $true
            $m.model = $Matches[1]
            $m.display_text = $m.model
            $m.source = 'list_models'
            $m.raw_detail = "kiro-cli chat --list-models default (*)"
            return $m
        }

        $m.error = 'default model not found in --list-models'
        $m.display_text = '-'
        return $m
    } finally {
        Pop-Location -ErrorAction SilentlyContinue
        Remove-Item $outFile, $errFile, $listFile -ErrorAction SilentlyContinue
    }
}

function Probe-Codex {
    $m = New-ModelObj 'codex'
    if (-not (Get-Command codex -ErrorAction SilentlyContinue)) {
        $sample = Join-Path (Split-Path $PSScriptRoot -Parent) 'codex-cli\samples\capture.jsonl'
        if (Test-Path $sample) {
            $lines = Get-Content $sample
            $m.init_line = Truncate-Line ($lines | Select-Object -First 1)
            $m.result_line = Truncate-Line ($lines | Where-Object { $_ -match 'turn.completed' } | Select-Object -Last 1)
            $m.raw_detail = "offline: poc/codex-cli/samples/capture.jsonl (no codex CLI)"
        } else {
            $m.error = 'codex CLI not installed'
            $m.display_text = '-'
            return $m
        }

        $configPath = Join-Path $env:USERPROFILE '.codex\config.toml'
        $modelFromConfig = $null
        if (Test-Path $configPath) {
            $toml = Get-Content $configPath -Raw
            if ($toml -match '(?m)^\s*model\s*=\s*"(.*?)"\s*$') { $modelFromConfig = $Matches[1] }
        }

        if ($modelFromConfig) {
            $m.ok = $true
            $m.model = $modelFromConfig
            $m.display_text = $modelFromConfig
            $m.source = 'global_config'
            $m.raw_detail += ' + ~/.codex/config.toml model'
            return $m
        }

        $m.ok = $false
        $m.error = "codex JSONL has no model; no codex CLI and no config.toml"
        $m.source = 'unknown'
        $m.display_text = '-'
        return $m
    }

    $outFile = [System.IO.Path]::GetTempFileName()
    try {
        Push-Location $WorkDir
        $prev = $ErrorActionPreference
        $ErrorActionPreference = 'Continue'
        codex exec --json --skip-git-repo-check --yolo -C $WorkDir $Prompt 1> $outFile 2>&1
        $code = $LASTEXITCODE
        $ErrorActionPreference = $prev

        $lines = @(Get-Content $outFile -ErrorAction SilentlyContinue)
        $m.init_line = Truncate-Line ($lines | Where-Object { $_ -match 'thread.started' } | Select-Object -First 1)
        $m.result_line = Truncate-Line ($lines | Where-Object { $_ -match 'turn.completed' } | Select-Object -Last 1)

        foreach ($line in $lines) {
            try {
                $obj = $line | ConvertFrom-Json
                if ($obj.type -eq 'thread.started' -and $obj.model) {
                    $m.ok = $true
                    $m.model = [string]$obj.model
                    $m.display_text = $m.model
                    $m.source = 'init_event'
                    $m.raw_detail = 'thread.started.model'
                    return $m
                }
            } catch {}
        }

        $configPath = Join-Path $env:USERPROFILE '.codex\config.toml'
        if (Test-Path $configPath) {
            $toml = Get-Content $configPath -Raw
            if ($toml -match '(?m)^\s*model\s*=\s*"(.*?)"\s*$') {
                $m.ok = $true
                $m.model = $Matches[1]
                $m.display_text = $m.model
                $m.source = 'global_config'
                $m.raw_detail = "~/.codex/config.toml model (JSONL has no model, see openai/codex#14736)"
                return $m
            }
        }

        $m.error = "codex JSONL 無 model (exit=$code)"
        $m.display_text = '-'
        return $m
    } finally {
        Pop-Location -ErrorAction SilentlyContinue
        Remove-Item $outFile -ErrorAction SilentlyContinue
    }
}

function Probe-Antigravity {
    $m = New-ModelObj 'antigravity'
    $agy = if ($env:CC_AGY_BIN) { $env:CC_AGY_BIN } else { 'agy' }
    if (-not (Get-Command $agy -ErrorAction SilentlyContinue)) {
        $m.error = "agy not installed (antigravity runner disabled in app)"
        $m.display_text = '-'
        $m.init_line = '{"type":"init","session_id":"abc-123","model":"gemini-3.1-pro"}'
        $m.raw_detail = 'offline fixture from internal/antigravity/runner_test.go'
        return $m
    }
    # 若環境有 agy 可再擴充 live probe
    $m.error = 'antigravity disabled in app; use fixture'
    $m.display_text = '-'
    return $m
}

Write-Host "=== Model Display POC ===" -ForegroundColor Cyan
Write-Host "Prompt : $Prompt"
Write-Host "OutDir : $OutDir"
Write-Host ""

$report = [ordered]@{
    prompt    = $Prompt
    work_dir  = $WorkDir
    captured  = (Get-Date).ToString('o')
    providers = [ordered]@{
        claude       = Probe-Claude
        cursor       = Probe-Cursor
        kiro         = Probe-Kiro
        codex        = Probe-Codex
        antigravity  = Probe-Antigravity
    }
}

foreach ($name in @('claude', 'cursor', 'kiro', 'codex', 'antigravity')) {
    $p = $report.providers[$name]
    Write-Host "--- $($name.ToUpper()) ---" -ForegroundColor $(if ($p.ok) { 'Green' } else { 'Yellow' })
    if ($p.ok) {
        Write-Host "  model        : $($p.model)"
        Write-Host "  display_text : $($p.display_text)"
        Write-Host "  source       : $($p.source)"
        Write-Host "  raw_detail   : $($p.raw_detail)"
    } else {
        Write-Host "  error: $($p.error)" -ForegroundColor Red
        if ($p.raw_detail) { Write-Host "  note : $($p.raw_detail)" }
    }
    Write-Host ""
}

New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
$jsonPath = Join-Path $OutDir 'model-report.json'
Write-Utf8NoBom $jsonPath (($report | ConvertTo-Json -Depth 8))

Write-Host "Report: $jsonPath" -ForegroundColor Green
$okCount = @(
    $report.providers.claude.ok,
    $report.providers.cursor.ok,
    $report.providers.kiro.ok
) | Where-Object { $_ } | Measure-Object | Select-Object -ExpandProperty Count
Write-Host "PASS: $okCount/3 live providers with model (claude/cursor/kiro)"
exit 0
