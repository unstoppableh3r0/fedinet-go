package main

import (
	"log"
	"net/http"
)

func main() {
	InitDB()
	ApplyMigrations()

	// Existing user routes
	http.HandleFunc("/follow", FollowHandler)
	http.HandleFunc("/message", MessageHandler)
	http.HandleFunc("/user/search", UserSearchHandler)
	http.HandleFunc("/register", RegisterHandler)
	http.HandleFunc("/user/me", MeHandler)
	http.HandleFunc("/profile/update", UpdateProfileHandler)
	http.HandleFunc("/post/create", CreatePostHandler)
	http.HandleFunc("/posts/user", GetUserPostsHandler)

	// Social Routes
	http.HandleFunc("/post/like", ToggleLikeHandler)
	http.HandleFunc("/post/repost", ToggleRepostHandler)
	http.HandleFunc("/post/reply", CreateReplyHandler)

	// User notification routes
	http.HandleFunc("/notifications", GetUserNotificationsHandler)
	http.HandleFunc("/notifications/read", MarkNotificationReadHandler)

	// Admin routes (unprotected)
	http.HandleFunc("/admin/login", AdminLoginHandler)

	// Admin routes (protected with JWT middleware)
	http.Handle("/admin/config/server", AdminAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			GetServerConfigHandler(w, r)
		} else {
			UpdateServerConfigHandler(w, r)
		}
	})))
	http.Handle("/admin/config/test-db", AdminAuthMiddleware(http.HandlerFunc(TestDatabaseHandler)))
	http.Handle("/admin/migrate/start", AdminAuthMiddleware(http.HandlerFunc(StartMigrationHandler)))
	http.Handle("/admin/migrate/status", AdminAuthMiddleware(http.HandlerFunc(GetMigrationStatusHandler)))
	http.Handle("/admin/users/list", AdminAuthMiddleware(http.HandlerFunc(GetAllUsersHandler)))
	http.Handle("/admin/stats", AdminAuthMiddleware(http.HandlerFunc(GetStatsHandler)))

	log.Println("Go server running on :8080")
	log.Println("Admin endpoints available at /admin/*")
	log.Println("User endpoints available at /*")

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
