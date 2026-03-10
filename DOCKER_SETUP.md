# Fedinet Docker Federation Setup

> Complete guide to running **Server A** and **Server B** locally using Docker for testing the federated handshake.

---

## Prerequisites

- [Docker Desktop](https://www.docker.com/products/docker-desktop/) installed and running
- PowerShell (comes with Windows 10+)

---

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    Docker Network                       │
│                                                         │
│  ┌──────────────┐                                       │
│  │   Postgres    │  Port 5432                           │
│  │  (shared DB)  │                                      │
│  │               │                                      │
│  │ ┌───────────┐ │                                      │
│  │ │ server_a  │ │                                      │
│  │ │ server_b  │ │  ← Two separate databases            │
│  │ └───────────┘ │                                      │
│  └──────┬───┬────┘                                      │
│         │   │                                           │
│    ┌────┘   └────┐                                      │
│    ▼             ▼                                      │
│  Server A      Server B                                 │
│  Identity:8080 Identity:9080                            │
│  Federation:8081 Federation:9081                        │
└─────────────────────────────────────────────────────────┘
```

### Port Map

| Service              | URL                        |
|----------------------|----------------------------|
| Server A Identity    | http://localhost:8080       |
| Server A Federation  | http://localhost:8081       |
| Server B Identity    | http://localhost:9080       |
| Server B Federation  | http://localhost:9081       |
| Postgres             | localhost:5432              |

---
    
## Quick Start (One Command)

```powershell
cd fedinet-go
.\setup-federation.bat
```

This script does everything below automatically. If you prefer manual steps, continue reading.

---

## Manual Setup

### Step 1: Clean Up (if re-running)

If you've run Docker before with the old config, remove the old volume:

```powershell
docker-compose -f docker-compose.federation.yml down -v
```

### Step 2: Start All Containers

```powershell
cd fedinet-go
docker-compose -f docker-compose.federation.yml up --build -d
```

This starts 5 containers:

| Container               | Image          | Purpose                   |
|-------------------------|----------------|---------------------------|
| `fedinet_postgres`      | postgres:15    | Shared database           |
| `server_a_identity`     | Built locally  | Server A user management  |
| `server_a_federation`   | Built locally  | Server A federation layer |
| `server_b_identity`     | Built locally  | Server B user management  |
| `server_b_federation`   | Built locally  | Server B federation layer |

### Step 3: Wait for Postgres

Wait about 10 seconds for Postgres to initialize and create both databases (`fedinet_server_a` and `fedinet_server_b`).

Check readiness:

```powershell
docker exec fedinet_postgres pg_isready -U postgres
```

### Step 4: Run Database Migrations

Both **identity** and **federation** schemas need to be applied to both databases:

```powershell
# 001 — Server identity, admins, invites tables
Get-Content .\internal\identity\migrations\001_server_initialization.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_a
Get-Content .\internal\identity\migrations\001_server_initialization.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b

# 002 — Core schema (identities, profiles, posts, follows, messages, etc.)
Get-Content .\internal\identity\migrations\002_core_schema.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_a
Get-Content .\internal\identity\migrations\002_core_schema.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b

# 003 — Registration sessions
Get-Content .\internal\identity\migrations\003_registration_sessions.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_a
Get-Content .\internal\identity\migrations\003_registration_sessions.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b

# 004 — Session keys
Get-Content .\internal\identity\migrations\004_session_keys.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_a
Get-Content .\internal\identity\migrations\004_session_keys.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b

# 005 — Federated messages
Get-Content .\internal\identity\migrations\005_federated_messages.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_a
Get-Content .\internal\identity\migrations\005_federated_messages.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b

# 006 — Fix messages schema
Get-Content .\internal\identity\migrations\006_fix_messages_schema.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_a
Get-Content .\internal\identity\migrations\006_fix_messages_schema.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b

# 007 — TOTP authenticator
Get-Content .\internal\identity\migrations\007_totp.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_a
Get-Content .\internal\identity\migrations\007_totp.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b

# 008 — Ephemeral posts
Get-Content .\internal\identity\migrations\008_ephemeral_posts.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_a
Get-Content .\internal\identity\migrations\008_ephemeral_posts.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b

# 009 — Hashtags
Get-Content .\internal\identity\migrations\009_hashtags.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_a
Get-Content .\internal\identity\migrations\009_hashtags.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b

# 009 — Identity vouches
Get-Content .\internal\identity\migrations\009_identity_vouches.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_a
Get-Content .\internal\identity\migrations\009_identity_vouches.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b

# 010 — Disable resharing
Get-Content .\internal\identity\migrations\010_disable_resharing.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_a
Get-Content .\internal\identity\migrations\010_disable_resharing.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b

# 011 — Post visibility
Get-Content .\internal\identity\migrations\011_post_visibility.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_a
Get-Content .\internal\identity\migrations\011_post_visibility.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b

# 012 — Passkeys (WebAuthn)
Get-Content .\internal\identity\migrations\012_passkeys.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_a
Get-Content .\internal\identity\migrations\012_passkeys.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b

# Federation tables
Get-Content .\internal\federation\migrations.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_a
Get-Content .\internal\federation\migrations.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b
```

> **Note:** You may see `NOTICE: relation already exists` messages — that's fine. The `setup-federation.bat` / `setup-federation.sh` scripts run all of the above automatically.

### Step 5: Restart Identity Containers

After applying migrations, restart the identity containers so they pick up the new tables:

```powershell
docker restart server_a_identity server_b_identity
```

Wait a few seconds, then check logs to confirm they started cleanly:

```powershell
docker logs server_a_identity --tail 5
docker logs server_b_identity --tail 5
```

### Step 6: Initialize Both Servers

Each server needs a one-time initialization that creates core tables, generates Ed25519 identity keys, and sets up the admin account.

**Initialize Server A:**

```powershell
Invoke-RestMethod -Uri "http://localhost:8080/initialize" -Method Post -Body '{"server_name":"Server A","admin_username":"admin","admin_password":"password123"}' -ContentType "application/json"
```

**Initialize Server B:**

```powershell
Invoke-RestMethod -Uri "http://localhost:9080/initialize" -Method Post -Body '{"server_name":"Server B","admin_username":"admin","admin_password":"password123"}' -ContentType "application/json"
```

### Step 7: Verify Both Servers Are Running

```powershell
Invoke-RestMethod -Uri "http://localhost:8080/health"
Invoke-RestMethod -Uri "http://localhost:9080/health"
```

Both should return `200 OK` with a health status.

---

## Testing Server A ↔ Server B Communication

This section walks you through **8 progressive steps** to verify that both servers are running, can discover each other, and can exchange signed federated requests.

### Step 1: Check Federation Health (Both Servers)

Verify both federation services are alive and reporting metrics:

```powershell
# Server A federation health
Write-Host "=== Server A Federation ===" -ForegroundColor Cyan
Invoke-RestMethod -Uri "http://localhost:8081/federation/health"

# Server B federation health
Write-Host "=== Server B Federation ===" -ForegroundColor Cyan
Invoke-RestMethod -Uri "http://localhost:9081/federation/health"
```

✅ **Expected:** Both return JSON with `status`, `uptime`, `total_messages`, etc.

### Step 2: Check Identity Health (Both Servers)

```powershell
# Server A identity health
Invoke-RestMethod -Uri "http://localhost:8080/health"

# Server B identity health
Invoke-RestMethod -Uri "http://localhost:9080/health"
```

✅ **Expected:** Both return `200 OK` with `healthy`.

### Step 3: View Server Capabilities

Each federation service advertises what features it supports:

```powershell
# Server A capabilities
Write-Host "=== Server A Capabilities ===" -ForegroundColor Cyan
Invoke-RestMethod -Uri "http://localhost:8081/federation/capabilities"

# Server B capabilities
Write-Host "=== Server B Capabilities ===" -ForegroundColor Cyan
Invoke-RestMethod -Uri "http://localhost:9081/federation/capabilities"
```

✅ **Expected:** Both return JSON listing supported activity types and protocol versions.

### Step 4: Get Server Identity & Keys

Each server has a unique identity (server ID, name, Ed25519 public key):

```powershell
# Server A identity info
Write-Host "=== Server A Identity ===" -ForegroundColor Green
$serverA = Invoke-RestMethod -Uri "http://localhost:8080/server/info"
$serverA

# Server B identity info
Write-Host "=== Server B Identity ===" -ForegroundColor Green
$serverB = Invoke-RestMethod -Uri "http://localhost:9080/server/info"
$serverB
```

✅ **Expected:** Each returns `server_id`, `server_name`, `public_key`, `version`.

### Step 5: Create Users on Both Servers

Register a user on each server for cross-server lookup testing:

```powershell
# --- Register Alice on Server B ---
$loginB = Invoke-RestMethod -Uri "http://localhost:9080/admin/login" -Method Post -Body '{"username":"admin","password":"password123"}' -ContentType "application/json"
$tokenB = $loginB.token
$inviteB = Invoke-RestMethod -Uri "http://localhost:9080/admin/invites/generate" -Method Post -Body '{"invite_type":"user","max_uses":-1}' -ContentType "application/json" -Headers @{Authorization="Bearer $tokenB"}
Invoke-RestMethod -Uri "http://localhost:9080/register" -Method Post -Body "{`"username`":`"alice`",`"password`":`"alice123`",`"invite_code`":`"$($inviteB.invite_code)`"}" -ContentType "application/json"
Write-Host "Alice registered on Server B" -ForegroundColor Green

# --- Register Bob on Server A ---
$loginA = Invoke-RestMethod -Uri "http://localhost:8080/admin/login" -Method Post -Body '{"username":"admin","password":"password123"}' -ContentType "application/json"
$tokenA = $loginA.token
$inviteA = Invoke-RestMethod -Uri "http://localhost:8080/admin/invites/generate" -Method Post -Body '{"invite_type":"user","max_uses":-1}' -ContentType "application/json" -Headers @{Authorization="Bearer $tokenA"}
Invoke-RestMethod -Uri "http://localhost:8080/register" -Method Post -Body "{`"username`":`"bob`",`"password`":`"bob123`",`"invite_code`":`"$($inviteA.invite_code)`"}" -ContentType "application/json"
Write-Host "Bob registered on Server A" -ForegroundColor Green
```

✅ **Expected:** Both return `user_id`, `home_server`, and `recovery_key`.

### Step 6: Test Unsigned Lookup (Should Fail — 401)

Try to look up Alice on Server B's federation endpoint **without** a signature:

```powershell
try {
    Invoke-RestMethod -Uri "http://localhost:9081/federation/lookup?id=alice@localhost"
} catch {
    Write-Host "Status: $($_.Exception.Response.StatusCode)" -ForegroundColor Yellow
    Write-Host "This is EXPECTED - unsigned requests are rejected by the signature middleware" -ForegroundColor Green
}
```

✅ **Expected:** `401 Unauthorized` — this proves the **VerifySignatureMiddleware** is working. Unsigned requests are rejected.

### Step 7: Establish Trust (Automatic Handshake)

Server A initiates a handshake with Server B — both servers automatically exchange public keys:

```powershell
# Server A initiates handshake with Server B
Invoke-RestMethod -Uri "http://localhost:8081/federation/handshake/initiate" -Method Post -Body '{"target_server":"http://server-b-federation:8081"}' -ContentType "application/json"
```

✅ **Expected:** Returns `"Handshake complete — mutual trust established"` with both servers' identities and `"status": "trusted"`.

Verify the trust was stored:

```powershell
# Check Server A's trusted peers
docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_a -c "SELECT server_id, server_name FROM trusted_servers;"

# Check Server B's trusted peers
docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b -c "SELECT server_id, server_name FROM trusted_servers;"
```

✅ **Expected:** Each database shows the other server's entry.

### Step 8: Run the Signed Request Test

This test generates a signed federated lookup request and sends it to Server B:

```powershell
go run ./cmd/signtest/
```

✅ **Expected output:**
- `401 Unauthorized` → The test uses a **fresh key pair** (not registered in Server B). This proves the middleware correctly rejects unknown signers.
- If you see `200 OK` → The request was signed with a known key and the lookup succeeded.

### Step 9: Cross-Server Capability Discovery

Server A can dynamically discover Server B's capabilities (and vice versa):

```powershell
# Server A discovers Server B's capabilities
Write-Host "=== Server A discovers Server B ===" -ForegroundColor Cyan
Invoke-RestMethod -Uri "http://localhost:8081/federation/discover" -Method Post -Body '{"server_url":"http://server-b-federation:8081"}' -ContentType "application/json"

# Server B discovers Server A's capabilities  
Write-Host "=== Server B discovers Server A ===" -ForegroundColor Cyan
Invoke-RestMethod -Uri "http://localhost:9081/federation/discover" -Method Post -Body '{"server_url":"http://server-a-federation:8081"}' -ContentType "application/json"
```

> **Note:** The discover endpoint uses Docker internal hostnames (`server-a-federation`, `server-b-federation`) because the containers communicate over the Docker network, not `localhost`.

✅ **Expected:** Each returns the other server's capability list.

### Step 10: Send a Federated Activity

Queue an activity from Server A to be delivered to Server B:

> **User ID format:** After `/initialize` is called with `server_name: "Server A"` / `"Server B"`, the `SERVER_ID` env vars (`server_a` / `server_b`) are used as the domain suffix in all user IDs. Users registered on Server A have IDs `username@server_a`; users on Server B have IDs `username@server_b`. Using `@localhost` only works when `SERVER_ID` is unset (no Docker).

```powershell
Invoke-RestMethod -Uri "http://localhost:8081/federation/send" -Method Post -Body '{
    "activity_type": "Follow",
    "actor_id": "bob@server_a",
    "target_server": "http://server-b-federation:8081",
    "target_id": "alice@server_b",
    "payload": {"message": "Bob wants to follow Alice"}
}' -ContentType "application/json"
```

✅ **Expected:** Returns `201 Created` with an `activity_id`, meaning the activity has been queued for delivery to Server B.

Check Server A's outbox to confirm:

```powershell
Invoke-RestMethod -Uri "http://localhost:8081/federation/outbox"
```

---

### Quick Communication Health Check (One-Liner)

Run this to quickly verify all 4 services are responding:

```powershell
@("http://localhost:8080/health", "http://localhost:9080/health", "http://localhost:8081/federation/health", "http://localhost:9081/federation/health") | ForEach-Object { Write-Host "$_ -> " -NoNewline; try { $r = Invoke-WebRequest -Uri $_ -UseBasicParsing; Write-Host $r.StatusCode -ForegroundColor Green } catch { Write-Host "FAIL" -ForegroundColor Red } }
```

✅ **Expected:** All 4 URLs return `200`.

---

## Useful Commands

```powershell
# View logs for all containers
docker-compose -f docker-compose.federation.yml logs -f

