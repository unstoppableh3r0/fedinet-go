package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/joho/godotenv"
)

func main() {

	err := godotenv.Load(".env")
	if err != nil {
		log.Println("Warning: .env file not found, using system environment variables")
	}

	InitDB()
	ApplyMigrations()

	var isInitialized bool
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

	// Middleware: User Auth
	userAuth := func(h http.HandlerFunc) http.Handler {
		return UserAuthMiddleware(http.HandlerFunc(requireInit(h)))
	}

	adminMiddleware := func(h http.Handler) http.Handler {
		return AdminAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isInitialized {
				RespondWithError(w, http.StatusServiceUnavailable, "Server not initialized")
				return
			}
			h.ServeHTTP(w, r)
		}))
	}

	http.HandleFunc("/initialize", func(w http.ResponseWriter, r *http.Request) {
		InitializeHandler(w, r)

		if init, _ := CheckInitializationStatus(); init {
			isInitialized = true
			log.Println("✅ Server initialized successfully")
		}
	})

	http.HandleFunc("/status", StatusHandler)
	http.HandleFunc("/health", HealthCheckHandler)
	http.HandleFunc("/server/info", GetServerInfoHandler)
	http.HandleFunc("/register", requireInit(RegisterHandler))
	http.HandleFunc("/login", requireInit(LoginHandler))

	http.Handle("/user/me", userAuth(MeHandler))
	http.Handle("/profile/update", userAuth(UpdateProfileHandler))

	http.Handle("/follow", userAuth(FollowHandler))
	http.Handle("/unfollow", userAuth(UnfollowHandler))
	http.Handle("/follower/remove", userAuth(RemoveFollowerHandler))
	http.Handle("/followers", userAuth(GetFollowersHandler))
	http.Handle("/following", userAuth(GetFollowingHandler))

	http.Handle("/message", userAuth(MessageHandler))
	http.Handle("/messages", userAuth(GetConversationsHandler))
	http.Handle("/messages/conversation", userAuth(GetConversationMessagesHandler))

	http.Handle("/post/create", userAuth(CreatePostHandler))
	http.Handle("/posts/user", userAuth(GetUserPostsHandler))
	http.Handle("/post/like", userAuth(ToggleLikeHandler))
	http.Handle("/post/repost", userAuth(ToggleRepostHandler))
	http.Handle("/post/reply", userAuth(CreateReplyHandler))
	http.Handle("/post/replies", userAuth(GetPostRepliesHandler))
	http.Handle("/feed", userAuth(GetFeedHandler))

	http.Handle("/notifications", userAuth(GetNotificationsHandler))
	http.Handle("/notifications/read", userAuth(MarkNotificationsReadHandler))

	// Identity actions
	http.Handle("/identity/revoke", userAuth(RevokeKeyHandler))
	http.Handle("/identity/revocations", userAuth(GetRevocationsHandler))
	http.Handle("/identity/recover", userAuth(RecoverAccountHandler))
	http.Handle("/identity/block", userAuth(BlockUserHandler))
	http.Handle("/identity/unblock", userAuth(UnblockUserHandler))
	http.Handle("/identity/blocks", userAuth(GetBlocksHandler))

	http.HandleFunc("/admin/login", requireInit(AdminLoginHandler))

	http.Handle("/admin/config/server", adminMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			GetServerConfigHandler(w, r)
		} else {
			UpdateServerConfigHandler(w, r)
		}
	})))

	http.Handle("/admin/config/test-db", adminMiddleware(http.HandlerFunc(TestDatabaseHandler)))
	http.Handle("/admin/stats", adminMiddleware(http.HandlerFunc(GetStatsHandler)))
	http.Handle("/admin/users/list", adminMiddleware(http.HandlerFunc(GetAllUsersHandler)))

	http.Handle("/admin/invites/generate", adminMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req GenerateInviteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			RespondWithError(w, http.StatusBadRequest, "invalid json")
			return
		}
		invite, err := GenerateInvite(req, "admin")
		if err != nil {
			RespondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}
		RespondWithJSON(w, http.StatusOK, invite)
	})))

	http.Handle("/admin/invites/list", adminMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		invites, err := ListInvites()
		if err != nil {
			RespondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}
		RespondWithJSON(w, http.StatusOK, map[string]interface{}{"invites": invites})
	})))

	http.Handle("/admin/invites/revoke", adminMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	// -------------------------
	// Public Invite Validation
	// -------------------------
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

		RespondWithJSON(w, http.StatusOK, map[string]interface{}{
			"valid":       true,
			"invite_type": invite.InviteType,
		})
	})

	// -------------------------
	// Start Server
	// -------------------------
	log.Println("🚀 Identity service running on :8082")

	if !isInitialized {
		log.Println("⚠️  Server NOT initialized.")
	} else {
		log.Println("✅ Server initialized and ready")
	}

	log.Fatal(http.ListenAndServe(":8082", enableCORS(http.DefaultServeMux)))
}

// -------------------------
// CORS Middleware
// -------------------------
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
