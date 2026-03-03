# FedNet-Go Backend — Workflow Documentation

> **Last Updated:** 2026-03-02  
> **Project:** `fedinet-go` — A federated social network backend written in Go.

---

## 1. Overview — How It Actually Works

`fedinet-go` is a **microservices-based federated social network backend**. It is split into three independent services, each running on its own port and communicating over HTTP.

```
┌────────────────────────────────────────────────────────────────────────────┐
│                          Client (Frontend / Mobile)                        │
└────────────────────┬──────────────────────┬────────────────────────────────┘
                     │                      │
            :8082    │             :8081    │               :8090
    ┌────────────────▼───┐   ┌─────────────▼──────┐   ┌────────────────────┐
    │  Identity Service  │   │ Federation Service  │   │ Moderation Service │
    │  (cmd/identity)    │   │  (cmd/federation)   │   │  (cmd/moderation)  │
    └────────────────────┘   └────────────────────┘   └────────────────────┘
              │                         │
              │                         │
    ┌─────────▼─────────┐    ┌──────────▼────────┐
    │   PostgreSQL DB    │    │  PostgreSQL DB     │
    │   (identity)       │    │  (federation)      │
    └────────────────────┘    └───────────────────┘
```

### Request Lifecycle

1. **Client** sends an HTTP request (e.g., `POST /login`).
2. Request passes through the **CORS middleware** (allows all origins, methods, and headers).
3. The **router (`http.ServeMux`)** matches the path to a handler function.
4. The handler function (a **controller**) decodes the JSON body / query params, calls internal **business logic functions** (e.g., `FollowUser`, `SendMessage`), and writes a JSON response.
5. For **federated actions** (e.g., messaging a user on another server), the identity service directly calls the remote server's federation endpoints over HTTP.

---

## 2. Services at a Glance

| Service | Entry Point | Port | Package | DB |
|---|---|---|---|---|
| **Identity** | `cmd/identity/main.go` | `:8082` | `internal/identity` | PostgreSQL |
| **Federation** | `cmd/federation/main.go` | `:8081` | `internal/federation` | PostgreSQL |
| **Moderation** | `cmd/moderation/main.go` | `:8090` | `internal/moderation` | PostgreSQL |

---

## 3. Identity Service — Port `:8082`

**Entry point:** `cmd/identity/main.go`

### Startup Sequence
1. Load `.env` file via `godotenv`.
2. Connect to PostgreSQL using `DATABASE_URL`.
3. Call `identity.SetDB(database)` to inject the DB connection into the package.
4. Call `identity.ApplyMigrations()` to run schema migrations.
5. Start background goroutines: `StartSessionKeyWorker()` and `StartInviteSweeper()`.
6. Register all HTTP routes on `http.ServeMux`.
7. Wrap the mux with CORS middleware.
8. Listen on `:8082`.

---

### 3.1 Identity Routes — Complete Reference

All handler functions live in the `internal/identity/` package.

#### Server Initialization & Status

| Route | Method | Controller | File | Description / Use |
|---|---|---|---|---|
| `/initialize` | POST | `InitializeHandler` | `init.go` | First-run setup: creates server identity (name, ID), generates Ed25519 keypair, creates super-admin account. Can only be called once. |
| `/status` | GET | `StatusHandler` | `init.go` | Returns whether the server has been initialized, along with server ID, name, and public key. |
| `/server-info` | GET | `GetServerInfoHandler` | `init.go` | Returns server_id, server_name, public_key, and version — used by remote servers during federation. |
| `/server/info` | GET | `GetServerInfoHandler` | `init.go` | Alias of `/server-info` for frontend compatibility. |
| `/health` | GET | *(inline lambda)* | `cmd/identity/main.go` | Simple health check returning `{"status":"ok"}`. Used by load balancers / monitoring. |

#### Authentication

| Route | Method | Controller | File | Description / Use |
|---|---|---|---|---|
| `/register` | POST | `RegisterHandler` | `handlers.go` | Register a new user. Requires a valid invite code. Hashes password, creates account + keypair, returns access token, refresh token, and a one-time recovery key. |
| `/login` | POST | `LoginHandler` | `login_handler.go` | Authenticate an existing user with username + password. Returns access token and refresh token. |
| `/recover` | POST | `RecoverAccountHandler` | `recovery_handler.go` | Reset account access using the recovery key issued at registration. Replaces the password. |

#### Social — Core

