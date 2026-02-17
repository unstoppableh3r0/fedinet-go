package main

import (
	"log"
)

// ApplyMigrations creates required moderation tables
func ApplyMigrations() {
	// Reports table
	_, err := db.Exec(`
	CREATE TABLE IF NOT EXISTS reports (
		id SERIAL PRIMARY KEY,
		reporter_id TEXT NOT NULL,
		target_ref TEXT NOT NULL,
		target_server TEXT NOT NULL,
		reason TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		created_at TIMESTAMP NOT NULL DEFAULT NOW(),
		resolved_at TIMESTAMP NULL,
		resolved_by TEXT NULL
	);
	`)
	if err != nil {
		log.Fatalf("Failed to create reports table: %v", err)
	}

	// Blocked servers table
	_, err = db.Exec(`
	CREATE TABLE IF NOT EXISTS blocked_servers (
		id SERIAL PRIMARY KEY,
		domain TEXT NOT NULL UNIQUE,
		reason TEXT NOT NULL,
		blocked_at TIMESTAMP NOT NULL DEFAULT NOW(),
		blocked_by TEXT NOT NULL
	);
	`)
	if err != nil {
		log.Fatalf("Failed to create blocked_servers table: %v", err)
	}
}
