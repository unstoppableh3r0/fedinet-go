package main

import (
	"log"
	"net/http"

	"github.com/joho/godotenv"
)

func main() {
	err := godotenv.Load("internal/moderation/.env")
	if err != nil {
		log.Println("Warning: moderation .env file not found, using system environment variables")
	}
	InitDB()
	ApplyMigrations()
	repo := NewRepository(db)
	service := NewService(repo)
	handler := NewHandler(service)

	mux := http.NewServeMux()
	RegisterRoutes(mux, handler)

	log.Println("Moderation service running on :8082")
	log.Fatal(http.ListenAndServe(":8082", mux))
}
