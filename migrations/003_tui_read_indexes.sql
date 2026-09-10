BEGIN;

-- Supports selecting the most recent assessment for each deal.
CREATE INDEX IF NOT EXISTS idx_assessments_latest_by_deal
    ON deal_assessments (deal_id, created_at DESC, id DESC);

-- Supports dashboard filtering after restricting findings to current assessments.
CREATE INDEX IF NOT EXISTS idx_findings_assessment_severity_rule
    ON audit_findings (assessment_id, severity, rule_name);

-- Supports opening the latest persisted conversation analysis without inference.
CREATE INDEX IF NOT EXISTS idx_analyses_latest_by_deal
    ON conversation_analyses (deal_id, created_at DESC, id DESC);

COMMIT;
