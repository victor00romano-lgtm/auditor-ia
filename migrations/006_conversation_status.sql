BEGIN;

ALTER TABLE conversation_analyses
    ADD COLUMN conversation_status VARCHAR(30);

UPDATE conversation_analyses
SET conversation_status = CASE
    WHEN message_count > 0 THEN 'AVAILABLE'
    WHEN error_message IS NOT NULL AND error_message <> '' THEN 'NO_CONVERSATION'
    ELSE 'EMPTY_CONVERSATION'
END
WHERE conversation_status IS NULL;

ALTER TABLE conversation_analyses
    ALTER COLUMN conversation_status SET DEFAULT 'AVAILABLE',
    ALTER COLUMN conversation_status SET NOT NULL;

ALTER TABLE conversation_analyses
    ADD CONSTRAINT conversation_analyses_conversation_status_check
    CHECK (conversation_status IN ('NO_CONVERSATION', 'EMPTY_CONVERSATION', 'ACCESS_DENIED', 'AVAILABLE'));

COMMIT;
