package main

import (
	"encoding/json"
	"log"
	"net/http"
)

func main() {
	InitDB()
	ApplyMigrations()

	// Global initialization state
	var isInitialized bool
	var err error
	isInitialized, err = CheckInitializationStatus()
	if err != nil {
		log.Printf("⚠️  Error checking initialization status: %v", err)
	}

	// Middleware to check initialization
	requireInit := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !isInitialized {
				RespondWithError(w, http.StatusServiceUnavailable, "Server not initialized")
				return
			}
			next(w, r)
		}
	}

	// Initialization Handler Wrapper
	http.HandleFunc("/initialize", func(w http.ResponseWriter, r *http.Request) {
		InitializeHandler(w, r)
		// Update state on success check
		if init, _ := CheckInitializationStatus(); init {
			isInitialized = true
			log.Println("✅ Server initialized successfully (Hot Reload)")
		}
	})

	// Public Status Routes (Always available)
	http.HandleFunc("/status", StatusHandler)
	http.HandleFunc("/health", HealthCheckHandler)
	http.HandleFunc("/server/info", GetServerInfoHandler)

	// User Routes (Protected by Init Check)
	http.HandleFunc("/follow", requireInit(FollowHandler))
	http.HandleFunc("/message", requireInit(MessageHandler))
	http.HandleFunc("/user/search", requireInit(UserSearchHandler))
	http.HandleFunc("/register", requireInit(RegisterHandler))
	http.HandleFunc("/login", requireInit(LoginHandler))
	http.HandleFunc("/user/me", requireInit(MeHandler))
	http.HandleFunc("/profile/update", requireInit(UpdateProfileHandler))
	http.HandleFunc("/post/create", requireInit(CreatePostHandler))
	http.HandleFunc("/posts/user", requireInit(GetUserPostsHandler))

	// Revocation
	http.HandleFunc("/identity/revoke", requireInit(RevokeKeyHandler))
	http.HandleFunc("/identity/revocations", requireInit(GetRevocationsHandler))

	// Recovery & Blocking
	http.HandleFunc("/identity/recover", requireInit(RecoverAccountHandler))
	http.HandleFunc("/identity/block", requireInit(BlockUserHandler))
	http.HandleFunc("/identity/unblock", requireInit(UnblockUserHandler))
	http.HandleFunc("/identity/blocks", requireInit(GetBlocksHandler))

	// Social Routes
	http.HandleFunc("/post/like", requireInit(ToggleLikeHandler))
	http.HandleFunc("/post/repost", requireInit(ToggleRepostHandler))
	http.HandleFunc("/post/reply", requireInit(CreateReplyHandler))
	http.HandleFunc("/post/replies", requireInit(GetPostRepliesHandler))

	// Feed and Social Discovery
	http.HandleFunc("/feed", requireInit(GetFeedHandler))
	http.HandleFunc("/followers", requireInit(GetFollowersHandler))
	http.HandleFunc("/following", requireInit(GetFollowingHandler))
	http.HandleFunc("/follower/remove", requireInit(RemoveFollowerHandler))
	http.HandleFunc("/unfollow", requireInit(UnfollowHandler))
	http.HandleFunc("/messages", requireInit(GetConversationsHandler))
	http.HandleFunc("/messages/conversation", requireInit(GetConversationMessagesHandler))

	// User notification routes
	http.HandleFunc("/notifications", requireInit(GetNotificationsHandler))
	http.HandleFunc("/notifications/read", requireInit(MarkNotificationsReadHandler))

	// Admin routes (unprotected - but login needs init)
	http.HandleFunc("/admin/login", requireInit(AdminLoginHandler))

	// Admin routes (protected) - These have their own Auth Middleware, but we also check Init
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

	// Invite Management
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

	// Admin Stats & Users List (Missing in previous context but requested by user)
	http.Handle("/admin/stats", adminMiddleware(http.HandlerFunc(GetStatsHandler)))
	http.Handle("/admin/users/list", adminMiddleware(http.HandlerFunc(GetAllUsersHandler)))
	http.Handle("/admin/users/delete", adminMiddleware(http.HandlerFunc(DeleteUserHandler)))

	http.HandleFunc("/invite/validate", func(w http.ResponseWriter, r *http.Request) {
		// Public endpoint, but needs server info.
		// If not initialized, ValidateInvite will likely fail or server info query will fail.
		// We can allow it but it might return errors if DB empty.
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
