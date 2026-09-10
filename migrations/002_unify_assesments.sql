BEGIN;

CREATE TABLE deal_assessments (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,

    deal_id BIGINT NOT NULL
        REFERENCES deals(id)
        ON DELETE CASCADE,

    status VARCHAR(30) NOT NULL DEFAULT 'PROCESSING',

    score NUMERIC(5, 2),

    final_result VARCHAR(50),
    result_source TEXT,
    summary TEXT,

    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT deal_assessments_status_check
        CHECK (
            status IN (
                'PENDING',
                'PROCESSING',
                'COMPLETED',
                'FAILED'
            )
        ),

    CONSTRAINT deal_assessments_score_check
        CHECK (
            score IS NULL OR
            (score >= 0 AND score <= 100)
        ),

    CONSTRAINT deal_assessments_dates_check
        CHECK (
            finished_at IS NULL OR
            finished_at >= started_at
        )
);

ALTER TABLE conversation_analyses
    ADD COLUMN assessment_id BIGINT
        REFERENCES deal_assessments(id)
        ON DELETE CASCADE;

ALTER TABLE audit_findings
    ADD COLUMN assessment_id BIGINT
        REFERENCES deal_assessments(id)
        ON DELETE CASCADE;

ALTER TABLE analysis_jobs
    ADD COLUMN assessment_id BIGINT
        REFERENCES deal_assessments(id)
        ON DELETE SET NULL;

-- A restrio antiga impediria a mesma regra de aparecer
-- novamente em avaliaes futuras do mesmo negcio.
ALTER TABLE audit_findings
    DROP CONSTRAINT audit_findings_unique;

CREATE UNIQUE INDEX conversation_analyses_assessment_unique
    ON conversation_analyses(assessment_id)
    WHERE assessment_id IS NOT NULL;

CREATE UNIQUE INDEX audit_findings_assessment_rule_unique
    ON audit_findings(assessment_id, rule_name)
    WHERE assessment_id IS NOT NULL;

CREATE INDEX idx_assessments_deal_id
    ON deal_assessments(deal_id);

CREATE INDEX idx_assessments_status
    ON deal_assessments(status);

CREATE INDEX idx_conversation_analyses_assessment
    ON conversation_analyses(assessment_id);

CREATE INDEX idx_audit_findings_assessment
    ON audit_findings(assessment_id);

COMMIT;
