# Dicionário de métricas empresariais

| Métrica | Definição e fórmula | Fonte | Limitação |
|---|---|---|---|
| Taxa de conclusão | `(COMPLETED + FAILED) / avaliados` | `current_deal_assessments` | Indisponível sem amostra |
| Score médio | Média do score atual | `deal_assessments` | Não representa negócios não avaliados |
| Conversão | ganhos / (ganhos + perdidos) | estágio e resultado atual | Indisponível sem encerrados |
| Pipeline | Soma dos valores positivos dos negócios abertos | `deals.amount > 0` | Indisponível quando todos os valores relevantes são nulos ou zero |
| Receita ganha/perdida | Soma por semântica S/F | `deals.amount` | Depende do preenchimento |
| Priority score | comprovante +40; IA venda +35; perdido negociável +25; parado +20; valor alto +15; regular +10; ruim +20; sem ação +10; confiança alta +10; limite 100 | dados persistidos | Itens sem atributo não recebem pontos |
| CRM Health Score | 100 menos penalidades proporcionais de valor ausente, parada, conversa ausente, indeterminação e divergência | dados atuais | É qualidade de processo, não de pessoa |
| Cobertura de mensagens | analisadas versus persistidas | `message_count` e `conversation_messages` | Não usa mensagens salvas como fonte exclusiva |
| Qualidade da IA | sucesso, falhas seguras, modelo, prompt, confiança e status | análise atual | duração p50/p95 indisponível até persistência específica |

Atualização ocorre sob demanda (`r`) e usa somente PostgreSQL. Filtros reduzem a amostra e devem ser exibidos junto ao indicador.
