[CmdletBinding()]
param(
 [double]$BudgetUsd=0,
 [ValidatePattern('^U-[0-9]{3}$')][string]$UserID='U-100',
 [switch]$Stop
)
$ErrorActionPreference='Stop'
$root=$PSScriptRoot
$state=Join-Path $root '.support-assistant/state'
$url='http://127.0.0.1:8766'
if($Stop){
 $token=[IO.File]::ReadAllText((Join-Path $state 'access-token.txt')).Trim()
 Invoke-RestMethod -Uri "$url/api/shutdown" -Method Post -Headers @{Authorization="Bearer $token"} -ContentType 'application/json' -Body '{}' | Out-Null
 Write-Host 'Сервис поддержки остановлен.'
 return
}
if($BudgetUsd -le 0 -or $BudgetUsd -gt 0.20){throw 'Укажите явный бюджет: -BudgetUsd 0.20 (максимум $0.20).'}
$port=[Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback,8766)
try{$port.Start()}finally{$port.Stop()}
$runtime=Join-Path $root '.support-assistant'
New-Item -ItemType Directory -Force $runtime | Out-Null
$binary=Join-Path $runtime 'support-assistant.exe'
Push-Location (Join-Path $root 'projects/deepseek-client')
try{& go build -o $binary ./cmd/support-assistant;if($LASTEXITCODE -ne 0){throw 'Сборка не прошла'}}finally{Pop-Location}
$before=$env:DEEPSEEK_API_KEY
try{
 if(-not $env:DEEPSEEK_API_KEY){
  $private=Get-Content (Join-Path $env:APPDATA 'deepseek-client/config.json') -Raw | ConvertFrom-Json
  $envName=$private.profiles.deepseek.api_key_env
  $key=if($envName){[Environment]::GetEnvironmentVariable($envName)}else{''}
  if(-not $key -and (-not $private.active_profile -or $private.active_profile -eq 'deepseek')){$key=$private.api_token}
  if(-not $key){$key=$private.deepseek_api_token}
  if(-not $key){throw 'Не найден ключ DeepSeek. Задайте DEEPSEEK_API_KEY.'}
  $env:DEEPSEEK_API_KEY=$key
 }
 $arguments=@('--crm',('"{0}"' -f (Join-Path $root 'examples/support-assistant/crm.json')),'--docs',('"{0}"' -f (Join-Path $root 'examples/support-assistant/docs')),'--user',$UserID,'--state',('"{0}"' -f $state),'--budget-usd',$BudgetUsd.ToString([Globalization.CultureInfo]::InvariantCulture))
 $process=Start-Process -FilePath $binary -ArgumentList $arguments -WorkingDirectory $root -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $runtime 'service.log') -RedirectStandardError (Join-Path $runtime 'service-error.log')
}finally{$env:DEEPSEEK_API_KEY=$before;$private=$null;$key=$null}
$ready=$false
for($i=0;$i -lt 50;$i++){
 if($process.HasExited){throw 'Сервис не запустился. Проверьте локальный service-error.log.'}
 try{$health=Invoke-RestMethod "$url/health" -TimeoutSec 1;if($health.status -eq 'ok'){$ready=$true;break}}catch{}
 Start-Sleep -Milliseconds 200
}
if(-not $ready){throw 'Сервис не стал готовым вовремя.'}
$token=[IO.File]::ReadAllText((Join-Path $state 'access-token.txt')).Trim()
$before=$env:DEEPSEEK_API_KEY
try{
 $env:DEEPSEEK_API_KEY=$null
 Start-Process ($url+'/#access='+$token)
}finally{$env:DEEPSEEK_API_KEY=$before;$token=$null}
Write-Host "TaskDesk открыт: $url · пользователь $UserID"
Write-Host 'Для остановки: .\Start-Support-Assistant.ps1 -Stop'
