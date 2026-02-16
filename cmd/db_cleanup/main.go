package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/lib/pq"
)

// Tables to truncate (CASCADE handles dependencies)
var tablesToClear = []string{
	// User Content
	"identities",
	"profiles",
	"posts",
	"activities",
	"follows",
	"messages",
	"likes",
	"replies",
	"reposts",
	"notifications",

	// Security/Auth
	"invites",
	"invite_usage",
	"block_events",
	"key_revocations",
	"registration_sessions",

	// Federation
	"federation_messages",
	"delivery_attempts",
	"inbox_activities",
	"outbox_activities",
	"delivery_acknowledgments",
	"rate_limits",
	"server_capabilities",
	"blocked_servers",
}

func main() {
	databases := []string{
		"postgres://postgres:postgres@localhost:5432/fedinet_server_a?sslmode=disable",
		"postgres://postgres:postgres@localhost:5432/fedinet_server_b?sslmode=disable",
	}

	for _, dsn := range databases {
		fmt.Printf("Connecting to database: %s\n", dsn)
		if err := clearDatabase(dsn); err != nil {
			log.Printf("Error clearing database %s: %v", dsn, err)
		} else {
			fmt.Println("Successfully cleared user data.")
		}
		fmt.Println("---------------------------------------------------")
	}
}

func clearDatabase(dsn string) error {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	for _, table := range tablesToClear {
		// Check if table exists first to avoid errors
		var exists bool
		query := `SELECT EXISTS (
			SELECT FROM information_schema.tables 
			WHERE  table_schema = 'public'
			AND    table_name   = $1
		);`

		err := tx.QueryRow(query, table).Scan(&exists)
		if err != nil {
			return fmt.Errorf("failed to check existence of table %s: %w", table, err)
		}

		if exists {
			fmt.Printf("  Truncating table: %s\n", table)
			_, err := tx.Exec(fmt.Sprintf("TRUNCATE TABLE %s CASCADE;", table))
			if err != nil {
				return fmt.Errorf("failed to truncate table %s: %w", table, err)
			}
		} else {
			fmt.Printf("  Skipping table (not found): %s\n", table)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}
