BEGIN;

ALTER TABLE deal_assessments
    ADD COLUMN IF NOT EXISTS trace_id VARCHAR(32);

ALTER TABLE deal_assessments
    DROP CONSTRAINT IF EXISTS deal_assessments_trace_id_check;

ALTER TABLE deal_assessments
    ADD CONSTRAINT deal_assessments_trace_id_check
    CHECK (trace_id IS NULL OR trace_id ~ '^[0-9a-f]{32}$');

COMMIT;
