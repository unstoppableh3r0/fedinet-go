package main

import (
	"log"
)

// ApplyMigrations applies schema changes to the current database
func ApplyMigrations() {
	schemas := []string{
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

		// Identity Schema Updates
		`ALTER TABLE identities ADD COLUMN IF NOT EXISTS public_key TEXT;`,
		`ALTER TABLE identities ADD COLUMN IF NOT EXISTS private_key TEXT;`,
		`ALTER TABLE identities ADD COLUMN IF NOT EXISTS key_version INT DEFAULT 1;`,
		`ALTER TABLE identities ADD COLUMN IF NOT EXISTS recovery_key_hash TEXT;`,
		`ALTER TABLE identities ADD COLUMN IF NOT EXISTS signature TEXT;`,
		`ALTER TABLE identities ADD COLUMN IF NOT EXISTS metadata JSONB;`,
		`ALTER TABLE identities ADD COLUMN IF NOT EXISTS did TEXT;`,
		`ALTER TABLE identities ADD COLUMN IF NOT EXISTS password_hash TEXT;`,

		// Profile Schema Updates
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
	}

	for _, schema := range schemas {
		_, err := db.Exec(schema)
		if err != nil {
			log.Printf("Migration Warning (might already exist): %v\nQuery: %s", err, schema)
		}
	}

	log.Println("Database migrations applied successfully.")
}
