# Timeline API Examples

Quick reference guide for all timeline API endpoints with curl examples.

## Table of Contents

1. [Customizable Ranking](#customizable-ranking)
2. [Post Versioning](#post-versioning)
3. [Offline Mode](#offline-mode)
4. [Adaptive Refresh](#adaptive-refresh)

---

## Customizable Ranking

### 1. Get User's Ranking Preference

```bash
curl "http://localhost:8081/timeline/ranking/preference?user_id=alice@server1.com"
```

**Response:**

```json
{
  "user_id": "alice@server1.com",
  "preference": "chronological"
}
```

### 2. Set Ranking Preference

```bash
curl -X POST http://localhost:8081/timeline/ranking/preference \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "alice@server1.com",
    "preference": "trending"
  }'
```

**Valid preferences:** `chronological`, `popular`, `relevance`, `trending`

**Response:**

```json
{
  "success": true,
  "message": "Ranking preference updated"
}
```

### 3. Get Timeline (with default ranking)

```bash
curl "http://localhost:8081/timeline?user_id=alice@server1.com&limit=20&offset=0"
```

### 4. Get Timeline (with specific ranking)

```bash
curl "http://localhost:8081/timeline?user_id=alice@server1.com&ranking_mode=popular&limit=20"
```

**Response:**

```json
{
  "posts": [
    {
      "id": "2",
      "author": "user2",
      "content": "Trending post with lots of engagement!",
      "created_at": "2026-02-05T17:49:38Z",
      "like_count": 150,
      "reply_count": 45,
      "repost_count": 30,
      "rank_score": 225
    }
  ],
  "ranking_mode": "popular",
  "total": 3,
  "has_more": false
}
```

---

## Post Versioning

### 1. Edit a Post (creates new version)

```bash
curl -X POST http://localhost:8081/timeline/post/edit \
  -H "Content-Type: application/json" \
  -d '{
    "post_id": "post-123",
    "editor_id": "alice@server1.com",
    "new_content": "This is the updated content of my post.",
    "change_note": "Fixed typo in second paragraph"
  }'
```

**Response:**

```json
{
  "success": true,
  "version": 2,
  "message": "Post edited successfully"
}
```

### 2. Get Full Version History

```bash
curl "http://localhost:8081/timeline/post/versions?post_id=post-123"
```

**Response:**

```json
{
  "post_id": "post-123",
  "versions": [
    {
      "id": "550e8400-e29b-41d4-a716-446655440003",
      "post_id": "post-123",
      "version": 2,
      "content": "This is the updated content of my post.",
      "editor_id": "alice@server1.com",
      "edited_at": "2026-02-05T18:10:00Z",
      "change_note": "Fixed typo in second paragraph"
    },
    {
      "id": "550e8400-e29b-41d4-a716-446655440002",
      "post_id": "post-123",
      "version": 1,
      "content": "This is the original content of my post.",
      "editor_id": "alice@server1.com",
      "edited_at": "2026-02-05T17:00:00Z",
      "change_note": null
    }
  ],
  "count": 2
}
```

### 3. Get Specific Version

```bash
curl "http://localhost:8081/timeline/post/version?post_id=post-123&version=1"
```

**Response:**

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440002",
  "post_id": "post-123",
  "version": 1,
  "content": "This is the original content of my post.",
  "editor_id": "alice@server1.com",
  "edited_at": "2026-02-05T17:00:00Z",
  "change_note": null
}
```

---

## Offline Mode

### 1. Cache Timeline for Offline Access

```bash
curl -X POST http://localhost:8081/timeline/cache \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "alice@server1.com"
  }'
```

**Response:**

```json
{
  "success": true,
  "posts_count": 50,
  "expires_at": "2026-02-06T18:15:00Z"
}
```

### 2. Retrieve Cached Timeline

```bash
curl "http://localhost:8081/timeline/cache?user_id=alice@server1.com"
```

**Response:**

```json
{
  "posts": [
    {
      "id": "1",
      "author": "user1",
      "content": "First post!",
      "created_at": "2026-02-05T16:19:38Z",
      "like_count": 42,
      "reply_count": 5,
      "repost_count": 3
    }
  ],
  "count": 50
}
```

**Error (if cache expired or not found):**

```json
{
  "error": "No cached data available"
}
```

### 3. Refresh Cache

```bash
curl -X POST http://localhost:8081/timeline/cache/refresh \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "alice@server1.com"
  }'
```

**Response:**

```json
{
  "success": true,
  "message": "Cache refreshed"
}
```

---

## Adaptive Refresh

### 1. Get Recommended Refresh Interval

```bash
curl "http://localhost:8081/timeline/refresh/interval?user_id=alice@server1.com"
```

**Response:**

```json
{
  "user_id": "alice@server1.com",
  "interval_ms": 30000,
  "activity_level": "high",
  "load_level": "normal",
  "last_activity": "2026-02-05T18:18:30Z",
  "adaptive_enabled": true
}
```

**Activity Levels:**

- `high` - Active within 5 minutes → min interval
- `medium` - Active within 15 minutes → base interval
- `low` - Active within 1 hour → 2x base interval
- `idle` - Inactive over 1 hour → max interval

**Load Levels:**

- `normal` - No throttling
- `high` - 2x interval
- `critical` - Max interval

### 2. Update User Activity

Call this when user interacts with the app:

```bash
curl -X POST http://localhost:8081/timeline/activity/update \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "alice@server1.com"
  }'
