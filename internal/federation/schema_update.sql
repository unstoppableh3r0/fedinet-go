-- Admin Panel Database Schema Updates
-- Run this on your PostgreSQL database

-- Server configuration table
CREATE TABLE IF NOT EXISTS server_config (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at TIMESTAMP DEFAULT NOW(),
  updated_by TEXT
);

-- Insert default server name
INSERT INTO server_config (key, value, updated_by) 
VALUES ('server_name', 'localhost', 'system')
ON CONFLICT (key) DO NOTHING;

-- Notifications table for user alerts
CREATE TABLE IF NOT EXISTS notifications (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id TEXT NOT NULL,
  title TEXT NOT NULL,
  message TEXT NOT NULL,
  type TEXT NOT NULL, -- 'info', 'warning', 'server_change'
  is_read BOOLEAN DEFAULT FALSE,
  created_at TIMESTAMP DEFAULT NOW(),
  CONSTRAINT notifications_user_id_fkey
    FOREIGN KEY (user_id)
    REFERENCES identities(user_id)
    ON DELETE CASCADE
);

-- Migration status tracking table
CREATE TABLE IF NOT EXISTS migration_status (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  from_db TEXT NOT NULL,
  to_db TEXT NOT NULL,
  status TEXT NOT NULL, -- 'pending', 'in_progress', 'completed', 'failed'
  tables_migrated JSONB,
  error_message TEXT,
  started_at TIMESTAMP DEFAULT NOW(),
  completed_at TIMESTAMP
);

-- Create index on notifications for faster queries
CREATE INDEX IF NOT EXISTS idx_notifications_user_id ON notifications(user_id);
CREATE INDEX IF NOT EXISTS idx_notifications_is_read ON notifications(is_read);

-- Trigger for server_config updated_at
CREATE OR REPLACE FUNCTION update_server_config_timestamp()
RETURNS TRIGGER AS $$
BEGIN
  NEW.updated_at = NOW();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER server_config_updated_at_trigger
BEFORE UPDATE ON server_config
FOR EACH ROW
EXECUTE FUNCTION update_server_config_timestamp();
