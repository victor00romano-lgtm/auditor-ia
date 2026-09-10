BEGIN;

CREATE INDEX IF NOT EXISTS idx_assessments_current_deal
    ON deal_assessments (deal_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_analyses_current_deal
    ON conversation_analyses (deal_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_deals_stage ON deals (stage_id);
CREATE INDEX IF NOT EXISTS idx_deals_assignee ON deals (assigned_by_id);
CREATE INDEX IF NOT EXISTS idx_deals_synced_at ON deals (synced_at DESC);
CREATE INDEX IF NOT EXISTS idx_analyses_receipt ON conversation_analyses (possible_receipt) WHERE possible_receipt;
CREATE INDEX IF NOT EXISTS idx_analyses_conversation_status ON conversation_analyses (conversation_status);
CREATE INDEX IF NOT EXISTS idx_assessments_final_result ON deal_assessments (final_result);

CREATE OR REPLACE VIEW current_deal_assessments AS
SELECT DISTINCT ON (deal_id) *
FROM deal_assessments
ORDER BY deal_id, created_at DESC, id DESC;

CREATE OR REPLACE VIEW current_conversation_analyses AS
SELECT DISTINCT ON (deal_id) *
FROM conversation_analyses
ORDER BY deal_id, created_at DESC, id DESC;

CREATE OR REPLACE VIEW business_dashboard_summary AS
SELECT
    COUNT(*) AS deals_synced,
    COUNT(a.id) AS deals_evaluated,
    COUNT(a.id) FILTER (WHERE a.status = 'COMPLETED') AS completed,
    COUNT(a.id) FILTER (WHERE a.status = 'FAILED') AS failed,
    AVG(a.score) AS average_score,
    COUNT(a.id) FILTER (WHERE d.amount IS NULL OR d.amount <= 0) AS without_value
FROM deals d
LEFT JOIN current_deal_assessments a ON a.deal_id = d.id;

CREATE OR REPLACE VIEW crm_quality_summary AS
SELECT
    COUNT(a.id) AS sample,
    COUNT(a.id) FILTER (WHERE d.amount IS NULL OR d.amount <= 0) AS without_value,
    COUNT(a.id) FILTER (WHERE d.assigned_by_id IS NULL) AS without_assignee,
    COUNT(a.id) FILTER (WHERE ca.conversation_status = 'NO_CONVERSATION') AS without_conversation,
    COUNT(a.id) FILTER (WHERE ca.conversation_status = 'EMPTY_CONVERSATION') AS empty_conversation,
    COUNT(a.id) FILTER (WHERE ca.conversation_status = 'ACCESS_DENIED') AS access_denied
FROM deals d
LEFT JOIN current_deal_assessments a ON a.deal_id = d.id
LEFT JOIN current_conversation_analyses ca ON ca.deal_id = d.id;

CREATE OR REPLACE VIEW sales_priority_opportunities AS
SELECT d.id AS deal_id, d.bitrix_deal_id, d.title, d.assigned_by_id, d.stage_id,
       d.stage_semantic_id, d.amount, d.currency, d.updated_at_bitrix,
       a.status AS analysis_status, a.score AS audit_score, ca.probable_result, ca.main_reason,
       ca.possible_receipt, ca.recommended_action, ca.confidence
FROM deals d
JOIN current_deal_assessments a ON a.deal_id = d.id
LEFT JOIN current_conversation_analyses ca ON ca.deal_id = d.id;

CREATE OR REPLACE VIEW crm_ai_divergences AS
SELECT d.id AS deal_id, d.bitrix_deal_id, d.stage_semantic_id,
       ca.probable_result, ca.final_result, ca.possible_receipt,
       ca.main_reason, ca.confidence, ca.recommended_action,
       (d.amount IS NULL OR d.amount <= 0) AS without_value,
       (NOT ca.possible_receipt AND (
           lower(ca.main_reason) LIKE '%pagamento já realizado%'
           OR lower(ca.main_reason) LIKE '%pagamento realizado%'
           OR lower(ca.main_reason) LIKE '%comprovante de pagamento%'
           OR lower(ca.main_reason) LIKE '%envio do comprovante%'
           OR lower(ca.main_reason) LIKE '%comprovante enviado%'
           OR lower(ca.main_reason) LIKE '%paguei%'
       )) AS payment_text_detector_conflict
FROM deals d
JOIN current_conversation_analyses ca ON ca.deal_id = d.id
WHERE (ca.probable_result = 'venda' AND d.stage_semantic_id <> 'S')
   OR (ca.probable_result = 'em negociação' AND d.stage_semantic_id = 'F')
   OR (ca.possible_receipt AND d.stage_semantic_id <> 'S')
   OR (ca.possible_receipt AND (d.amount IS NULL OR d.amount <= 0))
   OR (NOT ca.possible_receipt AND (
       lower(ca.main_reason) LIKE '%pagamento já realizado%'
       OR lower(ca.main_reason) LIKE '%pagamento realizado%'
       OR lower(ca.main_reason) LIKE '%comprovante de pagamento%'
       OR lower(ca.main_reason) LIKE '%envio do comprovante%'
       OR lower(ca.main_reason) LIKE '%comprovante enviado%'
       OR lower(ca.main_reason) LIKE '%paguei%'
   ));

CREATE OR REPLACE VIEW objection_summary AS
SELECT customer_objections, COUNT(*) AS occurrences
FROM current_conversation_analyses
GROUP BY customer_objections;

CREATE OR REPLACE VIEW monthly_business_metrics AS
SELECT date_trunc('month', d.synced_at) AS month,
       COUNT(*) AS deals,
       COUNT(*) FILTER (WHERE d.stage_semantic_id = 'S') AS won,
       COUNT(*) FILTER (WHERE d.stage_semantic_id = 'F') AS lost,
       SUM(d.amount) FILTER (WHERE d.stage_semantic_id = 'S' AND d.amount > 0) AS won_revenue
FROM deals d
GROUP BY date_trunc('month', d.synced_at);

CREATE OR REPLACE VIEW agent_performance_summary AS
SELECT d.assigned_by_id, COUNT(*) AS deals,
       COUNT(*) FILTER (WHERE NOT d.closed) AS open_deals,
       COUNT(*) FILTER (WHERE d.stage_semantic_id = 'S') AS won,
       COUNT(*) FILTER (WHERE d.stage_semantic_id = 'F') AS lost,
       SUM(d.amount) FILTER (WHERE NOT d.closed AND d.amount > 0) AS pipeline
FROM deals d
GROUP BY d.assigned_by_id;

COMMIT;
