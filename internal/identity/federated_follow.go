package identity

import (
    "bytes"
    "encoding/json"
    "fmt"
    "log"
    "net/http"
    "strings"
    "time"
)

// ============================================================================
// FEDERATION PROTOCOL: CROSS-SERVER FOLLOW MECHANICS
// ============================================================================

// FederatedFollowRequest defines the standard DTO (Data Transfer Object) used
// for server-to-server (S2S) follow synchronization.
type FederatedFollowRequest struct {
    Follower  string `json:"follower"`  // Globally unique ID (e.g., alice@server_a.com)
    Followee  string `json:"followee"`  // Globally unique ID (e.g., bob@server_b.com)
    Action    string `json:"action"`    // Semantic verb: "follow" or "unfollow"
    ServerID  string `json:"server_id"` // Verification token identifying the origin server
    Timestamp string `json:"timestamp"` // ISO 8601 timestamp for replay protection
}



// DeliverFederatedFollow is the "Outbound Dispatcher". It is responsible for
// pushing local follow events to remote instances.
//
// Workflow:
// 1. Parses the target server from the followee's ID.
// 2. Ensures the remote server is in our trusted_servers whitelist.
// 3. Serializes the request into JSON.
// 4. Executes a POST request to the remote federation endpoint.
func DeliverFederatedFollow(followerID, followeeID, action string) {
    // 1. TARGET RESOLUTION
    // Splits "user@domain.com" to isolate the "domain.com" part.
    parts := strings.Split(followeeID, "@")
    if len(parts) != 2 {
        log.Printf("DeliverFederatedFollow: invalid followee ID %s", followeeID)
        return
    }
    targetServerID := parts[1]

    // 2. TRUST HANDSHAKE
    // Ensures the target server is known. This function may trigger an automatic
    // key exchange if the server is encountered for the first time.
    remoteServer, _, err := EnsureServerTrusted(targetServerID)
    if err != nil {
        log.Printf("DeliverFederatedFollow: cannot reach %s: %v", targetServerID, err)
        return
    }

    // 3. PAYLOAD PREPARATION
    payload := FederatedFollowRequest{
        Follower:  followerID,
        Followee:  followeeID,
        Action:    action,
        ServerID:  getServerID(), // Our local server identifier
        Timestamp: time.Now().UTC().Format(time.RFC3339),
    }

    body, err := json.Marshal(payload)
    if err != nil {
        log.Printf("DeliverFederatedFollow: marshal error: %v", err)
        return
    }

    // 4. NETWORK TRANSMISSION
    // We use a 10-second timeout to prevent "hanging" goroutines if the remote
    // server is offline or experiencing high latency.
    client := &http.Client{Timeout: 10 * time.Second}
    resp, err := client.Post(
        fmt.Sprintf("%s/federation/follow", remoteServer.Endpoint),
        "application/json",
        bytes.NewBuffer(body),
    )
    if err != nil {
        log.Printf("DeliverFederatedFollow: delivery to %s failed: %v", targetServerID, err)
        return
    }
    defer resp.Body.Close()

    // 5. RESPONSE HANDLING
    if resp.StatusCode != http.StatusOK {
        log.Printf("DeliverFederatedFollow: remote %s returned %d", targetServerID, resp.StatusCode)
        return
    }

    log.Printf("✅ Federated %s delivered: %s → %s", action, followerID, followeeID)
}

// HandleIncomingFederatedFollow is the "Inbound Listener". It processes
// synchronization requests coming FROM remote servers.
//
// This function acts as a gatekeeper, ensuring that remote servers can only
// modify follow relationships where the "Followee" lives on THIS instance.
func HandleIncomingFederatedFollow(w http.ResponseWriter, r *http.Request) {
    // 1. REQUEST VALIDATION
    if r.Method != http.MethodPost {
        RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
        return
    }

    var req FederatedFollowRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        RespondWithError(w, http.StatusBadRequest, "invalid request body")
        return
    }

    // 2. SCHEMA SANITY CHECK
    if req.Follower == "" || req.Followee == "" || req.Action == "" || req.ServerID == "" {
        RespondWithError(w, http.StatusBadRequest, "missing required fields")
        return
    }

    // 3. ORIGIN VERIFICATION
    // Security check: Verify the follower's claimed domain matches the server_id
    // that initiated the HTTP request. This prevents "Identity Spoofing".
    followerParts := strings.Split(req.Follower, "@")
    if len(followerParts) != 2 || followerParts[1] != req.ServerID {
        RespondWithError(w, http.StatusBadRequest, "follower server does not match server_id")
        return
    }

    // 4. SENDER TRUST VERIFICATION
    _, _, err := EnsureServerTrusted(req.ServerID)
    if err != nil {
        log.Printf("HandleIncomingFederatedFollow: untrusted server %s: %v", req.ServerID, err)
        RespondWithError(w, http.StatusForbidden, "server not trusted")
        return
    }

    // 5. LOCAL DOMAIN CHECK
    // Ensure the followee is actually a resident of THIS server.
    followeeParts := strings.Split(req.Followee, "@")
    if len(followeeParts) != 2 || followeeParts[1] != getServerID() {
        RespondWithError(w, http.StatusBadRequest, "followee is not on this server")
        return
    }

    // 6. STATE SYNCHRONIZATION
    switch req.Action {
    case "follow":
        // Persist the relationship in our local database so the local user's
        // "Followers" list and count reflect this remote user.
        _, err = db.Exec(`
            INSERT INTO follows (follower_user_id, follower_home_server, followee_user_id, followee_home_server)
            VALUES ($1, $2, $3, $4)
            ON CONFLICT DO NOTHING`,
            req.Follower, remoteEndpointForServer(req.ServerID),
            req.Followee, getLocalEndpoint(),
        )
        if err != nil {
            log.Printf("HandleIncomingFederatedFollow: db insert error: %v", err)
            RespondWithError(w, http.StatusInternalServerError, "failed to record follow")
            return
        }

        // Cache Invalidation ensures the local user sees the follower count update immediately.
        invalidateFollowCaches(req.Follower, req.Followee)
        log.Printf("✅ Recorded federated follow: %s → %s", req.Follower, req.Followee)

    case "unfollow":
        // Remove the relationship record from the local table.
        _, err = db.Exec(`
            DELETE FROM follows WHERE follower_user_id = $1 AND followee_user_id = $2`,
            req.Follower, req.Followee,
        )
        if err != nil {
            log.Printf("HandleIncomingFederatedFollow: db delete error: %v", err)
            RespondWithError(w, http.StatusInternalServerError, "failed to remove follow")
            return
        }
        invalidateFollowCaches(req.Follower, req.Followee)
        log.Printf("✅ Removed federated follow: %s → %s", req.Follower, req.Followee)

    default:
        RespondWithError(w, http.StatusBadRequest, "action must be 'follow' or 'unfollow'")
        return
    }

    // Return 200 OK to acknowledge successful processing to the remote server.
    RespondWithJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// remoteEndpointForServer is a helper function to resolve a ServerID to a
// physical URL endpoint via the trusted_servers lookup table.
func remoteEndpointForServer(serverID string) string {
    ts, err := GetTrustedServer(serverID)
    if err != nil {
        return ""
    }
    return ts.Endpoint
}