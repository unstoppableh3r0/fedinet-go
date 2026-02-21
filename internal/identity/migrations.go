package main

import (
	"log"
)

func ApplyMigrations() {
	schemas := []string{

		`CREATE TABLE IF NOT EXISTS likes (
			user_id TEXT NOT NULL,
			post_id UUID NOT NULL,
			created_at TIMESTAMP DEFAULT NOW(),
			PRIMARY KEY (user_id, post_id),
			FOREIGN KEY (user_id) REFERENCES identities(user_id) ON DELETE CASCADE,
			FOREIGN KEY (post_id) REFERENCES posts(id) ON DELETE CASCADE
		);`,

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

		`CREATE TABLE IF NOT EXISTS reposts (
			user_id TEXT NOT NULL,
			post_id UUID NOT NULL,
			created_at TIMESTAMP DEFAULT NOW(),
			PRIMARY KEY (user_id, post_id),
			FOREIGN KEY (user_id) REFERENCES identities(user_id) ON DELETE CASCADE,
			FOREIGN KEY (post_id) REFERENCES posts(id) ON DELETE CASCADE
		);`,

		`CREATE INDEX IF NOT EXISTS idx_reposts_post_id ON reposts(post_id);`,
		`CREATE INDEX IF NOT EXISTS idx_reposts_user_id ON reposts(user_id);`,

		`ALTER TABLE identities ADD COLUMN IF NOT EXISTS public_key TEXT;`,
		`ALTER TABLE identities ADD COLUMN IF NOT EXISTS private_key TEXT;`,
		`ALTER TABLE identities ADD COLUMN IF NOT EXISTS key_version INT DEFAULT 1;`,
		`ALTER TABLE identities ADD COLUMN IF NOT EXISTS recovery_key_hash TEXT;`,
		`ALTER TABLE identities ADD COLUMN IF NOT EXISTS signature TEXT;`,
		`ALTER TABLE identities ADD COLUMN IF NOT EXISTS metadata JSONB;`,
		`ALTER TABLE identities ADD COLUMN IF NOT EXISTS did TEXT;`,
		`ALTER TABLE identities ADD COLUMN IF NOT EXISTS password_hash TEXT;`,
		`ALTER TABLE identities ADD COLUMN IF NOT EXISTS client_public_key TEXT;`,
		`ALTER TABLE identities ADD COLUMN IF NOT EXISTS allow_discovery BOOLEAN DEFAULT true;`,

		`ALTER TABLE profiles ADD COLUMN IF NOT EXISTS version INT DEFAULT 1;`,

		`CREATE TABLE IF NOT EXISTS key_revocations (
			key_id TEXT PRIMARY KEY,
			identity_id UUID NOT NULL,
			reason TEXT,
			revoked_at TIMESTAMP DEFAULT NOW(),
			signature TEXT NOT NULL,
			FOREIGN KEY (identity_id) REFERENCES identities(id) ON DELETE CASCADE
		);`,

		`CREATE TABLE IF NOT EXISTS block_events (
			blocker_id TEXT NOT NULL,
			blocked_id TEXT NOT NULL,
			reason TEXT,
			created_at TIMESTAMP DEFAULT NOW(),
			signature TEXT NOT NULL,
			PRIMARY KEY (blocker_id, blocked_id),
			FOREIGN KEY (blocker_id) REFERENCES identities(user_id) ON DELETE CASCADE
		);`,

		// Trusted servers for federation
		`CREATE TABLE IF NOT EXISTS trusted_servers (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			server_id TEXT NOT NULL UNIQUE,
			server_name TEXT NOT NULL,
			public_key TEXT NOT NULL DEFAULT '',
			endpoint TEXT NOT NULL,
			trusted_at TIMESTAMP DEFAULT NOW()
		);`,

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

		// ActivityStreams 2.0 payload column (added after initial schema)
		`ALTER TABLE notifications ADD COLUMN IF NOT EXISTS activity_stream JSONB;`,

		// Cache table for profile data received from federated (remote) servers.
		// Keyed by the internal user_id (e.g. alice@server_b).
		`CREATE TABLE IF NOT EXISTS remote_profiles (
			user_id       TEXT PRIMARY KEY,
			display_name  TEXT NOT NULL DEFAULT '',
			bio           TEXT NOT NULL DEFAULT '',
			avatar_url    TEXT NOT NULL DEFAULT '',
			banner_url    TEXT NOT NULL DEFAULT '',
			location      TEXT NOT NULL DEFAULT '',
			portfolio_url TEXT NOT NULL DEFAULT '',
			version       INT  NOT NULL DEFAULT 0,
			updated_at    TIMESTAMP DEFAULT NOW()
		);`,
	}

	for _, schema := range schemas {
		_, err := db.Exec(schema)
		if err != nil {
			log.Printf("Migration Warning (might already exist): %v\nQuery: %s", err, schema)
		}
	}

	log.Println("Database migrations applied successfully.")
}
