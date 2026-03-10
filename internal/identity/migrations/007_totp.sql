-- 007: TOTP / Authenticator-App key protection
-- Adds TOTP secret storage and encrypted-private-key backup to identities.

ALTER TABLE identities
    ADD COLUMN IF NOT EXISTS totp_secret_encrypted  TEXT    DEFAULT NULL,
    ADD COLUMN IF NOT EXISTS totp_enabled            BOOLEAN DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS client_private_key_enc  TEXT    DEFAULT NULL;

-- Partial-login tokens (issued after password check, before TOTP verification)
CREATE TABLE IF NOT EXISTS totp_partial_tokens (
    token       TEXT        PRIMARY KEY,
    user_id     TEXT        NOT NULL REFERENCES identities(user_id) ON DELETE CASCADE,
    expires_at  TIMESTAMPTZ NOT NULL,
    used        BOOLEAN     DEFAULT FALSE
);

-- Auto-expire cleanup index
CREATE INDEX IF NOT EXISTS idx_totp_partial_tokens_expires
    ON totp_partial_tokens (expires_at);