| Route | Method | Controller | File | Description / Use |
|---|---|---|---|---|
| `/feed` | GET | `GetFeedHandler` | `social_handlers.go` | Returns the authenticated user's home feed (posts from people they follow + their own posts), ordered by recency. Query: `user_id`, `limit`. |
| `/follow` | POST | `FollowHandler` | `handlers.go` | Follow another user. Body: `{ follower, followee }`. Handles both local and federated users. |
| `/unfollow` | POST | `UnfollowHandler` | `social_handlers.go` | Unfollow a user. Body: `{ follower, followee }`. |
| `/followers` | GET | `GetFollowersHandler` | `social_handlers.go` | List all users who follow a given user. Query: `user_id`, `limit`, `offset`. |
| `/following` | GET | `GetFollowingHandler` | `social_handlers.go` | List all users that a given user is following. Query: `user_id`, `limit`, `offset`. |
| `/followers/remove` | POST | `RemoveFollowerHandler` | `social_handlers.go` | Remove a specific follower from your followers list. Body: `{ user_id, follower_id }`. |
| `/follower/remove` | POST | `RemoveFollowerHandler` | `social_handlers.go` | Frontend alias for `/followers/remove`. |

#### Posts

| Route | Method | Controller | File | Description / Use |
|---|---|---|---|---|
| `/post/create` | POST | `CreatePostHandler` | `post_handlers.go` | Create a new post. Body: `{ user_id, content }`. Returns the created post object. |
| `/post/like` | POST | `ToggleLikeHandler` | `post_handlers.go` | Toggle like on a post (like if not liked, unlike if already liked). Body: `{ user_id, post_id }`. |
| `/post/repost` | POST | `ToggleRepostHandler` | `post_handlers.go` | Toggle repost on a post. Body: `{ user_id, post_id }`. |
| `/post/reply` | POST | `CreateReplyHandler` | `social_handlers.go` | Create a reply to a post. Body: `{ user_id, post_id, content }`. Frontend alias. |
| `/post/replies` | GET | `GetPostRepliesHandler` | `social_handlers.go` | Get all replies to a specific post. Query: `post_id`. Frontend alias. |
| `/reply` | POST | `CreateReplyHandler` | `social_handlers.go` | Alias for `/post/reply`. |
| `/replies` | GET | `GetPostRepliesHandler` | `social_handlers.go` | Alias for `/post/replies`. |
| `/posts/recent` | GET | `GetRecentPostsHandler` | `post_handlers.go` | Get the most recent posts across the server. Query: `limit` (default 20), `user_id` (optional, for has_liked/has_reposted flags). |
| `/posts/user` | GET | `GetUserPostsHandler` | `post_handlers.go` | Get all posts by a specific user. Query: `user_id` (required), `viewer_id` (optional), `limit`, `offset`. |
| `/posts/user/replies` | GET | `GetUserRepliesHandler` | `post_handlers.go` | Get all replies made by a specific user. Query: `user_id`, `limit`, `offset`. |
| `/posts/user/likes` | GET | `GetUserLikedPostsHandler` | `post_handlers.go` | Get all posts liked by a specific user. Query: `user_id`, `viewer_id` (optional), `limit`, `offset`. |

#### Users

| Route | Method | Controller | File | Description / Use |
|---|---|---|---|---|
| `/user/me` | GET | `MeHandler` | `post_handlers.go` | Get the authenticated user's own full profile (identity + profile). Query: `user_id`. |
| `/profile/update` | POST | `UpdateProfileHandler` | `handlers.go` | Update the user's profile fields (display name, bio, avatar, etc.). Body: `models.UpdateProfileRequest`. |
| `/user/search` | GET | `UserSearchHandler` | `handlers.go` | Look up a local user by their user_id. Returns identity + profile + follower/following counts + is_following flag. |
| `/users/suggested` | GET | `GetSuggestedUsersHandler` | `post_handlers.go` | Returns a list of suggested users to follow. Query: `limit` (default 20). |
| `/user` | GET | `GetPublicUserHandler` | `federated_search.go` | Get public profile of a user (local or remote). Used by remote servers to look up users on this server. |

#### Messages

| Route | Method | Controller | File | Description / Use |
|---|---|---|---|---|
| `/message` | POST | `MessageHandler` | `handlers.go` | Send a message to a user. Automatically routes to local delivery or federated delivery depending on recipient. Body: `{ from, to, content, image_url? }`. |
| `/messages` | GET | `GetConversationsHandler` | `social_handlers.go` | Get all conversations (inbox) for a user. Query: `user_id`. |
| `/messages/conversation` | GET | `GetConversationMessagesHandler` | `social_handlers.go` | Get all messages in a specific conversation. Query: `user_id`, `other_user_id`. |
| `/conversations` | GET | `GetConversationsHandler` | `social_handlers.go` | Alias for `/messages`. |
| `/conversation/messages` | GET | `GetConversationMessagesHandler` | `social_handlers.go` | Alias for `/messages/conversation`. |

