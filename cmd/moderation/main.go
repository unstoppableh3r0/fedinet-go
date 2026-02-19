package main

import (
	"log"
	"net/http"

	"github.com/joho/godotenv"
	"github.com/unstoppableh3r0/fedinet-go/internal/moderation"
)

func main() {

	// Load .env
	_ = godotenv.Load()

	// Init DB
	db := moderation.InitDB()
	defer db.Close()

	// Apply migrations
	moderation.ApplyMigrations(db)

	// Create repo, service, handler
	repo := moderation.NewRepository(db)
	service := moderation.NewService(repo)
	handler := moderation.NewHandler(service)

	// Router
	mux := http.NewServeMux()

	// Register moderation routes
	moderation.RegisterRoutes(mux, handler)

	log.Println("🚀 Moderation service running on :8090")

	log.Fatal(http.ListenAndServe(":8090", mux))
}
