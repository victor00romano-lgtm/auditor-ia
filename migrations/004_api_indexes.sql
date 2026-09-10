BEGIN;

-- Stable, descending pagination of the complete assessment history.
CREATE INDEX IF NOT EXISTS idx_assessments_api_history
    ON deal_assessments (created_at DESC, id DESC);

-- Stable finding pagination with the API's severity and rule filters.
CREATE INDEX IF NOT EXISTS idx_findings_api_filters
    ON audit_findings (severity, rule_name, created_at DESC, id DESC);

COMMIT;
