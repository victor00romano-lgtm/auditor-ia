BEGIN;

ALTER TABLE conversation_analyses
    ADD COLUMN collected_message_count INTEGER NOT NULL DEFAULT 0 CHECK (collected_message_count >= 0),
    ADD COLUMN persisted_message_count INTEGER NOT NULL DEFAULT 0 CHECK (persisted_message_count >= 0),
    ADD COLUMN analyzed_message_count INTEGER NOT NULL DEFAULT 0 CHECK (analyzed_message_count >= 0);

COMMIT;
