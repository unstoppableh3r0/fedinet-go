# Running federated-frontend + fedinet-go Locally

## What stays local (never pushed to GitHub)

| File | Why |
|------|-----|
| `federated-frontend/.env.local` | Contains your local backend URL, already in `.gitignore` |
| `fedinet-go/.env` | If you create one for local overrides, already in `.gitignore` |

> **Note:** `docker-compose.yml` and `docker-compose.federation.yml` _are_ committed to git. They contain hardcoded dev-only credentials (`postgres:postgres`, `password123`, placeholder JWT secrets). That is intentional — these are throwaway local dev values only. Never put real/production credentials in those files.

---

## Quick Start

### 1 — Start the backend (Docker)

**Single server (simplest):**
```bash
cd fedinet-go
docker-compose up --build
```
This starts Postgres on `:5432` and Server A identity on `:8080`.

**Full two-server federation:**
```bash
cd fedinet-go
docker-compose -f docker-compose.federation.yml up --build
```
This starts:
- Server A identity → `http://localhost:8080`
- Server A federation relay → `http://localhost:8081`
- Server B identity → `http://localhost:9080`
- Server B federation relay → `http://localhost:9081`
- Shared Postgres → `localhost:5432`

---

### 2 — Initialize the server (first run only)

After the containers are healthy, hit the init endpoint once to seed the admin account:

```bash
# Server A
curl -s -X POST http://localhost:8080/initialize \
  -H "Content-Type: application/json" \
  -d '{"admin_username":"admin","admin_password":"password123","server_name":"Server A"}'

# Server B (federation setup only)
curl -s -X POST http://localhost:9080/initialize \
  -H "Content-Type: application/json" \
  -d '{"admin_username":"admin","admin_password":"password123","server_name":"Server B"}'
```

You only need to do this once — Postgres data persists in the `postgres_data` Docker volume between restarts.

---

### 3 — Start the frontend

```bash
cd federated-frontend
npm install      # first time only
npm run dev
```

Runs on `http://localhost:3000`.

---

### 4 — Frontend environment file

Create `federated-frontend/.env.local` (this file is gitignored, so you manage it yourself):

```env
# Single-server setup
NEXT_PUBLIC_BACKEND_URL=http://localhost:8080

# If using Server B as well, add:
# NEXT_PUBLIC_BACKEND_URL_B=http://localhost:9080
```

This file already exists if you've run the app before. Do not commit it.

---

## Credentials reference (local dev only)

| What | Value | Where it's set |
|------|-------|----------------|
| Postgres user | `postgres` | `docker-compose*.yml` → `POSTGRES_USER` |
| Postgres password | `postgres` | `docker-compose*.yml` → `POSTGRES_PASSWORD` |
| Postgres DB (single) | `fedinet_timeline` | `docker-compose.yml` |
| Postgres DBs (federation) | `fedinet_server_a`, `fedinet_server_b` | `docker-compose.federation.yml` + `init-dbs.sql` |
| Admin username | `admin` | Set at `/initialize` |
| Admin password | `password123` (default) | Set at `/initialize` → you can use any value |
| JWT secret (Server A) | `fedinet-dev-secret-key-server-a` | `docker-compose.federation.yml` → `JWT_SECRET` |
| JWT secret (Server B) | `fedinet-dev-secret-key-server-b` | `docker-compose.federation.yml` → `JWT_SECRET` |
| Server identity private key | Long hex string | `docker-compose.federation.yml` → `SERVER_IDENTITY_PRIVATE_KEY` |

> **All of the above are dev-only throwaway values.** For any real deployment, replace every secret (Postgres password, JWT secret, private key) with strong, randomly generated values, and supply them via a secrets manager or a `.env` file that is never committed.

---

## Stopping everything

```bash
# Stop and remove containers (keeps DB volume)
cd fedinet-go
docker-compose -f docker-compose.federation.yml down

# Stop and wipe the DB volume (clean slate)
docker-compose -f docker-compose.federation.yml down -v
```

Or use the provided PowerShell helper from the workspace root:
```powershell
.\stop-all.ps1
```

---

## Port summary

| Port | Service |
|------|---------|
| 3000 | federated-frontend (Next.js dev) |
| 8080 | Server A — identity API |
| 8081 | Server A — federation relay |
| 9080 | Server B — identity API |
| 9081 | Server B — federation relay |
| 5432 | PostgreSQL |
