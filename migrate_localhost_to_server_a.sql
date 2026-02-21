-- Migration script to update all user_ids from @localhost to @server-a
BEGIN;

-- Temporarily disable triggers to avoid foreign key issues
SET session_replication_role = replica;

-- Update all tables with user_id references
UPDATE identities SET user_id = REPLACE(user_id, '@localhost', '@server-a') WHERE user_id LIKE '%@localhost';
UPDATE profiles SET user_id = REPLACE(user_id, '@localhost', '@server-a') WHERE user_id LIKE '%@localhost';
UPDATE posts SET author = REPLACE(author, '@localhost', '@server-a') WHERE author LIKE '%@localhost';
UPDATE likes SET user_id = REPLACE(user_id, '@localhost', '@server-a') WHERE user_id LIKE '%@localhost';
UPDATE reposts SET user_id = REPLACE(user_id, '@localhost', '@server-a') WHERE user_id LIKE '%@localhost';
UPDATE follows SET follower_user_id = REPLACE(follower_user_id, '@localhost', '@server-a'), followee_user_id = REPLACE(followee_user_id, '@localhost', '@server-a') WHERE follower_user_id LIKE '%@localhost' OR followee_user_id LIKE '%@localhost';
UPDATE messages SET sender = REPLACE(sender, '@localhost', '@server-a'), receiver = REPLACE(receiver, '@localhost', '@server-a') WHERE sender LIKE '%@localhost' OR receiver LIKE '%@localhost';
UPDATE notifications SET recipient_id = REPLACE(recipient_id, '@localhost', '@server-a'), actor_id = REPLACE(actor_id, '@localhost', '@server-a') WHERE recipient_id LIKE '%@localhost' OR actor_id LIKE '%@localhost';

-- Re-enable triggers
SET session_replication_role = DEFAULT;

COMMIT;
