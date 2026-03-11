package identity

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

// ============================================================================
// PROFILE PROPAGATION: CROSS-SERVER SYNCHRONIZATION
// ============================================================================

// getLocalEndpoint returns the endpoint of this server as reachable by peer servers.
// Uses SERVER_ENDPOINT (Docker-internal hostname) when available (e.g., inside docker-compose);
// falls back to SERVER_URL (public address) so peer-to-peer calls work in local dev too.
func getLocalEndpoint() string {
    if ep := os.Getenv("SERVER_ENDPOINT"); ep != "" {
        return ep
    }
    if u := os.Getenv("SERVER_URL"); u != "" {
        return u
    }
    // Default fallback for unconfigured environments.
    return "http://localhost:8080"
}

// PropagateProfileToTrustedServers fans out a profile update to every server
// in trusted_servers asynchronously. The caller already saved the change to
// the local DB and replied 200; this runs in the background.
//
// This implements a "Broadcast" pattern:
// 1. Fetch all peers from the trust store.
// 2. Map the internal Go struct to a standardized ActivityStreams 2.0 "Update" activity.
// 3. Launch a goroutine per peer to handle the network delivery.
func PropagateProfileToTrustedServers(userID string, req models.UpdateProfileRequest) {
    // 1. PEER DISCOVERY
    // Retrieve the list of all servers we are permitted to talk to.
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

    // Optimization: If no peers exist, exit early to save processing.
    if len(peers) == 0 {
        log.Printf("[profile-sync] no trusted servers to propagate to")
        return
    }

    // 2. ACTIVITY ASSEMBLY (ActivityStreams 2.0)
    // We fetch the latest version number to ensure the remote server can
    // perform "Conflict Resolution" (ignoring older updates arriving late).
    var version int
    db.QueryRow("SELECT version FROM profiles WHERE user_id=$1", userID).Scan(&version)

    // The 'Object' is the entity being updated (the Person).
    obj := map[string]interface{}{
        "type":    "Person",
        "id":      ToExternalID(userID),
        "version": version,
        "updated": time.Now().UTC().Format(time.RFC3339),
    }

    // Map internal fields to AS2 properties (e.g., bio maps to summary).
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

    // The 'Activity' is the verb ("Update") wrapping the Person object.
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

    // 3. ASYNCHRONOUS FAN-OUT
    // Use goroutines to ensure one slow/down server doesn't delay the
    // propagation to other healthy servers.
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

    // Set headers for server-to-server (S2S) identification and trust.
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
    // 1. REQUEST VALIDATION
    if r.Method != http.MethodPost {
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        return
    }

    // 2. TRUST VERIFICATION
    // Check if the server sending the update is known to us.
    senderServerID := r.Header.Get("X-Server-ID")
    if senderServerID == "" {
        RespondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "missing X-Server-ID header"})
        return
    }

    // Verify sender is a trusted server (cryptographic handshake check).
    if _, _, err := EnsureServerTrusted(senderServerID); err != nil {
        log.Printf("[profile-sync] untrusted sender %s: %v", senderServerID, err)
        RespondWithJSON(w, http.StatusForbidden, map[string]string{"error": "server not trusted"})
        return
    }

    // 3. PAYLOAD DECODING
    var activity map[string]interface{}
    if err := json.NewDecoder(r.Body).Decode(&activity); err != nil {
        RespondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
        return
    }

    // 4. SEMANTIC VALIDATION
    // We only process 'Update' activities here.
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

    // Convert external URI to our internal DB format.
    internalUserID := ToInternalID(userID)
    log.Printf("[profile-sync] received Update for %s from %s", userID, senderServerID)

    // 5. CACHE SYNCHRONIZATION
    // Upsert into remote_profiles cache table. This table stores "mirror"
    // copies of users who live on other servers.
    if err := upsertRemoteProfile(internalUserID, obj); err != nil {
        log.Printf("[profile-sync] upsert failed for %s: %v", internalUserID, err)
        RespondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": "storage error"})
        return
    }

    // 6. CACHE INVALIDATION
    // Ensure that any social graph lists (followers/following) currently
    // held in RAM are wiped so they re-fetch the new profile data.
    invalidateProfileInFollowCaches(internalUserID)

    RespondWithJSON(w, http.StatusOK, map[string]interface{}{
        "message": "profile update received",
        "user_id": userID,
    })
}

// upsertRemoteProfile stores or updates the profile fields received from a
// remote server into the remote_profiles table.
//
// CONFLICT RESOLUTION LOGIC:
// We use the 'version' field provided by the originating server.
// We only update our local cache if the incoming version is GREATER THAN
// or EQUAL TO what we currently have. This prevents "Out of Order" network
// packets from reverting a profile to an older state.
func upsertRemoteProfile(internalUserID string, obj map[string]interface{}) error {
    // Extract properties with fallback handling.
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

    // The SQL query uses a WHERE clause on EXCLUDED.version to enforce
    // strict version ordering during the UPSERT.
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
// This is critical for UI consistency: when a user changes their name,
// their followers should see the change immediately.
func invalidateProfileInFollowCaches(updatedUserID string) {
    // 1. Wipe the specific user's cached metadata.
    muFollowers.Lock()
    delete(followersCache, updatedUserID)
    muFollowers.Unlock()

    muFollowing.Lock()
    delete(followingCache, updatedUserID)
    muFollowing.Unlock()

    // 2. Identify and wipe caches for the user's "Social Circle".
    // We query the DB to find all followers because those followers'
    // 'Following' lists now contain stale data.
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
            // Wiping the follower's cache entry forces a re-fetch from the
            // DB/remote_profiles table next time they view their feed.
            delete(followingCache, followerID)
        }
    }
    muFollowing.Unlock()
}