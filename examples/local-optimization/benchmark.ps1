[CmdletBinding()]
param([ValidateSet('before','q4','q8')][string]$Variant,[Parameter(Mandatory)][string]$OutputDirectory)
$ErrorActionPreference='Stop'
$root=Split-Path (Split-Path $PSScriptRoot -Parent) -Parent
$out=[IO.Path]::GetFullPath($OutputDirectory)
if(Test-Path -LiteralPath $out){throw 'Use a fresh output directory.'}
$profile=Get-Content (Join-Path $PSScriptRoot "$Variant.json") -Raw | ConvertFrom-Json
$model=$profile.profiles.ollama_optimized.model
$warm=@{model=$model;prompt='';stream=$false;keep_alive='30m'} | ConvertTo-Json
$null=Invoke-RestMethod http://127.0.0.1:11434/api/generate -Method Post -Body $warm -ContentType application/json -TimeoutSec 180
$env:HW29_LOCAL_TEST='1';$env:HW29_VARIANT=$Variant;$env:HW29_OUTPUT=$out
$env:DEEPSEEK_RAG_PYTHON='C:\Users\erdni\Documents\ChatGPT\RAG\.venv\Scripts\python.exe'
$env:DEEPSEEK_RAG_MODEL_CACHE='C:\Users\erdni\Documents\ChatGPT\RAG\data\models'
$env:HF_HUB_OFFLINE='1';$env:TRANSFORMERS_OFFLINE='1'
$trace=Join-Path $root ".local-llm\hw29-$Variant-$([DateTime]::UtcNow.Ticks)"
New-Item -ItemType Directory $trace | Out-Null
$process=Start-Process (Join-Path $root 'bin\hw29-demo.exe') -ArgumentList '-test.run=^TestHomework29Optimization$','-test.v' -WorkingDirectory (Join-Path $root 'projects\deepseek-client') -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $trace 'stdout.txt') -RedirectStandardError (Join-Path $trace 'stderr.txt')
$samples=[Collections.Generic.List[object]]::new()
while(-not $process.HasExited){
 $gpu=& nvidia-smi --query-gpu=memory.used,utilization.gpu,power.draw --format=csv,noheader,nounits
 $ollama=@(Get-Process -Name '*ollama*' -ErrorAction SilentlyContinue)
 $python=@(Get-Process -Name 'python*' -ErrorAction SilentlyContinue)
 $samples.Add(@{utc=[DateTime]::UtcNow.ToString('o');gpu_csv=$gpu;ollama_working_set_bytes=($ollama|Measure-Object WorkingSet64 -Sum).Sum;python_working_set_bytes=($python|Measure-Object WorkingSet64 -Sum).Sum})
 Start-Sleep -Milliseconds 1000
 $process.Refresh()
}
$process.WaitForExit()
if(Test-Path $out){
 $samples|ConvertTo-Json -Depth 5 | Set-Content (Join-Path $out 'resources.json') -Encoding utf8
 Copy-Item (Join-Path $trace 'stdout.txt') (Join-Path $out 'test.txt')
 Copy-Item (Join-Path $trace 'stderr.txt') (Join-Path $out 'stderr.txt')
 $show=Invoke-RestMethod http://127.0.0.1:11434/api/show -Method Post -Body (@{model=$model}|ConvertTo-Json) -ContentType application/json
 $show|ConvertTo-Json -Depth 30 | Set-Content (Join-Path $out 'model.json') -Encoding utf8
}
Get-Content (Join-Path $trace 'stdout.txt') -Tail 23
if($process.ExitCode -ne 0){Get-Content (Join-Path $trace 'stderr.txt');throw "Benchmark exit: $($process.ExitCode)"}
