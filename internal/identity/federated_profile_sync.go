package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/unstoppableh3r0/fedinet-go/pkg/models"
)

// getLocalEndpoint returns the public endpoint of this server (as seen by peers).
// Uses SERVER_ENDPOINT env var set in docker-compose.
func getLocalEndpoint() string {
	if ep := os.Getenv("SERVER_ENDPOINT"); ep != "" {
		return ep
	}
	return "http://localhost:8082"
}

// PropagateProfileToTrustedServers fans out a profile update to every server
// in trusted_servers asynchronously.  The caller already saved the change to
// the local DB and replied 200; this runs in the background.
func PropagateProfileToTrustedServers(userID string, req models.UpdateProfileRequest) {
	rows, err := db.Query(`SELECT server_id, endpoint FROM trusted_servers`)
	if err != nil {
		log.Printf("[profile-sync] failed to query trusted_servers: %v", err)
		return
	}
	defer rows.Close()

	type peer struct {
		id       string
		endpoint string
	}
	var peers []peer
	for rows.Next() {
		var p peer
		if err := rows.Scan(&p.id, &p.endpoint); err == nil {
			peers = append(peers, p)
		}
	}

	if len(peers) == 0 {
		log.Printf("[profile-sync] no trusted servers to propagate to")
		return
	}

	// Build ActivityStreams2 Update activity.
	var version int
	db.QueryRow("SELECT version FROM profiles WHERE user_id=$1", userID).Scan(&version)

	obj := map[string]interface{}{
		"type":    "Person",
		"id":      ToExternalID(userID),
		"version": version,
		"updated": time.Now().UTC().Format(time.RFC3339),
	}
	if req.DisplayName != nil {
		obj["display_name"] = *req.DisplayName
		obj["name"] = *req.DisplayName
	}
	if req.Bio != nil {
		obj["bio"] = *req.Bio
		obj["summary"] = *req.Bio
	}
	if req.AvatarURL != nil {
		obj["avatar_url"] = *req.AvatarURL
	}
	if req.BannerURL != nil {
		obj["banner_url"] = *req.BannerURL
	}
	if req.Location != nil {
		obj["location"] = *req.Location
	}
	if req.PortfolioURL != nil {
		obj["portfolio_url"] = *req.PortfolioURL
	}

	activity := map[string]interface{}{
		"@context":  "https://www.w3.org/ns/activitystreams",
		"type":      "Update",
		"actor":     ToExternalID(userID),
		"object":    obj,
		"published": time.Now().UTC().Format(time.RFC3339),
	}

	payload, err := json.Marshal(activity)
	if err != nil {
		log.Printf("[profile-sync] json marshal error: %v", err)
		return
	}

	for _, p := range peers {
		go deliverProfileUpdate(p.id, p.endpoint, payload)
	}
}

// deliverProfileUpdate POSTs the AS2 Update payload to a remote server.
func deliverProfileUpdate(serverID, endpoint string, payload []byte) {
	url := fmt.Sprintf("%s/api/profile/federated", endpoint)
	client := &http.Client{Timeout: 10 * time.Second}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		log.Printf("[profile-sync] build request to %s failed: %v", serverID, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Server-ID", getServerID())
	req.Header.Set("X-Timestamp", time.Now().UTC().Format(time.RFC3339))

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[profile-sync] delivery to %s (%s) failed: %v", serverID, endpoint, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		log.Printf("[profile-sync] ✅ delivered to %s (%d)", serverID, resp.StatusCode)
	} else {
		log.Printf("[profile-sync] ⚠️  server %s returned %d", serverID, resp.StatusCode)
	}
}