#### Notifications

| Route | Method | Controller | File | Description / Use |
|---|---|---|---|---|
| `/notifications` | GET | `GetNotificationsHandler` | `notification_handlers.go` | Get the last 50 notifications for a user (likes, follows, replies, server updates, etc.). Query: `user_id`. |
| `/notifications/read` | POST | `MarkNotificationsReadHandler` | `notification_handlers.go` | Mark all notifications as read for a user. Body: `{ user_id }`. |

#### Blocking

| Route | Method | Controller | File | Description / Use |
|---|---|---|---|---|
| `/block` | POST | `BlockUserHandler` | `blocking.go` | Block another user. Prevents them from seeing or interacting with you. Body: `{ blocker_id, blocked_id }`. |
| `/unblock` | POST | `UnblockUserHandler` | `blocking.go` | Remove a block. Body: `{ blocker_id, blocked_id }`. |
| `/blocks` | GET | `GetBlocksHandler` | `blocking.go` | Get the list of users blocked by a given user. Query: `user_id`. |

#### Keys & Revocations

| Route | Method | Controller | File | Description / Use |
|---|---|---|---|---|
| `/revoke-key` | POST | `RevokeKeyHandler` | `revocation.go` | Revoke a user's public key (e.g., on device compromise). Announces revocation to trusted servers. |
| `/revocations` | GET | `GetRevocationsHandler` | `revocation.go` | Get the list of revoked public keys — polled by remote servers to maintain trust. |

#### Export & Import

| Route | Method | Controller | File | Description / Use |
|---|---|---|---|---|
| `/export` | GET | `ExportProfileHandler` | `export_import.go` | Export all user data (profile, posts, follows, etc.) as a JSON archive for data portability. Query: `user_id`. |
| `/import` | POST | `ImportProfileHandler` | `export_import.go` | Import a user data archive from another server (data portability / account migration). |

#### Federated Search & Cross-Server API

| Route | Method | Controller | File | Description / Use |
|---|---|---|---|---|
| `/search` | GET | `FederatedSearchHandler` | `federated_search.go` | Search for users across local and remote servers. Supports `alice@server_b` format. Query: `q`. |
| `/api/search` | GET | `FederatedSearchHandler` | `federated_search.go` | Same as `/search`, exposed as `/api/search` for frontend cross-server lookups. |
| `/api/users/` | GET | `GetPublicUserHandler` | `federated_search.go` | Remote server user lookup endpoint. Called by remote servers when they need public profile info. Path: `/api/users/{username}`. |
| `/api/posts/federated` | GET | `FederatedUserPostsHandler` | `federated_search.go` | Proxy endpoint: fetches posts from a remote federated user and returns them to the frontend. |

#### Federation Incoming (Called by Remote Servers)

| Route | Method | Controller | File | Description / Use |
|---|---|---|---|---|
| `/federation/handshake` | POST | `HandleHandshakeRequest` | `server_handshake.go` | Receive a handshake from a remote server. Verifies the server, stores it as trusted, exchanges public keys. |
| `/federation/handshake/ack` | POST | `HandleHandshakeAcknowledgment` | `server_handshake.go` | Receive acknowledgment from the remote server after a handshake was initiated from this server. |
| `/federation/profile` | POST | `HandleIncomingProfileUpdate` | `federated_profile_sync.go` | Receive a profile update pushed from a remote server (e.g., display name or avatar change). |
| `/federation/message` | POST | `HandleIncomingFederatedMessage` | `federated_messaging.go` | Receive a direct message from a user on a remote server and deliver it to the local recipient. |
| `/api/message/federated` | POST | `HandleIncomingFederatedMessage` | `federated_messaging.go` | Alias for `/federation/message` used by `DeliverFederatedMessage`. |
| `/federation/notification` | POST | `HandleIncomingFederatedNotification` | `federated_notifications.go` | Receive a notification (like, follow, etc.) from a user on a remote server. |
| `/api/notification/federated` | POST | `HandleIncomingFederatedNotification` | `federated_notifications.go` | Alias for `/federation/notification`. |
| `/federation/follow` | POST | `HandleIncomingFederatedFollow` | `federated_follow.go` | Receive a follow request from a user on a remote server. |

#### Server Trust Management

