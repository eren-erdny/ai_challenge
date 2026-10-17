param([Parameter(Mandatory)][string]$Server,[string]$KeyPath="$env:USERPROFILE\.ssh\hw1_deploy",[int]$LocalPort=11438)
$ErrorActionPreference='Stop'
$root=Split-Path (Split-Path $PSScriptRoot -Parent) -Parent
$state=Join-Path $root '.local-llm/private-service'
$binary=Join-Path $state 'deepseek-client.exe'
New-Item -ItemType Directory -Force $state | Out-Null
if (-not (Test-Path $binary)) {
 Push-Location (Join-Path $root 'projects/deepseek-client')
 try {go build -o $binary .;if($LASTEXITCODE -ne 0){throw 'Client build failed'}} finally {Pop-Location}
}
$configDir=Join-Path $state 'deepseek-client'
New-Item -ItemType Directory -Force $configDir | Out-Null
$configPath=Join-Path $configDir 'config.json'
if(-not (Test-Path $configPath)) {
 (Get-Content (Join-Path $PSScriptRoot 'client.example.json') -Raw).Replace('127.0.0.1:11436',"127.0.0.1:$LocalPort") | Set-Content -Encoding utf8 $configPath
}
$envNames=@('APPDATA','OPENAI_BASE_URL','OPENAI_API_KEY','OPENAI_MODEL','RAG_URL')
$saved=@{};foreach($name in $envNames){$saved[$name]=[Environment]::GetEnvironmentVariable($name,'Process')}
$tunnel=$null
try {
 $arguments=@('-N','-o','BatchMode=yes','-o','IdentitiesOnly=yes','-o','ExitOnForwardFailure=yes','-o','ServerAliveInterval=30','-i',('"'+$KeyPath+'"'),'-L',"127.0.0.1:${LocalPort}:127.0.0.1:11436",$Server)
 $tunnel=Start-Process ssh -ArgumentList $arguments -PassThru -WindowStyle Hidden
 $ready=$false
 for($i=0;$i -lt 20;$i++) {
  Start-Sleep -Milliseconds 250
  if($tunnel.HasExited){throw 'SSH tunnel failed. If your key has a passphrase, load it into ssh-agent first. Check the local port is free.'}
  try {$null=Invoke-RestMethod "http://127.0.0.1:$LocalPort/health" -TimeoutSec 1;$ready=$true;break} catch {}
 }
 if(-not $ready){throw 'Private API did not become available'}
 $env:APPDATA=$state
 foreach($name in $envNames | Where-Object {$_ -ne 'APPDATA'}) {[Environment]::SetEnvironmentVariable($name,$null,'Process')}
 Write-Host 'Existing DeepSeek Client -> SSH -> VPS Ollama. Separate chat history. /tools off for this small model.'
 & $binary
} finally {
 foreach($name in $envNames){[Environment]::SetEnvironmentVariable($name,$saved[$name],'Process')}
 if($tunnel -and -not $tunnel.HasExited){Stop-Process -Id $tunnel.Id}
}
