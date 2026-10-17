[CmdletBinding()]
param(
 [string]$Python=(Join-Path ([Environment]::GetFolderPath('MyDocuments')) 'ChatGPT/RAG/.venv/Scripts/python.exe'),
 [string]$ModelCache=(Join-Path ([Environment]::GetFolderPath('MyDocuments')) 'ChatGPT/RAG/data/models'),
 [switch]$PrepareOnly,
 [switch]$Rebuild
)
$ErrorActionPreference='Stop'
$repo=$PSScriptRoot
$project=Join-Path $repo 'projects/deepseek-client'
$local=Join-Path $repo '.project-assistant'
$binary=Join-Path $local 'deepseek-client.exe'
$pointer=Join-Path $local 'current-index.txt'
$digestFile=Join-Path $local 'corpus-sha256.json'
if (-not (Test-Path -LiteralPath $Python)) { throw 'Specify -Python with an existing RAG environment.' }
if (-not (Test-Path -LiteralPath $ModelCache)) { throw 'Specify -ModelCache with an existing model cache.' }
New-Item -ItemType Directory -Force $local | Out-Null
$names=@('DEEPSEEK_PROJECT_ROOT','DEEPSEEK_PROJECT_INDEX','DEEPSEEK_PROJECT_STATE','DEEPSEEK_RAG_PYTHON','DEEPSEEK_RAG_MODEL_CACHE','MCP_SERVERS_FILE','GIT_MCP_URL','GIT_MCP_TOKEN','RAG_URL','RAG_DATA_DIR','RAG_MODEL_CACHE','QDRANT_URL','OLLAMA_URL','OLLAMA_HOST','RAG_EMBEDDING_MODEL','HF_HUB_OFFLINE','TRANSFORMERS_OFFLINE','PYTHONUTF8')
$saved=@{}
foreach($name in $names) { $saved[$name]=[Environment]::GetEnvironmentVariable($name,'Process') }
$previousLocation=Get-Location
$ownedOllama=$null
try {
 Set-Location $repo
 foreach($name in $names) { Remove-Item -LiteralPath ("Env:"+$name) -ErrorAction SilentlyContinue }
 $env:DEEPSEEK_PROJECT_ROOT=$project
 $env:DEEPSEEK_PROJECT_STATE=Join-Path $local 'state'
 $env:DEEPSEEK_RAG_PYTHON=(Resolve-Path -LiteralPath $Python).Path
 $env:DEEPSEEK_RAG_MODEL_CACHE=(Resolve-Path -LiteralPath $ModelCache).Path
 $env:RAG_MODEL_CACHE=$env:DEEPSEEK_RAG_MODEL_CACHE
 $env:OLLAMA_URL='http://127.0.0.1:11434'
 $env:OLLAMA_HOST='127.0.0.1:11434'
 $env:RAG_EMBEDDING_MODEL='nomic-embed-text:latest'
 $env:HF_HUB_OFFLINE='1'
 $env:TRANSFORMERS_OFFLINE='1'
 $env:PYTHONUTF8='1'
 try { $tags=Invoke-RestMethod 'http://127.0.0.1:11434/api/tags' -TimeoutSec 3 }
 catch {
  $ollama=Get-Command ollama -ErrorAction SilentlyContinue
  $ollamaPath=if($ollama){$ollama.Source}else{Join-Path $env:LOCALAPPDATA 'Programs/Ollama/ollama.exe'}
  if(-not(Test-Path -LiteralPath $ollamaPath)){throw 'Install/start Ollama and prepare nomic-embed-text:latest.'}
  $ownedOllama=Start-Process -FilePath $ollamaPath -ArgumentList 'serve' -WindowStyle Hidden -PassThru
  $tags=$null
  for($attempt=0;$attempt -lt 30;$attempt++){
   Start-Sleep -Milliseconds 500
   try { $tags=Invoke-RestMethod 'http://127.0.0.1:11434/api/tags' -TimeoutSec 2; break } catch {}
  }
  if(-not $tags){throw 'Ollama did not start.'}
 }
 if('nomic-embed-text:latest' -notin @($tags.models.name)){throw 'Prepare nomic-embed-text:latest in Ollama first; this launcher does not download models.'}
 & go build -o $binary ./projects/deepseek-client
 if($LASTEXITCODE -ne 0){throw 'Client build failed.'}
 $files=@((Join-Path $project 'README.md'))+@(Get-ChildItem -LiteralPath (Join-Path $project 'docs') -File -Recurse | Sort-Object FullName | ForEach-Object {$_.FullName})
 $hashes=[ordered]@{}
 foreach($file in $files){$hashes[[IO.Path]::GetRelativePath($project,$file)]=(Get-FileHash -LiteralPath $file -Algorithm SHA256).Hash}
 $digest=$hashes | ConvertTo-Json -Compress
 $manifest=$null
 if((Test-Path -LiteralPath $pointer) -and -not $Rebuild){
  $manifest=(Get-Content -LiteralPath $pointer -Raw).Trim()
  if(-not(Test-Path -LiteralPath $digestFile) -or (Get-Content -LiteralPath $digestFile -Raw).Trim() -ne $digest){throw 'Documentation changed. Run with -Rebuild.'}
  $resolved=[IO.Path]::GetFullPath($manifest)
  $allowed=[IO.Path]::GetFullPath((Join-Path $local 'indexes'))+[IO.Path]::DirectorySeparatorChar
  if(-not $resolved.StartsWith($allowed,[StringComparison]::OrdinalIgnoreCase) -or -not(Test-Path -LiteralPath $resolved)){throw 'Invalid index pointer; run with -Rebuild.'}
 }
 if(-not $manifest){
  $indexDir=Join-Path $local ('indexes/'+[guid]::NewGuid().ToString('N'))
  New-Item -ItemType Directory -Force $indexDir | Out-Null
  $env:RAG_DATA_DIR=$indexDir
  $manifest=Join-Path $indexDir 'index.json'
  Push-Location (Join-Path $project 'rag-service')
  try {
   & $Python -m rag index (Join-Path $project 'README.md') --add-source (Join-Path $project 'docs') --output $manifest
   if($LASTEXITCODE -ne 0){throw 'Project documentation indexing failed.'}
  } finally { Pop-Location }
  Set-Content -LiteralPath $digestFile -Value $digest -Encoding utf8NoBOM
  Set-Content -LiteralPath $pointer -Value $manifest -Encoding utf8NoBOM
 }
 $env:DEEPSEEK_PROJECT_INDEX=$manifest
 & $binary --mcp-multi-call git__git_status '{}'
 if($LASTEXITCODE -ne 0){throw 'Local Git MCP check failed.'}
 Write-Host 'Project: DeepSeek Client; corpus: project README.md + docs; MCP: local Git over stdio.'
 if(-not $PrepareOnly){
  Write-Host 'Ask: /help Как устроен проект?'
  & $binary
  if($LASTEXITCODE -ne 0){throw 'Client exited with an error.'}
 }
} finally {
 foreach($name in $names){
  if($null -eq $saved[$name]){Remove-Item -LiteralPath ("Env:"+$name) -ErrorAction SilentlyContinue}
  else{[Environment]::SetEnvironmentVariable($name,$saved[$name],'Process')}
 }
 Set-Location $previousLocation
 if($ownedOllama -and -not $ownedOllama.HasExited){$ownedOllama.Kill();$ownedOllama.WaitForExit()}
}
