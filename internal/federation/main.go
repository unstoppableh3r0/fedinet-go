package main

import (
	"log"
	"net/http"
)

func main() {
	InitDB()

	http.HandleFunc("/follow", FollowHandler)
	http.HandleFunc("/message", MessageHandler)
	http.HandleFunc("/user/search", UserSearchHandler)
	http.HandleFunc("/register", RegisterHandler)
	http.HandleFunc("/user/me", MeHandler)

	log.Println("Go server running on :8080")

	// THIS is what keeps the backend alive
	log.Fatal(http.ListenAndServe(":8080", enableCORS(http.DefaultServeMux)))
}

func enableCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set CORS headers
		w.Header().Set("Access-Control-Allow-Origin", "*") // Allow all origins for dev
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		// Handle preflight requests
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}
