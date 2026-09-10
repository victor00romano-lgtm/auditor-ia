# Auditor IA

## Segurança e privacidade

Copie `.env.example` para `.env` local e substitua todos os placeholders; nunca versione `.env`. O padrão `AI_PRIVACY_MODE=anonymize` remove dados pessoais imediatamente antes do Ollama. Em produção, `preserve` só é aceito com `ALLOW_PII_TO_AI=true`. Credenciais e conteúdo de conversas não devem ser registrados.

```powershell
go run ./cmd/securitycheck
powershell -ExecutionPolicy Bypass -File .\scripts\check-secrets.ps1
go run ./cmd/retention --dry-run
go run ./cmd/datadelete --deal 8260 --dry-run
```

Aplicação destrutiva exige confirmação adicional: `retention --apply --confirm-retention-delete` ou `datadelete --apply --confirm-deal-id 8260`. Consulte [retenção](docs/data-retention.md), [dados pessoais](docs/personal-data.md), [rotação de credenciais](docs/credential-rotation.md) e [papéis PostgreSQL](docs/postgres-roles.md).

Auditor configurável para dados do Bitrix24, escrito em Go como monólito modular com DDD e Clean Architecture. As regras são avaliadas localmente e, opcionalmente, os achados recebem uma análise textual do Ollama.

## Executar no Windows, sem Docker

O projeto não carrega `.env` automaticamente. Crie o arquivo local e importe suas variáveis para cada sessão do PowerShell que executar a API, a TUI ou o worker:

```powershell
Copy-Item .env.example .env
Get-Content .env | ForEach-Object {
    if ($_ -match '^\s*([^#][^=]*)=(.*)$') {
        [Environment]::SetEnvironmentVariable($matches[1].Trim(), $matches[2].Trim(), 'Process')
    }
}

go mod tidy
go test ./...
go run ./cmd/auditor
```

Use endereços do host na configuração nativa:

```text
DATABASE_URL=postgres://USUARIO:SENHA@localhost:5432/auditor_ia?sslmode=disable
OLLAMA_URL=http://localhost:11434
```

### TUI pelo Docker Compose (opcional)

A TUI usa a mesma imagem da API, mas só é iniciada explicitamente pelo profile `tools`. PostgreSQL e Ollama permanecem internos à rede do Compose e não publicam portas no host.

```powershell
New-Item -ItemType Directory -Force .\reports | Out-Null
docker compose up -d postgres ollama
docker compose --profile tools run --rm auditor-tui
```

O comando anexado preserva tamanho do terminal, stdin e sinais. Use `q` ou `Ctrl+C` para sair; `--rm` remove o container da ferramenta. PDFs gerados pela tecla `p` aparecem em `./reports` no Windows por meio de `REPORTS_DIR=/reports`.

Validação do hardening:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\validate-tui-container.ps1
docker compose --profile tools config
docker compose build auditor-tui
```

Teste manual: abra a TUI, navegue por Visão Geral, Achados e Detalhes, gere um PDF com `p`, confirme o arquivo em `./reports`, saia com `q` e repita saindo com `Ctrl+C`. Ao final, `docker ps -a --filter label=com.docker.compose.service=auditor-tui` não deve mostrar container remanescente da execução `--rm`.

Sem `BITRIX_WEBHOOK_URL`, o programa usa dados de demonstração. Sem Ollama disponível, a auditoria continua funcionando e exibe os achados determinísticos.

## API HTTP

Variáveis obrigatórias:

```powershell
$env:DATABASE_URL = "postgres://auditor:SENHA@localhost:5432/auditor_ia"
$env:BITRIX_WEBHOOK_URL = "https://seu-bitrix/rest/usuario/token/"
$env:BITRIX_SUPPORT_ASSIGNEE_IDS = "3066"
$env:AUDITOR_API_KEY = "uma-chave-local-forte"
$env:OLLAMA_URL = "http://localhost:11434"
$env:OLLAMA_MODEL = "gemma3:4b"
$env:BITRIX_HTTP_TIMEOUT = "30s"
$env:OLLAMA_HTTP_TIMEOUT = "9m"
$env:API_AUDIT_TIMEOUT = "10m"
```

`BITRIX_SUPPORT_ASSIGNEE_IDS` aceita um ou mais IDs estáveis do Bitrix separados por vírgula. O ID `3066`, confirmado para Camila Rodrigues, é o padrão. Em negócios abertos, esses responsáveis determinam `pós-venda/suporte`; estágios ganhos (`S`) e perdidos (`F`) continuam tendo precedência.

Inicie o PostgreSQL instalado no Windows e aplique as migrations em ordem com o `psql`. Para uma instalação existente, aplique somente as migrations ainda pendentes. A funcionalidade de lotes exige a `009`:

```powershell
$psql = "C:\Program Files\PostgreSQL\17\bin\psql.exe"
& $psql "$env:DATABASE_MIGRATOR_URL" -v ON_ERROR_STOP=1 -f .\migrations\009_audit_batches.sql
```

Inicie a API:

```powershell
go run ./cmd/api
```

O endpoint de saúde não exige autenticação:

```powershell
Invoke-RestMethod http://localhost:8080/health
```

As rotas `/api/v1` exigem `X-API-Key`:

```powershell
$headers = @{ "X-API-Key" = $env:AUDITOR_API_KEY }