| Route | Method | Controller | File | Description / Use |
|---|---|---|---|---|
| `/trusted-servers` | GET | `GetTrustedServersHandler` | `server_trust_handlers.go` | List all servers that this server has established trust with. |
| `/trusted-servers/add` | POST | `AddTrustedServerHandler` | `server_trust_handlers.go` | Add a new trusted server by initiating a handshake with it (fetches its public key and endpoint automatically). |
| `/trusted-servers/update` | POST | `UpdateTrustedServerHandler` | `server_trust_handlers.go` | Update the details (endpoint, trust level) of an existing trusted server. |
| `/trusted-servers/remove` | POST | `RemoveTrustedServerHandler` | `server_trust_handlers.go` | Remove a server from the trusted list, terminating federation with it. |
| `/server-public-key` | GET | `GetServerPublicKeyHandler` | `server_trust_handlers.go` | Get the stored public key of a specific trusted server. Query: `server_id`. |

#### Admin Panel (JWT-Protected via `AdminAuthMiddleware`)

> All `/admin/*` routes (except `/admin/login`) require a valid JWT in the `Authorization: Bearer <token>` header.

| Route | Method | Controller | File | Description / Use |
|---|---|---|---|---|
| `/admin/login` | POST | `AdminLoginHandler` | `admin_handlers.go` | Admin login with username + password. Returns a signed JWT used for all other admin routes. |
| `/admin/config` | GET | `GetServerConfigHandler` | `admin_handlers.go` | Get the current server configuration (server name, updated_at, etc.). |
| `/admin/config/update` | POST | `UpdateServerConfigHandler` | `admin_handlers.go` | Update server configuration (e.g., rename the server). Notifies all users of the change. |
| `/admin/config/server` | GET/PUT | `GetServerConfigHandler` / `UpdateServerConfigHandler` | `admin_handlers.go` | Combined GET/PUT alias used by the admin frontend. |
| `/admin/test-db` | POST | `TestDatabaseHandler` | `admin_handlers.go` | Test connectivity to a new database connection string before migrating. |
| `/admin/config/test-db` | POST | `TestDatabaseHandler` | `admin_handlers.go` | Alias for `/admin/test-db`. |
| `/admin/migrate` | POST | `StartMigrationHandler` | `admin_handlers.go` | Start a live database migration to a new PostgreSQL instance. Runs asynchronously. |
| `/admin/migrate/start` | POST | `StartMigrationHandler` | `admin_handlers.go` | Alias for `/admin/migrate`. |
| `/admin/migrate/status` | GET | `GetMigrationStatusHandler` | `admin_handlers.go` | Poll the status of an in-progress database migration. Query: `id`. |
| `/admin/users` | GET | `GetAllUsersHandler` | `admin_handlers.go` | Get a list of all registered users on this server. |
| `/admin/users/list` | GET | `GetAllUsersHandler` | `admin_handlers.go` | Alias for `/admin/users`. |
| `/admin/stats` | GET | `GetStatsHandler` | `admin_handlers.go` | Get server statistics: total users, posts, activities, follows, DB status, uptime. |

#### Admin — Invite Management (JWT-Protected)

| Route | Method | Controller | File | Description / Use |
|---|---|---|---|---|
| `/admin/invites/list` | GET | `ListInvitesHandler` | `admin_handlers.go` | List all generated invite codes (active, used, and expired). |
| `/admin/invites/generate` | POST | `GenerateInviteHandler` | `admin_handlers.go` | Generate a new one-time invite code for a user to register. |
| `/admin/invites/revoke` | POST | `RevokeInviteHandler` | `admin_handlers.go` | Revoke an unused invite code so it cannot be used. Body: `{ invite_code }`. |
| `/admin/invites/qr` | GET | `GetInviteQRHandler` | `admin_handlers.go` | Returns a PNG QR code image for a given invite code. Query: `code`. |

#### Admin — Trusted Server Management (JWT-Protected)

| Route | Method | Controller | File | Description / Use |
|---|---|---|---|---|
| `/admin/trusted-servers/list` | GET | `GetTrustedServersHandler` | `server_trust_handlers.go` | Admin-panel view of all trusted servers. |
| `/admin/trusted-servers/add` | POST | `AddTrustedServerHandler` | `server_trust_handlers.go` | Add a trusted server from the admin panel (triggers handshake). |
| `/admin/trusted-servers/remove` | POST | `RemoveTrustedServerHandler` | `server_trust_handlers.go` | Remove a trusted server from the admin panel. |
| `/admin/trusted-servers/test` | POST | `TestTrustedServerConnectionHandler` | `server_trust_handlers.go` | Test connectivity to a trusted server from the backend (pings its `/health` endpoint). |

#### Image Uploads

| Route | Method | Controller | File | Description / Use |
|---|---|---|---|---|
| `/upload/image` | POST | `UploadImageHandler` | `upload_handler.go` | Upload a profile/post image. Returns the URL of the uploaded file stored under `./uploads/`. |
| `/uploads/` | GET | *(static file server)* | `cmd/identity/main.go` | Serve static uploaded files (images) from the `./uploads/` directory. |

