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

```bash
# From the workspace root
cd fedinet-go
docker-compose up --build
```

What starts:
| Service | URL |
|---------|-----|
| Server A identity API | http://localhost:8080 |
| PostgreSQL | localhost:5432 |

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

If you have Go 1.21+ and a local Postgres instance, you can run the server directly:

```bash
cd fedinet-go

# Export required env vars (adjust values as needed)
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/fedinet_timeline?sslmode=disable"
export JWT_SECRET="dev-secret"
export PORT=8080

go run ./cmd/identity/...
```

On Windows PowerShell:
```powershell
$env:DATABASE_URL = "postgres://postgres:postgres@localhost:5432/fedinet_timeline?sslmode=disable"
$env:JWT_SECRET   = "dev-secret"
$env:PORT         = "8080"
go run ./cmd/identity/...
```

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
