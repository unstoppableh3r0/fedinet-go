-- Migration 011: Per-post visibility levels + close friends list
-- Adds FOLLOWERS and CLOSE_FRIENDS as user-selectable post visibility levels.
-- The existing 'visibility' column is extended (it was TEXT already).

-- Ensure the visibility column exists (it was added by earlier ad-hoc changes)
ALTER TABLE posts ADD COLUMN IF NOT EXISTS visibility TEXT NOT NULL DEFAULT 'PUBLIC';

-- Index to speed up the access-control filter on large post tables
CREATE INDEX IF NOT EXISTS idx_posts_visibility ON posts (visibility);
CREATE INDEX IF NOT EXISTS idx_posts_author_visibility ON posts (author, visibility);

-- Close friends: a user can tag specific followers as "close friends"
-- who are then allowed to see CLOSE_FRIENDS-visibility posts.
CREATE TABLE IF NOT EXISTS close_friends (
  user_id   TEXT NOT NULL,   -- the list owner (author of restricted posts)
  friend_id TEXT NOT NULL,   -- the follower who is granted close-friend access
  created_at TIMESTAMP DEFAULT NOW(),
  PRIMARY KEY (user_id, friend_id)
);

CREATE INDEX IF NOT EXISTS idx_close_friends_user ON close_friends (user_id);
CREATE INDEX IF NOT EXISTS idx_close_friends_friend ON close_friends (friend_id);
