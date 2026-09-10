\if :{?auditor_app_password}
\else
\error 'informe -v auditor_migrator_password, auditor_app_password e auditor_readonly_password'
\endif
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
CREATE ROLE auditor_migrator LOGIN PASSWORD :'auditor_migrator_password';
CREATE ROLE auditor_app LOGIN PASSWORD :'auditor_app_password';
CREATE ROLE auditor_readonly LOGIN PASSWORD :'auditor_readonly_password';
GRANT USAGE ON SCHEMA public TO auditor_app, auditor_readonly;
GRANT USAGE, CREATE ON SCHEMA public TO auditor_migrator;
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO auditor_migrator;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO auditor_migrator;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO auditor_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO auditor_app;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO auditor_readonly;
ALTER DEFAULT PRIVILEGES FOR ROLE auditor_migrator IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO auditor_app;
ALTER DEFAULT PRIVILEGES FOR ROLE auditor_migrator IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO auditor_app;
ALTER DEFAULT PRIVILEGES FOR ROLE auditor_migrator IN SCHEMA public GRANT SELECT ON TABLES TO auditor_readonly;
