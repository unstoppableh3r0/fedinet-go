# Running fedinet-go Locally

---

## Prerequisites

| Tool | Check | Install |
|------|-------|---------|
| Docker Desktop | `docker --version` | https://www.docker.com/products/docker-desktop |
| Docker Compose | `docker-compose --version` | Bundled with Docker Desktop |
| Git | `git --version` | https://git-scm.com |

> **Docker Desktop must be open and running** (look for the whale icon in the system tray) before any step below.

---

## Quickest start — two-server federation with Docker

This is the recommended path for a first-time clone.
**One script does everything:** builds images, creates databases, runs all migrations, and initializes both servers.

```powershell
# From the repo root
cd fedinet-go
.\setup-federation.bat
```

On Mac / Linux use the shell version instead:
```bash
cd fedinet-go
chmod +x setup-federation.sh
./setup-federation.sh
```

### What the script does

| Step | What happens |
|------|-------------|
| 1 | Builds Docker images for identity and federation services |
| 2 | Starts 5 containers (Postgres + Server A identity + Server A federation + Server B identity + Server B federation) |
| 3 | Waits for Postgres to be healthy |
| 4 | Runs all 6 identity migrations + federation migrations on both databases |
| 5 | Restarts identity containers to pick up the new schema |
| 6 | Calls `POST /initialize` on both servers to create the admin account and generate Ed25519 keys |
| 7 | Calls `POST /trusted-servers/add` on each server so they mutually trust each other (handshake uses Docker-internal service names) |

The script takes **~1–2 minutes** on the first run (image build). Subsequent runs are faster because layers are cached.

### What you get when it finishes

| Service | URL |
|---------|-----|
| Server A — identity API | http://localhost:8080 |
| Server A — federation relay | http://localhost:8081 |
| Server B — identity API | http://localhost:9080 |
| Server B — federation relay | http://localhost:9081 |
| PostgreSQL (shared) | localhost:5432 |

Admin credentials for **both** servers: `admin` / `password123`

---

## Verifying the setup worked

Run this after the script finishes to confirm all 4 services are healthy:

```powershell
@(
  "http://localhost:8080/health",
  "http://localhost:9080/health",
  "http://localhost:8081/federation/health",
  "http://localhost:9081/federation/health"
) | ForEach-Object {
  $r = Invoke-RestMethod -Uri $_
  Write-Host "$_ -> $($r.status)" -ForegroundColor Green
}
```

Expected output:
```
http://localhost:8080/health -> ok
http://localhost:9080/health -> ok
http://localhost:8081/federation/health -> healthy
http://localhost:9081/federation/health -> healthy
```

You can also check logs if a container looks stuck:

```powershell
docker logs server_a_identity --tail 20
docker logs server_b_identity --tail 20
```

---

## Running the frontend (optional)

The frontend is a separate Next.js app. After the backend is running:

```powershell
cd ..\federated-frontend     # from fedinet-go/
```

Create the environment file (only needed once):
```powershell
'NEXT_PUBLIC_BACKEND_URL=http://localhost:8080' | Out-File .env.local -Encoding utf8
```

Then install and start:
```powershell
npm install      # first time only
npm run dev      # -> http://localhost:3000
```

> Switch `NEXT_PUBLIC_BACKEND_URL` to `http://localhost:9080` in `.env.local` to point the UI at Server B instead.

---

## Stopping everything

```powershell
# Stop containers, keep database data
cd fedinet-go
docker-compose -f docker-compose.federation.yml down

# Stop AND wipe all data (full clean slate — next run re-initializes from scratch)
docker-compose -f docker-compose.federation.yml down -v
```

---

## Restarting after a previous stop

If you stopped with `down` (data kept), just start again — no need to re-run the full setup script:

```powershell
cd fedinet-go
docker-compose -f docker-compose.federation.yml up -d
```

If you stopped with `down -v` (data wiped), run the full script again:

```powershell
.\setup-federation.bat
```

---

## Troubleshooting

