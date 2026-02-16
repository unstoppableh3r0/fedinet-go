-- Add client_public_key column to identities table
ALTER TABLE identities 
  ADD COLUMN IF NOT EXISTS client_public_key TEXT;

-- Create user_session_keys table for symmetric key management
CREATE TABLE IF NOT EXISTS user_session_keys (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id TEXT NOT NULL REFERENCES identities(user_id) ON DELETE CASCADE,
  symmetric_key_encrypted TEXT NOT NULL,  -- AES-256 key encrypted with SERVER_MASTER_KEY
  key_version INTEGER NOT NULL DEFAULT 1,
  signature TEXT NOT NULL,  -- Signed by server private key
  created_at TIMESTAMP DEFAULT NOW(),
  expires_at TIMESTAMP NOT NULL,
  is_active BOOLEAN DEFAULT TRUE,
  UNIQUE(user_id, key_version)
);

-- Create indexes for efficient querying
CREATE INDEX IF NOT EXISTS idx_user_session_keys_user ON user_session_keys(user_id);
CREATE INDEX IF NOT EXISTS idx_user_session_keys_active ON user_session_keys(is_active, expires_at);
CREATE INDEX IF NOT EXISTS idx_user_session_keys_version ON user_session_keys(user_id, key_version) WHERE is_active = TRUE;
