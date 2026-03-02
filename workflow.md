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

### 3.1 Identity Routes & Controllers

All handler functions live in the `internal/identity/` package. The table below shows every route, its HTTP method, the controller function, and the source file.

#### Server Initialization & Status

| Route | Method | Controller | File |
|---|---|---|---|
| `/initialize` | POST | `InitializeHandler` | `init.go` |
| `/status` | GET | `StatusHandler` | `init.go` |
| `/server-info` | GET | `GetServerInfoHandler` | `init.go` |
| `/server/info` | GET | `GetServerInfoHandler` *(alias)* | `init.go` |
| `/health` | GET | *(inline lambda)* | `cmd/identity/main.go` |

#### Authentication

| Route | Method | Controller | File |
|---|---|---|---|
| `/register` | POST | `RegisterHandler` | `handlers.go` |
| `/login` | POST | `LoginHandler` | `login_handler.go` |
| `/recover` | POST | `RecoverAccountHandler` | `recovery_handler.go` |

#### Social — Core

| Route | Method | Controller | File |
|---|---|---|---|
| `/feed` | GET | `GetFeedHandler` | `social_handlers.go` |
| `/follow` | POST | `FollowHandler` | `handlers.go` |
| `/unfollow` | POST | `UnfollowHandler` | `social_handlers.go` |
| `/followers` | GET | `GetFollowersHandler` | `social_handlers.go` |
| `/following` | GET | `GetFollowingHandler` | `social_handlers.go` |
| `/followers/remove` | POST | `RemoveFollowerHandler` | `social_handlers.go` |
| `/follower/remove` | POST | `RemoveFollowerHandler` *(alias)* | `social_handlers.go` |

#### Posts

| Route | Method | Controller | File |
|---|---|---|---|
| `/post/create` | POST | `CreatePostHandler` | `post_handlers.go` |
| `/post/like` | POST | `ToggleLikeHandler` | `post_handlers.go` |
| `/post/repost` | POST | `ToggleRepostHandler` | `post_handlers.go` |
| `/post/reply` | POST | `CreateReplyHandler` | `social_handlers.go` |
| `/post/replies` | GET | `GetPostRepliesHandler` | `social_handlers.go` |
| `/reply` | POST | `CreateReplyHandler` *(alias)* | `social_handlers.go` |
| `/replies` | GET | `GetPostRepliesHandler` *(alias)* | `social_handlers.go` |
| `/posts/recent` | GET | `GetRecentPostsHandler` | `post_handlers.go` |
| `/posts/user` | GET | `GetUserPostsHandler` | `post_handlers.go` |
| `/posts/user/replies` | GET | `GetUserRepliesHandler` | `post_handlers.go` |
| `/posts/user/likes` | GET | `GetUserLikedPostsHandler` | `post_handlers.go` |

#### Users

| Route | Method | Controller | File |
|---|---|---|---|
| `/user/me` | GET | `MeHandler` | `post_handlers.go` |
| `/profile/update` | POST | `UpdateProfileHandler` | `handlers.go` |
| `/user/search` | GET | `UserSearchHandler` | `handlers.go` |
| `/users/suggested` | GET | `GetSuggestedUsersHandler` | `post_handlers.go` |
| `/user` | GET | `GetPublicUserHandler` | `federated_search.go` |

#### Messages

| Route | Method | Controller | File |
|---|---|---|---|
| `/message` | POST | `MessageHandler` | `handlers.go` |
| `/messages` | GET | `GetConversationsHandler` | `social_handlers.go` |
| `/messages/conversation` | GET | `GetConversationMessagesHandler` | `social_handlers.go` |
| `/conversations` | GET | `GetConversationsHandler` *(alias)* | `social_handlers.go` |
| `/conversation/messages` | GET | `GetConversationMessagesHandler` *(alias)* | `social_handlers.go` |

#### Notifications

| Route | Method | Controller | File |
|---|---|---|---|
| `/notifications` | GET | `GetNotificationsHandler` | `notification_handlers.go` |
| `/notifications/read` | POST | `MarkNotificationsReadHandler` | `notification_handlers.go` |

