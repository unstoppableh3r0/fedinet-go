# Timeline Service

This module implements advanced timeline features for the FediNet federated social network.

## Overview

The timeline service provides four major features:

1. **Customizable Ranking** (User Story 4.9)
2. **Post Versioning** (User Story 4.13)
3. **Offline Mode** (User Story 4.14)
4. **Adaptive Feed Refresh** (User Story 4.15)

## Architecture

### File Structure

```
internal/timeline/
├── main.go          # Service entry point and route definitions
├── models.go        # Data structures and types
├── db.go            # Database connection
├── migrations.go    # Database schema migrations
├── timeline.go      # Core business logic
├── handlers.go      # HTTP request handlers
├── errors.go        # Error types and codes
├── utils.go         # Utility functions
└── README.md        # This file
```

## Features

### 1. Customizable Ranking (User Story 4.9)

Allows users to choose how their timeline is sorted.

#### Supported Ranking Modes

- **Chronological**: Newest posts first (default)
- **Popular**: Sorted by total engagement (likes + replies + reposts)
- **Relevance**: Weighted engagement with time decay
- **Trending**: Recent posts with high engagement velocity

#### API Endpoints

**Get Ranking Preference**

```
GET /timeline/ranking/preference?user_id={user_id}
```

**Set Ranking Preference**

```
POST /timeline/ranking/preference
Content-Type: application/json

{
  "user_id": "user123",
  "preference": "trending"
}
```

**Get Timeline with Ranking**

```
GET /timeline?user_id={user_id}&ranking_mode={mode}&limit=50&offset=0
```

#### Implementation Details

- **Task 4.9.1**: Defined `RankingMode` enum with four modes
- **Task 4.9.2**: Implemented ranking algorithms in `RankPosts()` function
- **Task 4.9.3**: Persisted preferences in `user_ranking_preferences` table

#### Ranking Algorithms

**Chronological**

```go
score = post.CreatedAt.Unix()  // Unix timestamp
```

**Popular**

```go
score = likes + replies + reposts
```

**Relevance**

```go
engagement = (likes * 2) + (replies * 3) + (reposts * 4)
decayFactor = 1.0 / (1.0 + ageHours/24.0)
score = engagement * decayFactor
```

**Trending**

```go
velocity = engagement / ageHours
recencyBoost = max(0, 48 - ageHours) / 48
score = velocity * (1 + recencyBoost)
```

---

### 2. Post Versioning (User Story 4.13)

Maintains complete edit history for all posts.

#### API Endpoints

**Edit Post**

```
POST /timeline/post/edit
Content-Type: application/json

{
  "post_id": "post123",
  "editor_id": "user456",
  "new_content": "Updated content",
  "change_note": "Fixed typo"
}
```

**Get Version History**

```
GET /timeline/post/versions?post_id={post_id}
```

**Get Specific Version**

```
GET /timeline/post/version?post_id={post_id}&version=2
```

#### Implementation Details

- **Task 4.13.1**: Version history stored in `post_versions` table
- **Task 4.13.2**: Each version includes timestamp and editor ID
- **Task 4.13.3**: Versions retrievable by post ID and version number

#### Database Schema

```sql
CREATE TABLE post_versions (
    id UUID PRIMARY KEY,
    post_id VARCHAR(255) NOT NULL,
    version INTEGER NOT NULL,
    content TEXT NOT NULL,
    editor_id VARCHAR(255) NOT NULL,
    edited_at TIMESTAMP NOT NULL,
    change_note TEXT,
    UNIQUE(post_id, version)
);
```

---

### 3. Offline Mode (User Story 4.14)

Caches timeline content for offline viewing.

#### API Endpoints

**Cache Timeline**

```
POST /timeline/cache
Content-Type: application/json

{
  "user_id": "user123"
}
```

**Get Cached Timeline**

```
GET /timeline/cache?user_id={user_id}
```

**Refresh Cache**

```
POST /timeline/cache/refresh
Content-Type: application/json

{
  "user_id": "user123"
}
```

#### Implementation Details

- **Task 4.14.1**: Timeline data cached in `cached_timelines` table as JSONB
- **Task 4.14.2**: Configurable storage limits per user
- **Task 4.14.3**: Auto-refresh when connectivity resumes

#### Default Configuration

```go
MaxCacheSizeBytes: 50 MB
MaxPostsPerUser:   500
CacheDuration:     24 hours
AutoRefresh:       true
```

#### Database Schema

```sql
CREATE TABLE cached_timelines (
    id UUID PRIMARY KEY,
    user_id VARCHAR(255) NOT NULL UNIQUE,
    post_data JSONB NOT NULL,
    cached_at TIMESTAMP NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    size_bytes BIGINT NOT NULL
);
```

