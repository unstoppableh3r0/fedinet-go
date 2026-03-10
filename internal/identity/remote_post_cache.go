package identity

import (
	"database/sql"
	"encoding/json"
	"log"
	"time"

	"github.com/unstoppableh3r0/fedinet-go/pkg/models"
)

const (
	// remotePostCacheTTL is how long a fresh cache entry is considered valid.
	// Requests hitting an unexpired entry never make a network call.
	remotePostCacheTTL = 5 * time.Minute

	// remotePostCacheStale is the window beyond TTL during which the stale
	// cache is still served to the caller while a background refresh runs.
	// This implements stale-while-revalidate: callers never block on the
	// network, even if the cache has expired.
	remotePostCacheStale = 30 * time.Minute
)

// remotePostCacheEntry is the in-DB representation.
type remotePostCacheEntry struct {
	Posts     []models.Post
	FetchedAt time.Time
	ExpiresAt time.Time
}

// getCachedRemoteSlice looks up the cache for (userID, remoteServer).
//
// Returns (posts, fresh, found):
//   - found=false  → no entry exists yet
//   - fresh=true   → entry is within TTL, no refresh needed
//   - fresh=false  → entry is stale but within the stale window; caller should
//     serve it and trigger a background refresh
func getCachedRemoteSlice(userID, remoteServer string) (posts []models.Post, fresh bool, found bool) {
	var postsJSON []byte
	var expiresAt time.Time

	err := db.QueryRow(`
		SELECT posts_json, expires_at
		FROM remote_post_cache
		WHERE user_id = $1 AND remote_server = $2
	`, userID, remoteServer).Scan(&postsJSON, &expiresAt)

	if err == sql.ErrNoRows {
		return nil, false, false
	}
	if err != nil {
		log.Printf("remote_post_cache: read error for (%s, %s): %v", userID, remoteServer, err)
		return nil, false, false
	}

	if err := json.Unmarshal(postsJSON, &posts); err != nil {
		log.Printf("remote_post_cache: unmarshal error: %v", err)
		return nil, false, false
	}

	now := time.Now()
	if now.Before(expiresAt) {
		return posts, true, true // fresh
	}
	if now.Before(expiresAt.Add(remotePostCacheStale)) {
		return posts, false, true // stale-but-usable
	}

	// Too old — treat as not found so the caller does a synchronous refresh.
	return nil, false, false
}

// setCachedRemoteSlice upserts (userID, remoteServer) → posts in the cache.
func setCachedRemoteSlice(userID, remoteServer string, posts []models.Post) {
	if posts == nil {
		posts = []models.Post{}
	}

	data, err := json.Marshal(posts)
	if err != nil {
		log.Printf("remote_post_cache: marshal error: %v", err)
		return
	}

	expiresAt := time.Now().Add(remotePostCacheTTL)
	_, err = db.Exec(`
		INSERT INTO remote_post_cache (user_id, remote_server, posts_json, fetched_at, expires_at)
		VALUES ($1, $2, $3, NOW(), $4)
		ON CONFLICT (user_id, remote_server) DO UPDATE
			SET posts_json = EXCLUDED.posts_json,
			    fetched_at = EXCLUDED.fetched_at,
			    expires_at = EXCLUDED.expires_at
	`, userID, remoteServer, data, expiresAt)
	if err != nil {
		log.Printf("remote_post_cache: write error for (%s, %s): %v", userID, remoteServer, err)
	}
}

// invalidateRemotePostCache removes all cache entries for a given remote server
// across all users. Called when a Delete/Update federation activity arrives from
// that server so stale content is not served.
func invalidateRemotePostCache(remoteServer string) {
	_, err := db.Exec(`DELETE FROM remote_post_cache WHERE remote_server = $1`, remoteServer)
	if err != nil {
		log.Printf("remote_post_cache: invalidation error for %s: %v", remoteServer, err)
	}
}

// pruneExpiredRemotePostCache removes rows older than the stale window.
// Called lazily inside getCachedRemoteSlice (via the expiry check) and can
// also be wired to a periodic cleanup goroutine.
func pruneExpiredRemotePostCache() {
	cutoff := time.Now().Add(-remotePostCacheStale)
	res, err := db.Exec(`DELETE FROM remote_post_cache WHERE expires_at < $1`, cutoff)
	if err != nil {
		log.Printf("remote_post_cache: prune error: %v", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("remote_post_cache: pruned %d expired entries", n)
	}
}

// StartRemotePostCachePruner launches a background goroutine that prunes stale
// cache entries once per hour. Call from main() after the DB is ready.
func StartRemotePostCachePruner() {
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			pruneExpiredRemotePostCache()
		}
	}()
}
