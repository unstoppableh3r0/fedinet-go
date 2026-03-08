package moderation

import (
	"database/sql"
	"log"
	"os"

	_ "github.com/lib/pq"
)

func InitDB() *sql.DB {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL not set")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}

	if err = db.Ping(); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}

	return db
}

func ApplyMigrations(db *sql.DB) {
	// Create tables if not exist
	queries := []string{
		`CREATE TABLE IF NOT EXISTS reports (
			id BIGSERIAL PRIMARY KEY,
			reporter_id TEXT NOT NULL,
			target_ref TEXT NOT NULL,
			target_server TEXT NOT NULL,
			reason TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			created_at TIMESTAMP WITHOUT TIME ZONE DEFAULT NOW(),
			resolved_at TIMESTAMP WITHOUT TIME ZONE,
			resolved_by TEXT
		);`,
		`CREATE INDEX IF NOT EXISTS idx_reports_status ON reports(status);`,
		`CREATE INDEX IF NOT EXISTS idx_reports_target_server ON reports(target_server);`,
		`CREATE INDEX IF NOT EXISTS idx_reports_created_at ON reports(created_at DESC);`,

		`CREATE TABLE IF NOT EXISTS federation_events (
			id BIGSERIAL PRIMARY KEY,
			event_type TEXT NOT NULL,
			target_server TEXT NOT NULL,
			payload BYTEA NOT NULL,
			retry_count INTEGER DEFAULT 0,
			created_at TIMESTAMP WITHOUT TIME ZONE DEFAULT NOW(),
			last_tried_at TIMESTAMP WITHOUT TIME ZONE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_federation_events_target ON federation_events(target_server);`,
		`CREATE INDEX IF NOT EXISTS idx_federation_events_retry ON federation_events(retry_count);`,

		`CREATE TABLE IF NOT EXISTS backup_metadata (
			id BIGSERIAL PRIMARY KEY,
			created_at TIMESTAMP WITHOUT TIME ZONE DEFAULT NOW(),
			location TEXT NOT NULL,
			created_by TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_backup_created_at ON backup_metadata(created_at DESC);`,

		`CREATE TABLE IF NOT EXISTS blocked_servers (
			id BIGSERIAL PRIMARY KEY,
			server_url TEXT NOT NULL UNIQUE,
			reason TEXT NOT NULL,
			blocked_by TEXT NOT NULL,
			blocked_at TIMESTAMP WITHOUT TIME ZONE DEFAULT NOW(),
			is_active BOOLEAN DEFAULT TRUE,
			created_at TIMESTAMP WITHOUT TIME ZONE DEFAULT NOW(),
			updated_at TIMESTAMP WITHOUT TIME ZONE DEFAULT NOW()
		);`,
		`CREATE INDEX IF NOT EXISTS idx_blocked_servers_url ON blocked_servers(server_url);`,

		// User blocks table
		`CREATE TABLE IF NOT EXISTS user_blocks (
			id             BIGSERIAL PRIMARY KEY,
			blocker_user_id TEXT NOT NULL,
			blocked_user_id TEXT NOT NULL,
			reason          TEXT NOT NULL DEFAULT '',
			is_active       BOOLEAN NOT NULL DEFAULT TRUE,
			expires_at      TIMESTAMP WITHOUT TIME ZONE,
			created_at      TIMESTAMP WITHOUT TIME ZONE DEFAULT NOW(),
			CONSTRAINT uq_user_block UNIQUE (blocker_user_id, blocked_user_id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_user_blocks_blocker ON user_blocks(blocker_user_id);`,
		`CREATE INDEX IF NOT EXISTS idx_user_blocks_blocked ON user_blocks(blocked_user_id);`,

		// Moderation results from AI screening
		`CREATE TABLE IF NOT EXISTS moderation_results (
			id             BIGSERIAL PRIMARY KEY,
			content_id     TEXT NOT NULL UNIQUE,
			content_type   TEXT NOT NULL DEFAULT 'post',
			toxicity_score DOUBLE PRECISION NOT NULL DEFAULT 0.0,
			recommendation TEXT NOT NULL DEFAULT 'SAFE',
			review_status  TEXT NOT NULL DEFAULT 'PENDING',
			created_at     TIMESTAMP WITHOUT TIME ZONE DEFAULT NOW(),
			updated_at     TIMESTAMP WITHOUT TIME ZONE DEFAULT NOW()
		);`,
		`CREATE INDEX IF NOT EXISTS idx_moderation_results_status ON moderation_results(review_status);`,
		`CREATE INDEX IF NOT EXISTS idx_moderation_results_toxicity ON moderation_results(toxicity_score DESC);`,
	}

	for _, query := range queries {
		_, err := db.Exec(query)
		if err != nil {
			log.Fatalf("Failed to apply migration: %v\nQuery: %s", err, query)
		}
	}
}
