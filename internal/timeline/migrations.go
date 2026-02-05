package main

import (
	"log"
)

// ApplyMigrations creates necessary tables for timeline features
func ApplyMigrations() {
	migrations := []string{
		// User ranking preferences
		`CREATE TABLE IF NOT EXISTS user_ranking_preferences (
			user_id VARCHAR(255) PRIMARY KEY,
			preference VARCHAR(50) NOT NULL DEFAULT 'chronological',
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		// Post versions for edit history
		`CREATE TABLE IF NOT EXISTS post_versions (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			post_id VARCHAR(255) NOT NULL,
			version INTEGER NOT NULL,
			content TEXT NOT NULL,
			editor_id VARCHAR(255) NOT NULL,
			edited_at TIMESTAMP NOT NULL DEFAULT NOW(),
			change_note TEXT,
			UNIQUE(post_id, version)
		)`,

		// Index for faster version lookups
		`CREATE INDEX IF NOT EXISTS idx_post_versions_post_id 
		ON post_versions(post_id)`,

		// Cached timeline data for offline mode
		`CREATE TABLE IF NOT EXISTS cached_timelines (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			user_id VARCHAR(255) NOT NULL,
			post_data JSONB NOT NULL,
			cached_at TIMESTAMP NOT NULL DEFAULT NOW(),
			expires_at TIMESTAMP NOT NULL,
			size_bytes BIGINT NOT NULL,
			UNIQUE(user_id)
		)`,

		// Index for cache expiration cleanup
		`CREATE INDEX IF NOT EXISTS idx_cached_timelines_expires 
		ON cached_timelines(expires_at)`,

		// Refresh configurations for adaptive feed
		`CREATE TABLE IF NOT EXISTS refresh_configs (
			user_id VARCHAR(255) PRIMARY KEY,
			base_interval_seconds INTEGER NOT NULL DEFAULT 30,
			current_interval_seconds INTEGER NOT NULL DEFAULT 30,
			min_interval_seconds INTEGER NOT NULL DEFAULT 10,
			max_interval_seconds INTEGER NOT NULL DEFAULT 300,
			last_activity TIMESTAMP NOT NULL DEFAULT NOW(),
			last_refresh TIMESTAMP NOT NULL DEFAULT NOW(),
			adaptive_enabled BOOLEAN NOT NULL DEFAULT true,
			throttle_enabled BOOLEAN NOT NULL DEFAULT true
		)`,

		// Server load metrics for adaptive refresh
		`CREATE TABLE IF NOT EXISTS server_load_metrics (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			cpu_percent DECIMAL(5,2),
			memory_percent DECIMAL(5,2),
			active_connections INTEGER,
			requests_per_sec DECIMAL(10,2),
			timestamp TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		// Index for recent metrics queries
		`CREATE INDEX IF NOT EXISTS idx_server_load_timestamp 
		ON server_load_metrics(timestamp DESC)`,

		// Offline config storage
		`CREATE TABLE IF NOT EXISTS offline_configs (
			user_id VARCHAR(255) PRIMARY KEY,
			max_cache_size_bytes BIGINT NOT NULL DEFAULT 52428800,
			max_posts_per_user INTEGER NOT NULL DEFAULT 500,
			cache_duration_seconds INTEGER NOT NULL DEFAULT 86400,
			auto_refresh BOOLEAN NOT NULL DEFAULT true,
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
	}

	for _, migration := range migrations {
		if _, err := db.Exec(migration); err != nil {
			log.Printf("Migration failed: %v\nSQL: %s", err, migration)
		}
	}

	log.Println("Timeline migrations applied successfully")
}
