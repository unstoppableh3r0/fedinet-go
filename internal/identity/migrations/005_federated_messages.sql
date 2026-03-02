-- Migration: Add federated messaging support
-- This allows tracking messages sent/received from other federated servers

ALTER TABLE messages 
  ADD COLUMN IF NOT EXISTS is_federated BOOLEAN DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS origin_server VARCHAR(255);

-- Index for efficient federated message queries.
-- Uses a conditional block because migration 006 renames `receiver` → `recipient_id`;
-- on a fresh install 006 has not run yet so the column is still called `receiver`.
DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_name='messages' AND column_name='recipient_id'
  ) THEN
    IF NOT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname='idx_messages_federated') THEN
      CREATE INDEX idx_messages_federated ON messages(is_federated, recipient_id);
    END IF;
  ELSIF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_name='messages' AND column_name='receiver'
  ) THEN
    IF NOT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname='idx_messages_federated') THEN
      CREATE INDEX idx_messages_federated ON messages(is_federated, receiver);
    END IF;
  END IF;
END $$;

-- Index for querying by origin server
CREATE INDEX IF NOT EXISTS idx_messages_origin_server 
  ON messages(origin_server) 
  WHERE origin_server IS NOT NULL;
