package main

import (
	"database/sql"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/unstoppableh3r0/fedinet-go/internal/identity"
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
	go identity.StartTOTPPartialTokenSweeper()
	identity.StartInviteSweeper()
	identity.StartRemotePostCachePruner()

	// ── Rate limiters ───────────────────────────────────────────────────────
	// Auth endpoints: 10 requests / minute per IP (brute-force protection)
	authLimiter := identity.NewRateLimiter(10, time.Minute)
	// Write endpoints (posts, messages, etc.): 30 requests / minute per user
	writeLimiter := identity.NewRateLimiter(30, time.Minute)
	// General read endpoints: 120 requests / minute per IP
	readLimiter := identity.NewRateLimiter(120, time.Minute)
	// Federation endpoints: 60 requests / minute per IP
	fedLimiter := identity.NewRateLimiter(60, time.Minute)

	rlAuth := identity.AuthRateLimitMiddleware(authLimiter)
	rlWrite := identity.PerUserRateLimitMiddleware(writeLimiter)
	rlRead := identity.RateLimitMiddleware(readLimiter)
	rlFed := identity.RateLimitMiddleware(fedLimiter)


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
	mux.Handle("/register", rlAuth(http.HandlerFunc(identity.RegisterHandler)))
	mux.Handle("/login", rlAuth(http.HandlerFunc(identity.LoginHandler)))
	mux.Handle("/login/totp", rlAuth(http.HandlerFunc(identity.LoginTOTPHandler)))
	mux.HandleFunc("/logout", identity.LogoutHandler)
	mux.HandleFunc("/refresh-token", identity.RefreshTokenHandler)
	mux.HandleFunc("/recover", identity.RecoverAccountHandler)

	// TOTP / Authenticator-app key protection (all auth endpoints are rate-limited)
	mux.Handle("/totp/setup", rlAuth(http.HandlerFunc(identity.TOTPSetupHandler)))
	mux.Handle("/totp/enable", rlAuth(http.HandlerFunc(identity.TOTPEnableHandler)))
	mux.Handle("/totp/status", rlRead(http.HandlerFunc(identity.TOTPStatusHandler)))
	mux.Handle("/totp/verify", rlAuth(http.HandlerFunc(identity.TOTPVerifyHandler)))
	mux.Handle("/totp/disable", rlAuth(http.HandlerFunc(identity.TOTPDisableHandler)))
	// Backup recovery codes
	mux.Handle("/totp/backup-codes/generate", rlAuth(http.HandlerFunc(identity.TOTPGenerateBackupCodesHandler)))
	mux.Handle("/totp/backup-codes/count", rlRead(http.HandlerFunc(identity.TOTPBackupCodesCountHandler)))
	mux.Handle("/login/totp/backup", rlAuth(http.HandlerFunc(identity.LoginTOTPBackupHandler)))

	// Passkeys (WebAuthn) — passwordless primary login
	// Enroll (requires JWT)
	mux.Handle("/passkey/register/begin", rlAuth(http.HandlerFunc(identity.PasskeyRegisterBeginHandler)))
	mux.Handle("/passkey/register/complete", rlAuth(http.HandlerFunc(identity.PasskeyRegisterCompleteHandler)))
	// Login (public)
	mux.Handle("/passkey/login/begin", rlAuth(http.HandlerFunc(identity.PasskeyLoginBeginHandler)))
	mux.Handle("/passkey/login/complete", rlAuth(http.HandlerFunc(identity.PasskeyLoginCompleteHandler)))
	// Recovery via TOTP + recovery key (public, rate-limited to 5/hr via DB)
	mux.Handle("/passkey/recover/begin", rlAuth(http.HandlerFunc(identity.PasskeyRecoverBeginHandler)))
	mux.Handle("/passkey/recover/complete", rlAuth(http.HandlerFunc(identity.PasskeyRecoverCompleteHandler)))
	// Status and remove (requires JWT)
	mux.Handle("/passkey/status", rlRead(http.HandlerFunc(identity.PasskeyStatusHandler)))
	mux.Handle("/passkey/remove", rlWrite(http.HandlerFunc(identity.PasskeyRemoveHandler)))

	// Social — core
	mux.HandleFunc("/feed", identity.GetFeedHandler)
	mux.HandleFunc("/follow", identity.FollowHandler)
	mux.HandleFunc("/unfollow", identity.UnfollowHandler)
	mux.HandleFunc("/followers", identity.GetFollowersHandler)
	mux.HandleFunc("/following", identity.GetFollowingHandler)
	mux.HandleFunc("/followers/remove", identity.RemoveFollowerHandler)
	mux.HandleFunc("/follower/remove", identity.RemoveFollowerHandler) // alias used by frontend

	// Close friends (per-post fine-grained visibility)
	mux.HandleFunc("/close-friends", identity.CloseFriendsHandler)
	mux.HandleFunc("/close-friends/remove", identity.RemoveCloseFriendHandler)

	// Posts
	mux.HandleFunc("/post/get", identity.GetPostByIDHandler)
	mux.Handle("/post/create", rlWrite(http.HandlerFunc(identity.CreatePostHandler)))
	mux.Handle("/post/like", rlWrite(http.HandlerFunc(identity.ToggleLikeHandler)))
	mux.Handle("/post/repost", rlWrite(http.HandlerFunc(identity.ToggleRepostHandler)))
	mux.Handle("/post/reply", rlWrite(http.HandlerFunc(identity.CreateReplyHandler))) // frontend alias
	mux.HandleFunc("/post/replies", identity.GetPostRepliesHandler)                   // frontend alias
	mux.Handle("/reply", rlWrite(http.HandlerFunc(identity.CreateReplyHandler)))
	mux.HandleFunc("/replies", identity.GetPostRepliesHandler)
	mux.HandleFunc("/posts/recent", identity.GetRecentPostsHandler)
	mux.HandleFunc("/posts/user", identity.GetUserPostsHandler)
	mux.HandleFunc("/posts/user/replies", identity.GetUserRepliesHandler)
	mux.HandleFunc("/posts/user/likes", identity.GetUserLikedPostsHandler)
	mux.HandleFunc("/posts/user/reposts", identity.GetUserRepostedPostsHandler)

	// Users
	mux.HandleFunc("/user/me", identity.MeHandler)
	mux.HandleFunc("/profile/update", identity.UpdateProfileHandler)
	mux.HandleFunc("/user/search", identity.UserSearchHandler) // local user lookup
	mux.HandleFunc("/users/suggested", identity.GetSuggestedUsersHandler)

	// Messages
	mux.Handle("/message", rlWrite(http.HandlerFunc(identity.MessageHandler)))
	mux.HandleFunc("/messages", identity.GetConversationsHandler)                     // frontend alias
	mux.HandleFunc("/messages/conversation", identity.GetConversationMessagesHandler) // frontend alias
	mux.HandleFunc("/conversations", identity.GetConversationsHandler)
	mux.HandleFunc("/conversation/messages", identity.GetConversationMessagesHandler)

	// Notifications
	mux.HandleFunc("/notifications", identity.GetNotificationsHandler)
	mux.HandleFunc("/notifications/read", identity.MarkNotificationsReadHandler)

	// Privacy
	mux.HandleFunc("/privacy/settings", identity.PrivacySettingsHandler)

	// Blocking
	mux.HandleFunc("/block", identity.BlockUserHandler)
	mux.HandleFunc("/unblock", identity.UnblockUserHandler)
	mux.HandleFunc("/blocks", identity.GetBlocksHandler)

	// Keys & revocations
	mux.HandleFunc("/revoke-key", identity.RevokeKeyHandler)
	mux.HandleFunc("/revocations", identity.GetRevocationsHandler)

	// Export & import (rate-limited: export reads data, import writes it)
	mux.Handle("/export", rlRead(http.HandlerFunc(identity.ExportProfileHandler)))
	mux.Handle("/import", rlWrite(http.HandlerFunc(identity.ImportProfileHandler)))

	// Encrypted group messaging
	mux.Handle("/groups/create", rlWrite(http.HandlerFunc(identity.CreateGroupHandler)))
	mux.Handle("/groups", rlRead(http.HandlerFunc(identity.ListGroupsHandler)))
	mux.Handle("/groups/public", rlRead(http.HandlerFunc(identity.ListPublicGroupsHandler)))
	mux.Handle("/groups/members", rlRead(http.HandlerFunc(identity.GetGroupMembersHandler)))
	mux.Handle("/groups/members/add", rlWrite(http.HandlerFunc(identity.AddGroupMemberHandler)))
	mux.Handle("/groups/members/remove", rlWrite(http.HandlerFunc(identity.RemoveGroupMemberHandler)))
	mux.Handle("/groups/leave", rlWrite(http.HandlerFunc(identity.LeaveGroupHandler)))
	mux.Handle("/groups/join", rlWrite(http.HandlerFunc(identity.JoinGroupHandler)))
	mux.Handle("/groups/policy", rlWrite(http.HandlerFunc(identity.UpdateGroupJoinPolicyHandler)))
	mux.Handle("/groups/message", rlWrite(http.HandlerFunc(identity.SendGroupMessageHandler)))
	mux.Handle("/groups/messages", rlRead(http.HandlerFunc(identity.GetGroupMessagesHandler)))

	// Federated search
	mux.HandleFunc("/search", identity.FederatedSearchHandler)
	mux.HandleFunc("/user", identity.GetPublicUserHandler)

	// Federation API — called by frontend for cross-server lookups
	mux.HandleFunc("/api/search", identity.FederatedSearchHandler)             // frontend cross-server: /api/search?q=alice@server_b
	mux.HandleFunc("/api/users/", identity.GetPublicUserHandler)               // remote server user lookup: /api/users/{username}
	mux.HandleFunc("/api/posts/federated", identity.FederatedUserPostsHandler) // frontend federated posts proxy

	// Federation incoming — called by remote servers
	mux.Handle("/federation/handshake", rlFed(http.HandlerFunc(identity.HandleHandshakeRequest)))
	mux.Handle("/federation/handshake/ack", rlFed(http.HandlerFunc(identity.HandleHandshakeAcknowledgment)))
	mux.Handle("/federation/profile", rlFed(http.HandlerFunc(identity.HandleIncomingProfileUpdate)))
	mux.Handle("/federation/message", rlFed(http.HandlerFunc(identity.HandleIncomingFederatedMessage)))
	mux.Handle("/api/message/federated", rlFed(http.HandlerFunc(identity.HandleIncomingFederatedMessage))) // alias used by DeliverFederatedMessage
	mux.Handle("/federation/notification", rlFed(http.HandlerFunc(identity.HandleIncomingFederatedNotification)))
	mux.Handle("/api/notification/federated", rlFed(http.HandlerFunc(identity.HandleIncomingFederatedNotification))) // alias used by DeliverFederatedNotification
	mux.Handle("/federation/follow", rlFed(http.HandlerFunc(identity.HandleIncomingFederatedFollow)))
	// Multi-server linked posting
	mux.Handle("/federation/linked-post", rlFed(http.HandlerFunc(identity.HandleLinkedPostHandler)))
	// Capabilities endpoint so peer servers can discover linked_posts support
	mux.HandleFunc("/federation/capabilities", identity.ServerCapabilitiesHandler)
	mux.Handle("/federation/propagate-edit", rlFed(http.HandlerFunc(identity.PropagateEditHandler)))
	// Cross-server feed slices
	mux.Handle("/federation/feed-slice", rlFed(http.HandlerFunc(identity.HandleFeedSliceHandler)))

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

	// Privacy audit log (admin-only)
	mux.Handle("/admin/privacy/logs", identity.AdminAuthMiddleware(http.HandlerFunc(identity.GetPrivacyLogsHandler)))

	// Encryption policy (admin-only)
	mux.Handle("/admin/encryption/policy", identity.AdminAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			identity.GetEncryptionPolicyHandler(w, r)
		} else {
			identity.UpdateEncryptionPolicyHandler(w, r)
		}
	})))

	// Admin invite management
	mux.Handle("/admin/invites/list", identity.AdminAuthMiddleware(http.HandlerFunc(identity.ListInvitesHandler)))
	mux.Handle("/admin/invites/generate", identity.AdminAuthMiddleware(http.HandlerFunc(identity.GenerateInviteHandler)))
	mux.Handle("/admin/invites/revoke", identity.AdminAuthMiddleware(http.HandlerFunc(identity.RevokeInviteHandler)))
	mux.Handle("/admin/invites/qr", identity.AdminAuthMiddleware(http.HandlerFunc(identity.GetInviteQRHandler)))

	// QR code generator — encodes arbitrary caller-provided data (no auth required)
	mux.HandleFunc("/user/qr", identity.GetUserQRHandler)

	// Admin trusted-server management (admin-prefixed aliases for the admin panel)
	mux.Handle("/admin/trusted-servers/list", identity.AdminAuthMiddleware(http.HandlerFunc(identity.GetTrustedServersHandler)))
	mux.Handle("/admin/trusted-servers/add", identity.AdminAuthMiddleware(http.HandlerFunc(identity.AddTrustedServerHandler)))
	mux.Handle("/admin/trusted-servers/remove", identity.AdminAuthMiddleware(http.HandlerFunc(identity.RemoveTrustedServerHandler)))
	mux.Handle("/admin/trusted-servers/test", identity.AdminAuthMiddleware(http.HandlerFunc(identity.TestTrustedServerConnectionHandler)))

	// ── Admin snapshots (dashboard trend chart) ───────────────────────────────
	mux.Handle("/admin/snapshots", identity.AdminAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			identity.GetSnapshotsHandler(w, r)
		} else if r.Method == http.MethodPost {
			identity.SaveSnapshotHandler(w, r)
		} else {
			identity.RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})))

	// ── Admin account links (graph view) ──────────────────────────────────────
	mux.Handle("/admin/account/links", identity.AdminAuthMiddleware(http.HandlerFunc(identity.GetAccountLinksHandler)))

	// ── User account links (bidirectional connection requests) ────────────────
	mux.HandleFunc("/account/links", identity.GetAccountLinksUserHandler)
	mux.HandleFunc("/account/link/request", identity.RequestAccountLinkHandler)
	mux.HandleFunc("/account/link/accept", identity.AcceptAccountLinkHandler)
	mux.HandleFunc("/account/link/reject", identity.RejectAccountLinkHandler)
	mux.HandleFunc("/account/link/remove", identity.RemoveAccountLinkHandler)
	mux.HandleFunc("/account/link/switch", identity.SwitchAccountLinkHandler)
	mux.HandleFunc("/account/link/sync", identity.SyncAccountLinkStatusHandler)

	// ── Moderator role management (admin-only) ────────────────────────────────

	mux.Handle("/admin/moderators/assign", identity.AdminAuthMiddleware(http.HandlerFunc(identity.AssignModeratorHandler)))
	mux.Handle("/admin/moderators/remove", identity.AdminAuthMiddleware(http.HandlerFunc(identity.RemoveModeratorHandler)))
	mux.Handle("/admin/moderators/list", identity.AdminAuthMiddleware(http.HandlerFunc(identity.ListModeratorsHandler)))

	// ── Badge management (admin-only) ─────────────────────────────────────────
	mux.Handle("/admin/users/assign-badge", identity.AdminAuthMiddleware(http.HandlerFunc(identity.AssignBadgeHandler)))

	// ── Moderation feature toggle (admin-only) ────────────────────────────────
	mux.Handle("/admin/moderation/status", identity.AdminAuthMiddleware(http.HandlerFunc(identity.GetModerationStatusHandler)))
	mux.Handle("/admin/moderation/toggle", identity.AdminAuthMiddleware(http.HandlerFunc(identity.ToggleModerationHandler)))

	// ── Moderator login (public route — checked against moderator_roles table) ─
	mux.HandleFunc("/moderator/login", identity.ModeratorLoginHandler)

	// ── Moderation actions — protected by moderator OR admin JWT ─────────────
	mux.Handle("/moderation/queue", identity.ModeratorAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		repo := moderation.NewRepository(database)
		service := moderation.NewService(repo)
		handler := moderation.NewHandler(service)
		handler.GetModerationQueue(w, r)
	})))
	mux.Handle("/moderation/approve", identity.ModeratorAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		repo := moderation.NewRepository(database)
		service := moderation.NewService(repo)
		handler := moderation.NewHandler(service)
		handler.ApproveContent(w, r)
	})))
	mux.Handle("/moderation/reject", identity.ModeratorAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		repo := moderation.NewRepository(database)
		service := moderation.NewService(repo)
		handler := moderation.NewHandler(service)
		handler.RejectContent(w, r)
	})))

	// Moderator pending posts list (queries posts table directly by visibility)
	mux.Handle("/moderation/pending", identity.ModeratorAuthMiddleware(http.HandlerFunc(identity.GetPendingPostsHandler)))

	// Image uploads
	mux.HandleFunc("/upload/image", identity.UploadImageHandler)
	mux.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir("./uploads"))))

	// ── Identity vouching ─────────────────────────────────────────────────────
	// Admin: issue / revoke / list vouches
	mux.Handle("/admin/vouch", identity.AdminAuthMiddleware(http.HandlerFunc(identity.IssueVouchHandler)))
	mux.Handle("/admin/vouch/revoke", identity.AdminAuthMiddleware(http.HandlerFunc(identity.RevokeVouchHandler)))
	mux.Handle("/admin/vouches", identity.AdminAuthMiddleware(http.HandlerFunc(identity.ListIssuedVouchesHandler)))
	// Public: query vouches for any user (used by profile pages)
	mux.HandleFunc("/api/vouches", identity.GetVouchesHandler)
	// Federation: accept incoming vouch from a trusted peer
	mux.HandleFunc("/federation/vouch", identity.HandleIncomingVouch)

	// ── Zero-knowledge identity proofs ────────────────────────────────────────
	mux.Handle("/zkp/register-key", rlWrite(http.HandlerFunc(identity.ZKPRegisterKeyHandler)))
	mux.Handle("/zkp/challenge", rlWrite(http.HandlerFunc(identity.ZKPChallengeHandler)))
	mux.Handle("/zkp/prove", rlWrite(http.HandlerFunc(identity.ZKPProveHandler)))
	mux.Handle("/zkp/status", rlRead(http.HandlerFunc(identity.ZKPStatusHandler)))
	mux.Handle("/zkp/verify-token", rlRead(http.HandlerFunc(identity.ZKPVerifyTokenHandler)))

	// ── Server permission settings (admin-only) ───────────────────────────────
	mux.Handle("/admin/settings/permissions", identity.AdminAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			identity.GetPermissionsHandler(w, r)
		} else if r.Method == http.MethodPost {
			identity.UpdatePermissionsHandler(w, r)
		} else {
			identity.RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})))

	// ── Hashtag routes ────────────────────────────────────────────────────────
	mux.HandleFunc("/hashtags/trending", identity.GetTrendingHashtagsHandler)
	mux.HandleFunc("/hashtags/trending/global", identity.GetGlobalTrendingHashtagsHandler)
	mux.HandleFunc("/hashtags/posts", identity.GetHashtagPostsHandler)
	mux.HandleFunc("/hashtags/federated", identity.FederatedHashtagSearchHandler)

	// Start background sweeper for ephemeral (self-deleting) posts
	identity.StartEphemeralPostSweeper()

	handler := corsMiddleware(mux)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8082"
	}
	log.Printf("✅ Identity service listening on :%s", port)
	if err := http.ListenAndServe(":"+port, handler); err != nil {
		log.Fatalf("❌ Server failed: %v", err)
	}
}
