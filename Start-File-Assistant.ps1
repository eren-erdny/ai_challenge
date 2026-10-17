[CmdletBinding()]
param(
    [double]$BudgetUsd = 0,
    [switch]$Verify
)
$ErrorActionPreference = 'Stop'
if ([double]::IsNaN($BudgetUsd) -or [double]::IsInfinity($BudgetUsd) -or $BudgetUsd -le 0 -or $BudgetUsd -gt 0.20) {
    throw 'Specify the currently authorized budget: -BudgetUsd 0.20 (maximum $0.20).'
}
$root = $PSScriptRoot
$artifacts = Join-Path $root '.file-assistant'
$runs = Join-Path $artifacts 'runs'
$null = New-Item -ItemType Directory -Path $runs -Force
$runName = (Get-Date -Format 'yyyyMMdd-HHmmss') + '-' + ([guid]::NewGuid().ToString('N').Substring(0, 8))
$runPath = Join-Path $runs $runName
$binary = Join-Path $artifacts 'file-assistant.exe'
$previousGoWork = $env:GOWORK
Push-Location (Join-Path $root 'projects/deepseek-client')
try {
    $env:GOWORK = 'off'
    & go build -o $binary .
    if ($LASTEXITCODE -ne 0) { throw 'Build failed.' }
} finally {
    $env:GOWORK = $previousGoWork
    Pop-Location
}
$demoArgs = @('--file-assistant-demo', '--output', $runPath, '--ledger', (Join-Path $artifacts 'budget.json'), '--budget-usd', $BudgetUsd.ToString([Globalization.CultureInfo]::InvariantCulture))
if ($Verify) { $demoArgs += '--verify' }
& $binary @demoArgs
$result = $LASTEXITCODE
Write-Host "Files, tool log, verification and diff: $runPath"
if ($result -ne 0) { throw "File assistant returned $result; results and spending are preserved." }
