# Running fedinet-go Locally

## Prerequisites

| Tool | Version | Install |
|------|---------|---------|
| Docker Desktop | Latest | https://www.docker.com/products/docker-desktop |
| Docker Compose | Bundled with Docker Desktop | — |
| Git | Any | https://git-scm.com |

> Docker Desktop must be **running** before any `docker-compose` command.

---

## What stays local (never pushed to GitHub)

| File | Why |
|------|-----|
| `fedinet-go/.env` | Local overrides for env vars — already in `.gitignore` |
| `federated-frontend/.env.local` | Frontend backend URL — already in `.gitignore` |

> `docker-compose.yml` and `docker-compose.federation.yml` **are** committed. They use throwaway dev-only credentials. Never put real credentials in those files.

---

## How to Run

### Option A — Single server (quickest start)

`docker-compose.yml` only starts **Postgres**. The identity service is run separately as a local Go binary (useful for fast iteration without rebuilding Docker images).

```bash
# Terminal 1 — start Postgres
cd fedinet-go
docker-compose up -d

# Terminal 2 — run the identity service locally (requires Go 1.25+)
export SERVER_ID=server_a
export SERVER_URL=http://localhost:8080
export DATABASE_URL=postgres://postgres:postgres@localhost:5432/fedinet_server_a?sslmode=disable
export JWT_SECRET=dev-secret
export SERVER_IDENTITY_PRIVATE_KEY=c4b1e5a2f3d6c9b8e1a4d7f0c3b6a9e2d5f8c1b4e7a0d3f6c9b2e5a8d1f4c7b0a3d6e9c2f5b8a1d4e7c0b3f6a9d2e5c8b1f4a7d0e3c6b9a2d5f8e1c4b7a0d3f6
go run ./cmd/identity/
```

On Windows PowerShell:
```powershell
$env:SERVER_ID   = "server_a"
$env:SERVER_URL  = "http://localhost:8080"
$env:DATABASE_URL = "postgres://postgres:postgres@localhost:5432/fedinet_server_a?sslmode=disable"
$env:JWT_SECRET  = "dev-secret"
$env:SERVER_IDENTITY_PRIVATE_KEY = "c4b1e5a2f3d6c9b8e1a4d7f0c3b6a9e2d5f8c1b4e7a0d3f6c9b2e5a8d1f4c7b0a3d6e9c2f5b8a1d4e7c0b3f6a9d2e5c8b1f4a7d0e3c6b9a2d5f8e1c4b7a0d3f6"
go run ./cmd/identity/
```

What starts:
| Service | URL |
|---------|-----|
| Server A identity API | http://localhost:8080 |
| PostgreSQL | localhost:5432 |

> First run: you must create the `fedinet_server_a` database and apply migrations manually, or just use **Option B** (the setup script does all of this automatically).

---

### Option B — Full two-server federation

```bash
cd fedinet-go
docker-compose -f docker-compose.federation.yml up --build
```

What starts:
| Service | URL |
|---------|-----|
| Server A identity API | http://localhost:8080 |
| Server A federation relay | http://localhost:8081 |
| Server B identity API | http://localhost:9080 |
| Server B federation relay | http://localhost:9081 |
| PostgreSQL (shared) | localhost:5432 |

---

### Step 2 — Initialize the server (first run only)

Wait until containers print `listening on :8080`, then seed the admin account:

```bash
# Server A
curl -s -X POST http://localhost:8080/initialize \
  -H "Content-Type: application/json" \
  -d '{"admin_username":"admin","admin_password":"password123","server_name":"Server A"}'
```

```bash
# Server B (federation setup only)
curl -s -X POST http://localhost:9080/initialize \
  -H "Content-Type: application/json" \
  -d '{"admin_username":"admin","admin_password":"password123","server_name":"Server B"}'
```

You only need to do this once. Postgres data persists in the `postgres_data` Docker volume between restarts.

**Windows (PowerShell) equivalent:**
```powershell
Invoke-RestMethod -Method Post -Uri http://localhost:8080/initialize `
  -ContentType "application/json" `
  -Body '{"admin_username":"admin","admin_password":"password123","server_name":"Server A"}'
```

---

### Step 3 — Verify the server is running

```bash
curl http://localhost:8080/health
# Expected: {"status":"ok"} or similar
```

---

### Step 4 — Run the frontend (optional, separate step)

See [`federated-frontend/INSTRUCTIONS.md`](../federated-frontend/INSTRUCTIONS.md) for full frontend setup.

Quick version:
```bash
cd federated-frontend
npm install       # first time only
npm run dev       # → http://localhost:3000
```

Create `federated-frontend/.env.local` before running:
```env
NEXT_PUBLIC_BACKEND_URL=http://localhost:8080
```

