# Dashboard empresarial

A TUI e os endpoints `/api/v1/dashboard/*` oferecem inteligência comercial sobre dados persistidos. O Grafana continua responsável exclusivamente por logs, métricas e traces técnicos.

Cada negócio contribui no máximo uma vez aos indicadores de estado atual. A avaliação vigente é escolhida deterministicamente por `deal_id`, `created_at DESC`, `id DESC`. Avaliações históricas não são apagadas.

## Menu e teclas

`1–9`, `0` e `A–C` abrem os módulos. `↑/↓` e `j/k` navegam; `Enter` abre; `Esc` volta; `q` sai; `Home/g`, `End/G` e `PgUp/PgDown` rolam; `r` atualiza; `d` alterna o período; `c` limpa filtros; `/` busca; `p` gera PDF no detalhe.

Carregamentos são assíncronos via `tea.Cmd`; respostas atrasadas possuem token e não alteram outra tela. Terminais abaixo de 60×16 exibem orientação de redimensionamento; recomenda-se 80×24.

## Limitações

Histórico de etapas, probabilidade, próxima atividade, motivo de perda, origem/campanha, produto, data de pagamento e nomes dos responsáveis não estão persistidos. A visualização não consulta Bitrix ou Ollama para preencher lacunas. Revisão humana ficou fora desta entrega para evitar uma gravação parcial sem identidade/autorização de revisor.

Possível comprovante não confirma pagamento. Resultado provável não substitui o estágio oficial. Divergências exigem revisão humana. IA não deve ser usada isoladamente para punição ou decisão automática.
