-- 009_identity_vouches.sql
-- Cryptographically-signed attestations: a trusted server vouches that a user's
-- identity is real.  Any peer that holds the vouching server's public key can
-- independently verify the signature without contacting that server again.

CREATE TABLE IF NOT EXISTS identity_vouches (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    vouched_user_id  TEXT        NOT NULL,           -- e.g. "alice@server_b"
    vouching_server_id   TEXT    NOT NULL,           -- e.g. "server_a"
    vouching_server_name TEXT    NOT NULL DEFAULT '',
    signature        TEXT        NOT NULL,           -- hex Ed25519 sig over canonical payload
    issued_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at       TIMESTAMPTZ,                    -- NULL = never expires
    revoked_at       TIMESTAMPTZ,                    -- NULL = still valid
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- A server can only hold one active vouch per user
    UNIQUE (vouched_user_id, vouching_server_id)
);

CREATE INDEX IF NOT EXISTS idx_vouches_user   ON identity_vouches (vouched_user_id);
CREATE INDEX IF NOT EXISTS idx_vouches_server ON identity_vouches (vouching_server_id);
