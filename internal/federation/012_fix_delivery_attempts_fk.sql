-- Migration 012: Fix delivery_attempts.message_id foreign key
-- The FK previously referenced federation_messages(id) but QueueForRetry passes
-- outbox_activities.id as the message_id. This migration recreates the constraint
-- with the correct FK target. Applied after the original migrations.sql.

DO $$
BEGIN
    -- Drop the old FK constraint if it still points at federation_messages
    ALTER TABLE delivery_attempts DROP CONSTRAINT IF EXISTS delivery_attempts_message_id_fkey;

    -- Add the correct FK pointing at outbox_activities
    BEGIN
        ALTER TABLE delivery_attempts
            ADD CONSTRAINT delivery_attempts_message_id_fkey
            FOREIGN KEY (message_id) REFERENCES outbox_activities(id) ON DELETE CASCADE;
    EXCEPTION WHEN duplicate_object THEN
        NULL; -- constraint already correct
    END;
END
$$;
