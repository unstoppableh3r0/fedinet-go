package identity

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	fedcrypto "github.com/unstoppableh3r0/fedinet-go/pkg/crypto"
	"github.com/unstoppableh3r0/fedinet-go/pkg/models"
)

// FeedSliceRequest is posted by a remote server to request posts that local
// users have published for the given follower.
type FeedSliceRequest struct {
	RequesterID string `json:"requester_id"` // full user id, e.g. alice@serverA
	Limit       int    `json:"limit"`
}

// FeedSliceResponse is the payload returned by /federation/feed-slice.
type FeedSliceResponse struct {
	Posts []models.Post `json:"posts"`
}

// ─── Inbound handler ─────────────────────────────────────────────────────────

// HandleFeedSliceHandler serves POST /federation/feed-slice.
// Remote servers call this to retrieve the posts authored by local users that
// the requester follows, so they can include them in the requester's home feed.
func HandleFeedSliceHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// ── Verify server signature ──────────────────────────────────────────────
	serverID := r.Header.Get("X-Server-ID")
	signature := r.Header.Get("X-Signature")
	timestamp := r.Header.Get("X-Timestamp")

	if serverID == "" || signature == "" || timestamp == "" {
		RespondWithError(w, http.StatusUnauthorized, "missing federation headers")
		return
	}

	pubKey, err := GetTrustedServerPublicKey(serverID)
	if err != nil {
		RespondWithError(w, http.StatusForbidden, "untrusted server")
		return
	}

	message := serverID + ":" + timestamp
	valid, err := fedcrypto.VerifySignature([]byte(message), signature, pubKey)
	if err != nil || !valid {
		RespondWithError(w, http.StatusUnauthorized, "invalid signature")
		return
	}

	// ── Decode request body ───────────────────────────────────────────────────
	var req FeedSliceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RequesterID == "" {
		RespondWithError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Limit <= 0 || req.Limit > 100 {
		req.Limit = 20
	}

	// ── Query local posts for users the requester follows ─────────────────────
	// The follows table has rows inserted by HandleIncomingFederatedFollow:
	//   follower_user_id = req.RequesterID, followee_user_id = local user
	posts, err := getLocalFeedSlice(req.RequesterID, req.Limit)
	if err != nil {
		log.Printf("HandleFeedSliceHandler: db error: %v", err)
		RespondWithError(w, http.StatusInternalServerError, "failed to query posts")
		return
	}

	// Externalize author IDs before sending across servers.
	for i := range posts {
		posts[i].Author = ToExternalID(posts[i].Author)
	}

	RespondWithJSON(w, http.StatusOK, FeedSliceResponse{Posts: posts})
}

// getLocalFeedSlice returns recent PUBLIC posts from all users on this server
// that followerID follows, excluding expired posts.
func getLocalFeedSlice(followerID string, limit int) ([]models.Post, error) {
	query := `
		SELECT
			p.id, p.author, p.content, p.created_at, p.updated_at,
			COALESCE(COUNT(DISTINCT l.user_id), 0)  AS like_count,
			COALESCE(COUNT(DISTINCT r.id), 0)        AS reply_count,
			COALESCE(COUNT(DISTINCT rp.user_id), 0)  AS repost_count,
			FALSE AS has_liked,
			FALSE AS has_reposted,
			p.image_url, p.expires_at,
			p.group_id, p.origin_post, p.origin_server
		FROM posts p
		LEFT JOIN likes   l  ON p.id = l.post_id
		LEFT JOIN replies r  ON p.id = r.post_id
		LEFT JOIN reposts rp ON p.id = rp.post_id
		WHERE p.author IN (
			SELECT followee_user_id FROM follows WHERE follower_user_id = $1
		)
		  AND p.visibility = 'PUBLIC'
		  AND (p.expires_at IS NULL OR p.expires_at > NOW())
		GROUP BY p.id, p.author, p.content, p.created_at, p.updated_at,
		         p.image_url, p.expires_at, p.group_id, p.origin_post, p.origin_server
		ORDER BY p.created_at DESC
		LIMIT $2
	`

	rows, err := db.Query(query, followerID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var posts []models.Post
	for rows.Next() {
		var p models.Post
		err := rows.Scan(
			&p.ID, &p.Author, &p.Content, &p.CreatedAt, &p.UpdatedAt,
			&p.LikeCount, &p.ReplyCount, &p.RepostCount,
			&p.HasLiked, &p.HasReposted, &p.ImageURL, &p.ExpiresAt,
			&p.GroupID, &p.OriginPost, &p.OriginServer,
		)
		if err != nil {
			log.Printf("getLocalFeedSlice scan: %v", err)
			continue
		}
		posts = append(posts, p)
	}
	return posts, rows.Err()
}

// ─── Outbound fetch ───────────────────────────────────────────────────────────

// fetchRemoteFeedSlice calls a remote server's /federation/feed-slice endpoint
// and returns the posts it serves for our local user. Returns nil on error so
// the caller degrades gracefully.
func fetchRemoteFeedSlice(requesterID, remoteEndpoint string, limit int) []models.Post {
	payload := FeedSliceRequest{RequesterID: requesterID, Limit: limit}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil
	}

	sig := BuildFederationSignatureHeader()
	url := strings.TrimRight(remoteEndpoint, "/") + "/federation/feed-slice"

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(body))
	if err != nil {
		return nil
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Server-ID", sig.ServerID)
	req.Header.Set("X-Signature", sig.Signature)
	req.Header.Set("X-Timestamp", sig.Timestamp)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("fetchRemoteFeedSlice: request to %s failed: %v", remoteEndpoint, err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("fetchRemoteFeedSlice: %s returned %d", remoteEndpoint, resp.StatusCode)
		return nil
	}

	var result FeedSliceResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("fetchRemoteFeedSlice: decode error from %s: %v", remoteEndpoint, err)
		return nil
	}
	return result.Posts
}

// remoteFolloweeServerURLs returns endpoint URLs for all remote servers that
// the given user follows someone on.
func remoteFolloweeServerURLs(userID string) ([]string, error) {
	localEP := getLocalEndpoint()
	rows, err := db.Query(`
		SELECT DISTINCT followee_home_server
		FROM follows
		WHERE follower_user_id = $1
		  AND followee_home_server != $2
		  AND followee_home_server != ''
	`, userID, localEP)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var servers []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err == nil && s != "" {
			servers = append(servers, s)
		}
	}
	return servers, rows.Err()
}