---

### 4. Adaptive Feed Refresh (User Story 4.15)

Dynamically adjusts refresh rates based on user activity and server load.

#### API Endpoints

**Get Refresh Interval**

```
GET /timeline/refresh/interval?user_id={user_id}
```

**Update User Activity**

```
POST /timeline/activity/update
Content-Type: application/json

{
  "user_id": "user123"
}
```

**Record Server Load**

```
POST /timeline/server/load
Content-Type: application/json

{
  "cpu_percent": 45.5,
  "memory_percent": 60.2,
  "active_connections": 150,
  "requests_per_sec": 120.5
}
```

**Get Server Load**

```
GET /timeline/server/load
```

#### Implementation Details

- **Task 4.15.1**: Refresh frequency adjusted based on server metrics
- **Task 4.15.2**: Adapts to user activity patterns (active vs idle)
- **Task 4.15.3**: Throttling during high traffic periods

#### Activity Levels

| Level  | Last Activity | Refresh Behavior |
| ------ | ------------- | ---------------- |
| High   | < 5 min       | Min interval     |
| Medium | < 15 min      | Base interval    |
| Low    | < 1 hour      | 2x base interval |
| Idle   | > 1 hour      | Max interval     |

#### Load Levels

| Level    | Thresholds                          | Throttling   |
| -------- | ----------------------------------- | ------------ |
| Normal   | CPU < 60%, Memory < 60%, RPS < 500  | None         |
| High     | CPU > 60%, Memory > 60%, RPS > 500  | 2x interval  |
| Critical | CPU > 80%, Memory > 80%, RPS > 1000 | Max interval |

#### Adaptive Algorithm

```go
func CalculateAdaptiveInterval(userID string) time.Duration {
    config := GetRefreshConfig(userID)

    // Base interval from user preferences
    interval := config.BaseInterval

    // Adjust for user activity
    switch GetActivityLevel(config.LastActivity) {
        case ActivityHigh:   interval = config.MinInterval
        case ActivityMedium: interval = config.BaseInterval
        case ActivityLow:    interval = config.BaseInterval * 2
        case ActivityIdle:   interval = config.MaxInterval
    }

    // Apply server load throttling
    switch GetCurrentLoadLevel() {
        case LoadHigh:     interval *= 2
        case LoadCritical: interval = config.MaxInterval
    }

    // Enforce bounds
    return clamp(interval, config.MinInterval, config.MaxInterval)
}
```

---

## Database Tables

### user_ranking_preferences

Stores user's preferred timeline ranking mode.

```sql
CREATE TABLE user_ranking_preferences (
    user_id VARCHAR(255) PRIMARY KEY,
    preference VARCHAR(50) NOT NULL DEFAULT 'chronological',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);
```

### post_versions

Maintains version history for edited posts.

```sql
CREATE TABLE post_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    post_id VARCHAR(255) NOT NULL,
    version INTEGER NOT NULL,
    content TEXT NOT NULL,
    editor_id VARCHAR(255) NOT NULL,
    edited_at TIMESTAMP NOT NULL DEFAULT NOW(),
    change_note TEXT,
    UNIQUE(post_id, version)
);
```

### cached_timelines

Stores cached timeline data for offline access.

```sql
CREATE TABLE cached_timelines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id VARCHAR(255) NOT NULL UNIQUE,
    post_data JSONB NOT NULL,
    cached_at TIMESTAMP NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMP NOT NULL,
    size_bytes BIGINT NOT NULL
);
```

### refresh_configs

Manages adaptive refresh settings per user.

```sql
CREATE TABLE refresh_configs (
    user_id VARCHAR(255) PRIMARY KEY,
    base_interval_seconds INTEGER NOT NULL DEFAULT 30,
    current_interval_seconds INTEGER NOT NULL DEFAULT 30,
    min_interval_seconds INTEGER NOT NULL DEFAULT 10,
    max_interval_seconds INTEGER NOT NULL DEFAULT 300,
    last_activity TIMESTAMP NOT NULL DEFAULT NOW(),
    last_refresh TIMESTAMP NOT NULL DEFAULT NOW(),
    adaptive_enabled BOOLEAN NOT NULL DEFAULT true,
    throttle_enabled BOOLEAN NOT NULL DEFAULT true
);
```

### server_load_metrics

Records server performance metrics for adaptive refresh.

```sql
CREATE TABLE server_load_metrics (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    cpu_percent DECIMAL(5,2),
    memory_percent DECIMAL(5,2),
    active_connections INTEGER,
    requests_per_sec DECIMAL(10,2),
    timestamp TIMESTAMP NOT NULL DEFAULT NOW()
);
```