#### Blocking

| Route | Method | Controller | File |
|---|---|---|---|
| `/block` | POST | `BlockUserHandler` | `blocking.go` |
| `/unblock` | POST | `UnblockUserHandler` | `blocking.go` |
| `/blocks` | GET | `GetBlocksHandler` | `blocking.go` |

#### Keys & Revocations

| Route | Method | Controller | File |
|---|---|---|---|
| `/revoke-key` | POST | `RevokeKeyHandler` | `revocation.go` |
| `/revocations` | GET | `GetRevocationsHandler` | `revocation.go` |

#### Export & Import

| Route | Method | Controller | File |
|---|---|---|---|
| `/export` | GET | `ExportProfileHandler` | `export_import.go` |
| `/import` | POST | `ImportProfileHandler` | `export_import.go` |

#### Federated Search & Cross-Server API

| Route | Method | Controller | File |
|---|---|---|---|
| `/search` | GET | `FederatedSearchHandler` | `federated_search.go` |
| `/api/search` | GET | `FederatedSearchHandler` | `federated_search.go` |
| `/api/users/` | GET | `GetPublicUserHandler` | `federated_search.go` |
| `/api/posts/federated` | GET | `FederatedUserPostsHandler` | `federated_search.go` |

#### Federation Incoming (Called by Remote Servers)

| Route | Method | Controller | File |
|---|---|---|---|
| `/federation/handshake` | POST | `HandleHandshakeRequest` | `server_handshake.go` |
| `/federation/handshake/ack` | POST | `HandleHandshakeAcknowledgment` | `server_handshake.go` |
| `/federation/profile` | POST | `HandleIncomingProfileUpdate` | `federated_profile_sync.go` |
| `/federation/message` | POST | `HandleIncomingFederatedMessage` | `federated_messaging.go` |
| `/api/message/federated` | POST | `HandleIncomingFederatedMessage` *(alias)* | `federated_messaging.go` |
| `/federation/notification` | POST | `HandleIncomingFederatedNotification` | `federated_notifications.go` |
| `/api/notification/federated` | POST | `HandleIncomingFederatedNotification` *(alias)* | `federated_notifications.go` |
| `/federation/follow` | POST | `HandleIncomingFederatedFollow` | `federated_follow.go` |

#### Server Trust Management

| Route | Method | Controller | File |
|---|---|---|---|
| `/trusted-servers` | GET | `GetTrustedServersHandler` | `server_trust_handlers.go` |
| `/trusted-servers/add` | POST | `AddTrustedServerHandler` | `server_trust_handlers.go` |
| `/trusted-servers/update` | POST | `UpdateTrustedServerHandler` | `server_trust_handlers.go` |
| `/trusted-servers/remove` | POST | `RemoveTrustedServerHandler` | `server_trust_handlers.go` |
| `/server-public-key` | GET | `GetServerPublicKeyHandler` | `server_trust_handlers.go` |

#### Admin Panel (JWT-Protected via `AdminAuthMiddleware`)

| Route | Method | Controller | File |
|---|---|---|---|
| `/admin/login` | POST | `AdminLoginHandler` | `admin_handlers.go` |
| `/admin/config` | GET | `GetServerConfigHandler` | `admin_handlers.go` |
| `/admin/config/update` | POST | `UpdateServerConfigHandler` | `admin_handlers.go` |
| `/admin/config/server` | GET/PUT | `GetServerConfigHandler` / `UpdateServerConfigHandler` | `admin_handlers.go` |
| `/admin/test-db` | POST | `TestDatabaseHandler` | `admin_handlers.go` |
| `/admin/config/test-db` | POST | `TestDatabaseHandler` *(alias)* | `admin_handlers.go` |
| `/admin/migrate` | POST | `StartMigrationHandler` | `admin_handlers.go` |
| `/admin/migrate/start` | POST | `StartMigrationHandler` *(alias)* | `admin_handlers.go` |
| `/admin/migrate/status` | GET | `GetMigrationStatusHandler` | `admin_handlers.go` |
| `/admin/users` | GET | `GetAllUsersHandler` | `admin_handlers.go` |
| `/admin/users/list` | GET | `GetAllUsersHandler` *(alias)* | `admin_handlers.go` |
| `/admin/stats` | GET | `GetStatsHandler` | `admin_handlers.go` |

