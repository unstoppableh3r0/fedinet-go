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

	// Start background workers
	go identity.StartSessionKeyWorker()
	identity.StartInviteSweeper()

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

	// Social — core
	mux.HandleFunc("/feed", identity.GetFeedHandler)
	mux.HandleFunc("/follow", identity.FollowHandler)
	mux.HandleFunc("/unfollow", identity.UnfollowHandler)
	mux.HandleFunc("/followers", identity.GetFollowersHandler)
	mux.HandleFunc("/following", identity.GetFollowingHandler)
	mux.HandleFunc("/followers/remove", identity.RemoveFollowerHandler)
	mux.HandleFunc("/follower/remove", identity.RemoveFollowerHandler) // alias used by frontend

	// Posts
	mux.HandleFunc("/post/create", identity.CreatePostHandler)
	mux.HandleFunc("/post/like", identity.ToggleLikeHandler)
	mux.HandleFunc("/post/repost", identity.ToggleRepostHandler)
	mux.HandleFunc("/post/reply", identity.CreateReplyHandler)      // frontend alias
	mux.HandleFunc("/post/replies", identity.GetPostRepliesHandler) // frontend alias
	mux.HandleFunc("/reply", identity.CreateReplyHandler)
	mux.HandleFunc("/replies", identity.GetPostRepliesHandler)
	mux.HandleFunc("/posts/recent", identity.GetRecentPostsHandler)
	mux.HandleFunc("/posts/user", identity.GetUserPostsHandler)

	// Users
	mux.HandleFunc("/user/me", identity.MeHandler)
	mux.HandleFunc("/user/search", identity.UserSearchHandler) // local user lookup
	mux.HandleFunc("/users/suggested", identity.GetSuggestedUsersHandler)

	// Messages
	mux.HandleFunc("/message", identity.MessageHandler)
	mux.HandleFunc("/messages", identity.GetConversationsHandler)                     // frontend alias
	mux.HandleFunc("/messages/conversation", identity.GetConversationMessagesHandler) // frontend alias
	mux.HandleFunc("/conversations", identity.GetConversationsHandler)
	mux.HandleFunc("/conversation/messages", identity.GetConversationMessagesHandler)

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

	// Federation API — called by frontend for cross-server lookups
	mux.HandleFunc("/api/search", identity.FederatedSearchHandler)             // frontend cross-server: /api/search?q=alice@server_b
	mux.HandleFunc("/api/users/", identity.GetPublicUserHandler)               // remote server user lookup: /api/users/{username}
	mux.HandleFunc("/api/posts/federated", identity.FederatedUserPostsHandler) // frontend federated posts proxy

	// Federation incoming — called by remote servers
	mux.HandleFunc("/federation/handshake", identity.HandleHandshakeRequest)
	mux.HandleFunc("/federation/handshake/ack", identity.HandleHandshakeAcknowledgment)
	mux.HandleFunc("/federation/profile", identity.HandleIncomingProfileUpdate)
	mux.HandleFunc("/federation/message", identity.HandleIncomingFederatedMessage)
	mux.HandleFunc("/federation/notification", identity.HandleIncomingFederatedNotification)
	mux.HandleFunc("/api/notification/federated", identity.HandleIncomingFederatedNotification) // alias used by DeliverFederatedNotification
	mux.HandleFunc("/federation/follow", identity.HandleIncomingFederatedFollow)

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
	// Aliases matching what the admin frontend expects
	mux.Handle("/admin/config/server", identity.AdminAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut || r.Method == http.MethodPost {
			identity.UpdateServerConfigHandler(w, r)
		} else {
			identity.GetServerConfigHandler(w, r)
		}
	})))
	mux.Handle("/admin/config/test-db", identity.AdminAuthMiddleware(http.HandlerFunc(identity.TestDatabaseHandler)))
	mux.Handle("/admin/migrate", identity.AdminAuthMiddleware(http.HandlerFunc(identity.StartMigrationHandler)))
	mux.Handle("/admin/migrate/start", identity.AdminAuthMiddleware(http.HandlerFunc(identity.StartMigrationHandler)))
	mux.Handle("/admin/migrate/status", identity.AdminAuthMiddleware(http.HandlerFunc(identity.GetMigrationStatusHandler)))
	mux.Handle("/admin/users", identity.AdminAuthMiddleware(http.HandlerFunc(identity.GetAllUsersHandler)))
	mux.Handle("/admin/users/list", identity.AdminAuthMiddleware(http.HandlerFunc(identity.GetAllUsersHandler)))
	mux.Handle("/admin/stats", identity.AdminAuthMiddleware(http.HandlerFunc(identity.GetStatsHandler)))

	// Admin invite management
	mux.Handle("/admin/invites/list", identity.AdminAuthMiddleware(http.HandlerFunc(identity.ListInvitesHandler)))
	mux.Handle("/admin/invites/generate", identity.AdminAuthMiddleware(http.HandlerFunc(identity.GenerateInviteHandler)))
	mux.Handle("/admin/invites/revoke", identity.AdminAuthMiddleware(http.HandlerFunc(identity.RevokeInviteHandler)))
	mux.Handle("/admin/invites/qr", identity.AdminAuthMiddleware(http.HandlerFunc(identity.GetInviteQRHandler)))

	// Admin trusted-server management (admin-prefixed aliases for the admin panel)
	mux.Handle("/admin/trusted-servers/list", identity.AdminAuthMiddleware(http.HandlerFunc(identity.GetTrustedServersHandler)))
	mux.Handle("/admin/trusted-servers/add", identity.AdminAuthMiddleware(http.HandlerFunc(identity.AddTrustedServerHandler)))
	mux.Handle("/admin/trusted-servers/remove", identity.AdminAuthMiddleware(http.HandlerFunc(identity.RemoveTrustedServerHandler)))
	mux.Handle("/admin/trusted-servers/test", identity.AdminAuthMiddleware(http.HandlerFunc(identity.TestTrustedServerConnectionHandler)))

	handler := corsMiddleware(mux)

	log.Println("✅ Identity service listening on :8082")
	if err := http.ListenAndServe(":8082", handler); err != nil {
		log.Fatalf("❌ Server failed: %v", err)
	}
}
