# Fedinet Docker Federation Setup

> Complete guide to running **Server A** and **Server B** locally using Docker for testing the federated handshake.

---

## Prerequisites

- [Docker Desktop](https://www.docker.com/products/docker-desktop/) installed and running
- `curl` available in your terminal (comes with Windows 10+)

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

```bash
cd fedinet-go
setup-federation.bat
```

This script does everything below automatically. If you prefer manual steps, continue reading.

---

## Manual Setup

### Step 1: Clean Up (if re-running)

If you've run Docker before with the old config, remove the old volume:

```bash
docker-compose -f docker-compose.federation.yml down -v
```

### Step 2: Start All Containers

```bash
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

```bash
docker exec fedinet_postgres pg_isready -U postgres
```

### Step 4: Run Federation Migrations

The federation service needs its schema created manually:

```bash
# Server A database
docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_a < internal\federation\migrations.sql

# Server B database
docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b < internal\federation\migrations.sql
```

> **Note:** You may see `NOTICE: relation already exists` messages — that's fine.

### Step 5: Initialize Both Servers

Each server needs a one-time initialization that creates core tables, generates Ed25519 identity keys, and sets up the admin account.

**Initialize Server A:**

```bash
curl -X POST http://localhost:8080/initialize ^
  -H "Content-Type: application/json" ^
  -d "{\"server_name\": \"Server A\", \"admin_username\": \"admin\", \"admin_password\": \"password123\"}"
```

**Initialize Server B:**

```bash
curl -X POST http://localhost:9080/initialize ^
  -H "Content-Type: application/json" ^
  -d "{\"server_name\": \"Server B\", \"admin_username\": \"admin\", \"admin_password\": \"password123\"}"
```

### Step 6: Verify Both Servers Are Running

```bash
curl http://localhost:8080/health
curl http://localhost:9080/health
```

Both should return `200 OK` with a health status.

---

## Testing Federation Between Servers

### Register a User on Server B

```bash
curl -X POST http://localhost:9080/register ^
  -H "Content-Type: application/json" ^
  -d "{\"username\": \"alice\", \"password\": \"alice123\", \"display_name\": \"Alice\"}"
```

### Look Up Alice from Server A's Federation

```bash
curl http://localhost:8081/federation/lookup?id=alice@localhost
```

> **Note:** This will return `401 Unauthorized` because the `/federation/lookup` endpoint is now protected by the `VerifySignatureMiddleware`. This proves the signed handshake is working — unsigned requests are rejected.

### Run the Signed Request Test

```bash
go run ./cmd/signtest/
```

This generates a signed request from a simulated "Server A" and sends it to Server B. Expected result: `401` (the test key isn't registered in Server B's DB — proving the middleware correctly rejects unknown signers).

---

## Making Server A Successfully Query Server B

For Server A to actually retrieve Alice's profile from Server B, Server A's public key must be registered in Server B's database:

**1. Get Server A's public key:**

```bash
curl http://localhost:8080/server/info
```

Copy the `public_key` value from the response.

**2. Insert Server A's key into Server B's database:**

```bash
docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b -c ^
  "INSERT INTO identities (id, user_id, home_server, public_key, key_version, recovery_key_hash, allow_discovery, created_at, updated_at) VALUES (gen_random_uuid(), 'SERVER_A_ID', 'http://localhost:8080', 'PASTE_PUBLIC_KEY_HERE', 1, '', true, NOW(), NOW());"
```

Replace `SERVER_A_ID` with Server A's `server_id` and `PASTE_PUBLIC_KEY_HERE` with the public key.

**3. Now signed requests from Server A will be accepted by Server B.**

---

## Useful Commands

```bash
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

---

## File Reference

| File | Purpose |
|---|---|
| `docker-compose.federation.yml` | Main compose file (5 services) |
| `init-dbs.sql` | Creates `fedinet_server_a` and `fedinet_server_b` databases |
| `setup-federation.bat` | One-click setup script |
| `internal/identity/Dockerfile` | Builds the identity service image |
| `internal/federation/Dockerfile` | Builds the federation service image |
