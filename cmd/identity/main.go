package main

import (
	"database/sql"
	"log"
	"net/http"
	"os"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/unstoppableh3r0/fedinet-go/internal/identity"
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
	log.Println("🚀 Starting Identity Service...")

	// Load .env file (optional, for local dev)
	_ = godotenv.Load("../../.env")

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL not set")
	}

	database, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("❌ Failed to open database: %v", err)
	}
	defer database.Close()

	if err = database.Ping(); err != nil {
		log.Fatalf("❌ Failed to ping database: %v", err)
	}
	log.Println("✅ Database connected")

	// Inject database into identity package
	identity.SetDB(database)

	// Run migrations
	identity.ApplyMigrations()

	mux := http.NewServeMux()

	// Server initialization & status
	mux.HandleFunc("/initialize", identity.InitializeHandler)
	mux.HandleFunc("/status", identity.StatusHandler)
	mux.HandleFunc("/server-info", identity.GetServerInfoHandler)
	mux.HandleFunc("/server/info", identity.GetServerInfoHandler)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Auth
	mux.HandleFunc("/register", identity.RegisterHandler)
	mux.HandleFunc("/login", identity.LoginHandler)
	mux.HandleFunc("/recover", identity.RecoverAccountHandler)

	// Social — feed, followers, following, replies, conversations
	mux.HandleFunc("/feed", identity.GetFeedHandler)
	mux.HandleFunc("/followers", identity.GetFollowersHandler)
	mux.HandleFunc("/following", identity.GetFollowingHandler)
	mux.HandleFunc("/followers/remove", identity.RemoveFollowerHandler)
	mux.HandleFunc("/follower/remove", identity.RemoveFollowerHandler)
	mux.HandleFunc("/unfollow", identity.UnfollowHandler)
	mux.HandleFunc("/reply", identity.CreateReplyHandler)
	mux.HandleFunc("/replies", identity.GetPostRepliesHandler)
	mux.HandleFunc("/conversations", identity.GetConversationsHandler)
	mux.HandleFunc("/conversation/messages", identity.GetConversationMessagesHandler)

	// Social actions called directly by frontend
	mux.HandleFunc("/post/create", identity.CreatePostHandler)
	mux.HandleFunc("/post/like", identity.LikePostHandler)
	mux.HandleFunc("/post/repost", identity.RepostHandler)
	mux.HandleFunc("/post/reply", identity.PostReplyHandler)
	mux.HandleFunc("/post/replies", identity.GetPostRepliesAltHandler)
	mux.HandleFunc("/posts/user", identity.GetUserPostsHandler)
	mux.HandleFunc("/posts/recent", identity.GetRecentPostsHandler)
	mux.HandleFunc("/follow", identity.FollowHandler)
	mux.HandleFunc("/message", identity.SendMessageHandler)
	mux.HandleFunc("/messages", identity.GetMessagesHandler)
	mux.HandleFunc("/messages/conversation", identity.GetMessagesConversationHandler)
	mux.HandleFunc("/user/me", identity.GetUserMeHandler)
	mux.HandleFunc("/user/search", identity.GetUserSearchHandler)
	mux.HandleFunc("/users/suggested", identity.GetSuggestedUsersHandler)

	// Notifications
	mux.HandleFunc("/notifications", identity.GetNotificationsHandler)
	mux.HandleFunc("/notifications/read", identity.MarkNotificationsReadHandler)

	// Blocking
	mux.HandleFunc("/block", identity.BlockUserHandler)
	mux.HandleFunc("/unblock", identity.UnblockUserHandler)
	mux.HandleFunc("/blocks", identity.GetBlocksHandler)

	// Keys & revocations
	mux.HandleFunc("/revoke-key", identity.RevokeKeyHandler)
	mux.HandleFunc("/revocations", identity.GetRevocationsHandler)

	// Export & import
	mux.HandleFunc("/export", identity.ExportProfileHandler)
	mux.HandleFunc("/import", identity.ImportProfileHandler)

	// Federated search
	mux.HandleFunc("/search", identity.FederatedSearchHandler)
	mux.HandleFunc("/user", identity.GetPublicUserHandler)

	// Server trust
	mux.HandleFunc("/trusted-servers", identity.GetTrustedServersHandler)
	mux.HandleFunc("/trusted-servers/add", identity.AddTrustedServerHandler)
	mux.HandleFunc("/trusted-servers/update", identity.UpdateTrustedServerHandler)
	mux.HandleFunc("/trusted-servers/remove", identity.RemoveTrustedServerHandler)
	mux.HandleFunc("/server-public-key", identity.GetServerPublicKeyHandler)

	// Admin panel
	mux.HandleFunc("/admin/login", identity.AdminLoginHandler)
	mux.Handle("/admin/config", identity.AdminAuthMiddleware(http.HandlerFunc(identity.GetServerConfigHandler)))
	mux.Handle("/admin/config/update", identity.AdminAuthMiddleware(http.HandlerFunc(identity.UpdateServerConfigHandler)))
	mux.Handle("/admin/test-db", identity.AdminAuthMiddleware(http.HandlerFunc(identity.TestDatabaseHandler)))
	mux.Handle("/admin/migrate", identity.AdminAuthMiddleware(http.HandlerFunc(identity.StartMigrationHandler)))
	mux.Handle("/admin/migrate/status", identity.AdminAuthMiddleware(http.HandlerFunc(identity.GetMigrationStatusHandler)))
	mux.Handle("/admin/users", identity.AdminAuthMiddleware(http.HandlerFunc(identity.GetAllUsersHandler)))
	mux.Handle("/admin/stats", identity.AdminAuthMiddleware(http.HandlerFunc(identity.GetStatsHandler)))

	// Frontend-compatible admin route aliases
	mux.Handle("/admin/config/server", identity.AdminAuthMiddleware(http.HandlerFunc(identity.GetServerConfigHandler)))
	mux.Handle("/admin/users/list", identity.AdminAuthMiddleware(http.HandlerFunc(identity.GetAllUsersHandler)))
	mux.Handle("/admin/config/test-db", identity.AdminAuthMiddleware(http.HandlerFunc(identity.TestDatabaseHandler)))
	mux.Handle("/admin/migrate/start", identity.AdminAuthMiddleware(http.HandlerFunc(identity.StartMigrationHandler)))
	mux.Handle("/admin/invites/list", identity.AdminAuthMiddleware(http.HandlerFunc(identity.ListInvitesHandler)))
	mux.Handle("/admin/invites/generate", identity.AdminAuthMiddleware(http.HandlerFunc(identity.GenerateInviteHandler)))
	mux.Handle("/admin/invites/revoke", identity.AdminAuthMiddleware(http.HandlerFunc(identity.RevokeInviteHandler)))
	mux.Handle("/admin/invites/qr", identity.AdminAuthMiddleware(http.HandlerFunc(identity.InviteQRHandler)))

	handler := corsMiddleware(mux)

	log.Println("✅ Identity service listening on :yes")
	if err := http.ListenAndServe(":8082", handler); err != nil {
		log.Fatalf("❌ Server failed: %v", err)
	}
}
