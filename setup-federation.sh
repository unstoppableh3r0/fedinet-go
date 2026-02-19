#!/bin/bash
set -e

# =============================================================================
# Fedinet Two-Server Federation Setup Script
# =============================================================================
# This script starts Server A and Server B, then initializes them.
# Run from the fedinet-go directory.
# =============================================================================

echo ""
echo "============================================"
echo "  Fedinet Federation Setup"
echo "============================================"
echo ""

# Step 1: Start all containers
echo "[1/4] Starting Docker containers..."
docker-compose -f docker-compose.federation.yml up --build -d

# Step 2: Wait for Postgres to be healthy
echo "[2/4] Waiting for Postgres to be ready..."
# Loop until pg_isready returns 0
until docker exec fedinet_postgres pg_isready -U postgres > /dev/null 2>&1; do
  echo "  Waiting for Postgres..."
  sleep 2
done
echo "  Postgres is ready."

# Step 3: Run migrations on both databases
echo "[3/4] Running migrations..."

run_migration() {
  local file=$1
  local db=$2
  echo "  - Migrating $db with $file..."
  cat "$file" | docker exec -i fedinet_postgres psql -U postgres -d "$db" > /dev/null
}

# Identity Migrations
run_migration "internal/identity/migrations/001_server_initialization.sql" "fedinet_server_a"
run_migration "internal/identity/migrations/001_server_initialization.sql" "fedinet_server_b"

run_migration "internal/identity/migrations/002_core_schema.sql" "fedinet_server_a"
run_migration "internal/identity/migrations/002_core_schema.sql" "fedinet_server_b"

if [ -f "internal/identity/migrations/003_registration_sessions.sql" ]; then
  run_migration "internal/identity/migrations/003_registration_sessions.sql" "fedinet_server_a"
  run_migration "internal/identity/migrations/003_registration_sessions.sql" "fedinet_server_b"
fi

if [ -f "internal/identity/migrations/004_session_keys.sql" ]; then
  run_migration "internal/identity/migrations/004_session_keys.sql" "fedinet_server_a"
  run_migration "internal/identity/migrations/004_session_keys.sql" "fedinet_server_b"
fi

echo "  - Restarting Identity Services to pick up schema..."
docker restart server_a_identity server_b_identity
sleep 5

# Federation Migrations
echo "  - Migrating Server A Federation..."
cat internal/federation/migrations.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_a > /dev/null 2>&1 || echo "   WARNING: Server A federation migration had issues (tables may already exist)"

echo "  - Migrating Server B Federation..."
cat internal/federation/migrations.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b > /dev/null 2>&1 || echo "   WARNING: Server B federation migration had issues (tables may already exist)"


# Step 4: Initialize both servers
echo "[4/4] Initializing servers..."

echo "  - Initializing Server A (port 8080)..."
curl -s -X POST http://localhost:8080/initialize \
  -H "Content-Type: application/json" \
  -d '{"server_name": "Server A", "admin_username": "admin", "admin_password": "password123"}'
echo ""

echo "  - Initializing Server B (port 9080)..."
curl -s -X POST http://localhost:9080/initialize \
  -H "Content-Type: application/json" \
  -d '{"server_name": "Server B", "admin_username": "admin", "admin_password": "password123"}'
echo ""

echo ""
echo "============================================"
echo "  Setup Complete!"
echo "============================================"
echo ""
echo "  Server A Identity:    http://localhost:8080"
echo "  Server A Federation:  http://localhost:8081"
echo "  Server B Identity:    http://localhost:9080"
echo "  Server B Federation:  http://localhost:9081"
echo ""
echo "  Admin credentials for both: admin / password123"
echo ""
echo "  To check health:"
echo "    curl http://localhost:8080/health"
echo "    curl http://localhost:9080/health"
echo ""
echo "  To stop:"
echo "    docker-compose -f docker-compose.federation.yml down"
echo ""
