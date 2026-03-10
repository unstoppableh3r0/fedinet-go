-- 013: TOTP backup recovery codes
-- Single-use codes generated when TOTP is enabled; allow account recovery
-- if the authenticator device is lost.

CREATE TABLE IF NOT EXISTS totp_backup_codes (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    TEXT        NOT NULL REFERENCES identities(user_id) ON DELETE CASCADE,
    code_hash  TEXT        NOT NULL,
    used       BOOLEAN     NOT NULL DEFAULT FALSE,
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Fast lookup: find unused codes for a specific user
CREATE INDEX IF NOT EXISTS idx_backup_codes_user_unused
    ON totp_backup_codes (user_id) WHERE used = FALSE;
