-- Federated Server Initialization Schema
-- This migration creates the core tables for server identity, admin system, and invite management

-- ============================================================================
-- Server Identity Table (Singleton)
-- ============================================================================
CREATE TABLE IF NOT EXISTS server_identity (
    id INTEGER PRIMARY KEY CHECK (id = 1),    -- Enforce singleton
    server_id UUID UNIQUE NOT NULL,           -- Immutable internal server ID
    server_name VARCHAR(255) NOT NULL,        -- Mutable human-readable name
    public_key TEXT NOT NULL,                 -- Ed25519 public key (base64)
    private_key_encrypted TEXT NOT NULL,      -- Encrypted Ed25519 private key
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    initialized BOOLEAN DEFAULT TRUE
);

-- ============================================================================
-- Admin Accounts Table
-- ============================================================================
CREATE TABLE IF NOT EXISTS admins (
    id SERIAL PRIMARY KEY,
    username VARCHAR(255) UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    created_by VARCHAR(255),                  -- NULL for first admin
    is_super_admin BOOLEAN DEFAULT FALSE,     -- First admin is super admin
    active BOOLEAN DEFAULT TRUE
);

-- Create index for faster admin lookups
CREATE INDEX IF NOT EXISTS idx_admins_username ON admins(username);
CREATE INDEX IF NOT EXISTS idx_admins_active ON admins(active);

-- ============================================================================
-- Invites System Table
-- ============================================================================
CREATE TABLE IF NOT EXISTS invites (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    invite_code VARCHAR(64) UNIQUE NOT NULL,  -- Short code for QR/URL
    invite_type VARCHAR(20) NOT NULL CHECK (invite_type IN ('user', 'admin')),
    created_by VARCHAR(255) NOT NULL,         -- Admin who created it
    max_uses INTEGER DEFAULT 1,               -- -1 for unlimited
    current_uses INTEGER DEFAULT 0,
    expires_at TIMESTAMP,                     -- NULL for no expiry
    revoked BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    metadata JSONB DEFAULT '{}'::jsonb        -- Additional invite data
);

-- Create indexes for invite lookups
CREATE INDEX IF NOT EXISTS idx_invites_code ON invites(invite_code);
CREATE INDEX IF NOT EXISTS idx_invites_type ON invites(invite_type);
CREATE INDEX IF NOT EXISTS idx_invites_created_by ON invites(created_by);
CREATE INDEX IF NOT EXISTS idx_invites_active ON invites(revoked, expires_at);

-- ============================================================================
-- Invite Usage Tracking Table
-- ============================================================================
CREATE TABLE IF NOT EXISTS invite_usage (
    id SERIAL PRIMARY KEY,
    invite_id UUID REFERENCES invites(id) ON DELETE CASCADE,
    user_id VARCHAR(255),                     -- User who used the invite
    used_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    ip_address VARCHAR(45),                   -- IPv4 or IPv6
    user_agent TEXT,
    successful BOOLEAN DEFAULT TRUE
);

-- Create indexes for usage tracking
CREATE INDEX IF NOT EXISTS idx_invite_usage_invite ON invite_usage(invite_id);
CREATE INDEX IF NOT EXISTS idx_invite_usage_user ON invite_usage(user_id);
CREATE INDEX IF NOT EXISTS idx_invite_usage_time ON invite_usage(used_at);
