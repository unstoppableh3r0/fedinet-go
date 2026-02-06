# Timeline Service - Quick Start Guide

Get the timeline service up and running in 5 minutes!

## Prerequisites

- Go 1.21 or later
- PostgreSQL database
- `curl` for testing (optional)

## Step 1: Database Setup

### Option A: Use existing PostgreSQL instance

```bash
# Set the database URL
export DATABASE_URL="postgresql://user:password@localhost:5432/fedinet_timeline?sslmode=disable"
```

### Option B: Quick PostgreSQL setup (macOS)

```bash
# Install PostgreSQL
brew install postgresql

# Start PostgreSQL
brew services start postgresql

# Create database
createdb fedinet_timeline

# Set connection string
export DATABASE_URL="postgresql://localhost:5432/fedinet_timeline?sslmode=disable"
```

## Step 2: Start the Service

```bash
cd /Users/mithresh/Desktop/Sparkle/VI_SEM/SoftEng/fedinet-go/internal/timeline
go run .
```

You should see:

```
Timeline database connected successfully
Timeline migrations applied successfully
Timeline service running on :8081
Endpoints:
  GET/POST /timeline/ranking/preference - Get/set ranking preferences
  GET /timeline - Get ranked timeline
  ...
```

## Step 3: Test the Service

Open a new terminal and try these commands:

### Test 1: Set Ranking Preference

```bash
curl -X POST http://localhost:8081/timeline/ranking/preference \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "testuser",
    "preference": "trending"
  }'
```

**Expected:** `{"success":true,"message":"Ranking preference updated"}`

### Test 2: Get Timeline

```bash
curl "http://localhost:8081/timeline?user_id=testuser&limit=10"
```

**Expected:** JSON response with posts array and ranking info

### Test 3: Edit a Post

```bash
curl -X POST http://localhost:8081/timeline/post/edit \
  -H "Content-Type: application/json" \
  -d '{
    "post_id": "1",
    "editor_id": "testuser",
    "new_content": "This is my edited post!",
    "change_note": "Fixed typo"
  }'
```

**Expected:** `{"success":true,"version":1,"message":"Post edited successfully"}`

### Test 4: Cache Timeline

```bash
curl -X POST http://localhost:8081/timeline/cache \
  -H "Content-Type: application/json" \
  -d '{"user_id": "testuser"}'
```

**Expected:** Success message with post count and expiration time

### Test 5: Get Refresh Interval

```bash
curl "http://localhost:8081/timeline/refresh/interval?user_id=testuser"
```

**Expected:** JSON with interval, activity level, and load level

## Step 4: Verify Database

Connect to PostgreSQL and check the tables:

```bash
psql fedinet_timeline -c "\dt"
```

You should see:

- `user_ranking_preferences`
- `post_versions`
- `cached_timelines`
- `refresh_configs`
- `server_load_metrics`
- `offline_configs`

## Common Issues

### Issue: "Failed to connect to database"

**Solution:** Check your DATABASE_URL and ensure PostgreSQL is running:

```bash
pg_isready
```

### Issue: "bind: address already in use"

**Solution:** Port 8081 is already in use. Either:

1. Stop the other service using port 8081
2. Change the port in `main.go` (line 53)

### Issue: "no rows in result set"

**Solution:** This is normal for first run - the service returns defaults when no user data exists yet.

## Next Steps

1. **Read the Documentation**

   - `README.md` - Complete feature documentation
   - `API_EXAMPLES.md` - API usage examples
   - `IMPLEMENTATION_SUMMARY.md` - Implementation details

2. **Run the Tests**

   ```bash
   go test -v
   ```

3. **Try Different Ranking Modes**

   - `chronological` - Newest first
   - `popular` - Most engagement
   - `relevance` - Weighted with decay
   - `trending` - High velocity

4. **Explore Version History**

   - Edit a post multiple times
   - View version history
   - Retrieve specific versions

5. **Test Offline Mode**

   - Cache timeline
   - Retrieve cached data
   - Refresh cache

6. **Monitor Adaptive Refresh**
   - Update user activity
   - Record server load
   - Check refresh interval changes

## Integration with Main App

To integrate with the main fedinet-go application:

1. **Update main.go** in your app to start the timeline service
2. **Configure database** connection string
3. **Set up reverse proxy** if needed (e.g., nginx)
4. **Add authentication** middleware to protect endpoints
5. **Connect to posts table** (replace mock data in `fetchTimelinePosts`)

## Environment Variables

```bash
# Database connection
export DATABASE_URL="postgresql://localhost:5432/fedinet_timeline?sslmode=disable"

# Optional: Custom port
export TIMELINE_PORT="8081"
```

## Useful Commands

```bash
# Run service
go run .

# Run tests
go test -v

# Run specific test
go test -v -run TestRankingModes

# Build binary
go build -o timeline-service

# Run binary
./timeline-service

# Check dependencies
go mod tidy
go mod verify
```

## API Endpoints Summary

| Endpoint                       | Method   | Purpose                    |
| ------------------------------ | -------- | -------------------------- |
| `/timeline/ranking/preference` | GET/POST | Manage ranking preferences |
| `/timeline`                    | GET      | Get ranked timeline        |
| `/timeline/post/edit`          | POST     | Edit post with versioning  |
| `/timeline/post/versions`      | GET      | Get version history        |
| `/timeline/post/version`       | GET      | Get specific version       |
| `/timeline/cache`              | GET/POST | Cache management           |
| `/timeline/cache/refresh`      | POST     | Refresh cache              |
| `/timeline/refresh/interval`   | GET      | Get adaptive interval      |
| `/timeline/activity/update`    | POST     | Update user activity       |
| `/timeline/server/load`        | GET/POST | Server load metrics        |

## Support

For detailed API documentation, see `API_EXAMPLES.md`.

For implementation details, see `README.md`.

For troubleshooting, check the service logs in the terminal.

---

**Ready to go!** The timeline service is now running and ready to use. 🎉