#### Admin — Invite Management (JWT-Protected)

| Route | Method | Controller | File |
|---|---|---|---|
| `/admin/invites/list` | GET | `ListInvitesHandler` | `admin_handlers.go` |
| `/admin/invites/generate` | POST | `GenerateInviteHandler` | `admin_handlers.go` |
| `/admin/invites/revoke` | POST | `RevokeInviteHandler` | `admin_handlers.go` |
| `/admin/invites/qr` | GET | `GetInviteQRHandler` | `admin_handlers.go` |

#### Admin — Trusted Server Management (JWT-Protected)

| Route | Method | Controller | File |
|---|---|---|---|
| `/admin/trusted-servers/list` | GET | `GetTrustedServersHandler` | `server_trust_handlers.go` |
| `/admin/trusted-servers/add` | POST | `AddTrustedServerHandler` | `server_trust_handlers.go` |
| `/admin/trusted-servers/remove` | POST | `RemoveTrustedServerHandler` | `server_trust_handlers.go` |
| `/admin/trusted-servers/test` | POST | `TestTrustedServerConnectionHandler` | `server_trust_handlers.go` |

#### Image Uploads

| Route | Method | Controller | File |
|---|---|---|---|
| `/upload/image` | POST | `UploadImageHandler` | `upload_handler.go` |
| `/uploads/` | GET | *(static file server)* | `cmd/identity/main.go` |

---

### 3.2 Identity Controllers — File Reference

