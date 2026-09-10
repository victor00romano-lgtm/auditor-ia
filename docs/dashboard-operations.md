# Operação do dashboard

1. Inicie PostgreSQL e aplique a migration 007.
2. Inicie a API com `go run ./cmd/api`.
3. Inicie a TUI com `go run ./cmd/auditor`.
4. Use `r` para atualizar, `d` para alternar períodos e `Esc` para voltar.

Migration:

```powershell
$env:PGPASSWORD = "<senha>"
& "C:\Program Files\PostgreSQL\17\bin\psql.exe" -h 127.0.0.1 -p 5433 -U auditor -d auditor_ia -v ON_ERROR_STOP=1 -f .\migrations\007_business_dashboard.sql
```

Reconstrução containerizada:

```powershell
docker compose build auditor-api
docker compose --profile tools build auditor-tui
```

CSV é gravado em `REPORTS_DIR` com proteção contra formula injection e nome sem sobrescrita. Falhas de consulta são recuperáveis e não encerram a TUI.
