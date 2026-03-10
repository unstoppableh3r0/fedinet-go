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
echo "[1/5] Starting Docker containers..."
docker-compose -f docker-compose.federation.yml up --build -d

# Step 2: Wait for Postgres to be healthy
echo "[2/5] Waiting for Postgres to be ready..."
# Loop until pg_isready returns 0
until docker exec fedinet_postgres pg_isready -U postgres > /dev/null 2>&1; do
  echo "  Waiting for Postgres..."
  sleep 2
done
echo "  Postgres is ready."

# Step 3: Run migrations on both databases
echo "[3/5] Running migrations..."

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

if [ -f "internal/identity/migrations/005_federated_messages.sql" ]; then
  run_migration "internal/identity/migrations/005_federated_messages.sql" "fedinet_server_a"
  run_migration "internal/identity/migrations/005_federated_messages.sql" "fedinet_server_b"
fi

if [ -f "internal/identity/migrations/006_fix_messages_schema.sql" ]; then
  run_migration "internal/identity/migrations/006_fix_messages_schema.sql" "fedinet_server_a"
  run_migration "internal/identity/migrations/006_fix_messages_schema.sql" "fedinet_server_b"
fi

if [ -f "internal/identity/migrations/007_totp.sql" ]; then
  run_migration "internal/identity/migrations/007_totp.sql" "fedinet_server_a"
  run_migration "internal/identity/migrations/007_totp.sql" "fedinet_server_b"
fi

if [ -f "internal/identity/migrations/008_ephemeral_posts.sql" ]; then
  run_migration "internal/identity/migrations/008_ephemeral_posts.sql" "fedinet_server_a"
  run_migration "internal/identity/migrations/008_ephemeral_posts.sql" "fedinet_server_b"
fi

if [ -f "internal/identity/migrations/009_hashtags.sql" ]; then
  run_migration "internal/identity/migrations/009_hashtags.sql" "fedinet_server_a"
  run_migration "internal/identity/migrations/009_hashtags.sql" "fedinet_server_b"
fi

if [ -f "internal/identity/migrations/009_identity_vouches.sql" ]; then
  run_migration "internal/identity/migrations/009_identity_vouches.sql" "fedinet_server_a"
  run_migration "internal/identity/migrations/009_identity_vouches.sql" "fedinet_server_b"
fi

if [ -f "internal/identity/migrations/010_disable_resharing.sql" ]; then
  run_migration "internal/identity/migrations/010_disable_resharing.sql" "fedinet_server_a"
  run_migration "internal/identity/migrations/010_disable_resharing.sql" "fedinet_server_b"
fi

if [ -f "internal/identity/migrations/011_post_visibility.sql" ]; then
  run_migration "internal/identity/migrations/011_post_visibility.sql" "fedinet_server_a"
  run_migration "internal/identity/migrations/011_post_visibility.sql" "fedinet_server_b"
fi

if [ -f "internal/identity/migrations/012_passkeys.sql" ]; then
  run_migration "internal/identity/migrations/012_passkeys.sql" "fedinet_server_a"
  run_migration "internal/identity/migrations/012_passkeys.sql" "fedinet_server_b"
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
echo "[4/5] Initializing servers..."

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

# Step 5: Establish mutual federation trust
# The endpoint MUST be the Docker-internal service name:port, NOT localhost.
# The Go backend runs inside a container — localhost would resolve to the container itself.
echo "[5/5] Establishing mutual federation trust..."

echo "  - Server A trusting Server B..."
curl -s -X POST http://localhost:8080/trusted-servers/add \
  -H "Content-Type: application/json" \
  -d '{"server_id": "server_b", "server_name": "Server B", "endpoint": "http://server_b_identity:8082"}'
echo ""

echo "  - Server B trusting Server A..."
curl -s -X POST http://localhost:9080/trusted-servers/add \
  -H "Content-Type: application/json" \
  -d '{"server_id": "server_a", "server_name": "Server A", "endpoint": "http://server_a_identity:8082"}'
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
