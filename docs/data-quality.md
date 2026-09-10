# Qualidade e cobertura dos dados

Os dashboards representam apenas a amostra persistida e avaliada. Não extrapole os resultados para toda a empresa sem verificar cobertura temporal e operacional.

Valores ausentes tornam receita, pipeline e ticket parciais. `assigned_by_id` pode ser exibido, mas o nome fica indisponível; a TUI não chama Bitrix ao visualizar. `message_count` registra o que foi considerado na análise, enquanto `conversation_messages` pode conter somente uma fração; a diferença é apresentada explicitamente.

Dados ainda necessários: nomes de responsáveis, funil/categoria, produto, origem, campanha, próxima atividade, motivo de perda, probabilidade, histórico de etapas e data de pagamento. O próximo passo é ampliar a sincronização do Bitrix e persistir esses campos via migrations incrementais.
