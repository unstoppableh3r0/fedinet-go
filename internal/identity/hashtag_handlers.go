package identity

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// hashtagRe matches #word tokens inside post content.
var hashtagRe = regexp.MustCompile(`#([A-Za-z0-9_]{1,100})`)

// extractHashtags returns the unique, lowercase set of hashtag words from content.
func extractHashtags(content string) []string {
	matches := hashtagRe.FindAllStringSubmatch(content, -1)
	seen := make(map[string]struct{}, len(matches))
	var tags []string
	for _, m := range matches {
		tag := strings.ToLower(m[1])
		if _, ok := seen[tag]; !ok {
			seen[tag] = struct{}{}
			tags = append(tags, tag)
		}
	}
	return tags
}

// IndexPostHashtags extracts hashtags from content, upserts them into the
// hashtags table, and records the post→tag relationship in post_hashtags.
// This is called from CreatePost immediately after the post row is inserted.
func IndexPostHashtags(postID, content string) {
	tags := extractHashtags(content)
	if len(tags) == 0 {
		return
	}
	for _, tag := range tags {
		// Upsert hashtag row and bump count
		_, err := db.Exec(`
			INSERT INTO hashtags (tag, post_count, created_at, updated_at)
			VALUES ($1, 1, NOW(), NOW())
			ON CONFLICT (tag) DO UPDATE
			  SET post_count = hashtags.post_count + 1,
			      updated_at = NOW()
		`, tag)
		if err != nil {
			log.Printf("IndexPostHashtags upsert %q: %v", tag, err)
			continue
		}

		// Link post ↔ tag
		_, err = db.Exec(`
			INSERT INTO post_hashtags (post_id, tag) VALUES ($1, $2)
			ON CONFLICT DO NOTHING
		`, postID, tag)
		if err != nil {
			log.Printf("IndexPostHashtags link %q: %v", tag, err)
		}
	}
}

// DecrementPostHashtags decrements the post_count when a post is deleted.
func DecrementPostHashtags(postID string) {
	rows, err := db.Query(`
		SELECT tag FROM post_hashtags WHERE post_id = $1
	`, postID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			continue
		}
		db.Exec(`
			UPDATE hashtags SET post_count = GREATEST(0, post_count - 1), updated_at = NOW()
			WHERE tag = $1
		`, tag)
	}
}

// GetTrendingHashtags returns the most-used hashtags by post_count descending (all-time).
func GetTrendingHashtags(limit int) ([]map[string]interface{}, error) {
	return GetTrendingHashtagsWindowed("all", limit)
}

// GetTrendingHashtagsWindowed returns trending hashtags within a time window.
// window: "24h" | "7d" | "30d" | "all"
func GetTrendingHashtagsWindowed(window string, limit int) ([]map[string]interface{}, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}

	var rows interface {
		Next() bool
		Scan(...interface{}) error
		Close() error
		Err() error
	}
	var err error

	switch window {
	case "7d":
		rows, err = db.Query(`
			SELECT ph.tag, COUNT(*) AS cnt
			FROM post_hashtags ph
			JOIN posts p ON p.id = ph.post_id
			WHERE p.created_at > NOW() - INTERVAL '7 days'
			  AND p.visibility = 'PUBLIC'
			  AND (p.expires_at IS NULL OR p.expires_at > NOW())
			GROUP BY ph.tag
			ORDER BY cnt DESC
			LIMIT $1
		`, limit)
	case "30d":
		rows, err = db.Query(`
			SELECT ph.tag, COUNT(*) AS cnt
			FROM post_hashtags ph
			JOIN posts p ON p.id = ph.post_id
			WHERE p.created_at > NOW() - INTERVAL '30 days'
			  AND p.visibility = 'PUBLIC'
			  AND (p.expires_at IS NULL OR p.expires_at > NOW())
			GROUP BY ph.tag
			ORDER BY cnt DESC
			LIMIT $1
		`, limit)
	case "all":
		rows, err = db.Query(`
			SELECT tag, post_count AS cnt
			FROM hashtags
			WHERE post_count > 0
			ORDER BY post_count DESC, updated_at DESC
			LIMIT $1
		`, limit)
	default: // "24h" and anything unrecognised
		rows, err = db.Query(`
			SELECT ph.tag, COUNT(*) AS cnt
			FROM post_hashtags ph
			JOIN posts p ON p.id = ph.post_id
			WHERE p.created_at > NOW() - INTERVAL '24 hours'
			  AND p.visibility = 'PUBLIC'
			  AND (p.expires_at IS NULL OR p.expires_at > NOW())
			GROUP BY ph.tag
			ORDER BY cnt DESC
			LIMIT $1
		`, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var tag string
		var count int
		if err := rows.Scan(&tag, &count); err != nil {
			return nil, err
		}
		results = append(results, map[string]interface{}{
			"tag":        "#" + tag,
			"post_count": count,
		})
	}
	return results, rows.Err()
}

