-- 008: Ephemeral (self-deleting) posts + server permission flags
-- Run against both fedinet_server_a and fedinet_server_b

-- Add expiry column to posts
ALTER TABLE posts ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_posts_expires_at ON posts(expires_at)
    WHERE expires_at IS NOT NULL;

-- Permission flags stored in server_config (same table as moderation_enabled)
-- Seeded with defaults; ON CONFLICT means re-running this migration is safe.
INSERT INTO server_config (key, value, updated_by, updated_at)
VALUES
    ('allow_ephemeral_posts', 'true',  'system', NOW()),
    ('allow_image_uploads',   'true',  'system', NOW()),
    ('allow_direct_messages', 'true',  'system', NOW()),
    ('allow_reposts',         'true',  'system', NOW()),
    ('allow_replies',         'true',  'system', NOW())
ON CONFLICT (key) DO NOTHING;