---

## Stopping everything

```bash
# Stop containers, keep DB data
cd fedinet-go
docker-compose down                                # single-server
docker-compose -f docker-compose.federation.yml down   # federation

# Stop AND wipe the DB volume (clean slate)
docker-compose -f docker-compose.federation.yml down -v
```

**Windows shortcut** (from workspace root):
```powershell
.\stop-all.ps1
```

---

## Running without Docker (Go directly)

If you have **Go 1.25+** and a local Postgres instance, you can run the server directly:

> **Database name:** Use `fedinet_server_a` (not `fedinet_timeline`). The `fedinet_timeline` name is only referenced by the legacy `docker-compose.yml` postgres service; all identity & federation code uses `fedinet_server_a` / `fedinet_server_b`.

```bash
cd fedinet-go

# Export required env vars (adjust values as needed)
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/fedinet_server_a?sslmode=disable"
export JWT_SECRET="dev-secret"
export SERVER_ID="server_a"
export SERVER_URL="http://localhost:8080"
export SERVER_IDENTITY_PRIVATE_KEY="c4b1e5a2f3d6c9b8e1a4d7f0c3b6a9e2d5f8c1b4e7a0d3f6c9b2e5a8d1f4c7b0a3d6e9c2f5b8a1d4e7c0b3f6a9d2e5c8b1f4a7d0e3c6b9a2d5f8e1c4b7a0d3f6"
# Optional: enable cross-server search (format: "peer_id=http://host:port")
export FEDERATION_PEERS="server_b=http://localhost:9080"

go run ./cmd/identity/
```

On Windows PowerShell:
```powershell
$env:DATABASE_URL                = "postgres://postgres:postgres@localhost:5432/fedinet_server_a?sslmode=disable"
$env:JWT_SECRET                  = "dev-secret"
$env:SERVER_ID                   = "server_a"
$env:SERVER_URL                  = "http://localhost:8080"
$env:SERVER_IDENTITY_PRIVATE_KEY = "c4b1e5a2f3d6c9b8e1a4d7f0c3b6a9e2d5f8c1b4e7a0d3f6c9b2e5a8d1f4c7b0a3d6e9c2f5b8a1d4e7c0b3f6a9d2e5c8b1f4a7d0e3c6b9a2d5f8e1c4b7a0d3f6"
# Optional: enable cross-server search
$env:FEDERATION_PEERS            = "server_b=http://localhost:9080"
go run ./cmd/identity/
```

> `FEDERATION_PEERS` is **required** for cross-server search (`/search?q=alice@server_b`) and federated follow/message delivery to work. Without it, those calls return `"discovery_status": "not_found"`. In Docker the compose file injects this automatically.

---

## Credentials reference (local dev only)

| What | Value | Where it's set |
|------|-------|----------------|
| Postgres user | `postgres` | `docker-compose*.yml` → `POSTGRES_USER` |
| Postgres password | `postgres` | `docker-compose*.yml` → `POSTGRES_PASSWORD` |
| Postgres DB (single) | `fedinet_timeline` | `docker-compose.yml` |
| Postgres DBs (federation) | `fedinet_server_a`, `fedinet_server_b` | `docker-compose.federation.yml` + `init-dbs.sql` |
| Admin username | `admin` | Set at `/initialize` |
| Admin password | `password123` (default) | Set at `/initialize` — change to any value |
| JWT secret (Server A) | `fedinet-dev-secret-key-server-a` | `docker-compose.federation.yml` → `JWT_SECRET` |
| JWT secret (Server B) | `fedinet-dev-secret-key-server-b` | `docker-compose.federation.yml` → `JWT_SECRET` |
| Server identity private key | Long hex string | `docker-compose.federation.yml` → `SERVER_IDENTITY_PRIVATE_KEY` |

> **All values above are dev-only throwaway values.** For any real deployment, replace every secret with strong randomly generated values supplied via a secrets manager or a `.env` file that is never committed.

---

## Port summary

| Port | Service |
|------|---------|
| 8080 | Server A — identity API |
| 8081 | Server A — federation relay |
| 9080 | Server B — identity API |
| 9081 | Server B — federation relay |
| 5432 | PostgreSQL |
| 3000 | federated-frontend (Next.js dev) |

---

## Troubleshooting

| Problem | Fix |
|---------|-----|
| `port is already allocated` | Another process uses that port. Run `docker-compose down` then retry, or change the host port in `docker-compose*.yml`. |
| `connection refused` on `/initialize` | Containers are still starting. Wait ~10 s and retry. |
| Database errors after schema change | Run `docker-compose down -v` to wipe the volume, then `up --build`. |
| Changes to Go code not reflected | Always pass `--build` flag: `docker-compose up --build`. |