// GetPostsByHashtag returns PUBLIC, non-expired posts that contain a given tag.
func GetPostsByHashtag(tag, viewerUserID string, limit int) ([]map[string]interface{}, error) {
	tag = strings.ToLower(strings.TrimPrefix(tag, "#"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := db.Query(`
		SELECT p.id, p.author, p.content, p.created_at, p.image_url, p.expires_at,
		       (SELECT COUNT(*) FROM likes   WHERE post_id = p.id) AS like_count,
		       (SELECT COUNT(*) FROM replies WHERE post_id = p.id) AS reply_count,
		       (SELECT COUNT(*) FROM reposts WHERE post_id = p.id) AS repost_count,
		       EXISTS(SELECT 1 FROM likes  WHERE post_id = p.id AND user_id = $2) AS has_liked,
		       EXISTS(SELECT 1 FROM reposts WHERE post_id = p.id AND user_id = $2) AS has_reposted
		FROM posts p
		INNER JOIN post_hashtags ph ON ph.post_id = p.id
		WHERE ph.tag = $1
		  AND p.visibility = 'PUBLIC'
		  AND (p.expires_at IS NULL OR p.expires_at > NOW())
		ORDER BY p.created_at DESC
		LIMIT $3
	`, tag, viewerUserID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var posts []map[string]interface{}
	for rows.Next() {
		var (
			id, author, content                string
			imageURL                           *string
			expiresAt                          *time.Time
			createdAt                          time.Time
			likeCount, replyCount, repostCount int
			hasLiked, hasReposted              bool
		)
		if err := rows.Scan(&id, &author, &content, &createdAt, &imageURL, &expiresAt,
			&likeCount, &replyCount, &repostCount, &hasLiked, &hasReposted); err != nil {
			return nil, err
		}
		p := map[string]interface{}{
			"id": id, "author": ToExternalID(author), "content": content,
			"created_at": createdAt, "image_url": imageURL, "expires_at": expiresAt,
			"like_count": likeCount, "reply_count": replyCount, "repost_count": repostCount,
			"has_liked": hasLiked, "has_reposted": hasReposted,
		}
		posts = append(posts, p)
	}
	return posts, rows.Err()
}

// ────────────────────────────────────────────────────────────────────────────
// HTTP Handlers
// ────────────────────────────────────────────────────────────────────────────

// GetTrendingHashtagsHandler — GET /hashtags/trending?limit=10&window=24h
// window: "24h" (default) | "7d" | "30d" | "all"
func GetTrendingHashtagsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	limit := 10
	if l := r.URL.Query().Get("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}
	window := r.URL.Query().Get("window")
	if window == "" {
		window = "all"
	}
	tags, err := GetTrendingHashtagsWindowed(window, limit)
	if err != nil {
		log.Printf("GetTrendingHashtags: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to fetch trending hashtags")
		return
	}
	if tags == nil {
		tags = []map[string]interface{}{}
	}
	RespondWithJSON(w, http.StatusOK, map[string]interface{}{"hashtags": tags})
}

// queryPeerTrending calls a trusted peer's /hashtags/trending endpoint and returns its hashtag list.
func queryPeerTrending(endpoint, serverID, window string, limit int) []map[string]interface{} {
	path := fmt.Sprintf("/hashtags/trending?window=%s&limit=%d", window, limit)
	fullURL := endpoint + path

	timestamp := time.Now().UTC().Format(time.RFC3339)
	sig, err := SignFederatedRequest("GET", path, timestamp)
	if err != nil {
		log.Printf("queryPeerTrending sign error for %s: %v", serverID, err)
		return nil
	}

	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("X-Server-ID", InternalServerName)
	req.Header.Set("X-Timestamp", timestamp)
	req.Header.Set("X-Signature", sig)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("queryPeerTrending request to %s failed: %v", serverID, err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	var res struct {
		Hashtags []map[string]interface{} `json:"hashtags"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil
	}
	return res.Hashtags
}

// GetGlobalTrendingHashtagsHandler — GET /hashtags/trending/global?window=24h&limit=10
// Merges local windowed trends with trends from all trusted peers, re-ranked by total count.
func GetGlobalTrendingHashtagsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	window := r.URL.Query().Get("window")
	if window == "" {
		window = "24h"
	}
	limit := 10
	if l := r.URL.Query().Get("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}
	if limit > 50 {
		limit = 50
	}

	// Fetch local trends (request more to have room to merge)
	localTags, _ := GetTrendingHashtagsWindowed(window, limit*2)

	// Aggregate: tag -> {count, servers}
	type tagEntry struct {
		count   int
		servers []string
	}
	merged := make(map[string]*tagEntry)
	for _, t := range localTags {
		tag, _ := t["tag"].(string)
		cnt, _ := t["post_count"].(int)
		if tag == "" {
			continue
		}
		if merged[tag] == nil {
			merged[tag] = &tagEntry{}
		}
		merged[tag].count += cnt
		merged[tag].servers = append(merged[tag].servers, InternalServerName)
	}

	// Fan out to trusted peers
	peerRows, err := db.Query(`SELECT server_id, endpoint FROM trusted_servers`)
	if err == nil {
		defer peerRows.Close()
		type peerServer struct{ id, endpoint string }
		var peers []peerServer
		for peerRows.Next() {
			var p peerServer
			if err := peerRows.Scan(&p.id, &p.endpoint); err == nil && p.endpoint != "" {
				peers = append(peers, p)
			}
		}

		type peerResult struct {
			serverID string
			tags     []map[string]interface{}
		}
		ch := make(chan peerResult, len(peers))
		for _, p := range peers {
			go func(peer peerServer) {
				tags := queryPeerTrending(peer.endpoint, peer.id, window, limit*2)
				ch <- peerResult{serverID: peer.id, tags: tags}
			}(p)
		}

		timeout := time.After(6 * time.Second)
		for range peers {
			select {
			case res := <-ch:
				for _, t := range res.tags {
					tag, _ := t["tag"].(string)
					if tag == "" {
						continue
					}
					// peer returns numeric as float64 from JSON
					var cnt int
					switch v := t["post_count"].(type) {
					case float64:
						cnt = int(v)
					case int:
						cnt = v
					}
					if merged[tag] == nil {
						merged[tag] = &tagEntry{}
					}
					merged[tag].count += cnt
					merged[tag].servers = append(merged[tag].servers, res.serverID)
				}
			case <-timeout:
				goto doneMerge
			}
		}
	}
doneMerge:

	// Sort by total count descending
	type ranked struct {
		tag     string
		count   int
		servers []string
	}
	var ranked_ []ranked
	for tag, e := range merged {
		ranked_ = append(ranked_, ranked{tag: tag, count: e.count, servers: e.servers})
	}
	// Simple insertion sort (list is small)
	for i := 1; i < len(ranked_); i++ {
		for j := i; j > 0 && ranked_[j].count > ranked_[j-1].count; j-- {
			ranked_[j], ranked_[j-1] = ranked_[j-1], ranked_[j]
		}
	}
	if len(ranked_) > limit {
		ranked_ = ranked_[:limit]
	}

	out := make([]map[string]interface{}, 0, len(ranked_))
	for _, r := range ranked_ {
		out = append(out, map[string]interface{}{
			"tag":        r.tag,
			"post_count": r.count,
			"servers":    r.servers,
		})
	}
	RespondWithJSON(w, http.StatusOK, map[string]interface{}{"hashtags": out})
}

// GetHashtagPostsHandler — GET /hashtags/posts?tag=golang&limit=20&user_id=...
func GetHashtagPostsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	tag := r.URL.Query().Get("tag")
	if tag == "" {
		RespondWithError(w, http.StatusBadRequest, "missing tag parameter")
		return
	}
	limit := 20
	fmt.Sscanf(r.URL.Query().Get("limit"), "%d", &limit)
	viewerID := ToInternalID(r.URL.Query().Get("user_id"))

	posts, err := GetPostsByHashtag(tag, viewerID, limit)
	if err != nil {
		log.Printf("GetPostsByHashtag %q: %v", tag, err)
		RespondWithError(w, http.StatusInternalServerError, "failed to fetch hashtag posts")
		return
	}
	if posts == nil {
		posts = []map[string]interface{}{}
	}
	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"tag":   strings.ToLower(strings.TrimPrefix(tag, "#")),
		"posts": posts,
	})
}

// ────────────────────────────────────────────────────────────────────────────
// Federated hashtag search — queries trusted peer servers for the same tag
// ────────────────────────────────────────────────────────────────────────────

type federatedHashtagResult struct {
	Server string                   `json:"server"`
	Posts  []map[string]interface{} `json:"posts"`
}

// FederatedHashtagSearchHandler — GET /hashtags/federated?tag=golang&limit=10
// Queries trusted peer servers for the same hashtag and merges results.
func FederatedHashtagSearchHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	tag := strings.ToLower(strings.TrimPrefix(r.URL.Query().Get("tag"), "#"))
	if tag == "" {
		RespondWithError(w, http.StatusBadRequest, "missing tag parameter")
		return
	}
	limit := 10
	fmt.Sscanf(r.URL.Query().Get("limit"), "%d", &limit)
	if limit > 50 {
		limit = 50
	}

	viewerID := ToInternalID(r.URL.Query().Get("user_id"))

	// Local posts
	localPosts, _ := GetPostsByHashtag(tag, viewerID, limit)
	if localPosts == nil {
		localPosts = []map[string]interface{}{}
	}

	// Fetch from trusted peers asynchronously
	rows, err := db.Query(`SELECT server_id, endpoint FROM trusted_servers`)
	if err != nil {
		// Return just local results on DB error
		RespondWithJSON(w, http.StatusOK, map[string]interface{}{
			"tag":       tag,
			"local":     localPosts,
			"federated": []federatedHashtagResult{},
		})
		return
	}
	defer rows.Close()

	type server struct{ id, endpoint string }
	var peers []server
	for rows.Next() {
		var s server
		if err := rows.Scan(&s.id, &s.endpoint); err == nil && s.endpoint != "" {
			peers = append(peers, s)
		}
	}

	type result struct {
		serverID string
		posts    []map[string]interface{}
	}
	ch := make(chan result, len(peers))

	for _, peer := range peers {
		go func(p server) {
			posts := queryPeerHashtag(p.endpoint, p.id, tag, limit)
			ch <- result{serverID: p.id, posts: posts}
		}(peer)
	}

	var federated []federatedHashtagResult
	timeout := time.After(8 * time.Second)
	for range peers {
		select {
		case res := <-ch:
			if len(res.posts) > 0 {
				federated = append(federated, federatedHashtagResult{
					Server: res.serverID,
					Posts:  res.posts,
				})
			}
		case <-timeout:
			goto done
		}
	}
done:
	if federated == nil {
		federated = []federatedHashtagResult{}
	}

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"tag":       tag,
		"local":     localPosts,
		"federated": federated,
	})
}

// queryPeerHashtag calls a peer server's /hashtags/posts endpoint (signed request).
func queryPeerHashtag(endpoint, serverID, tag string, limit int) []map[string]interface{} {
	path := fmt.Sprintf("/hashtags/posts?tag=%s&limit=%d", tag, limit)
	fullURL := endpoint + path

	timestamp := time.Now().UTC().Format(time.RFC3339)
	sig, err := SignFederatedRequest("GET", path, timestamp)
	if err != nil {
		log.Printf("queryPeerHashtag sign error for %s: %v", serverID, err)
		return nil
	}

	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		return nil
	}

	req.Header.Set("X-Server-ID", InternalServerName)
	req.Header.Set("X-Timestamp", timestamp)
	req.Header.Set("X-Signature", sig)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 6 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("queryPeerHashtag request to %s failed: %v", serverID, err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	body, _ := io.ReadAll(resp.Body)
	var res struct {
		Posts []map[string]interface{} `json:"posts"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil
	}
	// Tag posts with their origin server so the frontend can display it
	for i := range res.Posts {
		if author, ok := res.Posts[i]["author"].(string); ok && !strings.Contains(author, "@") {
			res.Posts[i]["author"] = author + "@" + serverID
		}
		res.Posts[i]["origin_server"] = serverID
	}
	return res.Posts
}
