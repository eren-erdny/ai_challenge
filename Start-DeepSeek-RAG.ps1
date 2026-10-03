# Run directly or double-click Start-DeepSeek-RAG.cmd.
[CmdletBinding()]
param(
    [string]$Documents,
    [switch]$Reindex,
    [string]$Python,
    [switch]$Check
)

$ErrorActionPreference = 'Stop'
$serviceDirectory = Join-Path $PSScriptRoot 'projects\deepseek-client\rag-service'
$dataDirectory = Join-Path $serviceDirectory 'data'
$pythonExecutable = Join-Path $serviceDirectory '.venv\Scripts\python.exe'
$ragProcess = $null
$ollamaProcess = $null
$previousDirectory = Get-Location
$environmentNames = @('RAG_URL', 'RAG_DATA_DIR', 'RAG_MODEL_CACHE', 'OLLAMA_URL', 'RAG_EMBEDDING_MODEL', 'QDRANT_URL', 'GOCACHE')
$previousEnvironment = @{}
foreach ($name in $environmentNames) { $previousEnvironment[$name] = [Environment]::GetEnvironmentVariable($name, 'Process') }

function Invoke-Checked([string]$Executable, [string[]]$Arguments) {
    & $Executable @Arguments
    if ($LASTEXITCODE -ne 0) { throw "Command failed ($LASTEXITCODE): $Executable $($Arguments -join ' ')" }
}

function Find-Executable([string]$Name, [string[]]$Candidates) {
    $command = Get-Command $Name -ErrorAction SilentlyContinue
    if ($command) { return $command.Source }
    foreach ($candidate in $Candidates) {
        if (Test-Path -LiteralPath $candidate -PathType Leaf) { return $candidate }
    }
    return $null
}

function Test-LocalEndpoint([string]$Address) {
    try { return Invoke-RestMethod -Uri $Address -TimeoutSec 2 -ErrorAction Stop } catch { return $null }
}