| Problem | Fix |
|---------|-----|
| `port is already allocated` | Something else is using 8080/9080/8081/9081/5432. Run `docker-compose -f docker-compose.federation.yml down` then retry. If the port is used by a non-Docker process, stop it or change the host port in `docker-compose.federation.yml`. |
| Script fails at step [1/4] — Docker error | Docker Desktop is not running. Open it and wait for it to fully start, then retry. |
| `connection refused` after the script finishes | Containers are still starting. Wait 10 seconds and re-run the health check above. |
| `Server already initialized` on `/initialize` | You already ran the script once with data still in the volume. Either use the existing setup or run `docker-compose -f docker-compose.federation.yml down -v` to start clean. |
| Container keeps restarting (`Restarting` in `docker ps`) | Check logs: `docker logs server_a_identity`. Most common cause: database not yet ready on a slow machine. |
| `handshake failed: connection refused` on `/trusted-servers/add` | You passed `http://localhost:8080` as the endpoint. The backend runs inside Docker — `localhost` is the container itself. Use the Docker service name instead: `http://server_a_identity:8082` or `http://server_b_identity:8082`. The setup script does this automatically. |
| Go code changes not reflected | Always pass `--build`: `docker-compose -f docker-compose.federation.yml up --build -d`. |
| Need a completely fresh start | `docker-compose -f docker-compose.federation.yml down -v` then `.\setup-federation.bat`. |

---

## Credentials reference (dev only — never use in production)

| What | Value | Where it is set |
|------|-------|-----------------|
| Postgres user | `postgres` | `docker-compose*.yml`  `POSTGRES_USER` |
| Postgres password | `postgres` | `docker-compose*.yml`  `POSTGRES_PASSWORD` |
| Postgres databases | `fedinet_server_a`, `fedinet_server_b` | `init-dbs.sql` (auto-run on first Postgres start) |
| Admin username | `admin` | Passed to `/initialize` by the setup script |
| Admin password | `password123` | Passed to `/initialize` by the setup script |
| JWT secret — Server A | `fedinet-dev-secret-key-server-a` | `docker-compose.federation.yml`  `JWT_SECRET` |
| JWT secret — Server B | `fedinet-dev-secret-key-server-b` | `docker-compose.federation.yml`  `JWT_SECRET` |
| Ed25519 private key | long hex string | `docker-compose.federation.yml`  `SERVER_IDENTITY_PRIVATE_KEY` |

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
| 3001 | federated-admin panel (Next.js dev) |

---

## Running without Docker (advanced)

Only needed if you want to iterate quickly without rebuilding images, or if Docker is not available.
Requires **Go 1.25+** and a locally running Postgres instance.

```powershell
# Terminal 1 — Postgres only (quickest way without a local install)
cd fedinet-go
docker-compose up -d        # starts only Postgres on :5432

# Terminal 2 — identity service
$env:DATABASE_URL                = "postgres://postgres:postgres@localhost:5432/fedinet_server_a?sslmode=disable"
$env:JWT_SECRET                  = "dev-secret"
$env:SERVER_ID                   = "server_a"
$env:SERVER_URL                  = "http://localhost:8080"
$env:SERVER_IDENTITY_PRIVATE_KEY = "c4b1e5a2f3d6c9b8e1a4d7f0c3b6a9e2d5f8c1b4e7a0d3f6c9b2e5a8d1f4c7b0a3d6e9c2f5b8a1d4e7c0b3f6a9d2e5c8b1f4a7d0e3c6b9a2d5f8e1c4b7a0d3f6"
$env:FEDERATION_PEERS            = "server_b=http://localhost:9080"
go run ./cmd/identity/
```

You still need to apply all migrations manually on first run — see [`DOCKER_SETUP.md`](DOCKER_SETUP.md) Step 4 for the exact commands.

> `FEDERATION_PEERS` is required for cross-server search (`/search?q=alice@server_b`) and federated follows/messages. Without it those endpoints return `"discovery_status": "not_found"`. The Docker compose file sets this automatically.