$audit = Invoke-RestMethod -Method Post `
  -Uri http://localhost:8080/api/v1/audits `
  -Headers $headers `
  -ContentType "application/json" `
  -Body '{"bitrix_deal_id":8620}'

Invoke-RestMethod `
  -Uri "http://localhost:8080/api/v1/audits?page=1&page_size=20&bitrix_deal_id=8620" `
  -Headers $headers

Invoke-RestMethod `
  -Uri "http://localhost:8080/api/v1/findings?assessment_id=$($audit.data.assessment_id)" `
  -Headers $headers

Invoke-WebRequest `
  -Uri "http://localhost:8080/api/v1/audits/$($audit.data.assessment_id)/report.pdf" `
  -Headers $headers `
  -OutFile ".\auditoria-$($audit.data.assessment_id).pdf"
```

O `POST /api/v1/audits` é síncrono no MVP e pode demorar enquanto o Ollama local processa a conversa. Consultas e geração de PDF usam exclusivamente dados persistidos e não chamam novamente Bitrix ou Ollama.

Para volumes maiores, `POST /api/v1/audit-batches` cria um lote persistente com até 10.000 IDs. O processo `auditworker` reserva itens no PostgreSQL com `FOR UPDATE SKIP LOCKED`, aplica retry com backoff, recupera leases expirados e persiste o progresso. Consulte `GET /api/v1/audit-batches/{id}` e cancele com `POST /api/v1/audit-batches/{id}/cancel`. A porta `batch/domain.Queue` permite substituir a entrega PostgreSQL por SQS futuramente sem acoplar o caso de uso à AWS.

As estatísticas globais do Bitrix são atualizadas sem Ollama por `go run .\cmd\crmstatssync`. A Visão executiva lê o snapshot local, permite iniciar a coleta com `U` e cancelar com `x`. Fórmulas, cobertura e comandos de comparação estão em [estatísticas globais do CRM](docs/crm-global-statistics.md).

Na TUI, pressione `L` para importar um lote. Cole IDs separados por vírgula, ponto e vírgula, espaço ou quebra de linha; para ler um TXT/CSV local, informe `@` seguido do caminho completo (por exemplo, `@C:\dados\negocios.csv`). O cabeçalho opcional deve ser `id` ou `bitrix_deal_id`. Depois da criação, a tela mostra progresso persistente, atualiza automaticamente e permite solicitar cancelamento com `c`. Mantenha pelo menos um `auditworker` em execução para consumir a fila.

No Windows, execute em dois terminais PowerShell distintos, ambos com as mesmas variáveis importadas do `.env`:

```powershell
# Terminal 1: consumidor persistente da fila
go run .\cmd\auditworker

# Terminal 2: interface para importar a lista e acompanhar o progresso
go run .\cmd\auditor
```

A API é opcional para esse fluxo da TUI. Ela só precisa estar em execução quando os endpoints HTTP de lote também forem utilizados.