---

### 3.2 Identity Controllers — File Reference

| File | Responsibility | Key Functions / Handlers |
|---|---|---|
| `handlers.go` | Core handlers: follow, message, register, profile update, user search | `FollowHandler`, `MessageHandler`, `UserSearchHandler`, `RegisterHandler`, `UpdateProfileHandler` |
| `login_handler.go` | Login logic, JWT generation | `LoginHandler` |
| `recovery_handler.go` | Account recovery flow | `RecoverAccountHandler` |
| `post_handlers.go` | Post CRUD, likes, reposts, user-specific posts, `MeHandler`, suggestions | `CreatePostHandler`, `ToggleLikeHandler`, `ToggleRepostHandler`, `MeHandler`, `GetUserPostsHandler`, `GetUserRepliesHandler`, `GetUserLikedPostsHandler`, `GetSuggestedUsersHandler`, `GetRecentPostsHandler` |
| `social_handlers.go` | Social graph: followers, following, conversations, replies, feed | `GetFeedHandler`, `GetFollowersHandler`, `UnfollowHandler`, `RemoveFollowerHandler`, `GetFollowingHandler`, `CreateReplyHandler`, `GetPostRepliesHandler`, `GetConversationsHandler`, `GetConversationMessagesHandler` |
| `notification_handlers.go` | Get and mark notifications | `GetNotificationsHandler`, `MarkNotificationsReadHandler` |
| `admin_handlers.go` | Admin auth + server config + DB migration + invite management | `AdminLoginHandler`, `AdminAuthMiddleware`, `GetServerConfigHandler`, `UpdateServerConfigHandler`, `TestDatabaseHandler`, `StartMigrationHandler`, `GetMigrationStatusHandler`, `GetAllUsersHandler`, `GetStatsHandler`, `ListInvitesHandler`, `GenerateInviteHandler`, `RevokeInviteHandler`, `GetInviteQRHandler` |
| `admin.go` | Admin business logic & data models (`ServerConfig`, `ServerStats`, `MigrationStatus`) | `GetServerConfig`, `UpdateServerName`, `GetServerStats`, `GetAllUsers`, `MigrateDatabase`, `GetMigrationStatus`, `NotifyAllUsers` |
| `server_trust_handlers.go` | Manage trusted servers (allow/block/test federation) | `GetTrustedServersHandler`, `AddTrustedServerHandler`, `UpdateTrustedServerHandler`, `RemoveTrustedServerHandler`, `GetServerPublicKeyHandler`, `TestTrustedServerConnectionHandler` |
| `server_handshake.go` | Server-to-server handshake protocol | `HandleHandshakeRequest`, `HandleHandshakeAcknowledgment` |
| `federated_search.go` | Cross-server user and post lookup | `FederatedSearchHandler`, `GetPublicUserHandler`, `FederatedUserPostsHandler` |
| `federated_messaging.go` | Send/receive messages across servers | `HandleIncomingFederatedMessage`, `DeliverFederatedMessage`, `StoreSentFederatedMessage` |
| `federated_notifications.go` | Push notifications across servers | `HandleIncomingFederatedNotification`, `DeliverFederatedNotification` |
| `federated_follow.go` | Cross-server follow requests | `HandleIncomingFederatedFollow` |
| `federated_profile_sync.go` | Sync profile changes to/from remote servers | `HandleIncomingProfileUpdate` |
| `blocking.go` | User block/unblock at identity level | `BlockUserHandler`, `UnblockUserHandler`, `GetBlocksHandler` |
| `revocation.go` | Public key revocation and announcement | `RevokeKeyHandler`, `GetRevocationsHandler` |
| `export_import.go` | Data portability — export/import user data | `ExportProfileHandler`, `ImportProfileHandler` |
| `upload_handler.go` | Profile image uploads to local disk | `UploadImageHandler` |
| `invites.go` | Invite code business logic | `GenerateInvite`, `UseInvite`, `ListInvites`, `RevokeInvite`, `StartInviteSweeper` |
| `init.go` | First-run server initialization and status | `InitializeHandler`, `StatusHandler`, `GetServerInfoHandler`, `InitializeServer`, `CheckInitializationStatus` |
| `actions.go` | Core business logic: follow/unfollow, post create/like/repost, feed | `FollowUser`, `UnfollowUser`, `CreatePost`, `ToggleLike`, `ToggleRepost`, `GetFeed`, `GetPosts` |
| `social_actions.go` | Reply and conversation logic | `CreateReply`, `GetReplies`, `SendMessage`, `GetConversations`, `GetConversationMessages` |
| `auth.go` | Auth utilities: password hashing, JWT generation | `HashPassword`, `CheckPassword`, `GenerateTokenPair`, `GenerateSessionKey` |
| `session_keys.go` | Session key rotation background worker | `StartSessionKeyWorker`, `GenerateSessionKey` |
| `migrations.go` | Schema migration runner | `ApplyMigrations` |
| `account_with_client_key.go` | Account creation with client's public key | `CreateAccountWithClientKey` |
| `activitystreams.go` | ActivityStreams/ActivityPub data models | `Activity`, `Actor`, `Object` type definitions |
| `cache.go` | In-memory caching layer | Cache get/set helpers for user data |
| `utils.go` | Utility functions for HTTP responses and user ID formatting | `RespondWithJSON`, `RespondWithError`, `ToInternalID`, `ToExternalID`, `IsFederatedUser` |
| `validation.go` | Input validation helpers | Field validators (nonempty, length checks) |
| `email.go` | Email sending utilities | `SendEmail` |
| `errors.go` | Custom error types | Error constants |
| `db.go` | DB connection holder | `SetDB` (injects `*sql.DB` into the package) |

