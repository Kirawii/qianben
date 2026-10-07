param([ValidateSet('init','api','worker','test','apk')][string]$Task='api')
$ErrorActionPreference='Stop'
$taskRoot=Split-Path -Parent $PSScriptRoot
$taskGoCommand=Get-Command go -ErrorAction SilentlyContinue
$taskGo=if($taskGoCommand){$taskGoCommand.Source}else{Join-Path $taskRoot '.tools/go/bin/go.exe'}
if($Task -ne 'apk' -and !(Test-Path -LiteralPath $taskGo)){throw 'Install Go 1.24 or newer first.'}
Push-Location (Join-Path $taskRoot 'backend')
try {
 switch($Task){
  'init' {
   docker compose -p qianben -f ../compose.yaml up -d --wait
   if($LASTEXITCODE -ne 0){throw 'PostgreSQL startup failed'}
   $env:DATABASE_URL='postgres://qianben_admin:local-development-only@127.0.0.1:15432/qianben?sslmode=disable'
   & $taskGo run ./cmd/qianben migrate
   if($LASTEXITCODE -ne 0){throw 'Migration failed'}
   & $taskGo run ./cmd/qianben user
  }
  'api' {if(!$env:DATABASE_URL){$env:DATABASE_URL='postgres://qianben_app:dev-qianben-app@127.0.0.1:15432/qianben?sslmode=disable'};& $taskGo run ./cmd/qianben serve}
  'worker' {if(!$env:DATABASE_URL){$env:DATABASE_URL='postgres://qianben_app:dev-qianben-app@127.0.0.1:15432/qianben?sslmode=disable'};& $taskGo run ./cmd/qianben worker}
  'test' {
   $env:TEST_ADMIN_DATABASE_URL='postgres://qianben_admin:local-development-only@127.0.0.1:15432/qianben?sslmode=disable'
   $env:TEST_DATABASE_URL='postgres://qianben_app:dev-qianben-app@127.0.0.1:15432/qianben?sslmode=disable'
   & $taskGo test ./... -count=1
   if($LASTEXITCODE -ne 0){throw 'Tests failed'}
   & $taskGo vet ./...
  }
  'apk' {& ../android/gradlew.bat -p ../android :app:assembleDebug :app:lintDebug}
 }
 if($LASTEXITCODE -ne 0){throw 'Command failed'}
} finally {Pop-Location}
