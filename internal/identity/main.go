package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"
)

// InternalServerName is the internal identifier used in user_ids (e.g., server_a, server_b)
var InternalServerName = func() string {
	serverID := os.Getenv("SERVER_ID")
	if serverID != "" {
		return serverID
	}
	return "localhost" // fallback for development
}()

func main() {
	InitDB()
	ApplyMigrations()

	// Start session key rotation and cleanup worker
	go sessionKeyWorker()

	var isInitialized bool
	var err error
	isInitialized, err = CheckInitializationStatus()
	if err != nil {
		log.Printf("⚠️  Error checking initialization status: %v", err)
	}

	requireInit := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !isInitialized {
				RespondWithError(w, http.StatusServiceUnavailable, "Server not initialized")
				return
			}
			next(w, r)
		}
	}

	http.HandleFunc("/initialize", func(w http.ResponseWriter, r *http.Request) {
		InitializeHandler(w, r)

		if init, _ := CheckInitializationStatus(); init {
			isInitialized = true
			log.Println("✅ Server initialized successfully (Hot Reload)")
		}
	})

	http.HandleFunc("/status", StatusHandler)
	http.HandleFunc("/health", HealthCheckHandler)
	http.HandleFunc("/server/info", GetServerInfoHandler)

	// Federation handshake endpoints (public, no auth required)
	http.HandleFunc("/api/handshake", HandleHandshakeRequest)
	http.HandleFunc("/api/handshake/ack", HandleHandshakeAcknowledgment)

	// Federated messaging endpoint (public, signature-verified)
	http.HandleFunc("/api/message/federated", HandleIncomingFederatedMessage)

	http.HandleFunc("/follow", requireInit(FollowHandler))
	http.HandleFunc("/message", requireInit(MessageHandler))
	http.HandleFunc("/user/search", requireInit(UserSearchHandler))
	http.HandleFunc("/register", RegisterHandler)
	http.HandleFunc("/login", LoginHandler)
	http.HandleFunc("/user/me", requireInit(MeHandler))
	http.HandleFunc("/profile/update", requireInit(UpdateProfileHandler))
	http.HandleFunc("/post/create", requireInit(CreatePostHandler))
	http.HandleFunc("/posts/user", requireInit(GetUserPostsHandler))

	http.HandleFunc("/identity/revoke", requireInit(RevokeKeyHandler))
	http.HandleFunc("/identity/revocations", requireInit(GetRevocationsHandler))

	http.HandleFunc("/posts/recent", requireInit(GetRecentPostsHandler))
	http.HandleFunc("/users/suggested", requireInit(GetSuggestedUsersHandler))

	http.HandleFunc("/identity/recover", requireInit(RecoverAccountHandler))
	http.HandleFunc("/identity/block", requireInit(BlockUserHandler))
	http.HandleFunc("/identity/unblock", requireInit(UnblockUserHandler))
	http.HandleFunc("/identity/blocks", requireInit(GetBlocksHandler))

	http.HandleFunc("/post/like", requireInit(ToggleLikeHandler))
	http.HandleFunc("/post/repost", requireInit(ToggleRepostHandler))
	http.HandleFunc("/post/reply", requireInit(CreateReplyHandler))
	http.HandleFunc("/post/replies", requireInit(GetPostRepliesHandler))

	http.HandleFunc("/feed", requireInit(GetFeedHandler))
	http.HandleFunc("/followers", requireInit(GetFollowersHandler))
	http.HandleFunc("/following", requireInit(GetFollowingHandler))
	http.HandleFunc("/follower/remove", requireInit(RemoveFollowerHandler))
	http.HandleFunc("/unfollow", requireInit(UnfollowHandler))
	http.HandleFunc("/messages", requireInit(GetConversationsHandler))
	http.HandleFunc("/messages/conversation", requireInit(GetConversationMessagesHandler))

	http.HandleFunc("/notifications", requireInit(GetNotificationsHandler))
	http.HandleFunc("/notifications/read", requireInit(MarkNotificationsReadHandler))

	http.HandleFunc("/admin/login", requireInit(AdminLoginHandler))

	adminMiddleware := func(h http.Handler) http.Handler {
		return AdminAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isInitialized {
				RespondWithError(w, http.StatusServiceUnavailable, "Server not initialized")
				return
			}
			h.ServeHTTP(w, r)
		}))
	}

	http.Handle("/admin/config/server", adminMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			GetServerConfigHandler(w, r)
		} else {
			UpdateServerConfigHandler(w, r)
		}
	})))
	http.Handle("/admin/config/test-db", adminMiddleware(http.HandlerFunc(TestDatabaseHandler)))

	http.Handle("/admin/invites/generate", adminMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req GenerateInviteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			RespondWithError(w, http.StatusBadRequest, "invalid json")
			return
		}
		admin := "admin"
		invite, err := GenerateInvite(req, admin)
		if err != nil {
			RespondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}
		RespondWithJSON(w, http.StatusOK, invite)
	})))

	http.Handle("/admin/invites/list", adminMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		invites, err := ListInvites()
		if err != nil {
			RespondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}
		RespondWithJSON(w, http.StatusOK, map[string]interface{}{"invites": invites})
	})))

	http.Handle("/admin/invites/revoke", adminMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req struct {
			InviteCode string `json:"invite_code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			RespondWithError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if err := RevokeInvite(req.InviteCode); err != nil {
			RespondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}
		RespondWithJSON(w, http.StatusOK, map[string]string{"message": "invite revoked"})
	})))

	http.Handle("/admin/invites/qr", adminMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			RespondWithError(w, http.StatusBadRequest, "code required")
			return
		}
		png, err := GenerateInviteQR(code)
		if err != nil {
			RespondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(png)
	})))

	http.Handle("/admin/stats", adminMiddleware(http.HandlerFunc(GetStatsHandler)))
	http.Handle("/admin/users/list", adminMiddleware(http.HandlerFunc(GetAllUsersHandler)))

	// Trusted Servers Management
	http.Handle("/admin/trusted-servers/list", adminMiddleware(http.HandlerFunc(GetTrustedServersHandler)))
	http.Handle("/admin/trusted-servers/add", adminMiddleware(http.HandlerFunc(AddTrustedServerHandler)))
	http.Handle("/admin/trusted-servers/update", adminMiddleware(http.HandlerFunc(UpdateTrustedServerHandler)))
	http.Handle("/admin/trusted-servers/remove", adminMiddleware(http.HandlerFunc(RemoveTrustedServerHandler)))
	http.Handle("/admin/trusted-servers/key", adminMiddleware(http.HandlerFunc(GetServerPublicKeyHandler)))
	http.Handle("/admin/trusted-servers/test", adminMiddleware(http.HandlerFunc(TestTrustedServerConnectionHandler)))

	// Federated Search
	http.HandleFunc("/api/search", requireInit(FederatedSearchHandler))
	http.HandleFunc("/api/users/", requireInit(GetPublicUserHandler))

	http.HandleFunc("/invite/validate", func(w http.ResponseWriter, r *http.Request) {

		code := r.URL.Query().Get("code")
		if code == "" {
			RespondWithError(w, http.StatusBadRequest, "code required")
			return
		}
		invite, err := ValidateInvite(code)
		if err != nil {
			RespondWithError(w, http.StatusForbidden, err.Error())
			return
		}

		var serverID, serverName, publicKey string
		db.QueryRow("SELECT server_id, server_name, public_key FROM server_identity WHERE id = 1").Scan(&serverID, &serverName, &publicKey)

		RespondWithJSON(w, http.StatusOK, map[string]interface{}{
			"valid":       true,
			"invite_type": invite.InviteType,
			"server_id":   serverID,
			"server_name": serverName,
			"public_key":  publicKey,
		})
	})

	log.Println("Go server running on :8082")
	if !isInitialized {
		log.Println("⚠️  Server NOT initialized. Visit /initialize (or admin setup) to configure.")
	} else {
		log.Println("✅ Server initialized and ready")
	}

	log.Fatal(http.ListenAndServe(":8082", enableCORS(http.DefaultServeMux)))
}

// sessionKeyWorker runs periodically to rotate expired keys and cleanup old ones
func sessionKeyWorker() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	log.Println("Session key worker started")

	for range ticker.C {
		// Rotate expired keys
		if err := RotateExpiredKeys(); err != nil {
			log.Printf("Session key rotation error: %v", err)
		}

		// Cleanup old expired keys
		if err := CleanupExpiredKeys(); err != nil {
			log.Printf("Session key cleanup error: %v", err)
		}
	}
}

func enableCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}
