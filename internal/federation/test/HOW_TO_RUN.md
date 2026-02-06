# How to Run and Test the Federation Server

## Quick Start

### 1. Start the Server

```bash
cd /home/optimus/Documents/fedinet-go/internal/federation

# Unset any conflicting environment variables first
unset DATABASE_URL

# Start the server
go run .
```

**Expected output:**
```
2026/02/05 19:03:XX DATABASE_URL loaded: postgresql://postgres:agileprocessmodel...
2026/02/05 19:03:XX Federation database connected
2026/02/05 19:03:XX Federation server starting on :8081
2026/02/05 19:03:XX Endpoints:
  POST   /federation/inbox
  ...
2026/02/05 19:03:XX Retry worker started
2026/02/05 19:03:XX Expiration worker started
2026/02/05 19:03:XX Health worker started
```

### 2. Verify Server is Running

Open a **new terminal** and run:

```bash
curl -s http://localhost:8081/federation/health | jq .
```

**Expected output:**
```json
{
  "status": "healthy",
  "uptime_seconds": 10,
  "total_messages": 0,
  ...
}
```

### 3. Run Tests

#### Option A: Integration Tests (Bash)
```bash
cd /home/optimus/Documents/fedinet-go/internal/federation/test
./run_tests.sh
```

Tests all 10 user stories with colored output.

#### Option B: Unit Tests (Go)
```bash
cd /home/optimus/Documents/fedinet-go/internal/federation/test
go test -v
```

Fast unit tests with mock handlers.

#### Option C: Load Tests (Python)
```bash
cd /home/optimus/Documents/fedinet-go/internal/federation/test

# Install dependency first
pip3 install aiohttp

# Run tests
python3 load_test.py
```

Tests rate limiting, concurrency, and performance.

---

## Troubleshooting

### Error: "Tenant or user not found"

**Cause:** Wrong DATABASE_URL format or environment variable override.

**Solution:**
```bash
# Clear environment variable
unset DATABASE_URL

# Verify .env file
cat /home/optimus/Documents/fedinet-go/.env | grep DATABASE_URL

# Should show:
# DATABASE_URL=postgresql://postgres:PASSWORD@db.PROJECT.supabase.co:5432/postgres?sslmode=require

# Run server
cd /home/optimus/Documents/fedinet-go/internal/federation
go run .
```

### Error: "Port 8081 already in use"

**Solution:**
```bash
# Kill existing process
pkill -f "go.*federation"

# Or kill by port
lsof -ti:8081 | xargs kill -9

# Start again
go run .
```

### Server won't start

**Check database migrations:**
```bash
export DATABASE_URL="postgresql://postgres:PASSWORD@db.PROJECT.supabase.co:5432/postgres?sslmode=require"
psql "$DATABASE_URL" -f /home/optimus/Documents/fedinet-go/internal/federation/migrations.sql
```

---

## Test Individual Endpoints

### Health Check
```bash
curl http://localhost:8081/federation/health | jq .
```

### Get Capabilities
```bash
curl http://localhost:8081/federation/capabilities | jq .
```

### Send Activity to Inbox
```bash
curl -X POST http://localhost:8081/federation/inbox \
  -H "Content-Type: application/json" \
  -d '{
    "activity_type": "Follow",
    "actor": "alice",
    "actor_server": "https://test.com",
    "target": "bob",
    "payload": {"message": "Hello!"}
  }' | jq .
```

### Block a Server
```bash
curl -X POST http://localhost:8081/federation/admin/blocks \
  -H "Content-Type: application/json" \
  -d '{
    "server_url": "https://spam.com",
    "reason": "Spam"
  }' | jq .
```

### List Blocked Servers
```bash
curl http://localhost:8081/federation/admin/blocks | jq .
```

### Change Federation Mode
```bash
curl -X PUT http://localhost:8081/federation/admin/mode \
  -H "Content-Type: application/json" \
  -d '{
    "mode": "hard",
    "allow_unknown_servers": false
  }' | jq .
```

---

## Stop the Server

```bash
# In the terminal running the server, press:
Ctrl+C

# Or from another terminal:
pkill -f "go.*federation"
```

---

## Complete Test Workflow

```bash
# Terminal 1: Start server
cd /home/optimus/Documents/fedinet-go/internal/federation
unset DATABASE_URL
go run .

# Terminal 2: Run tests
cd /home/optimus/Documents/fedinet-go/internal/federation/test
./run_tests.sh

# When done:
# Terminal 1: Ctrl+C to stop server
```
