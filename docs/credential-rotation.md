# Rotação de credenciais

Aplica-se ao webhook Bitrix, `AUDITOR_API_KEY`, senhas PostgreSQL e futuras credenciais AWS ou Ollama. Para cada credencial: crie uma nova; configure-a fora do Git; reinicie a aplicação; teste o fluxo; revogue a antiga; examine logs e histórico Git com valores mascarados; mantenha um rollback curto para a credencial anterior até a validação.

Use `DATABASE_MIGRATOR_URL` apenas para migrations e `DATABASE_URL` com o papel `auditor_app` na aplicação. Aplique `scripts/postgres/create_roles.sql` com variáveis `psql -v`, nunca editando senhas no arquivo.

Qualquer webhook exibido em captura de tela, chat, ticket ou log deve ser considerado comprometido e rotacionado manualmente. O sistema não revoga credenciais externas.
