param([Parameter(Mandatory)][string]$Server,[string]$KeyPath="$env:USERPROFILE\.ssh\hw1_deploy")
$ErrorActionPreference='Stop'
$root=Split-Path (Split-Path $PSScriptRoot -Parent) -Parent
$output=Join-Path $root 'artifacts/hw30'
New-Item -ItemType Directory -Force $output | Out-Null
$savedOS=$env:GOOS;$savedArch=$env:GOARCH;$savedCGO=$env:CGO_ENABLED
Push-Location (Join-Path $root 'projects/deepseek-client')
try {
 $env:GOOS='linux';$env:GOARCH='amd64';$env:CGO_ENABLED='0'
 go build -o (Join-Path $output 'private-llm') ./cmd/private-llm
 if($LASTEXITCODE -ne 0){throw 'Build failed'}
} finally {Pop-Location;$env:GOOS=$savedOS;$env:GOARCH=$savedArch;$env:CGO_ENABLED=$savedCGO}
ssh -o BatchMode=yes -i $KeyPath $Server 'mkdir -p /opt/hw30'
if($LASTEXITCODE -ne 0){throw 'SSH failed'}
scp -i $KeyPath (Join-Path $output 'private-llm') "${Server}:/opt/hw30/private-llm.next"
if($LASTEXITCODE -ne 0){throw 'Binary upload failed'}
scp -i $KeyPath (Join-Path $PSScriptRoot 'private-llm.service') "${Server}:/opt/hw30/"
if($LASTEXITCODE -ne 0){throw 'Upload failed'}
ssh -o BatchMode=yes -i $KeyPath $Server 'install -m 755 /opt/hw30/private-llm.next /opt/hw30/private-llm && install -m 644 /opt/hw30/private-llm.service /etc/systemd/system/private-llm.service && systemctl daemon-reload && systemctl enable --now private-llm && systemctl restart private-llm'
if($LASTEXITCODE -ne 0){throw 'Service installation failed'}