---

## 4. Federation Service — Port `:8081`

**Entry point:** `cmd/federation/main.go`

### Startup Sequence
1. Call `federation.InitDB()` — connects to PostgreSQL, runs `migrations.sql`.
2. Register all HTTP routes on `http.ServeMux`.
3. Wrap the mux with CORS middleware (also allows `Signature`, `Date`, `Digest` headers for signed requests).
4. Listen on `:8081`.

### Routes & Controllers

All handler functions live in `internal/federation/handlers.go`.

| Route | Method | Controller | File | Description / Use |
|---|---|---|---|---|
| `/federation/inbox` | POST | `InboxHandler` | `handlers.go` | Receive an ActivityStreams activity (follow, like, post, etc.) from a remote server. Validates and stores the activity. |
| `/federation/outbox` | GET | `OutboxHandler` | `handlers.go` | Return stored outbound activities for this server — used by remote servers to catch up on missed events. |
| `/federation/send` | POST | `SendActivityHandler` | `handlers.go` | Sign and deliver an activity to a specific remote server's inbox. Body: `{ target_server, activity }`. |
| `/federation/acknowledgment` | POST | `AcknowledgmentHandler` | `handlers.go` | Receive acknowledgment from a remote server that a sent activity was processed. |
| `/federation/capabilities` | GET | `CapabilitiesHandler` | `handlers.go` | Advertise what capabilities this server supports (e.g., supported activity types, protocol version). |
| `/federation/capabilities/discover` | GET/POST | `DiscoverCapabilitiesHandler` | `handlers.go` | Query a remote server's capabilities. Body/Query: `{ server_url }`. |
| `/federation/discover` | GET/POST | `DiscoverCapabilitiesHandler` | `handlers.go` | Alias for `/federation/capabilities/discover`. |
| `/federation/health` | GET | `HealthHandler` | `handlers.go` | Returns health status: DB connectivity, version, uptime. Used by remote servers to verify this server is reachable. |
| `/federation/blocked` | GET/POST/DELETE | `BlockedServersHandler` | `handlers.go` | GET: list blocked servers. POST: block a remote server. DELETE: unblock a server. |
| `/federation/mode` | GET/POST | `FederationModeHandler` | `handlers.go` | GET: return current federation mode. POST: set federation mode (`open`, `restricted`, `closed`). |
| `/federation/rate-limits` | GET/POST | `RateLimitsHandler` | `handlers.go` | GET: view per-server rate limits. POST: configure rate limits for a specific remote server. |
| `/federation/handshake` | POST | `HandshakeHandler` | `handlers.go` | Receive an initial handshake from a remote server — verifies, stores trust, responds with this server's public key. |
| `/federation/initiate-handshake` | POST | `InitiateHandshakeHandler` | `handlers.go` | Proactively initiate a handshake with a remote server (fetches its public key, sends this server's info). |
| `/federation/handshake/initiate` | POST | `InitiateHandshakeHandler` | `handlers.go` | Alias for `/federation/initiate-handshake`. |
| `/federation/signed/inbox` | POST | `VerifySignatureMiddleware(InboxHandler)` | `handlers.go` | Same as `/federation/inbox` but requires a valid HTTP Signature header — used for authenticated federation. |
| `/federation/lookup` | GET | `VerifySignatureMiddleware(HandleFederatedLookup)` | `handlers.go` + `lookup.go` | Signature-verified lookup of a user or resource on this server. Called by remote servers to resolve user references. |

### Key Middleware

| Middleware | File | Purpose |
|---|---|---|
| `VerifySignatureMiddleware` | `handlers.go` | Parses the `Signature` HTTP header, fetches the sender's public key from `/server-public-key`, and verifies the request signature using Ed25519. |
| `corsMiddleware` | `cmd/federation/main.go` | Adds CORS headers (including `Signature`, `Date`, `Digest`) to every response. |

### Supporting Files

| File | Purpose |
|---|---|
| `handlers.go` | All route handlers + `VerifySignatureMiddleware` + internal helpers (`sendSuccess`, `sendError`) |
| `actions.go` | Business logic: store/send activities, manage blocked servers, track rate limits |
| `lookup.go` | Federated user/resource lookup logic used by `HandleFederatedLookup` |
| `db.go` | `InitDB()` — opens the DB and runs `migrations.sql` |
| `sync.go` | Synchronization helpers (e.g., push outbox to remote inboxes) |
| `verification.go` | Signature verification utility functions |

---

## 5. Moderation Service — Port `:8090`

**Entry point:** `cmd/moderation/main.go`

### Startup Sequence
1. Load `.env` via `godotenv`.
2. Call `moderation.InitDB()` to connect to PostgreSQL.
3. Call `moderation.ApplyMigrations(db)` to run schema setup from `migrations.sql`.
4. Construct the service stack: `NewRepository(db)` → `NewService(repo)` → `NewHandler(service)`.
5. Register routes via `moderation.RegisterRoutes(mux, handler)`.
6. Listen on `:8090`.

### Architecture Pattern

The moderation service follows a **clean layered architecture**:

```
HTTP Request
    │
    ▼
Handler (handlers.go)       ← Decode request, validate, write JSON response
    │
    ▼
Service (service.go)        ← Business logic and rules
    │
    ▼
Repository (repository.go)  ← Raw SQL queries against PostgreSQL
```

### Routes & Controllers

Routes are registered in `internal/moderation/routes.go`.

| Route | Method | Controller | File | Description / Use |
|---|---|---|---|---|
| `/reports` | POST | `Handler.SubmitReport` | `handlers.go` | Submit a moderation report against a user or post. Body: `{ reporter_id, target_ref, target_server, reason }`. Creates a record with `pending` status. |
| `/moderation/reports` | GET | `Handler.ListPendingReports` | `handlers.go` | List all moderation reports with `pending` status — used by moderators to see open cases. |
| `/moderation/resolve` | POST | `Handler.ResolveReport` | `handlers.go` | Mark a report as resolved. Query: `id` (report_id), `resolved_by` (moderator user_id). |
| `/servers/block` | POST | `Handler.BlockServer` | `handlers.go` | Admin action: block an entire remote server domain. Body: `{ domain, reason, admin_id }`. |
| `/users/block` | POST | `Handler.BlockUser` | `handlers.go` | Block a specific user. Body: `{ blocker_user_id, blocked_user_id, reason }`. |
| `/users/unblock` | POST | `Handler.UnblockUser` | `handlers.go` | Remove a block on a user. Body: `{ blocker_user_id, blocked_user_id }`. |
| `/users/blocked` | GET | `Handler.ListBlockedUsers` | `handlers.go` | Get all users blocked by a given user. Query: `blocker_user_id`. |
| `/users/block/check` | GET | `Handler.CheckUserBlock` | `handlers.go` | Check if user A has blocked user B. Query: `blocker_user_id`, `blocked_user_id`. Returns `{ is_blocked: true/false }`. |

### Supporting Files

| File | Purpose |
|---|---|
| `routes.go` | Registers all moderation routes on the mux via `RegisterRoutes(mux, handler)` |
| `handlers.go` | HTTP handlers: decode → call service → write response. Holds the `Handler` struct. |
| `service.go` | Business rules: `SubmitReport`, `ResolveReport`, `BlockUser`, `UnblockUser`, `BlockServer`, `IsUserBlocked`, `ListBlockedUsers` |
| `repository.go` | All SQL queries for moderation data (reports table, user_blocks table, server_blocks table) |
| `db.go` | `InitDB()` — opens DB connection and applies migrations |
| `migrations.sql` | SQL DDL: creates `reports`, `user_blocks`, and `server_blocks` tables |

---

## 6. Shared Packages (`pkg/`)

| Package | Path | Purpose |
|---|---|---|
| `models` | `pkg/models/` | Shared data models used across services: `UserDocument`, `UpdateProfileRequest`, `Activity`, etc. |
| `crypto` | `pkg/crypto/` | Cryptographic utilities: Ed25519 key generation, HTTP request signing, signature verification |
| `protocol` | `pkg/protocol/` | Federated protocol type definitions and constants (activity types, protocol version strings) |

---

## 7. Request Flow Examples

### 7.1 User Registration
```
POST /register  (Identity :8082)
  → RegisterHandler (handlers.go)
    → validate invite code  (invites.go → UseInvite)
    → HashPassword (auth.go)
    → CreateAccountWithClientKey (account_with_client_key.go)
    → GenerateSessionKey (session_keys.go)
    → GenerateTokenPair (auth.go)
  ← 201 { user_id, home_server, recovery_key, session_key, access_token, refresh_token }
```

### 7.2 Sending a Federated Message
```
POST /message  (Identity :8082)
  → MessageHandler (handlers.go)
    → IsFederatedUser(req.To)  — checks if recipient is on another server
    → [federated] DeliverFederatedMessage (federated_messaging.go)
         → HTTP POST to remote server: POST /api/message/federated
         → StoreSentFederatedMessage (stores copy for sender)
    → [local] SendMessage (social_actions.go)
  ← 200 { message: "message sent" }
```

### 7.3 Admin Login + Protected Route
```
POST /admin/login  (Identity :8082)
  → AdminLoginHandler (admin_handlers.go)
    → ValidateAdminCredentials (admin.go) — bcrypt compare
    → GenerateJWT (admin.go) — signs JWT with secret
  ← 200 { token: "..." }

GET /admin/stats  (Identity :8082)  [Authorization: Bearer <token>]
  → AdminAuthMiddleware (admin_handlers.go)
    → ValidateJWT (admin.go) — verifies JWT
    → GetStatsHandler (admin_handlers.go)
      → GetServerStats (admin.go) → SQL COUNT queries
  ← 200 { total_users, total_posts, total_activities, ... }
```

### 7.4 Federation Handshake Between Two Servers
```
POST /federation/initiate-handshake  (Federation :8081 — called by admin)
  → InitiateHandshakeHandler (handlers.go)
    → HTTP GET remote /server-info → fetch remote public key
    → HTTP POST remote /federation/handshake → send this server's info
    → Store remote server as trusted in DB
  ← 200 { status: "handshake initiated" }

POST /federation/handshake  (Federation :8081 — called by remote server)
  → HandshakeHandler (handlers.go)
    → Verify incoming payload
    → Store remote server as trusted
  ← 200 { status: "acknowledged" }
```

### 7.5 Submit a Moderation Report
```
POST /reports  (Moderation :8090)
  → Handler.SubmitReport (handlers.go)
    → Service.SubmitReport (service.go)
      → Repository.CreateReport (repository.go) → SQL INSERT into reports
  ← 201 Created

GET /moderation/reports  (Moderation :8090)
  → Handler.ListPendingReports (handlers.go)
    → Service.ListPendingReports (service.go)
      → Repository.ListPending (repository.go) → SQL SELECT WHERE status='pending'
  ← 200 [ { id, reporter_id, target_ref, reason, ... } ]
```

---

## 8. Background Workers

These goroutines start automatically at service boot inside the Identity service:

| Worker | Function | File | Description |
|---|---|---|---|
| Session Key Worker | `StartSessionKeyWorker()` | `session_keys.go` | Runs every N minutes; rotates old session keys and cleans up expired ones from the DB. |
| Invite Sweeper | `StartInviteSweeper()` | `invites.go` | Runs periodically; deletes expired (timed-out, unused) invite codes from the database. |

---

## 9. Middleware Summary

| Middleware | Service | File | Applied To | What It Does |
|---|---|---|---|---|
| `corsMiddleware` | Identity | `cmd/identity/main.go` | All routes | Sets `Access-Control-Allow-*` headers so browsers can call the API from any origin. |
| `corsMiddleware` | Federation | `cmd/federation/main.go` | All routes | Same as above, plus also allows `Signature`, `Date`, `Digest` headers for signed federation requests. |
| `AdminAuthMiddleware` | Identity | `internal/identity/admin_handlers.go` | All `/admin/*` except `/admin/login` | Reads `Authorization: Bearer <token>` header, validates the JWT, rejects with 401 if invalid. |
| `VerifySignatureMiddleware` | Federation | `internal/federation/handlers.go` | `/federation/signed/inbox`, `/federation/lookup` | Parses `Signature` header, fetches the remote server's public key, and verifies the HTTP request was signed by a trusted server. |

---

## 10. Database Migrations

Each service manages its own schema independently:

| Service | Migration Location | How It Runs |
|---|---|---|
| Identity | `internal/identity/migrations.go` + `migrations/` dir | `identity.ApplyMigrations()` called at startup — applies all pending SQL migrations in order |
| Federation | `internal/federation/migrations.sql` | `federation.InitDB()` executes the SQL directly at startup |
| Moderation | `internal/moderation/migrations.sql` | `moderation.ApplyMigrations(db)` executes the SQL at startup |
