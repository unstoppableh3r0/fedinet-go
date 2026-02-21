-- Migration: Add federated messaging support
-- This allows tracking messages sent/received from other federated servers

ALTER TABLE messages 
  ADD COLUMN IF NOT EXISTS is_federated BOOLEAN DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS origin_server VARCHAR(255);

-- Index for efficient federated message queries
CREATE INDEX IF NOT EXISTS idx_messages_federated 
  ON messages(is_federated, recipient_id);

-- Index for querying by origin server
CREATE INDEX IF NOT EXISTS idx_messages_origin_server 
  ON messages(origin_server) 
  WHERE origin_server IS NOT NULL;
