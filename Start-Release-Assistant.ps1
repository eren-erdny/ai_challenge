[CmdletBinding()]
param(
    [string]$BaseRef = 'w6-hw5',
    [string]$HeadRef = 'w7-hw4',
    [string]$ReleaseVersion = 'week-7-preview',
    [double]$BudgetUsd = 0,
    [switch]$DryRun,
    [switch]$Verify,
    [string]$NodePath = '',
    [string]$PythonPath = ''
)
$ErrorActionPreference = 'Stop'
if (!$DryRun -and ([double]::IsNaN($BudgetUsd) -or [double]::IsInfinity($BudgetUsd) -or $BudgetUsd -le 0 -or $BudgetUsd -gt 0.20)) {
    throw 'Specify a currently authorized budget: -BudgetUsd 0.20 (maximum $0.20).'
}
$root = $PSScriptRoot
$state = Join-Path $root '.release-assistant'
$runs = Join-Path $state 'runs'
$null = New-Item -ItemType Directory -Path $runs -Force
$runName = (Get-Date -Format 'yyyyMMdd-HHmmss') + '-' + ([guid]::NewGuid().ToString('N').Substring(0,8))
$output = Join-Path $runs $runName
$bundled = Join-Path $env:USERPROFILE '.cache/codex-runtimes/codex-primary-runtime/dependencies'
if (!$NodePath) { $candidate=Join-Path $bundled 'node/bin/node.exe'; $NodePath=if(Test-Path -LiteralPath $candidate){$candidate}else{'node'} }
if (!$PythonPath) { $candidate=Join-Path $bundled 'python/python.exe'; $PythonPath=if(Test-Path -LiteralPath $candidate){$candidate}else{'python'} }
$binary = Join-Path $state 'release-assistant.exe'
$oldGoWork=$env:GOWORK
Push-Location (Join-Path $root 'projects/deepseek-client')
try {
    $env:GOWORK='off'
    & go build -o $binary .
    if($LASTEXITCODE -ne 0){throw 'Build failed.'}
} finally { $env:GOWORK=$oldGoWork; Pop-Location }
$releaseArgs=@('--release-assistant','--repo',$root,'--base',$BaseRef,'--head',$HeadRef,'--version',$ReleaseVersion,'--output',$output,'--ledger',(Join-Path $state 'budget.json'),'--budget-usd',$BudgetUsd.ToString([Globalization.CultureInfo]::InvariantCulture),'--node',$NodePath,'--python',$PythonPath)
if($DryRun){$releaseArgs+='--dry-run'}
if($Verify){$releaseArgs+='--verify'}
& $binary @releaseArgs
$code=$LASTEXITCODE
Write-Host "Release bundle: $output"
if($code -ne 0){throw "Release preparation returned $code. Checks, errors and spending are preserved."}
