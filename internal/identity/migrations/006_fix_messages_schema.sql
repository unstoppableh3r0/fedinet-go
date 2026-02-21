-- Migration: Fix messages table schema to match code
-- Renames sender/receiver to sender_id/recipient_id

ALTER TABLE messages 
  RENAME COLUMN sender TO sender_id;

ALTER TABLE messages 
  RENAME COLUMN receiver TO recipient_id;
