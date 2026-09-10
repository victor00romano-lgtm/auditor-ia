$ErrorActionPreference = "Stop"

docker compose --profile observability config | Out-Null
$health = Invoke-RestMethod http://localhost:8080/health
$ready = Invoke-RestMethod http://localhost:8080/ready
$prometheus = Invoke-WebRequest http://localhost:9090/-/ready -UseBasicParsing

if ($health.data.status -ne "ok") { throw "liveness inválida" }
if ($ready.data.status -notin @("ready", "degraded")) { throw "readiness inválida" }
if ($prometheus.StatusCode -ne 200) { throw "Prometheus indisponível" }

$config = docker compose --profile observability config
if ($config -match '5432:5432|11434:11434|9091:9091|4317:4317') { throw "porta interna publicada no host" }

Write-Host "Observabilidade validada: health, readiness, Prometheus e isolamento de portas."
