# Papéis PostgreSQL

Execute como administrador, passando senhas pelo mecanismo de variáveis do `psql`:

`psql "$env:POSTGRES_ADMIN_URL" -v auditor_migrator_password="$env:AUDITOR_MIGRATOR_PASSWORD" -v auditor_app_password="$env:AUDITOR_APP_PASSWORD" -v auditor_readonly_password="$env:AUDITOR_READONLY_PASSWORD" -f scripts/postgres/create_roles.sql`

Use `auditor_migrator`/`DATABASE_MIGRATOR_URL` para migrations, `auditor_app` para a aplicação e `auditor_readonly` para dashboards isolados. Valide que `auditor_app` recebe negação em `DROP TABLE`, que `auditor_readonly` recebe negação em `INSERT`, e que `auditor_app` consegue executar sincronização normal. O rollback consiste em revogar grants e remover os papéis somente após encerrar sessões e confirmar que nenhuma aplicação os utiliza.
