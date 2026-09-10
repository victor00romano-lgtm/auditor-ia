$ErrorActionPreference = 'Stop'
$json = docker compose --profile tools config --format json | ConvertFrom-Json
$service = $json.services.'auditor-tui'
$api = $json.services.'auditor-api'
if ($null -eq $service) { throw 'serviço auditor-tui ausente' }
if ($service.user -ne '65532:65532') { throw "usuário inseguro: $($service.user)" }
if (-not $service.read_only) { throw 'root filesystem não está read-only' }
if (-not $service.stdin_open -or -not $service.tty) { throw 'stdin/tty não configurados' }
if ($service.privileged) { throw 'serviço privileged' }
if ($service.cap_drop -notcontains 'ALL') { throw 'cap_drop ALL ausente' }
if ($service.security_opt -notcontains 'no-new-privileges:true') { throw 'no-new-privileges ausente' }
if ($service.profiles -notcontains 'tools') { throw 'profile tools ausente' }
if ($service.environment.REPORTS_DIR -ne '/reports') { throw 'REPORTS_DIR incorreto' }
if ($service.environment.OLLAMA_URL -ne 'http://ollama:11434') { throw 'OLLAMA_URL interno incorreto' }
if ($service.environment.DATABASE_URL -notmatch '@postgres:5432/auditor_ia') { throw 'DATABASE_URL não usa PostgreSQL interno' }
if ($service.ports.Count -gt 0) { throw 'auditor-tui não deve publicar portas' }
if ($service.image -ne $api.image) { throw 'API e TUI não reutilizam a mesma imagem' }
Write-Output 'auditor-tui: configuração não root, read-only e profile tools validada.'