| File | Responsibility | Key Controllers / Functions |
|---|---|---|
| `handlers.go` | Core handlers: follow, message, register, profile update, user search | `FollowHandler`, `MessageHandler`, `UserSearchHandler`, `RegisterHandler`, `UpdateProfileHandler` |
| `login_handler.go` | Login logic, JWT generation | `LoginHandler` |
| `recovery_handler.go` | Account recovery flow | `RecoverAccountHandler` |
| `post_handlers.go` | Post CRUD, likes, reposts, feed, user-specific posts, `MeHandler` | `CreatePostHandler`, `ToggleLikeHandler`, `ToggleRepostHandler`, `GetFeedHandler` (via `social_handlers`), `MeHandler`, `GetUserPostsHandler`, `GetUserRepliesHandler`, `GetUserLikedPostsHandler`, `GetSuggestedUsersHandler` |
| `social_handlers.go` | Social graph: followers, following, conversations, replies | `GetFeedHandler`, `GetFollowersHandler`, `UnfollowHandler`, `RemoveFollowerHandler`, `GetFollowingHandler`, `CreateReplyHandler`, `GetPostRepliesHandler`, `GetConversationsHandler`, `GetConversationMessagesHandler` |
| `notification_handlers.go` | Get and mark notifications | `GetNotificationsHandler`, `MarkNotificationsReadHandler` |
| `admin_handlers.go` | Admin auth + server config + DB migration + invite management | `AdminLoginHandler`, `AdminAuthMiddleware`, `GetServerConfigHandler`, `UpdateServerConfigHandler`, `TestDatabaseHandler`, `StartMigrationHandler`, `GetMigrationStatusHandler`, `GetAllUsersHandler`, `GetStatsHandler`, `ListInvitesHandler`, `GenerateInviteHandler`, `RevokeInviteHandler`, `GetInviteQRHandler` |
| `admin.go` | Admin business logic & data models (`ServerConfig`, `ServerStats`, `MigrationStatus`) | `GetServerConfig`, `UpdateServerName`, `GetServerStats`, `GetAllUsers`, `MigrateDatabase`, `GetMigrationStatus` |
| `server_trust_handlers.go` | Manage trusted servers (allow/block federation) | `GetTrustedServersHandler`, `AddTrustedServerHandler`, `UpdateTrustedServerHandler`, `RemoveTrustedServerHandler`, `GetServerPublicKeyHandler`, `TestTrustedServerConnectionHandler` |
| `server_handshake.go` | Server-to-server handshake protocol | `HandleHandshakeRequest`, `HandleHandshakeAcknowledgment` |
| `federated_search.go` | Cross-server user and post lookup | `FederatedSearchHandler`, `GetPublicUserHandler`, `FederatedUserPostsHandler` |
| `federated_messaging.go` | Send/receive messages across servers | `HandleIncomingFederatedMessage`, `DeliverFederatedMessage`, `StoreSentFederatedMessage` |
| `federated_notifications.go` | Push notifications across servers | `HandleIncomingFederatedNotification`, `DeliverFederatedNotification` |
| `federated_follow.go` | Cross-server follow requests | `HandleIncomingFederatedFollow` |
| `federated_profile_sync.go` | Sync profile changes to remote servers | `HandleIncomingProfileUpdate` |
| `blocking.go` | User block/unblock at identity level | `BlockUserHandler`, `UnblockUserHandler`, `GetBlocksHandler` |
| `revocation.go` | Public key revocation | `RevokeKeyHandler`, `GetRevocationsHandler` |
| `export_import.go` | Data portability — export/import user data | `ExportProfileHandler`, `ImportProfileHandler` |
| `upload_handler.go` | Profile image uploads | `UploadImageHandler` |
| `invites.go` | Invite code business logic | `GenerateInvite`, `UseInvite`, `ListInvites`, `RevokeInvite` |
| `init.go` | Server initialization and status reporting | `InitializeHandler`, `StatusHandler`, `GetServerInfoHandler` |
| `actions.go` | Core business logic: follow/unfollow, post create/like/repost | `FollowUser`, `UnfollowUser`, `CreatePost`, `ToggleLike`, `ToggleRepost`, `GetFeed` |
| `social_actions.go` | Reply and conversation logic | `CreateReply`, `GetReplies`, `SendMessage`, `GetConversations` |
| `auth.go` | Auth utilities: password hashing, JWT token generation | `HashPassword`, `CheckPassword`, `GenerateTokenPair`, `GenerateSessionKey` |
| `session_keys.go` | Session key rotation worker | `StartSessionKeyWorker` |
| `migrations.go` | Schema migration runner | `ApplyMigrations` |
| `account_with_client_key.go` | Account creation with client public key | `CreateAccountWithClientKey` |
| `activitystreams.go` | ActivityStreams/ActivityPub data models | Type definitions for `Activity`, `Actor` etc. |
| `cache.go` | In-memory caching layer | Cache helpers |
| `utils.go` | Utility functions: `RespondWithJSON`, `RespondWithError`, `ToInternalID`, `ToExternalID` | `RespondWithJSON`, `RespondWithError` |
| `validation.go` | Input validation helpers | Field validators |
| `email.go` | Email sending utilities | `SendEmail` |
| `errors.go` | Custom error types | Error constants |
| `db.go` | DB connection holder (`var db *sql.DB`) | `SetDB` |

---

## 4. Federation Service — Port `:8081`

**Entry point:** `cmd/federation/main.go`

### Startup Sequence
1. Call `federation.InitDB()` to connect to the federation's PostgreSQL database.
2. Register all HTTP routes on `http.ServeMux`.
3. Wrap the mux with CORS middleware (includes `Signature`, `Date`, `Digest` extra headers).
4. Listen on `:8081`.

### Routes & Controllers

All handler functions live in `internal/federation/handlers.go`.

