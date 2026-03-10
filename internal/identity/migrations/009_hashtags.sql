-- 009_hashtags.sql
-- Adds hashtag support: hashtags table, post_hashtags join table, and federated hashtag search.

CREATE TABLE IF NOT EXISTS hashtags (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tag         TEXT NOT NULL UNIQUE,          -- lowercase, no leading #
    post_count  INT  NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_hashtags_tag ON hashtags(tag);
CREATE INDEX IF NOT EXISTS idx_hashtags_post_count ON hashtags(post_count DESC);

CREATE TABLE IF NOT EXISTS post_hashtags (
    post_id     UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    tag         TEXT NOT NULL,
    PRIMARY KEY (post_id, tag)
);

CREATE INDEX IF NOT EXISTS idx_post_hashtags_tag     ON post_hashtags(tag);
CREATE INDEX IF NOT EXISTS idx_post_hashtags_post_id ON post_hashtags(post_id);
