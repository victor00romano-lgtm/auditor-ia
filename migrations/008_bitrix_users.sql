BEGIN;

CREATE TABLE IF NOT EXISTS bitrix_users (
    bitrix_user_id BIGINT PRIMARY KEY,
    display_name TEXT NOT NULL,
    active BOOLEAN,
    synced_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_bitrix_users_display_name
    ON bitrix_users (display_name);

COMMIT;