# View logs for a specific service
docker-compose -f docker-compose.federation.yml logs -f server-a-identity

# Stop all containers
docker-compose -f docker-compose.federation.yml down

# Stop and remove all data (fresh start)
docker-compose -f docker-compose.federation.yml down -v

# Rebuild after code changes
docker-compose -f docker-compose.federation.yml up --build -d

# Connect to Postgres directly
docker exec -it fedinet_postgres psql -U postgres -d fedinet_server_a
```

---

## Troubleshooting

| Problem | Solution |
|---|---|
| `init-dbs.sql` didn't run | Remove volume: `docker-compose ... down -v` then retry |
| Port already in use | Stop conflicting services or change ports in `docker-compose.federation.yml` |
| Identity container exits | Check logs: `docker logs server_a_identity` — likely DB connection issue |
| Federation migrations fail | Make sure Postgres is ready before running the SQL |
| `401 Unauthorized` on lookup | This is correct — means the signature middleware is working |
| `invalid credentials` on admin login | Ensure `ADMIN_USERNAME` and `ADMIN_PASSWORD` are set in `docker-compose.federation.yml` |
| `invite usage limit reached` | Generate invite with `max_uses: -1` for unlimited uses |

---

## File Reference

| File | Purpose |
|---|---|
| `docker-compose.federation.yml` | Main compose file (5 services) |
| `init-dbs.sql` | Creates `fedinet_server_a` and `fedinet_server_b` databases |
| `setup-federation.bat` | One-click setup script |
| `internal/identity/Dockerfile` | Builds the identity service image |
| `internal/federation/Dockerfile` | Builds the federation service image |
| `internal/identity/migrations/001_server_initialization.sql` | Server identity, admins, invites tables |
| `internal/identity/migrations/002_core_schema.sql` | Core tables: identities, profiles, posts, follows, etc. |
| `internal/identity/migrations/003_registration_sessions.sql` | Registration session tracking |
| `internal/identity/migrations/004_session_keys.sql` | Session key rotation |
| `internal/identity/migrations/005_federated_messages.sql` | Cross-server message delivery tables |
| `internal/identity/migrations/006_fix_messages_schema.sql` | Schema fixes for federated messages |
| `internal/identity/migrations/007_totp.sql` | TOTP authenticator support |
| `internal/identity/migrations/008_ephemeral_posts.sql` | Ephemeral / disappearing posts |
| `internal/identity/migrations/009_hashtags.sql` | Hashtag indexing |
| `internal/identity/migrations/009_identity_vouches.sql` | Cross-server identity vouches |
| `internal/identity/migrations/010_disable_resharing.sql` | Per-post resharing controls |
| `internal/identity/migrations/011_post_visibility.sql` | Post visibility settings |
| `internal/identity/migrations/012_passkeys.sql` | WebAuthn passkeys + recovery attempts |
| `internal/federation/migrations.sql` | Federation-specific tables |
