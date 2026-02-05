-- Update existing users to federated format (username@localhost)
-- Must update parent table (identities) FIRST, then child tables

BEGIN;

-- Step 1: Update identities FIRST (parent table) - TEXT user_id column  
-- This is safe because no other table has FK pointing TO identities requiring strict matching yet
UPDATE identities 
SET user_id = user_id || '@localhost' 
WHERE user_id NOT LIKE '%@%' AND user_id::text IS NOT NULL;

-- Step 2: Update profiles (references identities.user_id)
UPDATE profiles 
SET user_id = user_id || '@localhost' 
WHERE user_id NOT LIKE '%@%';

-- Step 3: Update all other child tables
UPDATE posts 
SET author = author || '@localhost' 
WHERE author NOT LIKE '%@%';

UPDATE activities 
SET actor_id = actor_id || '@localhost' 
WHERE actor_id NOT LIKE '%@%';

UPDATE activities 
SET target_id = target_id || '@localhost' 
WHERE target_id NOT LIKE '%@%' AND target_id IS NOT NULL AND target_id != '';

UPDATE follows 
SET follower_user_id = follower_user_id || '@localhost' 
WHERE follower_user_id NOT LIKE '%@%';

UPDATE follows 
SET followee_user_id = followee_user_id || '@localhost' 
WHERE followee_user_id NOT LIKE '%@%';

UPDATE messages 
SET sender = sender || '@localhost' 
WHERE sender NOT LIKE '%@%';

UPDATE messages 
SET receiver = receiver || '@localhost' 
WHERE receiver NOT LIKE '%@%';

UPDATE notifications 
SET user_id = user_id || '@localhost' 
WHERE user_id NOT LIKE '%@%';

COMMIT;
