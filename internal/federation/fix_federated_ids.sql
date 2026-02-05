-- Fix Federated IDs by temporarily dropping ALL foreign key constraints
BEGIN;

-- 1. Drop constraints
ALTER TABLE profiles DROP CONSTRAINT IF EXISTS profiles_user_id_fkey;
ALTER TABLE posts DROP CONSTRAINT IF EXISTS posts_author_fkey;
ALTER TABLE activities DROP CONSTRAINT IF EXISTS activities_actor_id_fkey;
ALTER TABLE follows DROP CONSTRAINT IF EXISTS follows_follower_user_id_fkey;
ALTER TABLE follows DROP CONSTRAINT IF EXISTS follows_followee_user_id_fkey;
ALTER TABLE messages DROP CONSTRAINT IF EXISTS messages_sender_fkey;
ALTER TABLE messages DROP CONSTRAINT IF EXISTS messages_receiver_fkey;
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_user_id_fkey;

-- 2. Update identities (the source of truth)
UPDATE identities 
SET user_id = user_id || '@localhost' 
WHERE user_id NOT LIKE '%@%' AND user_id::text IS NOT NULL;

-- 3. Update profiles
UPDATE profiles 
SET user_id = user_id || '@localhost' 
WHERE user_id NOT LIKE '%@%';

-- 4. Update interactions
UPDATE posts SET author = author || '@localhost' WHERE author NOT LIKE '%@%';
UPDATE activities SET actor_id = actor_id || '@localhost' WHERE actor_id NOT LIKE '%@%';
UPDATE activities SET target_id = target_id || '@localhost' WHERE target_id NOT LIKE '%@%' AND target_id IS NOT NULL AND target_id != '';
UPDATE follows SET follower_user_id = follower_user_id || '@localhost' WHERE follower_user_id NOT LIKE '%@%';
UPDATE follows SET followee_user_id = followee_user_id || '@localhost' WHERE followee_user_id NOT LIKE '%@%';
UPDATE messages SET sender = sender || '@localhost' WHERE sender NOT LIKE '%@%';
UPDATE messages SET receiver = receiver || '@localhost' WHERE receiver NOT LIKE '%@%';
UPDATE notifications SET user_id = user_id || '@localhost' WHERE user_id NOT LIKE '%@%';

-- 5. Restore constraints
ALTER TABLE profiles 
ADD CONSTRAINT profiles_user_id_fkey 
FOREIGN KEY (user_id) REFERENCES identities(user_id) ON DELETE CASCADE;

ALTER TABLE posts 
ADD CONSTRAINT posts_author_fkey 
FOREIGN KEY (author) REFERENCES identities(user_id) ON DELETE CASCADE;

ALTER TABLE activities 
ADD CONSTRAINT activities_actor_id_fkey 
FOREIGN KEY (actor_id) REFERENCES identities(user_id) ON DELETE CASCADE;

ALTER TABLE follows 
ADD CONSTRAINT follows_follower_user_id_fkey 
FOREIGN KEY (follower_user_id) REFERENCES identities(user_id) ON DELETE CASCADE;

ALTER TABLE follows 
ADD CONSTRAINT follows_followee_user_id_fkey 
FOREIGN KEY (followee_user_id) REFERENCES identities(user_id) ON DELETE CASCADE;

ALTER TABLE messages 
ADD CONSTRAINT messages_sender_fkey 
FOREIGN KEY (sender) REFERENCES identities(user_id) ON DELETE CASCADE;

ALTER TABLE messages 
ADD CONSTRAINT messages_receiver_fkey 
FOREIGN KEY (receiver) REFERENCES identities(user_id) ON DELETE CASCADE;

ALTER TABLE notifications 
ADD CONSTRAINT notifications_user_id_fkey 
FOREIGN KEY (user_id) REFERENCES identities(user_id) ON DELETE CASCADE;

COMMIT;

SELECT 'Migration completed successfully!' AS status;