try {
    $goExecutable = Find-Executable 'go.exe' @('C:\Program Files\Go\bin\go.exe', 'C:\Program Files (x86)\Go\bin\go.exe')
    $ollamaExecutable = Find-Executable 'ollama.exe' @((Join-Path $env:LOCALAPPDATA 'Programs\Ollama\ollama.exe'))
    if (-not $goExecutable) { throw 'Go is required. Install Go and run this launcher again.' }
    if (-not $ollamaExecutable) { throw 'Ollama is required. Install it from https://ollama.com/download/windows and run this launcher again.' }

    if (-not (Test-Path -LiteralPath $pythonExecutable)) {
        if ($Python) { $bootstrapPython = (Resolve-Path -LiteralPath $Python).Path }
        else {
            $bootstrapPython = Find-Executable 'python.exe' @((Join-Path $env:USERPROFILE '.cache\codex-runtimes\codex-primary-runtime\dependencies\python\python.exe'))
            if (-not $bootstrapPython) { $bootstrapPython = Find-Executable 'py.exe' @() }
        }
        if (-not $bootstrapPython) { throw 'Python 3.12+ is required. Install Python or pass -Python C:\path\python.exe.' }
        if (-not $Check) { Invoke-Checked $bootstrapPython @('-m', 'venv', (Join-Path $serviceDirectory '.venv')) }
    }
    if ($Check) {
        Write-Host "Go: $goExecutable"
        Write-Host "Ollama: $ollamaExecutable"
        if (Test-Path -LiteralPath $pythonExecutable) { Write-Host "Python: $pythonExecutable" } else { Write-Host "Python bootstrap: $bootstrapPython" }
        Write-Host 'Prerequisites found. No downloads or services started.'
        return
    }

    # This convenience launcher owns an isolated Local Qdrant deployment.
    $env:RAG_DATA_DIR = $dataDirectory
    $env:RAG_MODEL_CACHE = Join-Path $dataDirectory 'models'
    $env:OLLAMA_URL = 'http://127.0.0.1:11434'
    $env:RAG_EMBEDDING_MODEL = 'nomic-embed-text:latest'
    $env:QDRANT_URL = $null
    $env:RAG_URL = 'http://127.0.0.1:8765'
    $env:GOCACHE = Join-Path $PSScriptRoot '.gocache'
    New-Item -ItemType Directory -Force -Path $dataDirectory | Out-Null
    if (Test-LocalEndpoint "$env:RAG_URL/health") { throw 'A RAG service is already running on port 8765. Close the other RAG launcher before starting this one.' }
    Set-Location $serviceDirectory
    $requirements = Join-Path $serviceDirectory 'requirements.txt'
    $requirementsHash = (Get-FileHash -LiteralPath $requirements -Algorithm SHA256).Hash
    $dependencyMarker = Join-Path $serviceDirectory '.venv\rag-dependencies.txt'
    if (-not (Test-Path -LiteralPath $dependencyMarker) -or (Get-Content -LiteralPath $dependencyMarker -Raw).Trim() -ne $requirementsHash) {
        Write-Host 'First setup: installing RAG dependencies. This can take several minutes.'
        Invoke-Checked $pythonExecutable @('-m', 'pip', 'install', '-r', $requirements)
        Set-Content -LiteralPath $dependencyMarker -Value $requirementsHash -Encoding ascii
    }

    if (-not (Test-LocalEndpoint "$env:OLLAMA_URL/api/tags")) {
        Write-Host 'Starting local Ollama...'
        $ollamaProcess = Start-Process -FilePath $ollamaExecutable -ArgumentList 'serve' -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $dataDirectory 'ollama-output.log') -RedirectStandardError (Join-Path $dataDirectory 'ollama-error.log')
        for ($attempt = 0; $attempt -lt 40; $attempt++) {
            if (Test-LocalEndpoint "$env:OLLAMA_URL/api/tags") { break }
            if ($ollamaProcess.HasExited) { throw 'Ollama failed to start. See rag-service\data\ollama-error.log.' }
            Start-Sleep -Milliseconds 500
        }
        if (-not (Test-LocalEndpoint "$env:OLLAMA_URL/api/tags")) { throw 'Ollama did not become ready.' }
    }
    $tags = Test-LocalEndpoint "$env:OLLAMA_URL/api/tags"
    if (-not ($tags.models | Where-Object { $_.name -eq 'nomic-embed-text:latest' })) {
        Write-Host 'Downloading embedding model...'
        Invoke-Checked $ollamaExecutable @('pull', 'nomic-embed-text')
    }
    $modelMarker = Join-Path $dataDirectory 'models-ready.txt'
    if (-not (Test-Path -LiteralPath $modelMarker)) {
        Write-Host 'Downloading local tokenizer and reranker (first setup)...'
        Invoke-Checked $pythonExecutable @('-m', 'rag', 'download-models')
        Set-Content -LiteralPath $modelMarker -Value 'ready' -Encoding ascii
    }
    $manifestPath = Join-Path $dataDirectory 'index.json'
    if ($Reindex -or $Documents -or -not (Test-Path -LiteralPath $manifestPath)) {
        if (-not $Documents -and (Test-Path -LiteralPath $manifestPath)) {
            $Documents = (Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json).source_root
        }
        if (-not $Documents) {
            Write-Host 'Choose the folder containing documents for DeepSeek. Hidden files are excluded.'
            $Documents = (Read-Host 'Document folder path').Trim().Trim('"')
        }
        if (-not $Documents) { throw 'Document folder is required for the first index.' }
        $Documents = (Resolve-Path -LiteralPath $Documents).Path
        Write-Host "Building document index: $Documents"
        Invoke-Checked $pythonExecutable @('-m', 'rag', 'index', $Documents)
    }

    $ragProcess = Start-Process -FilePath $pythonExecutable -WorkingDirectory $serviceDirectory -ArgumentList @('-m', 'rag', 'serve') -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $dataDirectory 'service-output.log') -RedirectStandardError (Join-Path $dataDirectory 'service-error.log')
    for ($attempt = 0; $attempt -lt 90; $attempt++) {
        if (Test-LocalEndpoint "$env:RAG_URL/health") { break }
        if ($ragProcess.HasExited) { throw 'RAG failed to start. See rag-service\data\service-error.log.' }
        Start-Sleep -Milliseconds 500
    }
    if (-not (Test-LocalEndpoint "$env:RAG_URL/health")) { throw 'RAG did not become ready. See rag-service\data\service-error.log.' }
    Set-Location $PSScriptRoot
    Write-Host 'RAG ready. Ask DeepSeek to search your indexed documents and cite sources.'
    Invoke-Checked $goExecutable @('run', './projects/deepseek-client')
} catch {
    Write-Host $_.Exception.Message -ForegroundColor Red
    exit 1
} finally {
    foreach ($ownedProcess in @($ragProcess, $ollamaProcess)) {
        if ($null -ne $ownedProcess -and -not $ownedProcess.HasExited) { Stop-Process -Id $ownedProcess.Id -ErrorAction SilentlyContinue }
    }
    foreach ($name in $environmentNames) { [Environment]::SetEnvironmentVariable($name, $previousEnvironment[$name], 'Process') }
    Set-Location $previousDirectory
}
