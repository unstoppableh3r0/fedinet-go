package main

import (
	"log"
	"net/http"

	"github.com/joho/godotenv"
	"github.com/unstoppableh3r0/fedinet-go/internal/moderation"
)

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

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

	log.Fatal(http.ListenAndServe(":8090", corsMiddleware(mux)))
}
