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
echo [1/4] Starting Docker containers...
docker-compose -f docker-compose.federation.yml up --build -d
if %errorlevel% neq 0 (
    echo ERROR: Docker Compose failed. Is Docker running?
    exit /b 1
)

REM Step 2: Wait for Postgres to be healthy
echo [2/4] Waiting for Postgres to be ready...
timeout /t 10 /nobreak > nul

REM Step 3: Run migrations on both databases
echo [3/4] Running migrations...

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

echo   - Restarting Identity Services to pick up schema...
docker restart server_a_identity server_b_identity
timeout /t 5 /nobreak > nul

REM Step 3b: Run federation migrations
echo   - Migrating Server A Federation...
docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_a < internal\federation\migrations.sql
if %errorlevel% neq 0 (
    echo   WARNING: Server A federation migration had issues (tables may already exist)
)

echo   - Migrating Server B database...
docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b < internal\federation\migrations.sql
if %errorlevel% neq 0 (
    echo   WARNING: Server B federation migration had issues (tables may already exist)
)

REM Step 4: Initialize both servers
echo [4/4] Initializing servers...

echo   - Initializing Server A (port 8082)...
curl -s -X POST http://localhost:8082/initialize -H "Content-Type: application/json" -d "{\"server_name\": \"Server A\", \"admin_username\": \"admin\", \"admin_password\": \"password123\"}"
echo.

echo   - Initializing Server B (port 9082)...
curl -s -X POST http://localhost:9082/initialize -H "Content-Type: application/json" -d "{\"server_name\": \"Server B\", \"admin_username\": \"admin\", \"admin_password\": \"password123\"}"
echo.

echo.
echo ============================================
echo   Setup Complete!
echo ============================================
echo.
echo   Server A Identity:    http://localhost:8082
echo   Server A Federation:  http://localhost:8081
echo   Server B Identity:    http://localhost:9082
echo   Server B Federation:  http://localhost:9081
echo.
echo   Admin credentials for both: admin / password123
echo.
echo   To check health:
echo     curl http://localhost:8082/health
echo     curl http://localhost:9082/health
echo.
echo   To stop:
echo     docker-compose -f docker-compose.federation.yml down
echo.
