CREATE TABLE IF NOT EXISTS deals (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    bitrix_deal_id BIGINT NOT NULL UNIQUE,
    title TEXT NOT NULL DEFAULT '',
    stage_id TEXT NOT NULL DEFAULT '',
    stage_semantic_id TEXT NOT NULL DEFAULT '',
    closed BOOLEAN NOT NULL DEFAULT FALSE,
    amount NUMERIC(15, 2),
    currency VARCHAR(10),
    assigned_by_id BIGINT,
    created_at_bitrix TIMESTAMPTZ,
    updated_at_bitrix TIMESTAMPTZ,
    synced_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS conversation_messages (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    deal_id BIGINT NOT NULL REFERENCES deals(id) ON DELETE CASCADE,
    bitrix_message_id TEXT NOT NULL,
    session_id TEXT NOT NULL,
    sender_id TEXT,
    sender_role VARCHAR(30) NOT NULL,
    sender_name TEXT,
    message_text TEXT NOT NULL DEFAULT '',
    sent_at TIMESTAMPTZ,
    attachments JSONB NOT NULL DEFAULT '[]'::JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT conversation_messages_unique
        UNIQUE (session_id, bitrix_message_id)
);

CREATE TABLE IF NOT EXISTS conversation_analyses (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    deal_id BIGINT NOT NULL REFERENCES deals(id) ON DELETE CASCADE,
    status VARCHAR(30) NOT NULL DEFAULT 'COMPLETED',
    probable_result VARCHAR(50),
    final_result VARCHAR(50),
    result_source TEXT,
    main_reason TEXT,
    customer_objections TEXT,
    service_quality VARCHAR(30),
    recommended_action TEXT,
    confidence VARCHAR(20),
    observation TEXT,
    possible_receipt BOOLEAN NOT NULL DEFAULT FALSE,
    message_count INTEGER NOT NULL DEFAULT 0,
    model TEXT,
    prompt_version TEXT,
    raw_response TEXT,
    error_message TEXT,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS audit_findings (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    deal_id BIGINT NOT NULL REFERENCES deals(id) ON DELETE CASCADE,
    rule_name TEXT NOT NULL,
    description TEXT NOT NULL,
    severity VARCHAR(20) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT audit_findings_unique
        UNIQUE (deal_id, rule_name)
);

CREATE TABLE IF NOT EXISTS analysis_jobs (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    deal_id BIGINT NOT NULL REFERENCES deals(id) ON DELETE CASCADE,
    status VARCHAR(30) NOT NULL DEFAULT 'PENDING',
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    scheduled_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT analysis_jobs_status_check
        CHECK (status IN ('PENDING', 'PROCESSING', 'COMPLETED', 'FAILED'))
);

CREATE INDEX IF NOT EXISTS idx_messages_deal_id
    ON conversation_messages(deal_id);

CREATE INDEX IF NOT EXISTS idx_analyses_deal_id
    ON conversation_analyses(deal_id);

CREATE INDEX IF NOT EXISTS idx_findings_deal_id
    ON audit_findings(deal_id);

CREATE INDEX IF NOT EXISTS idx_jobs_status
    ON analysis_jobs(status);
