-- Core identity schema tables
-- These tables are needed for user registration, login, posts, follows, etc.

-- Enable extensions
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Helper function
CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Identities table
CREATE TABLE IF NOT EXISTS identities (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  did TEXT,
  user_id TEXT NOT NULL UNIQUE,
  home_server TEXT NOT NULL,
  public_key TEXT NOT NULL DEFAULT '',
  private_key TEXT DEFAULT '',
  key_version INTEGER DEFAULT 1,
  recovery_key_hash TEXT DEFAULT '',
  password_hash TEXT DEFAULT '',
  allow_discovery BOOLEAN DEFAULT true,
  created_at TIMESTAMP DEFAULT now(),
  updated_at TIMESTAMP DEFAULT now()
);

-- Profiles table
CREATE TABLE IF NOT EXISTS profiles (
  user_id TEXT PRIMARY KEY,
  display_name TEXT NOT NULL,
  avatar_url TEXT,
  banner_url TEXT,
  bio TEXT,
  portfolio_url TEXT,
  birth_date DATE,
  location TEXT,
  followers_visibility TEXT DEFAULT 'public',
  following_visibility TEXT DEFAULT 'public',
  version INTEGER DEFAULT 1,
  created_at TIMESTAMP DEFAULT now(),
  updated_at TIMESTAMP DEFAULT now()
);

-- Posts table
CREATE TABLE IF NOT EXISTS posts (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  author TEXT NOT NULL,
  content TEXT NOT NULL,
  created_at TIMESTAMP DEFAULT now(),
  updated_at TIMESTAMP DEFAULT now()
);

-- Activities table
CREATE TABLE IF NOT EXISTS activities (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  actor_id TEXT NOT NULL,
  verb TEXT NOT NULL,
  object_type TEXT,
  object_id TEXT,
  target_id TEXT,
  payload JSONB,
  created_at TIMESTAMP DEFAULT now()
);

-- Follows table
CREATE TABLE IF NOT EXISTS follows (
  follower_user_id TEXT NOT NULL,
  follower_home_server TEXT NOT NULL DEFAULT 'localhost',
  followee_user_id TEXT NOT NULL,
  followee_home_server TEXT NOT NULL DEFAULT 'localhost',
  created_at TIMESTAMP DEFAULT now(),
  updated_at TIMESTAMP DEFAULT now(),
  PRIMARY KEY (follower_user_id, followee_user_id)
);

-- Messages table
CREATE TABLE IF NOT EXISTS messages (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  sender TEXT NOT NULL,
  receiver TEXT NOT NULL,
  content TEXT NOT NULL,
  created_at TIMESTAMP DEFAULT now()
);

-- Server config table
CREATE TABLE IF NOT EXISTS server_config (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at TIMESTAMP DEFAULT NOW(),
  updated_by TEXT
);

-- Notifications table (new schema with recipient/actor)
CREATE TABLE IF NOT EXISTS notifications (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  recipient_id TEXT NOT NULL,
  actor_id TEXT,
  type TEXT NOT NULL,
  entity_id TEXT,
  is_read BOOLEAN DEFAULT FALSE,
  created_at TIMESTAMP DEFAULT NOW()
);

-- Migration status table
CREATE TABLE IF NOT EXISTS migration_status (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  from_db TEXT NOT NULL,
  to_db TEXT NOT NULL,
  status TEXT NOT NULL,
  tables_migrated JSONB,
  error_message TEXT,
  started_at TIMESTAMP DEFAULT NOW(),
  completed_at TIMESTAMP
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_notifications_recipient ON notifications(recipient_id);
CREATE INDEX IF NOT EXISTS idx_notifications_is_read ON notifications(is_read);
CREATE INDEX IF NOT EXISTS idx_posts_author ON posts(author);
CREATE INDEX IF NOT EXISTS idx_activities_actor ON activities(actor_id);
