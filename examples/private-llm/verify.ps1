param([string]$BaseURL='http://127.0.0.1:11437',[string]$OutputPath='')
$ErrorActionPreference='Stop'
$results=[System.Collections.Generic.List[object]]::new()
function Check([string]$Name,[string]$Body,[int]$Expected) {
    $timer=[Diagnostics.Stopwatch]::StartNew()
    $response=Invoke-WebRequest -Uri "$BaseURL/api/chat" -Method Post -ContentType 'application/json' -Body $Body -SkipHttpErrorCheck -TimeoutSec 180
    $parsed=$response.Content | ConvertFrom-Json
    $entry=[ordered]@{name=$Name;status=[int]$response.StatusCode;expected=$Expected;passed=([int]$response.StatusCode -eq $Expected);seconds=[math]::Round($timer.Elapsed.TotalSeconds,3)}
    if ($parsed.message) {$entry.answer=$parsed.message.content;$entry.prompt_tokens=$parsed.prompt_eval_count;$entry.output_tokens=$parsed.eval_count}
    if ($parsed.error) {$entry.error=$parsed.error.message}
    $results.Add($entry)
    Write-Host "$Name : HTTP $($entry.status), pass=$($entry.passed)"
    return $parsed
}
$health=Invoke-RestMethod "$BaseURL/health"
$null=Check 'context_rejected' '{"messages":[{"role":"user","content":"hello"}],"options":{"num_ctx":8192}}' 400
$null=Check 'output_rejected' '{"messages":[{"role":"user","content":"hello"}],"max_tokens":129}' 400
$long=@{messages=@(@{role='user';content=('a'*4097)})} | ConvertTo-Json -Depth 5 -Compress
$null=Check 'input_rejected' $long 413
$first=Check 'chat_turn_1' '{"model":"smollm2:135m","messages":[{"role":"user","content":"My name is Alex. Say hello briefly."}],"options":{"num_predict":32}}' 200
$follow=@{model='smollm2:135m';messages=@(@{role='user';content='My name is Alex. Say hello briefly.'},@{role='assistant';content=$first.message.content},@{role='user';content='What is my name?'});options=@{num_predict=32}} | ConvertTo-Json -Depth 6 -Compress
$null=Check 'chat_turn_2' $follow 200
$jobs=1..2 | ForEach-Object {Start-Job -ArgumentList $_,$BaseURL -ScriptBlock {
 param($n,$url)
 $r=Invoke-WebRequest -Uri "$url/api/chat" -Method Post -ContentType 'application/json' -Body '{"messages":[{"role":"user","content":"Say hello briefly."}],"options":{"num_predict":32}}' -SkipHttpErrorCheck -TimeoutSec 180
 [pscustomobject]@{name="concurrent_$n";status=[int]$r.StatusCode;expected=200;passed=([int]$r.StatusCode -eq 200)}
}}
try {$jobs | Wait-Job | Receive-Job | ForEach-Object {$results.Add($_); Write-Host "$($_.name) : HTTP $($_.status), pass=$($_.passed)"}} finally {$jobs | Remove-Job -Force}
$null=Check 'sequential_5' '{"messages":[{"role":"user","content":"Say hi."}],"options":{"num_predict":16}}' 200
$null=Check 'rate_limit_6' '{"messages":[{"role":"user","content":"Say hi."}]}' 429
$openai=Invoke-RestMethod "$BaseURL/v1/models"
$report=[ordered]@{verified_at=(Get-Date -Format o);transport='SSH tunnel from Windows PC to VPS loopback';health=$health;models=$openai;results=@($results.ToArray());passed=(@($results | Where-Object {-not $_.passed}).Count -eq 0)}
if($OutputPath){$report | ConvertTo-Json -Depth 10 | Set-Content -Encoding utf8 $OutputPath}
if(-not $report.passed){throw 'Some checks failed'}
Write-Host 'All network checks passed.'
