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
# Identity migrations (server initialization tables)
Get-Content .\internal\identity\migrations\001_server_initialization.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_a
Get-Content .\internal\identity\migrations\001_server_initialization.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b

# Core identity schema (users, profiles, posts, etc.)
Get-Content .\internal\identity\migrations\002_core_schema.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_a
Get-Content .\internal\identity\migrations\002_core_schema.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b

# Federation migrations
Get-Content .\internal\federation\migrations.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_a
Get-Content .\internal\federation\migrations.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b
```

> **Note:** You may see `NOTICE: relation already exists` messages — that's fine.

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

## Testing Federation Between Servers

### Register a User on Server B

First, generate an invite code (admin login required):

```powershell
# Login as admin
$login = Invoke-RestMethod -Uri "http://localhost:9080/admin/login" -Method Post -Body '{"username":"admin","password":"password123"}' -ContentType "application/json"
$token = $login.token

# Generate invite code
$invite = Invoke-RestMethod -Uri "http://localhost:9080/admin/invites/generate" -Method Post -Body '{"invite_type":"user","max_uses":-1}' -ContentType "application/json" -Headers @{Authorization="Bearer $token"}
$code = $invite.invite_code
Write-Host "Invite code: $code"

# Register Alice using the invite code
Invoke-RestMethod -Uri "http://localhost:9080/register" -Method Post -Body "{`"username`":`"alice`",`"password`":`"alice123`",`"display_name`":`"Alice`",`"invite_code`":`"$code`"}" -ContentType "application/json"
```

### Look Up Alice from Server A's Federation

```powershell
Invoke-RestMethod -Uri "http://localhost:8081/federation/lookup?id=alice@localhost"
```

> **Note:** This will return `401 Unauthorized` because the `/federation/lookup` endpoint is now protected by the `VerifySignatureMiddleware`. This proves the signed handshake is working — unsigned requests are rejected.

### Run the Signed Request Test

```powershell
go run ./cmd/signtest/
```

This generates a signed request from a simulated "Server A" and sends it to Server B. Expected result: `401` (the test key isn't registered in Server B's DB — proving the middleware correctly rejects unknown signers).

---

## Making Server A Successfully Query Server B

For Server A to actually retrieve Alice's profile from Server B, Server A's public key must be registered in Server B's database:

**1. Get Server A's public key:**

```powershell
Invoke-RestMethod -Uri "http://localhost:8080/server/info"
```

Copy the `public_key` value from the response.

**2. Insert Server A's key into Server B's database:**

```powershell
docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b -c "INSERT INTO identities (id, user_id, home_server, public_key, key_version, recovery_key_hash, allow_discovery, created_at, updated_at) VALUES (gen_random_uuid(), 'SERVER_A_ID', 'http://localhost:8080', 'PASTE_PUBLIC_KEY_HERE', 1, '', true, NOW(), NOW());"
```

Replace `SERVER_A_ID` with Server A's `server_id` and `PASTE_PUBLIC_KEY_HERE` with the public key.

**3. Now signed requests from Server A will be accepted by Server B.**

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
| `internal/federation/migrations.sql` | Federation-specific tables |