// HandleIncomingProfileUpdate receives a federated profile Update activity
// from a trusted peer, verifies the sender server, and upserts the remote
// user's profile fields in the local DB cache table.
//
// Route: POST /api/profile/federated
func HandleIncomingProfileUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	senderServerID := r.Header.Get("X-Server-ID")
	if senderServerID == "" {
		RespondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "missing X-Server-ID header"})
		return
	}

	// Verify sender is a trusted server.
	if _, _, err := EnsureServerTrusted(senderServerID); err != nil {
		log.Printf("[profile-sync] untrusted sender %s: %v", senderServerID, err)
		RespondWithJSON(w, http.StatusForbidden, map[string]string{"error": "server not trusted"})
		return
	}

	var activity map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&activity); err != nil {
		RespondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	// Validate it's an Update activity.
	if activity["type"] != "Update" {
		RespondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "expected Update activity"})
		return
	}

	obj, ok := activity["object"].(map[string]interface{})
	if !ok {
		RespondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "missing object"})
		return
	}

	userID, _ := obj["id"].(string)
	if userID == "" {
		RespondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "missing object.id"})
		return
	}

	internalUserID := ToInternalID(userID)
	log.Printf("[profile-sync] received Update for %s from %s", userID, senderServerID)

	// Upsert into remote_profiles cache table.
	if err := upsertRemoteProfile(internalUserID, obj); err != nil {
		log.Printf("[profile-sync] upsert failed for %s: %v", internalUserID, err)
		RespondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "storage error"})
		return
	}

	// Invalidate any in-memory follower/following cache entries that contain this user
	// (they'll be refreshed with the new display name on next read).
	invalidateProfileInFollowCaches(internalUserID)

	RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"message": "profile update received",
		"user_id": userID,
	})
}

// upsertRemoteProfile stores or updates the profile fields received from a
// remote server into the remote_profiles table.
// The table is created by a migration; if the user already has a local
// identity (same-server user somehow arriving here), we skip the upsert.
func upsertRemoteProfile(internalUserID string, obj map[string]interface{}) error {
	displayName, _ := obj["display_name"].(string)
	if n, ok := obj["name"].(string); ok && displayName == "" {
		displayName = n
	}
	bio, _ := obj["bio"].(string)
	if s, ok := obj["summary"].(string); ok && bio == "" {
		bio = s
	}
	avatarURL, _ := obj["avatar_url"].(string)
	bannerURL, _ := obj["banner_url"].(string)
	location, _ := obj["location"].(string)
	portfolioURL, _ := obj["portfolio_url"].(string)

	var version int
	if v, ok := obj["version"].(float64); ok {
		version = int(v)
	}

	_, err := db.Exec(`
		INSERT INTO remote_profiles
			(user_id, display_name, bio, avatar_url, banner_url, location, portfolio_url, version, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8, NOW())
		ON CONFLICT (user_id) DO UPDATE SET
			display_name  = EXCLUDED.display_name,
			bio           = EXCLUDED.bio,
			avatar_url    = EXCLUDED.avatar_url,
			banner_url    = EXCLUDED.banner_url,
			location      = EXCLUDED.location,
			portfolio_url = EXCLUDED.portfolio_url,
			version       = CASE WHEN EXCLUDED.version > remote_profiles.version
			                     THEN EXCLUDED.version
			                     ELSE remote_profiles.version END,
			updated_at    = NOW()
		WHERE EXCLUDED.version >= remote_profiles.version OR remote_profiles.version IS NULL
	`,
		internalUserID, displayName, bio, avatarURL, bannerURL, location, portfolioURL, version,
	)
	return err
}

// invalidateProfileInFollowCaches clears the in-memory follow caches for every
// user that follows or is followed by updatedUserID, so those lists are re-
// fetched from DB (with the new display_name etc) on the next request.
//
// Specifically:
//   - updatedUserID's own followers/following lists are wiped (they may
//     reference stale remote data themselves).
//   - Every LOCAL user who *follows* updatedUserID has their "following" cache
//     cleared, because that list contains updatedUserID's profile fields.
func invalidateProfileInFollowCaches(updatedUserID string) {
	// Clear the updated user's own cached lists.
	muFollowers.Lock()
	delete(followersCache, updatedUserID)
	muFollowers.Unlock()

	muFollowing.Lock()
	delete(followingCache, updatedUserID)
	muFollowing.Unlock()

	// Find all local users whose "following" list contains updatedUserID.
	// These users have a cached list that now has stale profile data for updatedUserID.
	rows, err := db.Query(
		`SELECT follower_user_id FROM follows WHERE followee_user_id = $1`,
		updatedUserID,
	)
	if err != nil {
		log.Printf("[profile-sync] can't query followers for cache invalidation: %v", err)
		return
	}
	defer rows.Close()

	muFollowing.Lock()
	for rows.Next() {
		var followerID string
		if err := rows.Scan(&followerID); err == nil {
			delete(followingCache, followerID)
		}
	}
	muFollowing.Unlock()
}
