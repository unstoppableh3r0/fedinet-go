package main

import (
	"log"
)

// ApplyMigrations applies schema changes to the current database
func ApplyMigrations() {
	schemas := []string{
		// Enable pgcrypto extension
		`CREATE EXTENSION IF NOT EXISTS pgcrypto;`,

		// Identities Table
		`CREATE TABLE IF NOT EXISTS identities (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			did TEXT,
			user_id TEXT NOT NULL UNIQUE,
			home_server TEXT NOT NULL,
			public_key TEXT,
			private_key TEXT,
			key_version INT DEFAULT 1,
			recovery_key_hash TEXT,
			password_hash TEXT,
			allow_discovery BOOLEAN DEFAULT true,
			signature TEXT,
			metadata JSONB,
			created_at TIMESTAMP DEFAULT now(),
			updated_at TIMESTAMP DEFAULT now()
		);`,

		// Profiles Table
		`CREATE TABLE IF NOT EXISTS profiles (
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
			version INT DEFAULT 1,
			created_at TIMESTAMP DEFAULT now(),
			updated_at TIMESTAMP DEFAULT now(),
			FOREIGN KEY (user_id) REFERENCES identities(user_id) ON DELETE CASCADE
		);`,

		// Posts Table
		`CREATE TABLE IF NOT EXISTS posts (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			author TEXT NOT NULL,
			content TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT now(),
			updated_at TIMESTAMP DEFAULT now(),
			FOREIGN KEY (author) REFERENCES identities(user_id) ON DELETE CASCADE
		);`,

		// Activities Table
		`CREATE TABLE IF NOT EXISTS activities (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			actor_id TEXT NOT NULL,
			verb TEXT NOT NULL,
			object_type TEXT,
			object_id TEXT,
			target_id TEXT,
			payload JSONB,
			created_at TIMESTAMP DEFAULT now(),
			FOREIGN KEY (actor_id) REFERENCES identities(user_id) ON DELETE CASCADE
		);`,

		// Follows Table
		`CREATE TABLE IF NOT EXISTS follows (
			follower_user_id TEXT NOT NULL,
			follower_home_server TEXT NOT NULL,
			followee_user_id TEXT NOT NULL,
			followee_home_server TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT now(),
			updated_at TIMESTAMP DEFAULT now(),
			PRIMARY KEY (follower_user_id, followee_user_id),
			FOREIGN KEY (follower_user_id) REFERENCES identities(user_id) ON DELETE CASCADE,
			FOREIGN KEY (followee_user_id) REFERENCES identities(user_id) ON DELETE CASCADE
		);`,

		// Messages Table
		`CREATE TABLE IF NOT EXISTS messages (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			sender_id TEXT NOT NULL,
			recipient_id TEXT NOT NULL,
			content TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT now(),
			FOREIGN KEY (sender_id) REFERENCES identities(user_id) ON DELETE CASCADE,
			FOREIGN KEY (recipient_id) REFERENCES identities(user_id) ON DELETE CASCADE
		);`,

		// Server Config Table
		`CREATE TABLE IF NOT EXISTS server_config (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at TIMESTAMP DEFAULT NOW(),
			updated_by TEXT
		);`,

		// Server Identity Table (for Federation/Init)
		`CREATE TABLE IF NOT EXISTS server_identity (
			id INT PRIMARY KEY,
			server_id UUID NOT NULL,
			server_name TEXT NOT NULL,
			public_key TEXT NOT NULL,
			private_key_encrypted TEXT NOT NULL,
			initialized BOOLEAN DEFAULT FALSE,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		);`,

		// Admins Table
		`CREATE TABLE IF NOT EXISTS admins (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			is_super_admin BOOLEAN DEFAULT FALSE,
			created_by UUID,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		);`,

		// Likes Table
		`CREATE TABLE IF NOT EXISTS likes (
			user_id TEXT NOT NULL,
			post_id UUID NOT NULL,
			created_at TIMESTAMP DEFAULT NOW(),
			PRIMARY KEY (user_id, post_id),
			FOREIGN KEY (user_id) REFERENCES identities(user_id) ON DELETE CASCADE,
			FOREIGN KEY (post_id) REFERENCES posts(id) ON DELETE CASCADE
		);`,

		// Replies Table (Threaded)
		`CREATE TABLE IF NOT EXISTS replies (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			post_id UUID NOT NULL,
			user_id TEXT NOT NULL,
			content TEXT NOT NULL,
			parent_id UUID,
			created_at TIMESTAMP DEFAULT NOW(),
			FOREIGN KEY (post_id) REFERENCES posts(id) ON DELETE CASCADE,
			FOREIGN KEY (user_id) REFERENCES identities(user_id) ON DELETE CASCADE,
			FOREIGN KEY (parent_id) REFERENCES replies(id) ON DELETE CASCADE
		);`,

		// Reposts Table
		`CREATE TABLE IF NOT EXISTS reposts (
			user_id TEXT NOT NULL,
			post_id UUID NOT NULL,
			created_at TIMESTAMP DEFAULT NOW(),
			PRIMARY KEY (user_id, post_id),
			FOREIGN KEY (user_id) REFERENCES identities(user_id) ON DELETE CASCADE,
			FOREIGN KEY (post_id) REFERENCES posts(id) ON DELETE CASCADE
		);`,

		// Indexes for performance
		`CREATE INDEX IF NOT EXISTS idx_reposts_post_id ON reposts(post_id);`,
		`CREATE INDEX IF NOT EXISTS idx_reposts_user_id ON reposts(user_id);`,

		// models.Identity Schema Updates
		`ALTER TABLE identities ADD COLUMN IF NOT EXISTS public_key TEXT;`,
		`ALTER TABLE identities ADD COLUMN IF NOT EXISTS private_key TEXT;`,
		`ALTER TABLE identities ADD COLUMN IF NOT EXISTS key_version INT DEFAULT 1;`,
		`ALTER TABLE identities ADD COLUMN IF NOT EXISTS recovery_key_hash TEXT;`,
		`ALTER TABLE identities ADD COLUMN IF NOT EXISTS signature TEXT;`,
		`ALTER TABLE identities ADD COLUMN IF NOT EXISTS metadata JSONB;`,
		`ALTER TABLE identities ADD COLUMN IF NOT EXISTS did TEXT;`,
		`ALTER TABLE identities ADD COLUMN IF NOT EXISTS password_hash TEXT;`,

		// models.Profile Schema Updates
		`ALTER TABLE profiles ADD COLUMN IF NOT EXISTS version INT DEFAULT 1;`,

		// Key Revocations Table
		`CREATE TABLE IF NOT EXISTS key_revocations (
			key_id TEXT PRIMARY KEY,
			identity_id UUID NOT NULL,
			reason TEXT,
			revoked_at TIMESTAMP DEFAULT NOW(),
			signature TEXT NOT NULL,
			FOREIGN KEY (identity_id) REFERENCES identities(id) ON DELETE CASCADE
		);`,

		// Block Events Table (for User Story 1.9)
		`CREATE TABLE IF NOT EXISTS block_events (
			blocker_id TEXT NOT NULL,
			blocked_id TEXT NOT NULL,
			reason TEXT,
			created_at TIMESTAMP DEFAULT NOW(),
			signature TEXT NOT NULL,
			PRIMARY KEY (blocker_id, blocked_id),
			FOREIGN KEY (blocker_id) REFERENCES identities(user_id) ON DELETE CASCADE
		);`,

		// Outbox Activities Table (for Federation)
		`CREATE TABLE IF NOT EXISTS outbox_activities (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			activity_type TEXT NOT NULL,
			actor_id TEXT NOT NULL,
			target_server TEXT NOT NULL,
			payload JSONB NOT NULL,
			delivery_status TEXT DEFAULT 'pending', -- pending, sent, failed
			attempt_count INT DEFAULT 0,
			last_attempt_at TIMESTAMP,
			created_at TIMESTAMP DEFAULT NOW()
		);`,

		// Notifications Table
		`CREATE TABLE IF NOT EXISTS notifications (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			recipient_id TEXT NOT NULL,
			actor_id TEXT NOT NULL,
			type TEXT NOT NULL, -- FOLLOW, LIKE, REPLY, REPOST
			entity_id TEXT, -- ID of the post or user involved
			is_read BOOLEAN DEFAULT FALSE,
			created_at TIMESTAMP DEFAULT NOW(),
			FOREIGN KEY (recipient_id) REFERENCES identities(user_id) ON DELETE CASCADE
		);`,

		// Schema Updates for Notifications (Fix for missing columns)
		`ALTER TABLE notifications ADD COLUMN IF NOT EXISTS actor_id TEXT;`,
		`ALTER TABLE notifications ADD COLUMN IF NOT EXISTS entity_id TEXT;`,
	}

	for _, schema := range schemas {
		_, err := db.Exec(schema)
		if err != nil {
			log.Printf("Migration Warning (might already exist): %v\nQuery: %s", err, schema)
		}
	}

	log.Println("Database migrations applied successfully.")
}
