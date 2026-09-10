# Persistência de mensagens e backfill

## Fluxo comum

`SyncAnalysis.Execute` busca e atualiza o negócio, coleta a conversa uma única vez, persiste todas as mensagens válidas com UPSERT e encerra essa transação antes de executar regras ou Ollama. `Analyze.ExecuteCollected` reutiliza a conversa em memória. Somente o analisador Ollama limita sua entrada às 50 mensagens elegíveis mais recentes.

- `message_count`: semântica histórica preservada, correspondente às mensagens válidas coletadas;
- `collected_message_count`: mensagens válidas devolvidas pela coleta Bitrix;
- `persisted_message_count`: mensagens confirmadas pelo UPSERT da execução;
- `analyzed_message_count`: subconjunto encaminhado ao analisador, limitado a 50.

A migration 011 adiciona os três campos explícitos. Ela deve ser aplicada pelo procedimento normal antes de executar uma versão que grave esses campos.

## Backfill sem IA

O comando não possui analisador nem repositório de avaliações. Ele não chama Ollama e não cria `deal_assessments` ou `conversation_analyses`.

```powershell
go run .\cmd\messagebackfill --deal 52 --limit 1 --concurrency 1
go run .\cmd\messagebackfill --limit 100 --concurrency 2 --dry-run
go run .\cmd\messagebackfill --limit 100 --concurrency 2
go run .\cmd\messagebackfill --limit 100 --concurrency 2 --all
```

O timeout global usa `MESSAGE_BACKFILL_TIMEOUT` (padrão `30m`). Ctrl+C cancela o processamento. Respostas 429 e 5xx usam retry exponencial e respeitam `Retry-After`.

## Validação no PostgreSQL local

```powershell
$psql = "C:\Program Files\PostgreSQL\17\bin\psql.exe"

# A. mensagens e negócios persistidos
& $psql $env:DATABASE_URL -c "SELECT COUNT(*) AS mensagens, COUNT(DISTINCT deal_id) AS negocios FROM conversation_messages;"

# B. análise atual comparada às mensagens persistidas
& $psql $env:DATABASE_URL -c "WITH atual AS (SELECT DISTINCT ON (deal_id) deal_id, assessment_id, message_count, collected_message_count, persisted_message_count, analyzed_message_count FROM conversation_analyses ORDER BY deal_id, created_at DESC, id DESC), persistidas AS (SELECT deal_id, COUNT(*) AS total FROM conversation_messages GROUP BY deal_id) SELECT d.bitrix_deal_id, a.message_count, a.collected_message_count, a.persisted_message_count, a.analyzed_message_count, COALESCE(p.total,0) AS mensagens_no_banco FROM atual a JOIN deals d ON d.id=a.deal_id LEFT JOIN persistidas p ON p.deal_id=a.deal_id ORDER BY d.bitrix_deal_id;"

# C. análise com mensagens e nenhuma mensagem persistida
& $psql $env:DATABASE_URL -c "WITH atual AS (SELECT DISTINCT ON (deal_id) deal_id, message_count FROM conversation_analyses ORDER BY deal_id, created_at DESC, id DESC) SELECT d.bitrix_deal_id, a.message_count FROM atual a JOIN deals d ON d.id=a.deal_id WHERE a.message_count>0 AND NOT EXISTS (SELECT 1 FROM conversation_messages m WHERE m.deal_id=a.deal_id) ORDER BY d.bitrix_deal_id;"

# D. duplicatas pela chave técnica; deve retornar zero linhas
& $psql $env:DATABASE_URL -c "SELECT session_id, bitrix_message_id, COUNT(*) FROM conversation_messages GROUP BY session_id, bitrix_message_id HAVING COUNT(*)>1;"

# E. registre antes e depois para confirmar que o backfill não criou avaliações
& $psql $env:DATABASE_URL -c "SELECT COUNT(*) AS avaliacoes FROM deal_assessments; SELECT COUNT(*) AS analises FROM conversation_analyses;"

# F. registre antes e depois para comparar cobertura
& $psql $env:DATABASE_URL -c "SELECT COUNT(*) AS mensagens, COUNT(DISTINCT deal_id) AS negocios_com_mensagens FROM conversation_messages; SELECT COUNT(*) AS negocios_sem_mensagens FROM deals d WHERE NOT EXISTS (SELECT 1 FROM conversation_messages m WHERE m.deal_id=d.id);"
```

O total persistido não precisa ser igual à soma histórica de `message_count`: o banco guarda todas as mensagens válidas coletadas, enquanto cada análise usa no máximo 50.
