-- 012: Passkey (WebAuthn) credentials
-- Stores public-key credentials for passwordless login.
-- Passkeys are the primary login method; TOTP + recovery key are used for
-- account recovery when the passkey device is lost.

CREATE TABLE IF NOT EXISTS passkeys (
    id            BIGSERIAL    PRIMARY KEY,
    user_id       TEXT         NOT NULL REFERENCES identities(user_id) ON DELETE CASCADE,
    credential_id BYTEA        NOT NULL UNIQUE,
    public_key    BYTEA        NOT NULL,
    sign_count    BIGINT       NOT NULL DEFAULT 0,
    aaguid        TEXT         NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_passkeys_user_id ON passkeys (user_id);

-- Rate-limit table for passkey recovery attempts (audit + lockout)
CREATE TABLE IF NOT EXISTS passkey_recovery_attempts (
    id           BIGSERIAL    PRIMARY KEY,
    user_id      TEXT         NOT NULL,
    ip_address   TEXT         NOT NULL DEFAULT '',
    attempted_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    succeeded    BOOLEAN      NOT NULL DEFAULT FALSE
);

CREATE INDEX IF NOT EXISTS idx_recovery_attempts_user_time
    ON passkey_recovery_attempts (user_id, attempted_at);
