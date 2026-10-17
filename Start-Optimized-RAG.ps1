[CmdletBinding()]
param(
 [ValidateSet("q4","q8")][string]$Variant="q8",
 [string]$Python='C:\Users\erdni\Documents\ChatGPT\RAG\.venv\Scripts\python.exe',
 [string]$ModelCache='C:\Users\erdni\Documents\ChatGPT\RAG\data\models'
)
$ErrorActionPreference='Stop'
Set-Location $PSScriptRoot
$ollamaCommand=Get-Command ollama -ErrorAction SilentlyContinue
if (-not $ollamaCommand) {
    $installed=Join-Path $env:LOCALAPPDATA 'Programs\Ollama\ollama.exe'
    if (-not (Test-Path -LiteralPath $installed)) { throw 'Install Ollama from https://ollama.com/download/windows first.' }
    $ollamaPath=$installed
} else { $ollamaPath=$ollamaCommand.Source }
try { $null=Invoke-RestMethod 'http://127.0.0.1:11434/api/version' -TimeoutSec 3 }
catch {
    Start-Process -FilePath $ollamaPath -ArgumentList 'serve' -WindowStyle Hidden
    $ready=$false
    for($i=0;$i -lt 20;$i++) {
        Start-Sleep -Milliseconds 500
        try { $null=Invoke-RestMethod 'http://127.0.0.1:11434/api/version' -TimeoutSec 2; $ready=$true; break } catch {}
    }
    if (-not $ready) { throw 'Local Ollama API did not start.' }
}
$baseModel=if($Variant -eq 'q8'){'qwen3.5:4b-q8_0'}else{'qwen3.5:4b'}
$model="qwen3.5:4b-rag-opt-$Variant"
$models=Invoke-RestMethod 'http://127.0.0.1:11434/api/tags' -TimeoutSec 5
if ($baseModel -notin @($models.models.name)) {
    Write-Host "Downloading local model: $baseModel"
    & $ollamaPath pull $baseModel
    if ($LASTEXITCODE -ne 0) { throw 'Model download failed.' }
}
if ($model -notin @($models.models.name)) {
    & $ollamaPath create $model -f (Join-Path $PSScriptRoot "examples\local-optimization\Modelfile.$Variant")
    if ($LASTEXITCODE -ne 0) { throw 'Local RAG model configuration failed.' }
}
# Warm up the model before the client's HTTP deadline starts.
$warm=@{model=$model;prompt='';stream=$false;keep_alive='30m'} | ConvertTo-Json -Depth 4
$null=Invoke-RestMethod 'http://127.0.0.1:11434/api/generate' -Method Post -ContentType 'application/json' -Body $warm -TimeoutSec 180
$state=Join-Path $PSScriptRoot ".local-llm\optimized-$Variant-appdata"
$profileDir=Join-Path $state 'deepseek-client'
New-Item -ItemType Directory -Force $profileDir | Out-Null
$configPath=Join-Path $profileDir 'config.json'
if (-not (Test-Path -LiteralPath $configPath)) {
    Copy-Item -LiteralPath (Join-Path $PSScriptRoot "examples\local-optimization\$Variant.json") -Destination $configPath
}
# Child-process environment: isolate private history and remove provider overrides.
$names=@('APPDATA','OPENAI_BASE_URL','OPENAI_MODEL','OPENAI_API_KEY','RAG_URL','GIT_MCP_URL','GIT_MCP_TOKEN','MCP_SERVERS_FILE','DEEPSEEK_RAG_PYTHON','DEEPSEEK_RAG_MODEL_CACHE','HF_HUB_OFFLINE','TRANSFORMERS_OFFLINE')
$saved=@{}
foreach($name in $names) { $saved[$name]=[Environment]::GetEnvironmentVariable($name,'Process') }
try {
    foreach($name in $names) { [Environment]::SetEnvironmentVariable($name,$null,'Process') }
    if (-not (Test-Path -LiteralPath $Python)) { throw 'Pass -Python pointing to an existing RAG environment.' }
    if (-not (Test-Path -LiteralPath $ModelCache)) { throw 'Pass -ModelCache pointing to the existing local model cache.' }
    $env:DEEPSEEK_RAG_PYTHON=(Resolve-Path -LiteralPath $Python).Path
    $env:DEEPSEEK_RAG_MODEL_CACHE=(Resolve-Path -LiteralPath $ModelCache).Path
    $env:HF_HUB_OFFLINE='1'
    $env:TRANSFORMERS_OFFLINE='1'
    $env:APPDATA=$state
    Write-Host 'In the client: F2 -> 7 -> select artifacts/hw21/index.json. Keep tools enabled for retrieval.'
    Write-Host "Profile: $model; ctx=8192; max_output=1024; temperature=0.1; RAG template=compact"
    Write-Host 'Building local client...'
    Push-Location (Join-Path $PSScriptRoot 'projects\deepseek-client')
    try {
        go build -o (Join-Path $PSScriptRoot 'bin\deepseek-local.exe') .
        if($LASTEXITCODE -ne 0) { throw 'Go build failed.' }
    } finally { Pop-Location }
    & (Join-Path $PSScriptRoot 'bin\deepseek-local.exe')
} finally {
    foreach($name in $names) { [Environment]::SetEnvironmentVariable($name,$saved[$name],'Process') }
}
