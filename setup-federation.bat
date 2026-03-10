@echo off
REM =============================================================================
REM Fedinet Two-Server Federation Setup Script
REM =============================================================================
REM This script starts Server A and Server B, then initializes them.
REM Run from the fedinet-go directory.
REM =============================================================================

echo.
echo ============================================
echo   Fedinet Federation Setup
echo ============================================
echo.

REM Step 1: Start all containers
echo [1/5] Starting Docker containers...
docker-compose -f docker-compose.federation.yml up --build -d
if %errorlevel% neq 0 (
    echo ERROR: Docker Compose failed. Is Docker running?
    exit /b 1
)

REM Step 2: Wait for Postgres to be healthy
echo [2/5] Waiting for Postgres to be ready...
timeout /t 10 /nobreak > nul

REM Step 3: Run migrations on both databases
echo [3/5] Running migrations...

echo   - Migrating Server A Identity (Init)...
type internal\identity\migrations\001_server_initialization.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_a > nul

echo   - Migrating Server B Identity (Init)...
type internal\identity\migrations\001_server_initialization.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b > nul

echo   - Migrating Server A Identity (Core Schema)...
type internal\identity\migrations\002_core_schema.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_a > nul

echo   - Migrating Server B Identity (Core Schema)...
type internal\identity\migrations\002_core_schema.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b > nul

echo   - Migrating Server A Identity (Registration Sessions)...
type internal\identity\migrations\003_registration_sessions.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_a > nul

echo   - Migrating Server B Identity (Registration Sessions)...
type internal\identity\migrations\003_registration_sessions.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b > nul

echo   - Migrating Server A Identity (Session Keys)...
type internal\identity\migrations\004_session_keys.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_a > nul

echo   - Migrating Server B Identity (Session Keys)...
type internal\identity\migrations\004_session_keys.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b > nul

echo   - Migrating Server A Identity (Federated Messages)...
type internal\identity\migrations\005_federated_messages.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_a > nul

echo   - Migrating Server B Identity (Federated Messages)...
type internal\identity\migrations\005_federated_messages.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b > nul

echo   - Migrating Server A Identity (Fix Messages Schema)...
type internal\identity\migrations\006_fix_messages_schema.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_a > nul

echo   - Migrating Server B Identity (Fix Messages Schema)...
type internal\identity\migrations\006_fix_messages_schema.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b > nul

echo   - Migrating Server A Identity (TOTP)...
type internal\identity\migrations\007_totp.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_a > nul

echo   - Migrating Server B Identity (TOTP)...
type internal\identity\migrations\007_totp.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b > nul

echo   - Migrating Server A Identity (Ephemeral Posts)...
type internal\identity\migrations\008_ephemeral_posts.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_a > nul

echo   - Migrating Server B Identity (Ephemeral Posts)...
type internal\identity\migrations\008_ephemeral_posts.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b > nul

echo   - Migrating Server A Identity (Hashtags)...
type internal\identity\migrations\009_hashtags.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_a > nul

echo   - Migrating Server B Identity (Hashtags)...
type internal\identity\migrations\009_hashtags.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b > nul

echo   - Migrating Server A Identity (Identity Vouches)...
type internal\identity\migrations\009_identity_vouches.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_a > nul

echo   - Migrating Server B Identity (Identity Vouches)...
type internal\identity\migrations\009_identity_vouches.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b > nul

echo   - Migrating Server A Identity (Disable Resharing)...
type internal\identity\migrations\010_disable_resharing.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_a > nul

echo   - Migrating Server B Identity (Disable Resharing)...
type internal\identity\migrations\010_disable_resharing.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b > nul

echo   - Migrating Server A Identity (Post Visibility)...
type internal\identity\migrations\011_post_visibility.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_a > nul

echo   - Migrating Server B Identity (Post Visibility)...
type internal\identity\migrations\011_post_visibility.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b > nul

echo   - Migrating Server A Identity (Passkeys)...
type internal\identity\migrations\012_passkeys.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_a > nul

echo   - Migrating Server B Identity (Passkeys)...
type internal\identity\migrations\012_passkeys.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b > nul

echo   - Migrating Server A Identity (TOTP Backup Codes)...
type internal\identity\migrations\013_totp_backup_codes.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_a > nul

echo   - Migrating Server B Identity (TOTP Backup Codes)...
type internal\identity\migrations\013_totp_backup_codes.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b > nul

echo   - Migrating Server A Identity (Passkey Flags)...
type internal\identity\migrations\014_passkey_flags.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_a > nul

echo   - Migrating Server B Identity (Passkey Flags)...
type internal\identity\migrations\014_passkey_flags.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b > nul

echo   - Restarting Identity Services to pick up schema...
docker restart server_a_identity server_b_identity
timeout /t 5 /nobreak > nul

REM Step 3b: Run federation migrations
echo   - Migrating Server A Federation...
docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_a < internal\federation\migrations.sql
if %errorlevel% neq 0 (
    echo   WARNING: Server A federation migration had issues (tables may already exist)
)

echo   - Migrating Server B Federation...
docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b < internal\federation\migrations.sql
if %errorlevel% neq 0 (
    echo   WARNING: Server B federation migration had issues (tables may already exist)
)

echo   - Migrating Server A Federation (Fix delivery_attempts FK)...
type internal\federation\012_fix_delivery_attempts_fk.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_a > nul

echo   - Migrating Server B Federation (Fix delivery_attempts FK)...
type internal\federation\012_fix_delivery_attempts_fk.sql | docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b > nul

REM Step 4: Initialize both servers
echo [4/5] Initializing servers...

echo   - Initializing Server A (port 8080)...
curl -s -X POST http://localhost:8080/initialize -H "Content-Type: application/json" -d "{\"server_name\": \"Server A\", \"admin_username\": \"admin\", \"admin_password\": \"password123\"}"
echo.

echo   - Initializing Server B (port 9080)...
curl -s -X POST http://localhost:9080/initialize -H "Content-Type: application/json" -d "{\"server_name\": \"Server B\", \"admin_username\": \"admin\", \"admin_password\": \"password123\"}"
echo.

REM Step 5: Establish mutual federation trust
REM The endpoint MUST be the Docker-internal service name:port, NOT localhost.
REM The Go backend runs inside a container — localhost would resolve to the container itself.
echo [5/5] Establishing mutual federation trust...

echo   - Server A trusting Server B...
curl -s -X POST http://localhost:8080/trusted-servers/add ^
  -H "Content-Type: application/json" ^
  -d "{\"server_id\": \"server_b\", \"server_name\": \"Server B\", \"endpoint\": \"http://server_b_identity:8082\"}"
echo.

echo   - Server B trusting Server A...
curl -s -X POST http://localhost:9080/trusted-servers/add ^
  -H "Content-Type: application/json" ^
  -d "{\"server_id\": \"server_a\", \"server_name\": \"Server A\", \"endpoint\": \"http://server_a_identity:8082\"}"
echo.

echo.
echo ============================================
echo   Setup Complete!
echo ============================================
echo.
echo   Server A Identity:    http://localhost:8080
echo   Server A Federation:  http://localhost:8081
echo   Server B Identity:    http://localhost:9080
echo   Server B Federation:  http://localhost:9081
echo.
echo   Admin credentials for both: admin / password123
echo.
echo   To check health:
echo     curl http://localhost:8080/health
echo     curl http://localhost:9080/health
echo.
echo   To stop:
echo     docker-compose -f docker-compose.federation.yml down
echo.
