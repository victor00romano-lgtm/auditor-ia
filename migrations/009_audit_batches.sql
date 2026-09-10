BEGIN;

ALTER TABLE deal_assessments ADD COLUMN execution_key TEXT;
CREATE UNIQUE INDEX idx_deal_assessments_execution_key
    ON deal_assessments (execution_key)
    WHERE execution_key IS NOT NULL;

CREATE TABLE audit_batches (
    id BIGSERIAL PRIMARY KEY,
    status TEXT NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING','RUNNING','COMPLETED','PARTIAL','FAILED','CANCELED')),
    total_items INTEGER NOT NULL CHECK (total_items >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ
);

CREATE TABLE audit_batch_items (
    id BIGSERIAL PRIMARY KEY,
    batch_id BIGINT NOT NULL REFERENCES audit_batches(id) ON DELETE CASCADE,
    bitrix_deal_id BIGINT NOT NULL CHECK (bitrix_deal_id > 0),
    status TEXT NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING','PROCESSING','COMPLETED','FAILED','CANCELED')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    lease_expires_at TIMESTAMPTZ,
    locked_by TEXT,
    assessment_id BIGINT REFERENCES deal_assessments(id),
    failure_category TEXT,
    safe_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ,
    UNIQUE (batch_id, bitrix_deal_id)
);

CREATE INDEX idx_audit_batch_items_claim
    ON audit_batch_items (available_at, id)
    WHERE status = 'PENDING';
CREATE INDEX idx_audit_batch_items_expired
    ON audit_batch_items (lease_expires_at)
    WHERE status = 'PROCESSING';

COMMIT;