### offline_configs

User-specific offline mode configuration.

```sql
CREATE TABLE offline_configs (
    user_id VARCHAR(255) PRIMARY KEY,
    max_cache_size_bytes BIGINT NOT NULL DEFAULT 52428800,
    max_posts_per_user INTEGER NOT NULL DEFAULT 500,
    cache_duration_seconds INTEGER NOT NULL DEFAULT 86400,
    auto_refresh BOOLEAN NOT NULL DEFAULT true,
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);
```

---

## Running the Service

### Prerequisites

- Go 1.21+
- PostgreSQL database
- Environment variable `DATABASE_URL` or default connection

### Start the Service

```bash
cd internal/timeline
go run .
```

The service will start on port **8081**.

### Environment Variables

```bash
export DATABASE_URL="postgresql://localhost:5432/fedinet_timeline?sslmode=disable"
```

---

## Background Tasks

The service runs two background cleanup tasks:

1. **Cache Cleanup** (every 1 hour)

   - Removes expired cache entries from `cached_timelines`

2. **Metrics Cleanup** (every 6 hours)
   - Removes server load metrics older than 24 hours

---

## API Examples

### Example 1: Set Ranking to Trending

```bash
curl -X POST http://localhost:8081/timeline/ranking/preference \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "alice@server1.com",
    "preference": "trending"
  }'
```

### Example 2: Get Timeline with Popular Ranking

```bash
curl "http://localhost:8081/timeline?user_id=alice@server1.com&ranking_mode=popular&limit=20"
```

### Example 3: Edit Post with Version History

```bash
curl -X POST http://localhost:8081/timeline/post/edit \
  -H "Content-Type: application/json" \
  -d '{
    "post_id": "post-123",
    "editor_id": "alice@server1.com",
    "new_content": "Updated post content",
    "change_note": "Corrected spelling"
  }'
```

### Example 4: Cache Timeline for Offline

```bash
curl -X POST http://localhost:8081/timeline/cache \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "alice@server1.com"
  }'
```

### Example 5: Get Adaptive Refresh Interval

```bash
curl "http://localhost:8081/timeline/refresh/interval?user_id=alice@server1.com"
```

Response:

```json
{
  "user_id": "alice@server1.com",
  "interval_ms": 30000,
  "activity_level": "high",
  "load_level": "normal",
  "last_activity": "2026-02-05T15:20:00Z",
  "adaptive_enabled": true
}
```

---

## Testing

### Manual Testing

1. **Start the service**

   ```bash
   go run .
   ```

2. **Test ranking preference**

   ```bash
   # Set preference
   curl -X POST http://localhost:8081/timeline/ranking/preference \
     -H "Content-Type: application/json" \
     -d '{"user_id":"test-user","preference":"trending"}'

   # Get preference
   curl "http://localhost:8081/timeline/ranking/preference?user_id=test-user"
   ```

3. **Test post versioning**

   ```bash
   # Edit a post
   curl -X POST http://localhost:8081/timeline/post/edit \
     -H "Content-Type: application/json" \
     -d '{"post_id":"1","editor_id":"user1","new_content":"New version"}'

   # Get version history
   curl "http://localhost:8081/timeline/post/versions?post_id=1"
   ```

4. **Test offline caching**

   ```bash
   # Cache timeline
   curl -X POST http://localhost:8081/timeline/cache \
     -H "Content-Type: application/json" \
     -d '{"user_id":"test-user"}'

   # Retrieve cache
   curl "http://localhost:8081/timeline/cache?user_id=test-user"
   ```

5. **Test adaptive refresh**

   ```bash
   # Record server load
   curl -X POST http://localhost:8081/timeline/server/load \
     -H "Content-Type: application/json" \
     -d '{"cpu_percent":75,"memory_percent":60,"active_connections":200,"requests_per_sec":400}'

   # Get adaptive interval
   curl "http://localhost:8081/timeline/refresh/interval?user_id=test-user"
   ```

---

## Future Enhancements

1. **Advanced Ranking**

   - Machine learning-based personalization
   - A/B testing for ranking algorithms
   - User feedback integration

2. **Versioning**

   - Diff view between versions
   - Rollback to previous versions
   - Version comparison UI

3. **Offline Mode**

   - Background sync
   - Conflict resolution
   - Selective caching

4. **Adaptive Refresh**
   - Predictive refresh based on user patterns
   - Battery-aware refresh on mobile
   - Network-aware caching strategies

---

## License

This module is part of the FediNet project and follows the same license.

---

## Contributors

Developed as part of Software Engineering coursework (VI Semester).
