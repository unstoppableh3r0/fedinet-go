# fedinet-go

Core backend service for the Federated Social Networking platform. Provides identity management, social graph operations, federation between servers, and admin tooling.

## Architecture

The project is structured as a Go module with distinct service layers:

```
fedinet-go/
  cmd/
    identity/        # Entry point — runs the identity HTTP server (port 8082)
    federation/      # Entry point — runs the federation relay (port 8081)
    moderation/      # Moderation tooling service
    db_cleanup/      # Database maintenance utilities
  internal/
    identity/        # Core domain — auth, users, posts, social graph, admin
    federation/      # Cross-server federation protocol
    moderation/      # Content moderation logic
    privacy/         # Privacy and blocking rules
    timeline/        # Feed assembly
  pkg/
    crypto/          # Key generation and signing primitives
    models/          # Shared data models
    protocol/        # Federation wire protocol types
```

The identity service is the main backend. The federation service proxies cross-server actions. Both are deployed as separate containers in the Docker Compose setup, with the identity service exposed on host port 8080 (Server A) and 9080 (Server B).

## Services

| Service | Internal Port | Host Port (A / B) | Purpose |
|---------|--------------|-------------------|---------|
| server_a_identity | 8082 | 8080 | Primary identity and API server |
| server_a_federation | 8081 | 8081 | Federation relay for Server A |
| server_b_identity | 8082 | 9080 | Identity server for Server B |
| server_b_federation | 8081 | 9081 | Federation relay for Server B |
| fedinet_postgres | 5432 | 5432 | Shared PostgreSQL database |

## API Surface

### Auth and Users

```
POST  /register                   Register a new user
POST  /login                      Login, returns JWT
GET   /user/me                    Get authenticated user
GET   /user/search?user_id=       Search local users
GET   /users/suggested            Get suggested users to follow
POST  /recover                    Account recovery
```

### Social Graph

```
POST  /follow                     Follow a user
POST  /unfollow                   Unfollow a user
GET   /followers                  Get follower list
GET   /following                  Get following list
POST  /block                      Block a user
POST  /unblock                    Unblock a user
GET   /blocks                     Get blocked users
```

### Posts

```
POST  /post/create                Create a post
GET   /posts/user                 Get posts by user
GET   /posts/recent               Get recent posts
GET   /feed                       Get authenticated user's feed
POST  /post/like                  Toggle like
POST  /post/repost                Toggle repost
POST  /reply                      Create a reply
GET   /replies                    Get replies for a post
```

### Messaging and Notifications

```
POST  /message                    Send a message
GET   /conversations              List conversations
GET   /conversation/messages      Get messages in a conversation
GET   /notifications              Get notifications
POST  /notifications/read         Mark notifications read
```

### Federation

```
GET   /search?q=                  Federated user search
GET   /server-info                Own server metadata
GET   /trusted-servers            List trusted peer servers
POST  /trusted-servers/add        Add a trusted server
POST  /federation/handshake       Incoming federation handshake
POST  /federation/follow          Incoming cross-server follow
POST  /federation/message         Incoming cross-server message
```

### Admin (requires Bearer token from /admin/login)

```
POST  /admin/login                Admin authentication
GET   /admin/stats                Server statistics
GET   /admin/users/list           List all users
GET   /admin/config/server        Get server config
PUT   /admin/config/server        Update server name
GET   /admin/invites/list         List invite codes
POST  /admin/invites/generate     Generate invite
POST  /admin/invites/revoke       Revoke invite
GET   /admin/invites/qr           Get invite QR code image
GET   /admin/trusted-servers/list List trusted servers
POST  /admin/trusted-servers/add  Add trusted server
DELETE /admin/trusted-servers/remove  Remove trusted server
POST  /admin/trusted-servers/test Test connectivity to a peer
```

## Environment Variables

The identity service reads from a `.env` file in the project root (loaded via godotenv).

| Variable | Required | Description |
|----------|----------|-------------|
| `DATABASE_URL` | Yes | PostgreSQL connection string |
| `ADMIN_USERNAME` | Yes | Admin login username |
| `ADMIN_PASSWORD` | Yes | Admin login password |
| `JWT_SECRET` | Yes | HMAC secret for signing JWTs |
| `SERVER_ID` | Yes | Unique server identifier (e.g. `server_a`) |
| `SERVER_NAME` | No | Human-readable server name |
| `SERVER_URL` | No | Public base URL of this server |
| `SERVER_MASTER_KEY` | No | Master signing key for federation |
| `SMTP_HOST` / `SMTP_PORT` | No | Email provider for account recovery |

## Setup

### Docker (recommended for two-server federation)

```bash
cd fedinet-go

# Windows
.\setup-federation.bat

# Linux / macOS
./setup-federation.sh
```

This builds and starts both servers with the full federation stack. Server A is at `localhost:8080`, Server B at `localhost:9080`.

### Local development (single server)

```bash
cd fedinet-go

# Copy and populate the env file
cp .env.example .env

# Start identity service
cd cmd/identity
go run .
```

### Default admin credentials (Docker)

```
Username: admin
Password: password123
```

These are set in `docker-compose.federation.yml`. Change them before any public deployment.

## Admin Panel

The admin panel is a separate Next.js application located at `federated-frontend/federated-admin/`. It connects to the identity service at `http://localhost:8080` by default (configurable via `NEXT_PUBLIC_BACKEND_URL`).

```bash
cd federated-frontend/federated-admin
npm install
npm run dev   # available at http://localhost:3001
```

## Running All Services

From the workspace root:

```powershell
.\start-all.ps1
```

This starts:
- Identity service on port 8080
- Federation service on port 8081
- Main frontend on port 3000
- Admin panel on port 3001

To stop everything:

```powershell
.\stop-all.ps1
```

## Database

PostgreSQL 18 is required. Migrations are applied automatically on startup via `identity.ApplyMigrations()`. The Docker setup provisions the database automatically via `fedinet_postgres`.

For manual setup:

```sql
CREATE DATABASE fedinet_timeline;
```

Then set `DATABASE_URL` accordingly.



## Development Notes

- Working on ActivityPub federation improvements.