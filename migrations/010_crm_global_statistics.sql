BEGIN;

CREATE TABLE crm_sync_runs (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    status TEXT NOT NULL CHECK (status IN ('RUNNING', 'COMPLETED', 'PARTIAL', 'FAILED', 'CANCELED')),
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ,
    deals_collected INTEGER NOT NULL DEFAULT 0 CHECK (deals_collected >= 0),
    activities_collected INTEGER NOT NULL DEFAULT 0 CHECK (activities_collected >= 0),
    markers_collected INTEGER NOT NULL DEFAULT 0 CHECK (markers_collected >= 0),
    marker_3264_available BOOLEAN NOT NULL DEFAULT FALSE,
    safe_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_crm_sync_runs_single_running
    ON crm_sync_runs ((1))
    WHERE status = 'RUNNING';

ALTER TABLE deals
    ADD COLUMN crm_sync_run_id BIGINT REFERENCES crm_sync_runs(id);

CREATE INDEX idx_deals_crm_sync_run_id ON deals (crm_sync_run_id);
CREATE INDEX idx_deals_crm_stats_assignee ON deals (crm_sync_run_id, assigned_by_id);
CREATE INDEX idx_deals_crm_stats_created ON deals (crm_sync_run_id, created_at_bitrix);
CREATE INDEX idx_deals_crm_stats_outcome ON deals (crm_sync_run_id, stage_semantic_id, created_at_bitrix);

CREATE TABLE deal_channel_markers (
    deal_id BIGINT NOT NULL REFERENCES deals(id) ON DELETE CASCADE,
    sync_run_id BIGINT NOT NULL REFERENCES crm_sync_runs(id) ON DELETE CASCADE,
    marker TEXT NOT NULL,
    source TEXT NOT NULL,
    detected_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (deal_id, sync_run_id, marker)
);

CREATE INDEX idx_deal_channel_markers_snapshot
    ON deal_channel_markers (sync_run_id, marker, deal_id);

COMMIT;
