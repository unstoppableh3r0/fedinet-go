package main

import (
	"database/sql"
	"log"
	"os"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

var db *sql.DB

func InitDB() {
	// Try to load .env from project root (../../.env relative to federation directory)
	err := godotenv.Load("../../.env")
	if err != nil {
		log.Printf("Warning: Error loading .env file: %v", err)
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL not set")
	}

	// Debug: Show first 50 chars of DSN (hide password)
	log.Printf("DATABASE_URL loaded: %s...", dsn[:50])

	var dbErr error
	db, dbErr = sql.Open("postgres", dsn)
	if dbErr != nil {
		log.Fatal(dbErr)
	}

	if dbErr = db.Ping(); dbErr != nil {
		log.Fatalf("Failed to ping database: %v", dbErr)
	}

	log.Println("Federation database connected")
}
