package federation

import (
	"database/sql"
	"embed"
	"log"
	"os"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

//go:embed migrations.sql
var migrationFile embed.FS

var db *sql.DB

func InitDB() {

	err := godotenv.Load("../../.env")
	if err != nil {
		log.Printf("Warning: Error loading .env file: %v", err)
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL not set")
	}

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

func ApplyMigrations() {
	content, err := migrationFile.ReadFile("migrations.sql")
	if err != nil {
		log.Printf("Warning: Failed to read federation migrations.sql: %v", err)
		return
	}
	log.Printf("Applying federation migration: migrations.sql")
	if _, err := db.Exec(string(content)); err != nil {
		log.Printf("Migration Warning (migrations.sql) - might already exist: %v", err)
	} else {
		log.Println("Federation database migrations applied successfully")
	}
}
