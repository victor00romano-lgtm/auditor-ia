# Tratamento técnico de dados pessoais

Esta documentação técnica não declara conformidade jurídica automática e não substitui avaliação jurídica de LGPD.

| dado | origem | finalidade | armazenamento | retenção | destino IA | exclusão |
|---|---|---|---|---|---|---|
| nome, telefone, e-mail, documento | Bitrix e formulário | contextualizar atendimento | mensagens/formulários derivados | mensagens: 180 dias | aliases no modo padrão | `datadelete` |
| conversa | IMOPENLINES | auditoria comercial | `conversation_messages` | 180 dias | anonimizada por padrão | retenção ou negócio |
| IP e URLs | formulário | contexto técnico | análise/contexto local | conforme avaliação | aliases; URLs secretas nunca | negócio |
| achados e score | regras locais | auditoria | `audit_findings`, `deal_assessments` | 730 dias | não necessário | negócio |
| resposta bruta | Ollama | rastreabilidade | `conversation_analyses` | 90 dias | produzida pela IA | retenção ou negócio |
| PDF | PostgreSQL | relatório | `REPORTS_DIR` | 90 dias | não | arquivo seguro |

`AI_PRIVACY_MODE=anonymize` é o padrão e cria aliases consistentes apenas em memória. O mapa reverso não é persistido nem registrado. `preserve` exige opção explícita e, em produção, `ALLOW_PII_TO_AI=true`. Mesmo assim, tokens, cookies, senhas, chaves, Bearer/JWT e URLs secretas são removidos.

Logs devem conter somente IDs técnicos, contagens, durações, status e códigos sanitizados. Restrinja banco, relatórios e backups por função. Incidentes exigem contenção, rotação de credenciais, preservação de evidências seguras e avaliação do responsável. O operador define base legal, acesso, retenção, backups e atendimento aos titulares.