| Route | Method | Controller | Notes |
|---|---|---|---|
| `/federation/inbox` | POST | `InboxHandler` | Receives incoming activities from remote servers |
| `/federation/outbox` | GET | `OutboxHandler` | Returns stored outbound activities |
| `/federation/send` | POST | `SendActivityHandler` | Sends a signed activity to a remote server |
| `/federation/acknowledgment` | POST | `AcknowledgmentHandler` | Handles ACK from remote servers |
| `/federation/capabilities` | GET | `CapabilitiesHandler` | Returns server capability advertisement |
| `/federation/capabilities/discover` | GET/POST | `DiscoverCapabilitiesHandler` | Probe remote server capabilities |
| `/federation/discover` | GET/POST | `DiscoverCapabilitiesHandler` *(alias)* | — |
| `/federation/health` | GET | `HealthHandler` | Returns service health + DB status |
| `/federation/blocked` | GET/POST/DELETE | `BlockedServersHandler` | Get/block/unblock remote servers |
| `/federation/mode` | GET/POST | `FederationModeHandler` | Get or set federation mode (open/restricted/closed) |
| `/federation/rate-limits` | GET/POST | `RateLimitsHandler` | View/set per-server rate limits |
| `/federation/handshake` | POST | `HandshakeHandler` | Receive handshake from a remote server |
| `/federation/initiate-handshake` | POST | `InitiateHandshakeHandler` | Proactively initiate handshake with a remote server |
| `/federation/handshake/initiate` | POST | `InitiateHandshakeHandler` *(alias)* | — |
| `/federation/signed/inbox` | POST | `VerifySignatureMiddleware(InboxHandler)` | Signature-verified inbox |
| `/federation/lookup` | GET | `VerifySignatureMiddleware(HandleFederatedLookup)` | Signature-verified user/resource lookup |

### Key Middleware

| Middleware | File | Purpose |
|---|---|---|
| `VerifySignatureMiddleware` | `handlers.go` | Parses `Signature` header, fetches remote server public key, verifies HTTP signature |
| `corsMiddleware` | `cmd/federation/main.go` | Adds CORS headers on every response |

### Supporting Files

| File | Purpose |
|---|---|
| `handlers.go` | All route handlers + signature verification middleware |
| `actions.go` | Business logic: store/send activities, manage blocked servers, rate limiting |
| `lookup.go` | Federated user lookup logic |
| `db.go` | DB initializer (`InitDB`) |
| `sync.go` | Synchronization helpers between servers |
| `verification.go` | Signature verification utilities |

---

## 5. Moderation Service — Port `:8090`

**Entry point:** `cmd/moderation/main.go`

### Startup Sequence
1. Load `.env` via `godotenv`.
2. Call `moderation.InitDB()` to connect to PostgreSQL.
3. Call `moderation.ApplyMigrations(db)` to run schema setup.
4. Construct the service stack: `NewRepository(db)` → `NewService(repo)` → `NewHandler(service)`.
5. Register routes via `moderation.RegisterRoutes(mux, handler)`.
6. Listen on `:8090`.

### Architecture Pattern

The moderation service follows a **clean layered architecture**:

```
HTTP Request
    │
    ▼
Handler (handlers.go)      ← HTTP decoding, validation, response writing
    │
    ▼
Service (service.go)       ← Business logic, rules
    │
    ▼
Repository (repository.go) ← SQL queries against PostgreSQL
```

### Routes & Controllers

Routes are registered in `internal/moderation/routes.go`.

| Route | Method | Controller (Handler Method) | File |
|---|---|---|---|
| `/reports` | POST | `Handler.SubmitReport` | `handlers.go` |
| `/moderation/reports` | GET | `Handler.ListPendingReports` | `handlers.go` |
| `/moderation/resolve` | POST | `Handler.ResolveReport` | `handlers.go` |
| `/servers/block` | POST | `Handler.BlockServer` | `handlers.go` |
| `/users/block` | POST | `Handler.BlockUser` | `handlers.go` |
| `/users/unblock` | POST | `Handler.UnblockUser` | `handlers.go` |
| `/users/blocked` | GET | `Handler.ListBlockedUsers` | `handlers.go` |
| `/users/block/check` | GET | `Handler.CheckUserBlock` | `handlers.go` |

