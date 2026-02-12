-- OTP and Token Authentication Tables Migration
-- This migration adds support for OTP verification and JWT refresh tokens

-- ============================================================================
-- OTP Codes Table
-- ============================================================================
CREATE TABLE IF NOT EXISTS otp_codes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email VARCHAR(255) NOT NULL,
    otp_code VARCHAR(6) NOT NULL,
    purpose VARCHAR(20) NOT NULL CHECK (purpose IN ('login', 'registration', 'password_reset')),
    session_id UUID NOT NULL DEFAULT gen_random_uuid(),
    verified BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL,
    attempts INTEGER DEFAULT 0,
    max_attempts INTEGER DEFAULT 3
);

-- Create indexes for OTP lookups
CREATE INDEX IF NOT EXISTS idx_otp_email ON otp_codes(email);
CREATE INDEX IF NOT EXISTS idx_otp_session ON otp_codes(session_id);
CREATE INDEX IF NOT EXISTS idx_otp_expires ON otp_codes(expires_at);
CREATE INDEX IF NOT EXISTS idx_otp_verified ON otp_codes(verified);

-- ============================================================================
-- Refresh Tokens Table
-- ============================================================================
CREATE TABLE IF NOT EXISTS refresh_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id VARCHAR(255) NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL,
    revoked BOOLEAN DEFAULT FALSE,
    revoked_at TIMESTAMP,
    device_info TEXT,
    ip_address VARCHAR(45),
    CONSTRAINT refresh_tokens_user_id_fkey
        FOREIGN KEY (user_id)
        REFERENCES identities(user_id)
        ON DELETE CASCADE
);

-- Create indexes for refresh token lookups
CREATE INDEX IF NOT EXISTS idx_refresh_token_user ON refresh_tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_refresh_token_hash ON refresh_tokens(token_hash);
CREATE INDEX IF NOT EXISTS idx_refresh_token_expires ON refresh_tokens(expires_at);
CREATE INDEX IF NOT EXISTS idx_refresh_token_revoked ON refresh_tokens(revoked);

-- ============================================================================
-- Add Email Column to Identities Table
-- ============================================================================
ALTER TABLE identities ADD COLUMN IF NOT EXISTS email VARCHAR(255);
CREATE UNIQUE INDEX IF NOT EXISTS idx_identities_email ON identities(email) WHERE email IS NOT NULL;

-- ============================================================================
-- Add Password Hash Column to Identities Table (if not exists)
-- ============================================================================
ALTER TABLE identities ADD COLUMN IF NOT EXISTS password_hash TEXT;
