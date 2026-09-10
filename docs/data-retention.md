# Retenção de dados

Os padrões são 180 dias para mensagens, 90 para respostas brutas da IA, 730 para avaliações, 30 para logs e 90 para PDFs. Configure `RETENTION_MESSAGES_DAYS`, `RETENTION_RAW_ANALYSIS_DAYS`, `RETENTION_ASSESSMENTS_DAYS`, `RETENTION_LOGS_DAYS` e `RETENTION_REPORTS_DAYS`. Valor `0` desabilita a categoria.

Use sempre primeiro `go run ./cmd/retention --dry-run`. Exclusões exigem simultaneamente `--apply --confirm-retention-delete`. A saída contém somente categoria e contagem. Avaliações excluídas respeitam as chaves estrangeiras; negócios não são removidos pela retenção. PDFs só podem ser removidos de `REPORTS_DIR` e quando seguem `auditoria-<id>[-data-hora].pdf`.

Backups devem possuir política equivalente e acesso restrito. Faça teste de restauração antes de aplicar retenção em produção.