### Supporting Files

| File | Purpose |
|---|---|
| `routes.go` | Registers all moderation routes on the mux |
| `handlers.go` | HTTP handlers (decode request → call service → write response) |
| `service.go` | Business rules: `SubmitReport`, `ResolveReport`, `BlockUser`, `UnblockUser`, `BlockServer`, `IsUserBlocked`, `ListBlockedUsers` |
| `repository.go` | Raw SQL queries for all moderation data |
| `db.go` | DB initializer + migration runner |
| `migrations.sql` | SQL DDL for moderation schema (reports, blocks tables) |

---

## 6. Shared Packages (`pkg/`)

| Package | Path | Purpose |
|---|---|---|
| `models` | `pkg/models/` | Shared data models: `UserDocument`, `UpdateProfileRequest`, `Activity`, etc. |
| `crypto` | `pkg/crypto/` | Cryptographic utilities: key generation, signing, verification |
| `protocol` | `pkg/protocol/` | Federated protocol type definitions and constants |

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
    → [federated] DeliverFederatedMessage
         → HTTP POST to remote server: /api/message/federated
    → [local] SendMessage (social_actions.go)
  ← 200 { message: "message sent" }
```

### 7.3 Admin Login + Protected Route
```
POST /admin/login  (Identity :8082)
  → AdminLoginHandler (admin_handlers.go)
    → ValidateAdminCredentials (admin.go)
    → GenerateJWT (admin.go)
  ← 200 { token: "..." }

GET /admin/stats  (Identity :8082)
  → AdminAuthMiddleware (admin_handlers.go)
    → ValidateJWT — checks Bearer token
    → pass to GetStatsHandler
      → GetServerStats (admin.go) → SQL queries
  ← 200 { total_users, total_posts, ... }
```

### 7.4 Server Federation Handshake
```
POST /federation/initiate-handshake  (Federation :8081)
  → InitiateHandshakeHandler (handlers.go)
    → Fetch remote server public key
    → Send signed handshake payload to remote /federation/handshake
    → Store handshake record in DB
  ← 200 { status: "handshake initiated" }

POST /federation/handshake  (Federation :8081 — called by remote)
  → HandshakeHandler (handlers.go)
    → Verify incoming signature
    → Store trusted server record
  ← 200 { status: "acknowledged" }
```

### 7.5 Submit a Moderation Report
```
POST /reports  (Moderation :8090)
  → Handler.SubmitReport (handlers.go)
    → Service.SubmitReport (service.go)
      → Repository.CreateReport (repository.go) → SQL INSERT
  ← 201 Created
```

---

## 8. Background Workers

These goroutines start automatically at service boot inside the Identity service:

| Worker | Function | File | Purpose |
|---|---|---|---|
| Session Key Worker | `StartSessionKeyWorker()` | `session_keys.go` | Periodically rotates/cleans up session keys |
| Invite Sweeper | `StartInviteSweeper()` | `invites.go` | Deletes expired invite codes from the DB |

---

## 9. Middleware Summary

| Middleware | Service | File | Applied To |
|---|---|---|---|
| `corsMiddleware` | Identity | `cmd/identity/main.go` | All routes |
| `corsMiddleware` | Federation | `cmd/federation/main.go` | All routes |
| `AdminAuthMiddleware` | Identity | `internal/identity/admin_handlers.go` | All `/admin/*` routes except `/admin/login` |
| `VerifySignatureMiddleware` | Federation | `internal/federation/handlers.go` | `/federation/signed/inbox`, `/federation/lookup` |

---

## 10. Database Migrations

Each service manages its own schema:

| Service | Migration Location | How It Runs |
|---|---|---|
| Identity | `internal/identity/migrations.go` + `migrations/` dir | Called via `identity.ApplyMigrations()` at startup |
| Federation | `internal/federation/migrations.sql` | Called via `federation.InitDB()` at startup |
| Moderation | `internal/moderation/migrations.sql` | Called via `moderation.ApplyMigrations(db)` at startup |
