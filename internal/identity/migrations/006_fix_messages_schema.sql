-- Migration: Fix messages table schema to match code
-- Renames sender/receiver to sender_id/recipient_id

ALTER TABLE messages 
  RENAME COLUMN sender TO sender_id;

ALTER TABLE messages 
  RENAME COLUMN receiver TO recipient_id;

-- Rebuild the federated-messages index now that the column has its final name.
-- Drop the old one (may reference `receiver`) and recreate on `recipient_id`.
DROP INDEX IF EXISTS idx_messages_federated;
CREATE INDEX IF NOT EXISTS idx_messages_federated ON messages(is_federated, recipient_id);
