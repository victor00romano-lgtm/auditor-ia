# Estatísticas globais do CRM

As estatísticas são calculadas sobre o último snapshot publicado por `crmstatssync`. A TUI, a API e o PDF consultam somente o PostgreSQL e não dependem do Bitrix durante a leitura.

## Atualização

Depois de aplicar manualmente a migration `010_crm_global_statistics.sql` no ambiente escolhido:

```powershell
go run .\cmd\crmstatssync
```

Também é possível pressionar `U` na Visão executiva da TUI. A tecla `x` solicita cancelamento. `r` apenas relê o PostgreSQL. Duas sincronizações simultâneas são rejeitadas pelo banco.

Variáveis:

```env
CRM_STATS_SYNC_TIMEOUT=30m
CRM_STATS_PAGE_DELAY=100ms
```

O endpoint autenticado de leitura é `GET /api/v1/dashboard/crm-statistics`. Não foi criado endpoint HTTP de escrita: neste estágio, o comando independente e a TUI possuem cancelamento e exclusão mútua persistente, enquanto a API não possui um supervisor durável de tarefas globais.

## Vendas concluídas e negócios perdidos

O total e a série dos últimos 12 meses usam o estado determinístico do Bitrix:

- `STAGE_SEMANTIC_ID = S`: venda concluída;
- `STAGE_SEMANTIC_ID = F`: negócio perdido.

Os totais representam o estado atual de todos os negócios do snapshot. A distribuição mensal agrupa esses negócios pelo mês de criação (`DATE_CREATE`) em `America/Sao_Paulo`, mantendo meses sem ocorrências com valor zero. Portanto, ela responde quantos negócios criados em cada mês estão atualmente ganhos ou perdidos; não representa necessariamente o mês em que o estágio foi encerrado.

## Fórmulas e fontes

- Total global: `COUNT(DISTINCT bitrix_deal_id)` do último snapshot publicado.
- Responsáveis: agrupamento pelo `ASSIGNED_BY_ID` atual; nomes vêm de `bitrix_users`; nulo ou zero vira `Sem responsável`.
- Meses: `DATE_CREATE` convertido para `America/Sao_Paulo`, nos 12 meses-calendário terminando no mês atual. Meses ausentes recebem zero.
- Canal 3264: negócios únicos com atividade global `IMOPENLINES_SESSION` cujo `SUBJECT` contém o token exato 3264. Várias atividades do mesmo negócio contam uma vez.
- Participação 3264: `negócios 3264 do responsável / total de negócios 3264 × 100`.

Uma coleta operacionalmente incompleta não é publicada. Se apenas as atividades forem negadas pelo Bitrix, os metadados são publicados como `PARTIAL` e o canal 3264 fica indisponível, nunca igual a zero. Dados com mais de 24 horas recebem aviso de desatualização.

## Validação manual externa

Os comandos abaixo são somente para PowerShell e não fazem parte do binário Go. Eles não imprimem o webhook.

```powershell
function Get-BitrixPages {
    param([string]$Method, [hashtable]$Payload)
    $all = @()
    $start = 0
    do {
        $body = @{} + $Payload
        $body.start = $start
        $response = Invoke-RestMethod `
            -Method Post `
            -Uri ($env:BITRIX_WEBHOOK_URL.TrimEnd('/') + "/$Method.json") `
            -ContentType 'application/json' `
            -Body ($body | ConvertTo-Json -Depth 8) `
            -TimeoutSec 30
        $all += @($response.result)
        $hasNext = $null -ne $response.next -and "$($response.next)" -ne ''
        if ($hasNext) { $start = [int]$response.next }
        Start-Sleep -Milliseconds 100
    } while ($hasNext)
    return $all
}

$deals = Get-BitrixPages 'crm.deal.list' @{
    order  = @{ ID = 'ASC' }
    select = @('ID','ASSIGNED_BY_ID','DATE_CREATE')
}

"Total Bitrix: $((@($deals.ID | Sort-Object -Unique)).Count)"
$deals | Group-Object ASSIGNED_BY_ID | Sort-Object Count -Descending |
    Select-Object Name, Count

$timezone = [TimeZoneInfo]::FindSystemTimeZoneById('E. South America Standard Time')
$deals | ForEach-Object {
    $instant = [DateTimeOffset]::Parse($_.DATE_CREATE)
    [TimeZoneInfo]::ConvertTime($instant, $timezone).ToString('yyyy-MM')
} | Group-Object | Sort-Object Name | Select-Object Name, Count

$activities = Get-BitrixPages 'crm.activity.list' @{
    order  = @{ ID = 'ASC' }
    filter = @{ OWNER_TYPE_ID = 2; PROVIDER_ID = 'IMOPENLINES_SESSION' }
    select = @('ID','OWNER_ID','SUBJECT','PROVIDER_ID')
}
$owners3264 = @($activities |
    Where-Object { $_.SUBJECT -match '(?<!\d)3264(?!\d)' } |
    Select-Object -ExpandProperty OWNER_ID -Unique)
"Total 3264 Bitrix: $($owners3264.Count)"
```

Comparação com o snapshot PostgreSQL:

```powershell
$psql = 'C:\Program Files\PostgreSQL\17\bin\psql.exe'
& $psql "$env:DATABASE_URL" -P pager=off -c @"
WITH latest AS (
  SELECT id, status, finished_at, marker_3264_available
  FROM crm_sync_runs
  WHERE status IN ('COMPLETED','PARTIAL')
  ORDER BY finished_at DESC, id DESC
  LIMIT 1
)
SELECT l.status, l.finished_at, l.marker_3264_available,
       COUNT(DISTINCT d.bitrix_deal_id) AS total_deals
FROM latest l
LEFT JOIN deals d ON d.crm_sync_run_id=l.id
GROUP BY l.status,l.finished_at,l.marker_3264_available;

WITH latest AS (
  SELECT id FROM crm_sync_runs
  WHERE status IN ('COMPLETED','PARTIAL')
  ORDER BY finished_at DESC,id DESC LIMIT 1
)
SELECT COALESCE(NULLIF(u.display_name,''),'Sem responsável') AS responsavel,
       COUNT(*) AS negocios
FROM latest l JOIN deals d ON d.crm_sync_run_id=l.id
LEFT JOIN bitrix_users u ON u.bitrix_user_id=d.assigned_by_id
GROUP BY d.assigned_by_id,u.display_name
ORDER BY negocios DESC;

WITH latest AS (
  SELECT id FROM crm_sync_runs
  WHERE status IN ('COMPLETED','PARTIAL')
  ORDER BY finished_at DESC,id DESC LIMIT 1
)
SELECT COUNT(*) AS total_3264
FROM latest l JOIN deal_channel_markers m ON m.sync_run_id=l.id
WHERE m.marker='3264';
"@
```

Limitações: negócios removidos durante a paginação podem produzir diferenças transitórias; os totais convergem na execução seguinte. A disponibilidade do canal 3264 depende da permissão global para `crm.activity.list` no webhook.