Configuração inicial recomendada para Ollama local: `AUDIT_WORKERS=2`, `BITRIX_MAX_CONCURRENCY=3`, `OLLAMA_MAX_CONCURRENCY=1`, `AUDIT_MAX_ATTEMPTS=5`, `AUDIT_LEASE_DURATION=15m` e `AUDIT_QUEUE_POLL_INTERVAL=1s`.

O estado `conversation_status` diferencia `NO_CONVERSATION`, `EMPTY_CONVERSATION`, `ACCESS_DENIED` e `AVAILABLE`. Quando uma sessão é localizada mas o webhook não pode ler o histórico, a auditoria determinística conclui normalmente, preserva score e achados e não chama o Ollama.

Os clientes HTTP são independentes: Bitrix usa `BITRIX_HTTP_TIMEOUT` e Ollama usa
`OLLAMA_HTTP_TIMEOUT`. A inicialização exige a hierarquia
`BITRIX_HTTP_TIMEOUT < OLLAMA_HTTP_TIMEOUT < API_AUDIT_TIMEOUT`.

A especificação OpenAPI está em [`docs/openapi.yaml`](docs/openapi.yaml).

Analisar um negócio usando conversas IMOPENLINES e atividades de formulário CRM
(`CRM_WEBFORM`):

```bash
go run ./cmd/conversation 4
```

O fluxo mantém as conversas e os formulários como contextos distintos. Conversas
incluem chats encerrados vinculados ao negócio ou ao contato, com deduplicação e
recorte pelo período do negócio. Os formulários são lidos de `crm.activity.list` e
podem ser analisados mesmo quando não existe conversa IMOPENLINES.

Por padrão, valores pessoais de formulários e conversas são anonimizados imediatamente
antes do Ollama. O modo `preserve` deve ser explícito e, em produção, exige
`ALLOW_PII_TO_AI=true`. Credenciais, tokens e URLs secretas nunca são enviados.
Formulários indicam interesse ou solicitação, mas não comprovam pagamento ou venda.
O conteúdo não é escrito nos logs; apenas IDs técnicos e contagens são exibidos.

## Observabilidade

Logs estruturados, métricas Prometheus, traces OpenTelemetry, readiness e o perfil local Grafana/Tempo estão documentados em [`docs/observability.md`](docs/observability.md).

```powershell
docker compose --profile observability up -d
.\scripts\validate-observability.ps1
```

## Dashboard empresarial

A TUI é a central de inteligência comercial baseada somente em dados persistidos; o Grafana permanece dedicado à observabilidade técnica. Abra com `go run ./cmd/auditor` ou `docker compose --profile tools run --rm auditor-tui`.

O menu oferece visão executiva, funil, prioridades, divergências CRM × IA, qualidade do CRM, conversas, objeções, equipe, tendências e qualidade da IA. Indicadores sem fonte confiável aparecem como `indisponível` com o motivo. A avaliação atual é sempre determinada por `ORDER BY deal_id, created_at DESC, id DESC`, preservando o histórico.

Antes de usar os dashboards em uma base existente, aplique `migrations/007_business_dashboard.sql`. Consulte [dashboard empresarial](docs/business-dashboard.md), [dicionário de métricas](docs/metrics-dictionary.md), [operações](docs/dashboard-operations.md) e [qualidade dos dados](docs/data-quality.md).

## Arquitetura

## Persistência e recuperação de mensagens

As auditorias persistem automaticamente todas as mensagens válidas antes da análise. Para recuperar históricos antigos sem chamar IA, consulte [persistência e backfill de mensagens](docs/message-persistence.md).

## Arquitetura

Cada módulo separa `domain`, `application`, `infrastructure` e `interfaces`. O caso de uso depende apenas de portas pequenas; Bitrix, Ollama e persistência são adaptadores substituíveis. O MVP é um monólito modular preparado para que workers de IA/relatório sejam extraídos depois, sem antecipar complexidade operacional.

As regras ficam em `configs/rules.yaml`. Condições suportadas: `is_empty`, `not_empty`, `equals`, `older_than_days`, `greater_than` e `less_than`.