```

**Response:**

```json
{
  "success": true
}
```

### 3. Record Server Load (Server-side)

```bash
curl -X POST http://localhost:8081/timeline/server/load \
  -H "Content-Type: application/json" \
  -d '{
    "cpu_percent": 65.5,
    "memory_percent": 58.2,
    "active_connections": 250,
    "requests_per_sec": 380.5
  }'
```

**Response:**

```json
{
  "success": true
}
```

### 4. Get Current Server Load

```bash
curl "http://localhost:8081/timeline/server/load"
```

**Response:**

```json
{
  "load_level": "normal",
  "timestamp": "2026-02-05T18:19:38Z"
}
```

---

## Complete Workflow Examples

### Example 1: New User Setup

```bash
# 1. Set user's preferred ranking
curl -X POST http://localhost:8081/timeline/ranking/preference \
  -H "Content-Type: application/json" \
  -d '{"user_id":"bob@server2.com","preference":"relevance"}'

# 2. Get their timeline
curl "http://localhost:8081/timeline?user_id=bob@server2.com&limit=50"

# 3. Cache for offline
curl -X POST http://localhost:8081/timeline/cache \
  -H "Content-Type: application/json" \
  -d '{"user_id":"bob@server2.com"}'
```

### Example 2: Active User Session

```bash
# 1. Get adaptive refresh interval
INTERVAL=$(curl -s "http://localhost:8081/timeline/refresh/interval?user_id=alice@server1.com" | jq -r '.interval_ms')

# 2. User performs action - update activity
curl -X POST http://localhost:8081/timeline/activity/update \
  -H "Content-Type: application/json" \
  -d '{"user_id":"alice@server1.com"}'

# 3. Refresh timeline after interval
sleep $(($INTERVAL / 1000))
curl "http://localhost:8081/timeline?user_id=alice@server1.com"
```

### Example 3: Post Editing Workflow

```bash
# 1. Edit the post
curl -X POST http://localhost:8081/timeline/post/edit \
  -H "Content-Type: application/json" \
  -d '{
    "post_id":"post-456",
    "editor_id":"alice@server1.com",
    "new_content":"Updated content",
    "change_note":"Clarified statement"
  }'

# 2. View edit history
curl "http://localhost:8081/timeline/post/versions?post_id=post-456"

# 3. View original version
curl "http://localhost:8081/timeline/post/version?post_id=post-456&version=1"
```

### Example 4: Offline to Online Transition

```bash
# While offline - retrieve cache
curl "http://localhost:8081/timeline/cache?user_id=alice@server1.com"

# When connectivity resumes - refresh cache
curl -X POST http://localhost:8081/timeline/cache/refresh \
  -H "Content-Type: application/json" \
  -d '{"user_id":"alice@server1.com"}'

# Get fresh timeline
curl "http://localhost:8081/timeline?user_id=alice@server1.com"
```

---

## Testing with jq

If you have `jq` installed, you can pretty-print and extract data:

```bash
# Get just the ranking mode
curl -s "http://localhost:8081/timeline/ranking/preference?user_id=alice@server1.com" | jq -r '.preference'

# Count posts in timeline
curl -s "http://localhost:8081/timeline?user_id=alice@server1.com" | jq '.total'

# List all post IDs
curl -s "http://localhost:8081/timeline?user_id=alice@server1.com" | jq -r '.posts[].id'

# Get version count
curl -s "http://localhost:8081/timeline/post/versions?post_id=post-123" | jq '.count'

# Get current interval in seconds
curl -s "http://localhost:8081/timeline/refresh/interval?user_id=alice@server1.com" | jq '.interval_ms / 1000'
```

---

## Error Responses

All endpoints return standard HTTP error codes:

- **400 Bad Request** - Invalid parameters
- **404 Not Found** - Resource not found
- **405 Method Not Allowed** - Wrong HTTP method
- **500 Internal Server Error** - Server error

Example error:

```json
{
  "error": "user_id required"
}
```

---

## Rate Limiting Considerations

When server load is high:

- Refresh intervals automatically increase
- Cache is recommended for frequent reads
- Activity tracking helps optimize resource usage

Monitor the `/timeline/server/load` endpoint to understand current system state.
