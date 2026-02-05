# Federation Test Suite

Comprehensive testing utilities for the federation module.

## Test Files

### 1. `run_tests.sh` - Bash Integration Tests
Shell script that tests all federation endpoints against a running server.

**Requirements:**
- Federation server running on port 8081
- `curl` and `jq` installed

**Usage:**
```bash
# Start the federation server first
cd /home/optimus/Documents/fedinet-go/internal/federation
go run . &

# Run the tests
cd test
./run_tests.sh
```

**User Stories Tested:**
- ✅ US 2.14: Instance Health API
- ✅ US 2.11: Capability Negotiation
- ✅ US 2.13: Federation Modes
- ✅ US 2.4: Inbox/Outbox Architecture
- ✅ US 2.12: Blocked Server Lists
- ✅ US 2.3: Secure Delivery with Retries
- ✅ US 2.8: Rate Limiting
- ✅ US 2.7: Delivery Acknowledgment
- ✅ US 2.10: Content Serialization
- Error handling validation

### 2. `federation_test.go` - Go Unit Tests
Go unit tests with mock handlers for testing without database dependencies.

**Usage:**
```bash
cd /home/optimus/Documents/fedinet-go/internal/federation/test
go test -v
```

**Tests Included:**
- `TestHealthEndpoint` - Health API response structure
- `TestCapabilitiesEndpoint` - Capabilities format validation
- `TestInboxValidation` - Input validation for inbox endpoint
- `TestFederationModes` - Mode switching functionality
- `TestRateLimitStructure` - Rate limit configuration
- `TestBlockServerFlow` - Server blocking workflow
- `TestRetryBackoff` - Exponential backoff calculation

## Quick Start

### Run All Tests

```bash
# 1. Make sure database is set up
export DATABASE_URL="your-postgres-connection-string"

# 2. Start federation server
cd /home/optimus/Documents/fedinet-go/internal/federation
go run . &
SERVER_PID=$!

# 3. Wait for server to start
sleep 2

# 4. Run integration tests
cd test
./run_tests.sh

# 5. Run unit tests
go test -v

# 6. Stop server
kill $SERVER_PID
```

### Run Individual Test Categories

```bash
# Only Go unit tests
go test -v

# Only specific test function
go test -v -run TestHealthEndpoint

# Only integration tests
./run_tests.sh
```

## Test Output

### Bash Tests (run_tests.sh)
```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Checking Server Status
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

▶ Testing: Server connectivity
✓ PASSED: Federation server is running

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
User Story 2.14: Instance Health API
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

▶ Testing: GET /federation/health
✓ PASSED: Health endpoint returns valid JSON
  Status: healthy
  Uptime: 120s
```

### Go Tests
```
=== RUN   TestHealthEndpoint
--- PASS: TestHealthEndpoint (0.00s)
=== RUN   TestCapabilitiesEndpoint
--- PASS: TestCapabilitiesEndpoint (0.00s)
=== RUN   TestInboxValidation
=== RUN   TestInboxValidation/Valid_activity
--- PASS: TestInboxValidation/Valid_activity (0.00s)
=== RUN   TestInboxValidation/Missing_activity_type
--- PASS: TestInboxValidation/Missing_activity_type (0.00s)
```

## Dependencies

### Bash Tests
```bash
sudo apt-get install curl jq  # Ubuntu/Debian
brew install curl jq          # macOS
```

### Go Tests
```bash
go get github.com/google/uuid
```

## Troubleshooting

### Server Not Running
If you get `Federation server is not running`:
```bash
cd /home/optimus/Documents/fedinet-go/internal/federation
go run .
```

### Database Connection Error
Make sure `DATABASE_URL` is set:
```bash
export DATABASE_URL="postgresql://user:pass@host:5432/database"
```

### Port Already in Use
If port 8081 is busy:
```bash
# Find and kill the process
lsof -i :8081
kill -9 <PID>
```

## CI/CD Integration

### GitHub Actions Example
```yaml
name: Federation Tests
on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    services:
      postgres:
        image: postgres:15
        env:
          POSTGRES_PASSWORD: test
        options: >-
          --health-cmd pg_isready
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5

    steps:
      - uses: actions/checkout@v2
      
      - name: Set up Go
        uses: actions/setup-go@v2
        with:
          go-version: 1.21
      
      - name: Install dependencies
        run: |
          sudo apt-get install -y jq
          go mod download
      
      - name: Apply migrations
        run: psql $DATABASE_URL < migrations.sql
        env:
          DATABASE_URL: postgresql://postgres:test@localhost:5432/postgres
      
      - name: Start federation server
        run: |
          cd internal/federation
          go run . &
          sleep 3
        env:
          DATABASE_URL: postgresql://postgres:test@localhost:5432/postgres
      
      - name: Run unit tests
        run: |
          cd internal/federation/test
          go test -v
      
      - name: Run integration tests
        run: |
          cd internal/federation/test
          ./run_tests.sh
```

## Test Coverage

Run with coverage:
```bash
go test -v -coverprofile=coverage.out
go tool cover -html=coverage.out
```

## Contributing

When adding new features:
1. Add unit tests in `federation_test.go`
2. Add integration tests in `run_tests.sh`
3. Update this README with new test descriptions
4. Ensure all tests pass before submitting PR
